// gen-rollout-dataset — 用 direct K-rollout-per-candidate 当 teacher 生成 student NN 训练数据.
//
// 跟 gen-mcts-dataset 区别:
//   mcts:    PUCT 探索, sims 分配不均, 候选 visits=1..N 不一致 → 信号有 noise
//   rollout: 每候选强制 K independent rollouts, SE = σ/√K 已知, 信号 clean
//
// 算法 (每 decision):
//   1. enumerate actions (含 hard rule filter)
//   2. 每 candidate 跑 K rollouts (并行 worker pool)
//   3. mean Q = sum / K
//   4. PolicyTarget: 1 if winner (max Q), 0 else (one-hot)
//   5. apply winner action, 继续下 round
//
// 输出 jsonl.gz, 跟 train.go -dataset-dir 兼容.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	numGames      = flag.Int("num-games", 100, "games to generate")
	numJokers     = flag.Int("jokers", 2, "")
	workersFlag   = flag.Int("workers", 0, "parallel workers (0=auto = NumCPU)")
	outDir        = flag.String("out-dir", "rollout-dataset", "")
	weightsIn     = flag.String("weights", "", "TrainedEval weights (for rollout MLP-greedy)")
	inDim         = flag.Int("indim", 132, "feature dim (132 = V2)")
	phantomOppMax = flag.Int("phantom-opponents", 2, "")
	shardSize     = flag.Int("shard-size", 5000, "")
	verbose       = flag.Bool("v", false, "verbose per decision")

	rolloutsPerCand = flag.Int("rollouts", 500, "K rollouts per candidate (SE = σ/√K, σ≈75 → K=500 → SE≈3.4)")
	r1Cap           = flag.Int("r1-cap", 0, "top-K R1 candidates by TrainedEval prerank (0=all, 推荐 20-30 防 joker game 爆候选)")

	foulCost      = flag.Float64("foul-cost", 6, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "")
)

// Sample — 跟 train.go schema 兼容
type Sample struct {
	Features     []float32 `json:"features"`
	McScore      float32   `json:"mcScore"`      // mean Q over K rollouts
	FanRate      float32   `json:"fanRate,omitempty"`
	FoulRate     float32   `json:"foulRate,omitempty"`
	PolicyTarget float32   `json:"policyTarget,omitempty"` // one-hot: 1 for max-Q winner, 0 else
	Round        int       `json:"round"`
}

type shardWriter struct {
	mu        sync.Mutex
	outDir    string
	round     int
	shardIdx  int
	count     int
	maxPerShd int
	curFile   *os.File
	curGz     *gzip.Writer
	curEnc    *json.Encoder
}

func newShardWriter(baseDir string, round, maxPerShd int) (*shardWriter, error) {
	d := filepath.Join(baseDir, fmt.Sprintf("round%d", round))
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	// resume: 扫已存在 shard-NNNNN.jsonl.gz, shardIdx 从 max 起 → rotate() 增到 max+1 新 shard
	maxIdx := 0
	entries, err := os.ReadDir(d)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			var n int
			if k, _ := fmt.Sscanf(e.Name(), "shard-%05d.jsonl.gz", &n); k == 1 && n > maxIdx {
				maxIdx = n
			}
		}
	}
	w := &shardWriter{outDir: d, round: round, maxPerShd: maxPerShd, shardIdx: maxIdx}
	if maxIdx > 0 {
		log.Printf("[gen] R%d: resume from shard-%05d (existing) → new shards from shard-%05d", round, maxIdx, maxIdx+1)
	}
	return w, w.rotate()
}

func (w *shardWriter) rotate() error {
	if w.curGz != nil {
		w.curGz.Close()
		w.curFile.Close()
	}
	w.shardIdx++
	w.count = 0
	path := filepath.Join(w.outDir, fmt.Sprintf("shard-%05d.jsonl.gz", w.shardIdx))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	w.curFile = f
	w.curGz = gz
	w.curEnc = json.NewEncoder(gz)
	return nil
}

func (w *shardWriter) Write(s Sample) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.count >= w.maxPerShd {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	if err := w.curEnc.Encode(s); err != nil {
		return err
	}
	w.count++
	return nil
}

func (w *shardWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.curGz != nil {
		if err := w.curGz.Close(); err != nil {
			return err
		}
		return w.curFile.Close()
	}
	return nil
}

func placementStr(gs *ofc.GameState) string {
	return fmt.Sprintf("头[%s] 中[%s] 底[%s]", cardsStr(gs.Top), cardsStr(gs.Middle), cardsStr(gs.Bottom))
}

func cardsStr(cards []ofc.Card) string {
	if len(cards) == 0 {
		return ""
	}
	s := cards[0].String()
	for i := 1; i < len(cards); i++ {
		s += " " + cards[i].String()
	}
	return s
}

func phantomCountFor(round, slot, opponents int) int {
	if opponents == 0 {
		return 0
	}
	count := opponents * 5
	if round >= 2 {
		count += opponents * 2 * (round - 1)
	}
	count -= slot * 2 * (round - 1)
	if round >= 2 {
		count -= slot * 5
	}
	if count < 0 {
		count = 0
	}
	return count
}

func shuffleDeck(deck []ofc.Card, rng *rand.Rand) {
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// candResult — per-candidate rollout 平均
type candResult struct {
	postState *ofc.GameState
	q         float32
	fanRate   float32
	foulRate  float32
	round     int
}

// rolloutCand — K rollouts 平均一个 candidate (single thread, 调用方并行)
func rolloutCand(post *ofc.GameState, round int, K int, cfg *ofc.RolloutConfig, rng *rand.Rand) candResult {
	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
	var sum float32
	var fanCnt, foulCnt int
	for k := 0; k < K; k++ {
		_, _, _ = er.QuickRolloutDetailed(post.Clone(), round)
		// 2026-05-19: 直接读 LastResult.FanBonus (cap-chain aware, 替代旧版手算 classifyFanBonus
		// 用 post.Top 误算 — post.Top 是 candidate 初始 top, NOT rollout 最终 top).
		r := er.LastResult
		if r.IsFoul {
			sum += -cfg.FoulCost
			foulCnt++
		} else if r.IsFantasy {
			sum += r.RawRoyalty + r.FanBonus
			fanCnt++
		} else {
			sum += r.RawRoyalty
		}
	}
	mean := sum / float32(K)
	return candResult{
		postState: post,
		q:         mean,
		fanRate:   float32(fanCnt) / float32(K),
		foulRate:  float32(foulCnt) / float32(K),
		round:     round,
	}
}

// classifyFanBonus 已删 (2026-05-19) — 旧版 cap-down 误算.
// 用 ofc.FantasyBonusFromBoard 或 ExpertRollout.LastResult.FanBonus 替代.

// candidateInfo — 决策时的候选
type candidateInfo struct {
	postState *ofc.GameState
	r1Place   []ofc.Row              // R1 use
	rnAction  *ofc.RoundNAction      // R2-R5 use
	isR1      bool
}

func enumerateAndFilter(state *ofc.GameState, dealt []ofc.Card, round int) []candidateInfo {
	var out []candidateInfo
	if round == 1 {
		placements := ofc.GenerateRound1Actions(dealt, state)
		seen := make(map[string]bool, len(placements))
		type cand struct {
			placement []ofc.Row
			gs        *ofc.GameState
		}
		cands := make([]cand, 0, len(placements))
		for _, p := range placements {
			tmp := state.Clone()
			for i, c := range dealt {
				tmp.PlaceCard(c, p[i])
			}
			key := stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			pCopy := make([]ofc.Row, len(p))
			copy(pCopy, p)
			cands = append(cands, cand{pCopy, tmp})
		}
		// Hard rules
		if !ofc.HardRulesDisabled && len(cands) > 0 {
			r1c := make([]ofc.R1Cand, len(cands))
			for i, c := range cands {
				r1c[i] = ofc.R1Cand{Placement: c.placement, GS: c.gs}
			}
			r1c = ofc.ApplyHardRulesR1(r1c, dealt, state)
			if len(r1c) < len(cands) {
				keep := make(map[string]bool, len(r1c))
				for _, c := range r1c {
					keep[stateKey(c.GS)] = true
				}
				filtered := make([]cand, 0, len(r1c))
				for _, c := range cands {
					if keep[stateKey(c.gs)] {
						filtered = append(filtered, c)
					}
				}
				cands = filtered
			}
		}
		for _, c := range cands {
			out = append(out, candidateInfo{postState: c.gs, r1Place: c.placement, isR1: true})
		}
	} else {
		actions := ofc.GenerateRoundNActions(dealt, state)
		seen := make(map[string]bool, len(actions))
		type cand struct {
			action    *ofc.RoundNAction
			gs        *ofc.GameState
		}
		cands := make([]cand, 0, len(actions))
		for i := range actions {
			a := &actions[i]
			tmp := state.Clone()
			tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
			tmp.SetDiscard(dealt[a.DiscardIdx]) // V3 N/N2 features 需要
			for k, c := range a.Kept {
				tmp.PlaceCard(c, a.Placement[k])
			}
			key := dealt[a.DiscardIdx].ID() + "|" + stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			cands = append(cands, cand{a, tmp})
		}
		if !ofc.HardRulesDisabled && len(cands) > 0 {
			rnc := make([]ofc.RNCand, len(cands))
			for i, c := range cands {
				rnc[i] = ofc.RNCand{Action: c.action, GS: c.gs}
			}
			rnc = ofc.ApplyHardRulesRN(rnc, dealt, state)
			if len(rnc) < len(cands) {
				keep := make(map[string]bool, len(rnc))
				for _, c := range rnc {
					keep[dealt[c.Action.DiscardIdx].ID()+"|"+stateKey(c.GS)] = true
				}
				filtered := make([]cand, 0, len(rnc))
				for _, c := range cands {
					if keep[dealt[c.action.DiscardIdx].ID()+"|"+stateKey(c.gs)] {
						filtered = append(filtered, c)
					}
				}
				cands = filtered
			}
		}
		for _, c := range cands {
			out = append(out, candidateInfo{postState: c.gs, rnAction: c.action, isR1: false})
		}
	}
	return out
}

func stateKey(gs *ofc.GameState) string {
	tids := cardIDs(gs.Top)
	mids := cardIDs(gs.Middle)
	bids := cardIDs(gs.Bottom)
	sort.Strings(tids)
	sort.Strings(mids)
	sort.Strings(bids)
	return joinIDs(tids) + "|" + joinIDs(mids) + "|" + joinIDs(bids)
}

func cardIDs(cards []ofc.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID()
	}
	return out
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

func genOneGame(gameIdx int, rng *rand.Rand, cfg *ofc.RolloutConfig) []Sample {
	state := ofc.NewGameState(*numJokers)
	deck := ofc.MakeDeck(*numJokers)
	shuffleDeck(deck, rng)

	opponents := 0
	slot := 0
	if *phantomOppMax > 0 {
		opponents = rng.Intn(*phantomOppMax + 1)
		if opponents > 0 {
			slot = rng.Intn(opponents + 1)
		}
	}
	maxPhantom := phantomCountFor(5, slot, opponents)
	if len(deck)-maxPhantom < 17 {
		maxPhantom = len(deck) - 17
		if maxPhantom < 0 {
			maxPhantom = 0
		}
	}
	phantomReserveStart := len(deck) - maxPhantom

	myCards := deck[:17]
	phantomAdded := 0
	out := make([]Sample, 0, 200)

	for round := 1; round <= 5; round++ {
		state.Round = round

		want := phantomCountFor(round, slot, opponents)
		if want > maxPhantom {
			want = maxPhantom
		}
		for phantomAdded < want {
			state.UsedCards[deck[phantomReserveStart+phantomAdded].ID()] = true
			phantomAdded++
		}

		var dealt []ofc.Card
		if round == 1 {
			dealt = myCards[0:5]
		} else {
			start := 5 + (round-2)*3
			dealt = myCards[start : start+3]
		}

		cands := enumerateAndFilter(state, dealt, round)
		if len(cands) == 0 {
			break
		}

		// R1 prerank filter: 仅取 top-K candidates 由 TrainedEval value head
		// (防 joker game 候选爆炸 200+ × K rollouts → 30+ min/R1)
		if round == 1 && *r1Cap > 0 && len(cands) > *r1Cap {
			scored := make([]struct {
				idx   int
				score float32
			}, len(cands))
			for i, c := range cands {
				scored[i].idx = i
				scored[i].score = ofc.TrainedEval(c.postState)
			}
			sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
			filtered := make([]candidateInfo, *r1Cap)
			for i := 0; i < *r1Cap; i++ {
				filtered[i] = cands[scored[i].idx]
			}
			cands = filtered
		}

		// 并行 rollout 每 candidate K 次
		results := make([]candResult, len(cands))
		W := *workersFlag
		if W <= 0 {
			W = runtime.NumCPU()
		}

		type job struct{ idx int }
		jobs := make(chan job, len(cands))
		for i := range cands {
			jobs <- job{i}
		}
		close(jobs)

		var wg sync.WaitGroup
		for w := 0; w < W; w++ {
			wg.Add(1)
			workerSeed := rng.Int63()
			go func(seed int64) {
				defer wg.Done()
				workerRng := rand.New(rand.NewSource(seed))
				for j := range jobs {
					results[j.idx] = rolloutCand(cands[j.idx].postState, round, *rolloutsPerCand, cfg, workerRng)
				}
			}(workerSeed)
		}
		wg.Wait()

		// 找 winner (max Q)
		bestIdx := 0
		for i := 1; i < len(results); i++ {
			if results[i].q > results[bestIdx].q {
				bestIdx = i
			}
		}

		// 写 sample for each candidate (PolicyTarget = 1 if winner, 0 else)
		for i, r := range results {
			features := ofc.BuildFeatures(r.postState, *inDim)
			policyTarget := float32(0)
			if i == bestIdx {
				policyTarget = 1
			}
			out = append(out, Sample{
				Features:     features,
				McScore:      r.q,
				FanRate:      r.fanRate,
				FoulRate:     r.foulRate,
				PolicyTarget: policyTarget,
				Round:        round,
			})
		}

		if *verbose {
			// 显示 top 5
			type tr struct {
				placement string
				q         float32
				fan       float32
				foul      float32
				winner    bool
			}
			trs := make([]tr, len(results))
			for i, r := range results {
				trs[i] = tr{
					placement: placementStr(r.postState),
					q:         r.q,
					fan:       r.fanRate,
					foul:      r.foulRate,
					winner:    i == bestIdx,
				}
			}
			sort.Slice(trs, func(i, j int) bool { return trs[i].q > trs[j].q })
			fmt.Printf("\n--- Game %d R%d (cands=%d, rollouts=%d each) ---\n", gameIdx, round, len(results), *rolloutsPerCand)
			fmt.Printf("%-50s %8s %6s %6s\n", "post-state", "Q", "fan", "foul")
			for i, t := range trs {
				if i >= 5 {
					break
				}
				marker := "  "
				if t.winner {
					marker = "★ "
				}
				fmt.Printf("%s%-48s %8.2f %6.2f %6.2f\n", marker, t.placement, t.q, t.fan, t.foul)
			}
		}

		// apply winner action
		bestCand := cands[bestIdx]
		if bestCand.isR1 {
			for i, c := range dealt {
				state.PlaceCard(c, bestCand.r1Place[i])
			}
		} else {
			a := bestCand.rnAction
			state.UsedCards[dealt[a.DiscardIdx].ID()] = true
			for k, c := range a.Kept {
				state.PlaceCard(c, a.Placement[k])
			}
		}
	}
	return out
}

func main() {
	flag.Parse()
	startT := time.Now()

	if *weightsIn != "" {
		if err := ofc.LoadWeightsFromFile(*weightsIn); err != nil {
			log.Fatalf("load weights: %v", err)
		}
		log.Printf("[gen] loaded TrainedEval weights from %s", *weightsIn)
	} else {
		log.Print("[gen] using embed default TrainedEval weights")
	}

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanBonusQQ)
	cfg.KKFanBonus = float32(*fanBonusKK)
	cfg.AAFanBonus = float32(*fanBonusAA)
	cfg.TripsFanBonus = float32(*fanBonusTrips)

	log.Printf("[gen] config: jokers=%d games=%d rollouts-per-cand=%d", *numJokers, *numGames, *rolloutsPerCand)
	log.Printf("[gen] knobs: foul-cost=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f", *foulCost, *fanBonusQQ, *fanBonusKK, *fanBonusAA, *fanBonusTrips)

	writers := make([]*shardWriter, 6)
	for r := 1; r <= 5; r++ {
		sw, err := newShardWriter(*outDir, r, *shardSize)
		if err != nil {
			log.Fatalf("shardWriter R%d: %v", r, err)
		}
		writers[r] = sw
	}
	closeWriters := func() {
		for r := 1; r <= 5; r++ {
			if writers[r] != nil {
				writers[r].Close()
				writers[r] = nil
			}
		}
	}
	defer closeWriters()

	// SIGINT/SIGTERM: flush shards 后才退 (避免 corrupt gzip)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("[gen] signal received, flushing shards...")
		closeWriters()
		log.Printf("[gen] shards flushed, exit.")
		os.Exit(0)
	}()

	doneGames := atomic.Int64{}
	totalSamples := atomic.Int64{}
	rngSeed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(rngSeed))

	progressEvery := 5
	if *numGames < 30 {
		progressEvery = 1
	}

	for gameIdx := 0; gameIdx < *numGames; gameIdx++ {
		samples := genOneGame(gameIdx, rng, &cfg)
		for _, s := range samples {
			if err := writers[s.Round].Write(s); err != nil {
				log.Printf("write err: %v", err)
			}
		}
		doneGames.Add(1)
		totalSamples.Add(int64(len(samples)))
		if (gameIdx+1)%progressEvery == 0 || gameIdx == *numGames-1 {
			elapsed := time.Since(startT).Minutes()
			gpm := float64(doneGames.Load()) / elapsed
			etaMin := float64(int64(*numGames)-doneGames.Load()) / gpm
			log.Printf("[gen] game %d/%d (%.2f g/min, ETA %.1f min, samples=%d)", doneGames.Load(), *numGames, gpm, etaMin, totalSamples.Load())
		}
	}

	log.Printf("[gen] done: %d games, %d samples in %.1f min", doneGames.Load(), totalSamples.Load(), time.Since(startT).Minutes())
}
