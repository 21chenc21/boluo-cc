// gen-oracle-dataset — 生成 oracle silver-label 数据集 (训练用).
//
// 每 game 已知 17 张牌 (R1=5, R2-R5 各 3), 用 perfect-info search (oracle) 求最优摆法.
// 每个决策点的每个候选记一个 Sample (features + oracle_score).
// R1 候选用 K=4 multi-future 平均 (R2-R5 tail 从剩余 deck 采样 4 次), 减少 EV 偏差.
// R2-R5 候选用 single future (剩余 dealt 已知, 无 EV 噪声).
//
// 输出: dir/round{N}/shard-NNNNN.jsonl.gz, 每行一个 Sample.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	numGames       = flag.Int("num-games", 2000, "games to generate")
	numJokers      = flag.Int("jokers", 2, "joker count in deck")
	workersFlag    = flag.Int("workers", 0, "parallel workers (0=auto = NumCPU)")
	outDir         = flag.String("out-dir", "oracle-dataset", "output dir")
	r1Cap          = flag.Int("r1-cap", 0, "top-K R1 candidates by TrainedEval prefilter (0=no cap, all unique placements)")
	r1MultiK       = flag.Int("r1-multi-k", 4, "K multi-future labels per R1 candidate (R2-R5 tail sampled)")
	weightsIn      = flag.String("weights", "", "TrainedEval weights for R1 candidate prefilter (optional, falls back to embed)")
	inDim          = flag.Int("indim", 90, "feature dim (90 = v14 partial-foul)")
	phantomOppMax  = flag.Int("phantom-opponents", 2, "max opponents for phantom usedCards (0=off)")
	shardSize      = flag.Int("shard-size", 5000, "samples per output shard")
	verbose        = flag.Bool("v", false, "verbose log per game")

	// Fan/foul knob (跟 train cmd 一致)
	foulCost      = flag.Float64("foul-cost", 6, "foul penalty in oracle label")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "QQ Fantasy bonus")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "KK Fantasy bonus")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "AA Fantasy bonus")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "Trips top Fantasy bonus")
)

// Sample — 跟 cmd/train/main.go 同 schema, 加 round 信息便于调试.
type Sample struct {
	Features []float32 `json:"features"`
	McScore  float32   `json:"mcScore"`
	FanRate  float32   `json:"fanRate,omitempty"`
	FoulRate float32   `json:"foulRate,omitempty"`
	Round    int       `json:"round"`
}

// shardWriter — 每 round 一个 dir, 内部 sharded JSONL.gz
type shardWriter struct {
	mu        sync.Mutex
	outDir    string
	round     int
	shardIdx  int
	count     int // 当前 shard 已写 sample 数
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
	// 扫已存在 shard, 找到最大 shardIdx, 从 maxIdx 续写 (append 模式, 不覆盖)
	maxExistIdx := 0
	if entries, err := os.ReadDir(d); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			var idx int
			n, _ := fmt.Sscanf(e.Name(), "shard-%05d.jsonl.gz", &idx)
			if n == 1 && idx > maxExistIdx {
				maxExistIdx = idx
			}
		}
	}
	w := &shardWriter{outDir: d, round: round, shardIdx: maxExistIdx, maxPerShd: maxPerShd}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
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
	if err := w.curEnc.Encode(&s); err != nil {
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
		w.curFile.Close()
	}
	return nil
}

func main() {
	flag.Parse()
	startT := time.Now()

	// Workers
	W := *workersFlag
	if W <= 0 {
		W = runtime.NumCPU()
	}
	if W > *numGames {
		W = *numGames
	}

	// Weights (TrainedEval R1 prefilter)
	if *weightsIn != "" {
		if err := ofc.LoadWeightsFromFile(*weightsIn); err != nil {
			log.Fatalf("load weights: %v", err)
		}
		log.Printf("[gen] loaded TrainedEval weights from %s", *weightsIn)
	} else {
		log.Print("[gen] using embed default TrainedEval weights for R1 prefilter")
	}

	// Cfg
	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanBonusQQ)
	cfg.KKFanBonus = float32(*fanBonusKK)
	cfg.AAFanBonus = float32(*fanBonusAA)
	cfg.TripsFanBonus = float32(*fanBonusTrips)

	log.Printf("[gen] config: jokers=%d games=%d workers=%d r1-cap=%d r1-K=%d phantom-opp=%d",
		*numJokers, *numGames, W, *r1Cap, *r1MultiK, *phantomOppMax)
	log.Printf("[gen] knobs: foul-cost=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f",
		cfg.FoulCost, cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)

	// Output writers (1 per round, shared across workers)
	writers := [6]*shardWriter{} // index 1..5
	for r := 1; r <= 5; r++ {
		w, err := newShardWriter(*outDir, r, *shardSize)
		if err != nil {
			log.Fatalf("open writer r%d: %v", r, err)
		}
		writers[r] = w
		defer w.Close()
	}

	// Worker pool
	gameCh := make(chan int, W*2)
	var totalSamples atomic.Int64
	var doneGames atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < W; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID*1009)))
			for gameIdx := range gameCh {
				samples := genOneGame(rng, &cfg, gameIdx)
				for _, s := range samples {
					if err := writers[s.Round].Write(s); err != nil {
						log.Printf("write err: %v", err)
					}
				}
				totalSamples.Add(int64(len(samples)))
				dn := doneGames.Add(1)
				if *verbose || dn%50 == 0 {
					elapsed := time.Since(startT).Minutes()
					rate := float64(dn) / elapsed
					eta := float64(int64(*numGames)-dn) / rate
					log.Printf("[gen] game %d/%d (%.1f games/min, ETA %.1f min, samples=%d)",
						dn, *numGames, rate, eta, totalSamples.Load())
				}
			}
		}(w)
	}

	// Feed games
	for i := 0; i < *numGames; i++ {
		gameCh <- i
	}
	close(gameCh)
	wg.Wait()

	for _, w := range writers[1:] {
		w.Close()
	}

	log.Printf("[gen] done: %d games, %d samples in %.1f min",
		*numGames, totalSamples.Load(), time.Since(startT).Minutes())
}

// shuffleDeck — Fisher-Yates 洗牌
func shuffleDeck(deck []ofc.Card, rng *rand.Rand) {
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// genOneGame — 跑一个完整 game 的 oracle dataset 生成
func genOneGame(rng *rand.Rand, cfg *ofc.RolloutConfig, gameIdx int) []Sample {
	state := ofc.NewGameState(*numJokers)
	deck := ofc.MakeDeck(*numJokers)
	shuffleDeck(deck, rng)

	// Phantom 选择 (跟 train.go 一致)
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

	// 我的 17 张牌从 deck[0:17].
	// R1 multi-future tail 从 deck[5:phantomReserveStart] 采 (排除 R1 已用 5 张 + phantom 区).
	// 注意: 这个 pool 包含我自己的 R2-R5 牌, 但 K=4 采样会随机选, 不一定全选我的, 跟"alternative future"语义一致.
	// (real future = my actual R2-R5; sampled future = 任意 12 张 from 这个 pool)
	myCards := deck[:17]
	futureTailPool := deck[5:phantomReserveStart]

	out := make([]Sample, 0, 200)
	phantomAdded := 0

	for round := 1; round <= 5; round++ {
		state.Round = round

		// 增量注入 phantom (跟 train.go 一致)
		want := phantomCountFor(round, slot, opponents)
		if want > maxPhantom {
			want = maxPhantom
		}
		for phantomAdded < want {
			state.UsedCards[deck[phantomReserveStart+phantomAdded].ID()] = true
			phantomAdded++
		}

		// 当前 round 的 dealt
		var dealt []ofc.Card
		if round == 1 {
			dealt = myCards[0:5]
		} else {
			start := 5 + (round-2)*3
			dealt = myCards[start : start+3]
		}

		// 收集 sample + 决定 best action
		var bestAction *actionInfo
		bestScore := float32(-1e9)

		samples := make([]Sample, 0, 50)

		if round == 1 {
			// === R1: K=4 multi-future ===
			candidates := enumerateR1Candidates(state, dealt, *r1Cap)
			for ci := range candidates {
				c := &candidates[ci]
				childState := state.Clone()
				for i, card := range dealt {
					childState.PlaceCard(card, c.placement[i])
				}

				// K-future multi-sample
				var sumScore float32
				var fanCnt, foulCnt int
				K := *r1MultiK
				for k := 0; k < K; k++ {
					tail := sampleTail(futureTailPool, 12, rng)
					futureRounds := [][]ofc.Card{tail[0:3], tail[3:6], tail[6:9], tail[9:12]}
					res := ofc.OracleSolveDetailed(childState, futureRounds, cfg)
					sumScore += res.Score
					if res.IsFantasy {
						fanCnt++
					}
					if res.IsFoul {
						foulCnt++
					}
				}
				avgScore := sumScore / float32(K)

				samples = append(samples, Sample{
					Features: ofc.BuildFeatures(childState, *inDim),
					McScore:  avgScore,
					FanRate:  float32(fanCnt) / float32(K),
					FoulRate: float32(foulCnt) / float32(K),
					Round:    round,
				})

				if avgScore > bestScore {
					bestScore = avgScore
					bestAction = &actionInfo{
						round:     round,
						placement: c.placement,
					}
				}
			}
		} else {
			// === R2-R5: single future (剩余 dealt 已知) ===
			actions := enumerateRoundNCandidatesAll(state, dealt)
			// 构造 future = remaining rounds dealt
			var futureRounds [][]ofc.Card
			for r2 := round + 1; r2 <= 5; r2++ {
				start := 5 + (r2-2)*3
				futureRounds = append(futureRounds, myCards[start:start+3])
			}
			for ai := range actions {
				a := &actions[ai]
				childState := state.Clone()
				childState.UsedCards[dealt[a.discardIdx].ID()] = true
				for k, c := range a.kept {
					childState.PlaceCard(c, a.placement[k])
				}
				res := ofc.OracleSolveDetailed(childState, futureRounds, cfg)
				score := res.Score

				fanRate := float32(0)
				foulRate := float32(0)
				if res.IsFantasy {
					fanRate = 1
				}
				if res.IsFoul {
					foulRate = 1
				}
				samples = append(samples, Sample{
					Features: ofc.BuildFeatures(childState, *inDim),
					McScore:  score,
					FanRate:  fanRate,
					FoulRate: foulRate,
					Round:    round,
				})

				if score > bestScore {
					bestScore = score
					bestAction = &actionInfo{
						round:       round,
						discardCard: dealt[a.discardIdx],
						kept:        a.kept,
						placement:   a.placement,
					}
				}
			}
		}

		out = append(out, samples...)

		// Apply best action to state
		if bestAction == nil {
			// No valid action (rare, e.g., empty candidate list); break
			break
		}
		if round == 1 {
			for i, card := range dealt {
				state.PlaceCard(card, bestAction.placement[i])
			}
		} else {
			state.UsedCards[bestAction.discardCard.ID()] = true
			for k, c := range bestAction.kept {
				state.PlaceCard(c, bestAction.placement[k])
			}
		}
	}

	return out
}

// sampleTail — 从 pool 随机抽 n 张 (无放回, 用 Fisher-Yates 前 n 张)
func sampleTail(pool []ofc.Card, n int, rng *rand.Rand) []ofc.Card {
	if n > len(pool) {
		n = len(pool)
	}
	// 复制 + shuffle 前 n
	cp := make([]ofc.Card, len(pool))
	copy(cp, pool)
	for i := 0; i < n; i++ {
		j := i + rng.Intn(len(cp)-i)
		cp[i], cp[j] = cp[j], cp[i]
	}
	return cp[:n]
}

// actionInfo — 简化 action representation, 兼容 R1 全摆 / RN 弃 1 摆 2
type actionInfo struct {
	round       int
	placement   []ofc.Row // R1=5 长, RN=2 长
	discardCard ofc.Card  // RN only
	kept        []ofc.Card
}

type r1Candidate struct {
	placement []ofc.Row // 长 5
	teScore   float32
}

// enumerateR1Candidates — R1 全部 placement, dedup by stateKey, top-K by TrainedEval
func enumerateR1Candidates(gs *ofc.GameState, dealt []ofc.Card, topK int) []r1Candidate {
	placements := ofc.GenerateRound1Actions(dealt, gs)
	type cand struct {
		placement []ofc.Row
		state     *ofc.GameState
		teScore   float32
	}
	cands := make([]cand, 0, len(placements))
	seen := make(map[string]bool, len(placements))
	for _, p := range placements {
		tmp := gs.Clone()
		for i, c := range dealt {
			tmp.PlaceCard(c, p[i])
		}
		k := stateKey(tmp)
		if seen[k] {
			continue
		}
		seen[k] = true
		// p is shared underlying slice from generator; copy
		pCopy := make([]ofc.Row, len(p))
		copy(pCopy, p)
		cands = append(cands, cand{placement: pCopy, state: tmp, teScore: ofc.TrainedEval(tmp)})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].teScore > cands[j].teScore })
	if topK > 0 && len(cands) > topK {
		cands = cands[:topK]
	}
	// topK <= 0: 不 cap, 全部 unique placements 都进 oracle (无 prefilter bias)
	out := make([]r1Candidate, len(cands))
	for i, c := range cands {
		out[i] = r1Candidate{placement: c.placement, teScore: c.teScore}
	}
	return out
}

type rnCandidate struct {
	discardIdx int
	kept       []ofc.Card
	placement  []ofc.Row
}

// enumerateRoundNCandidatesAll — R2-R5 全部 action, dedup by stateKey
func enumerateRoundNCandidatesAll(gs *ofc.GameState, dealt []ofc.Card) []rnCandidate {
	actions := ofc.GenerateRoundNActions(dealt, gs)
	out := make([]rnCandidate, 0, len(actions))
	seen := make(map[string]bool, len(actions))
	for i := range actions {
		a := &actions[i]
		tmp := gs.Clone()
		tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
		tmp.SetDiscard(dealt[a.DiscardIdx]) // V3 features
		for k, c := range a.Kept {
			tmp.PlaceCard(c, a.Placement[k])
		}
		k := dealt[a.DiscardIdx].ID() + "|" + stateKey(tmp)
		if seen[k] {
			continue
		}
		seen[k] = true
		// Copy slices
		keptCopy := make([]ofc.Card, len(a.Kept))
		copy(keptCopy, a.Kept)
		placeCopy := make([]ofc.Row, len(a.Placement))
		copy(placeCopy, a.Placement)
		out = append(out, rnCandidate{
			discardIdx: a.DiscardIdx,
			kept:       keptCopy,
			placement:  placeCopy,
		})
	}
	return out
}

// stateKey — 跟 ofc/expert_place.go 同 (这里复制一份避免 export)
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

// phantomCountFor — 跟 train.go 一致
func phantomCountFor(round, slot, opponents int) int {
	if opponents <= 0 {
		return 0
	}
	cardsDoneR := 5 + 2*(round-1)
	cardsDoneR1 := 0
	if round >= 2 {
		cardsDoneR1 = 5 + 2*(round-2)
	}
	before := slot
	after := opponents - before
	return before*cardsDoneR + after*cardsDoneR1
}
