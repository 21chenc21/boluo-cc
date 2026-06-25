// gen-mcts-dataset — 用 MCTS+init-n=20 当 teacher, 生成 student NN 蒸馏数据.
//
// 跟 gen-oracle-dataset 区别:
//   oracle: 已知未来牌, perfect-info search, 有 hindsight bias
//   mcts:   不知未来 (chance node sampling), 真实 EV 估计, 无 bias
//
// 每个决策点的每个候选记一个 Sample:
//   - features: 候选 post-state 132-d V2 features
//   - mcScore:  MCTS rollout mean Q (value 学习目标)
//   - policyTarget: visits / totalVisits (policy 学习目标)
//
// 输出格式跟 train.go -dataset-dir 兼容, 可直接读取训练.
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
	numGames      = flag.Int("num-games", 100, "games to generate")
	numJokers     = flag.Int("jokers", 2, "joker count in deck")
	workersFlag   = flag.Int("workers", 0, "parallel workers (0=auto = NumCPU). NOTE: MCTS 内部已并行, 外层 worker=1 推荐避免 over-subscribe.")
	outDir        = flag.String("out-dir", "mcts-dataset", "output dir")
	weightsIn     = flag.String("weights", "", "TrainedEval weights for MCTS prior (optional, falls back to embed)")
	inDim         = flag.Int("indim", 132, "feature dim (132 = V2)")
	phantomOppMax = flag.Int("phantom-opponents", 2, "max opponents for phantom usedCards (0=off)")
	shardSize     = flag.Int("shard-size", 5000, "samples per output shard")
	verbose       = flag.Bool("v", false, "verbose log per game")

	// MCTS knobs (validated config)
	mctsSims    = flag.Int("mcts-sims", 200, "MCTS sims per decision")
	mctsInitN   = flag.Int("mcts-init-n", 20, "per-candidate init rollouts (PUCT 解锁, validated)")
	mctsCPuct   = flag.Float64("mcts-cpuct", 1.5, "PUCT exploration constant")
	mctsLeafK   = flag.Int("mcts-leaf-k", 1, "leaf rollout K")

	// Rollout cfg
	foulCost      = flag.Float64("foul-cost", 6, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "")
)

// Sample — 跟 cmd/train/main.go 同 schema
type Sample struct {
	Features     []float32 `json:"features"`
	McScore      float32   `json:"mcScore"`
	PolicyTarget float32   `json:"policyTarget,omitempty"`
	Round        int       `json:"round"`
}

// shardWriter — 每 round 一个 dir, 内部 sharded JSONL.gz
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
	w := &shardWriter{outDir: d, round: round, maxPerShd: maxPerShd}
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

// phantomCountFor — 跟 train.go 一致
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

func placementStr(gs *ofc.GameState) string {
	return fmt.Sprintf("头[%s] 中[%s] 底[%s]", cardsStr(gs.Top), cardsStr(gs.Middle), cardsStr(gs.Bottom))
}

func stateStr(gs *ofc.GameState) string {
	if len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 0 {
		return "(empty)"
	}
	return placementStr(gs)
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

func shuffleDeck(deck []ofc.Card, rng *rand.Rand) {
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

func genOneGame(rng *rand.Rand, mctsCfg ofc.MCTSConfig, _ int) []Sample {
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

		// 每 MCTSSearch 重新随机 (cfg.Rng 共享, 每 sim 抽 next dealt)
		mctsCfg.Rng = rand.New(rand.NewSource(rng.Int63()))

		bestAction, stats := ofc.MCTSSearch(state, dealt, round, mctsCfg)

		// PolicyTarget 信号增强:
		//   总 visits 含 INIT_N baseline 浪费 → 用 sim-driven visits (n[i] - INIT_N) 算
		//   winner 拿大头 (e.g. 80/200=0.4), loser 都接近 0 → 信号强
		simDrivenSum := 0
		for _, s := range stats {
			adj := s.Visits[0] - *mctsInitN
			if adj > 0 {
				simDrivenSum += adj
			}
		}
		// fallback: 如果 sim-driven 全 0 (R5 forced placement 等), 用总 visits
		useTotal := false
		var totalVisits int
		if simDrivenSum == 0 {
			useTotal = true
			for _, s := range stats {
				totalVisits += s.Visits[0]
			}
			if totalVisits == 0 {
				totalVisits = 1
			}
		}

		// 写 sample for each candidate (含 INIT_N-only 的低 prior 候选)
		type candTrace struct {
			placement string
			visits    int
			q         float32
			policy    float32
		}
		traceList := make([]candTrace, 0, len(stats))
		for _, s := range stats {
			postState := state.Clone()
			ofc.ApplyMCTSAction(postState, dealt, s.Action)
			features := ofc.BuildFeatures(postState, *inDim)
			var policyTarget float32
			if useTotal {
				policyTarget = float32(s.Visits[0]) / float32(totalVisits)
			} else {
				adj := s.Visits[0] - *mctsInitN
				if adj < 0 {
					adj = 0
				}
				policyTarget = float32(adj) / float32(simDrivenSum)
			}
			out = append(out, Sample{
				Features:     features,
				McScore:      s.Q[0],
				PolicyTarget: policyTarget,
				Round:        round,
			})
			if *verbose {
				traceList = append(traceList, candTrace{
					placement: placementStr(postState),
					visits:    s.Visits[0],
					q:         s.Q[0],
					policy:    policyTarget,
				})
			}
		}

		// Verbose: 打印此决策的 top 候选
		if *verbose {
			sort.Slice(traceList, func(i, j int) bool { return traceList[i].visits > traceList[j].visits })
			fmt.Printf("\n--- Game R%d  state=%s  dealt=%s ---\n", round, stateStr(state), cardsStr(dealt))
			fmt.Printf("%-50s %-7s %-7s %-7s\n", "post-state", "visits", "Q", "policy")
			for i, t := range traceList {
				if i >= 5 {
					break
				}
				marker := "  "
				if i == 0 {
					marker = "★ "
				}
				fmt.Printf("%s%-48s %-7d %-7.2f %-7.3f\n", marker, t.placement, t.visits, t.q, t.policy)
			}
		}

		// apply best action
		ofc.ApplyMCTSAction(state, dealt, bestAction)
	}

	return out
}

func main() {
	flag.Parse()
	startT := time.Now()

	W := *workersFlag
	if W <= 0 {
		// MCTS 内部已并行 (parallel init rollouts), 外层默认 1 防 over-subscribe
		W = 1
	}

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

	ofc.MctsInitRollouts = *mctsInitN
	ofc.MctsLeafRollouts = *mctsLeafK

	mctsCfg := ofc.MCTSConfig{
		Sims:       *mctsSims,
		CPuct:      float32(*mctsCPuct),
		UseValue:   true,
		RolloutCfg: &cfg,
	}

	log.Printf("[gen] config: jokers=%d games=%d workers=%d mcts-sims=%d init-n=%d", *numJokers, *numGames, W, *mctsSims, *mctsInitN)
	log.Printf("[gen] knobs: foul-cost=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f", *foulCost, *fanBonusQQ, *fanBonusKK, *fanBonusAA, *fanBonusTrips)

	// 5 round writers
	writers := make([]*shardWriter, 6)
	for r := 1; r <= 5; r++ {
		sw, err := newShardWriter(*outDir, r, *shardSize)
		if err != nil {
			log.Fatalf("shardWriter R%d: %v", r, err)
		}
		writers[r] = sw
	}
	defer func() {
		for r := 1; r <= 5; r++ {
			if writers[r] != nil {
				writers[r].Close()
			}
		}
	}()

	doneGames := atomic.Int64{}
	totalSamples := atomic.Int64{}

	var wg sync.WaitGroup
	rngSeed := time.Now().UnixNano()
	jobs := make(chan int, *numGames)
	for i := 0; i < *numGames; i++ {
		jobs <- i
	}
	close(jobs)

	progressEvery := 5
	if *numGames < 50 {
		progressEvery = 1
	}

	for w := 0; w < W; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(rngSeed + int64(workerID)*1000))
			for gameIdx := range jobs {
				samples := genOneGame(rng, mctsCfg, gameIdx)
				for _, s := range samples {
					if err := writers[s.Round].Write(s); err != nil {
						log.Printf("write err: %v", err)
					}
				}
				done := doneGames.Add(1)
				totalSamples.Add(int64(len(samples)))
				if int(done)%progressEvery == 0 || done == int64(*numGames) {
					elapsed := time.Since(startT).Minutes()
					gpm := float64(done) / elapsed
					etaMin := float64(int64(*numGames)-done) / gpm
					log.Printf("[gen] game %d/%d (%.2f g/min, ETA %.1f min, samples=%d)", done, *numGames, gpm, etaMin, totalSamples.Load())
				}
			}
		}(w)
	}
	wg.Wait()

	_ = runtime.NumCPU() // silence unused
	log.Printf("[gen] done: %d games, %d samples in %.1f min", doneGames.Load(), totalSamples.Load(), time.Since(startT).Minutes())
}
