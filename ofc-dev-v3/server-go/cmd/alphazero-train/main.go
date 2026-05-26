// alphazero-train — AlphaZero 风格 self-play 训练循环.
//
// 每 iteration:
//   1. Self-play: 用当前 best NN + MCTS 跑 N 场, 收 (state, mcts_visits, eventual_score) samples
//   2. 训新 NN on samples (dual loss: value + policy)
//   3. Duel: new NN vs best NN, M 场 same-hand, 比胜率
//   4. If new wins ≥ gate% → promote new = best
//   5. Save ckpt, bench testcase (sanity)
//
// 用法:
//   ./alphazero-train -iters 50 -games-per-iter 1000 -mcts-sims 200 -duel-games 100 \
//     -warm-start v14-ckpt.json -ckpt-dir az-ckpts -policy v0-az-r1
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	iters            = flag.Int("iters", 30, "AlphaZero iterations")
	gamesPerIter     = flag.Int("games-per-iter", 1000, "self-play games per iter")
	mctsSims         = flag.Int("mcts-sims", 200, "MCTS sims per decision")
	duelGames        = flag.Int("duel-games", 100, "duel games for promotion gate")
	gatePct          = flag.Float64("gate-pct", 55.0, "win-rate gate %% to promote")
	warmStart        = flag.String("warm-start", "", "warm-start ckpt (default: random init)")
	ckptDir          = flag.String("ckpt-dir", "az-ckpts", "ckpt output dir")
	policyVer        = flag.String("policy", "v0-az-r1", "policy version label")
	jokers           = flag.Int("jokers", 2, "deck joker count")
	phantomOpp       = flag.Int("phantom-opponents", 2, "max phantom opponents")
	workers          = flag.Int("workers", 0, "self-play / duel parallel workers (0=auto NumCPU)")
	valueRollouts    = flag.Int("value-rollouts", 50, "dedicated rollouts per candidate for clean value label (跟 v14 silver-label 对齐); set 0 to fall back to MCTS Q (noisy)")
	rolloutEpsilon   = flag.Float64("rollout-epsilon", 0.1, "rollout ε-greedy exploration rate (跟 v14 train 一致); 强 baseline 必加, 否则 rollout 收敛到单一路径, Q label 不分散")
	inDim            = flag.Int("indim", 132, "feature dim (132=V2 default, 90=v14 legacy)")
	hiddenH1         = flag.Int("h1", 128, "hidden 1")
	hiddenH2         = flag.Int("h2", 64, "hidden 2")
	hiddenH3         = flag.Int("h3", 0, "hidden 3 (0=2-hidden legacy, >0=3-hidden big model)")
	trainEpochs      = flag.Int("epochs", 60, "epochs per iter")
	trainBin         = flag.String("train-bin", "/tmp/ofc-train", "path to ofc-train binary (will use to invoke training subprocess)")

	foulCost      = flag.Float64("foul-cost", 6, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "")
	fanW          = flag.Float64("fan-w", 0.40, "")
	foulW         = flag.Float64("foul-w", 0.10, "")
	policyW       = flag.Float64("policy-w", 0.30, "policy BCE weight")
	trainLR       = flag.Float64("lr", 0.001, "train base learning rate (传给 ofc-train); AZ-B 默认 0.001 比 train 默认 0.005 低 5x, 因为 AZ self-play 数据量大 (500K+) online SGD 容易发散")
	trainWarmLRMult = flag.Float64("warm-lr-mult", 0.5, "warm-start LR multiplier (传给 ofc-train); 1.0=不打折, default 0.5 = 折半")

	// Testcase bench: 每 iter 测 new vs best 的 testcase pass 数, 加入 PROMOTE 决策
	casesFile     = flag.String("cases-file", "cases/all-tests-expanded.json", "testcase JSON path (relative to cwd). \"\" disables testcase bench")
	benchSimsMult = flag.Float64("bench-sims-mult", 2.0, "MCTS_SIMS_MULT for in-process testcase bench (跟 run-cases.sh 的 MCTS_SIMS_MULT 对齐 — default 2 匹配生产 bench)")

	testcaseDropLimit = flag.Int("testcase-drop-limit", 5, "testcase 跌超过此阈值 → 强制 DISCARD (盖过 fantasy/score 规则). 0=禁用此 guard.")
)

func main() {
	flag.Parse()
	startT := time.Now()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if *workers <= 0 {
		*workers = runtime.NumCPU()
	}
	log.Printf("[az] parallel workers = %d (NumCPU=%d)", *workers, runtime.NumCPU())

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanBonusQQ)
	cfg.KKFanBonus = float32(*fanBonusKK)
	cfg.AAFanBonus = float32(*fanBonusAA)
	cfg.TripsFanBonus = float32(*fanBonusTrips)
	cfg.Epsilon = float32(*rolloutEpsilon)
	log.Printf("[az] rollout epsilon = %.2f (label diversity injection)", cfg.Epsilon)

	if err := os.MkdirAll(*ckptDir, 0755); err != nil {
		log.Fatalf("mkdir ckpt-dir: %v", err)
	}

	// Initial best ckpt
	bestCkpt := *warmStart
	if bestCkpt == "" {
		log.Print("[az] no warm-start, will use embedded weights for iter 0 self-play")
	} else {
		log.Printf("[az] warm-start from %s", bestCkpt)
		if err := ofc.LoadWeightsFromFile(bestCkpt); err != nil {
			log.Fatalf("load warm-start: %v", err)
		}
	}

	// Load testcases (if file specified). Bench best 一次 (缓存), 之后只 bench new.
	var testCases []testCase
	bestTestPass := -1 // -1 = 未 bench (testcase disabled 或 fresh)
	if *casesFile != "" {
		tc, err := loadTestCases(*casesFile)
		if err != nil {
			log.Printf("[az] testcase bench DISABLED — load %s failed: %v", *casesFile, err)
		} else {
			testCases = tc
			// Set MctsSimsMult globally for bench. Only affects ExpertPlace5/3 used by benchTestcases.
			// Self-play / duel use MCTSSearch (mcts.go) which 不读 MctsSimsMult.
			ofc.MctsSimsMult = float32(*benchSimsMult)
			// Env DISABLE_MCTS=1 → bench 用纯 MLP (prerank top-1, 跳过 rollout)
			// 用于以 NN 真实能力为 PROMOTE 决策依据 (生产模式对齐)
			if os.Getenv("DISABLE_MCTS") != "" {
				ofc.MctsDisabled = true
				log.Printf("[az] DISABLE_MCTS set → bench 用纯 MLP (no rollout)")
			}
			log.Printf("[az] testcase bench enabled — %d cases from %s, MctsSimsMult=%.1f (跟 run-cases.sh MCTS_SIMS_MULT 对齐)",
				len(testCases), *casesFile, *benchSimsMult)
			if bestCkpt != "" {
				t0 := time.Now()
				bestTestPass = benchTestcases(bestCkpt, testCases, *jokers, &cfg, rng, *workers)
				log.Printf("[az] initial best testcase: %d/%d pass (%.1fs)",
					bestTestPass, len(testCases), time.Since(t0).Seconds())
				// Reload best weights for self-play (bench 改了 global weights)
				if err := ofc.LoadWeightsFromFile(bestCkpt); err != nil {
					log.Printf("WARN: reload best after bench: %v", err)
				}
			}
		}
	}

	for iter := 0; iter < *iters; iter++ {
		log.Printf("\n=== AlphaZero Iteration %d/%d (elapsed %.1f min) ===",
			iter+1, *iters, time.Since(startT).Minutes())

		// Step 1: self-play
		log.Printf("[az iter %d] self-play %d games (mcts-sims=%d)...", iter+1, *gamesPerIter, *mctsSims)
		samplesDir := filepath.Join(*ckptDir, fmt.Sprintf("iter-%03d-samples", iter+1))
		os.MkdirAll(samplesDir, 0755)
		genStart := time.Now()
		samples := selfPlayCollect(*gamesPerIter, *mctsSims, *valueRollouts, &cfg, *jokers, *phantomOpp, *workers, rng)
		log.Printf("[az iter %d] collected %d samples in %.1f min",
			iter+1, len(samples), time.Since(genStart).Minutes())

		// Save samples to disk for train subprocess
		samplesPath := filepath.Join(samplesDir, "samples.jsonl.gz")
		if err := saveSamplesJSONL(samplesPath, samples); err != nil {
			log.Fatalf("save samples: %v", err)
		}

		// Step 2: train new NN — train binary 保存到 iter subdir, 再 rename 成 iter-NNN.json
		// init-from-ckpt: 用 best ckpt warm-start (跨 iter 累积学习, 避免每 iter from scratch)
		iterCkptDir := filepath.Join(*ckptDir, fmt.Sprintf("iter-%03d-train", iter+1))
		os.MkdirAll(iterCkptDir, 0755)
		log.Printf("[az iter %d] training new NN → %s (init-from %s)", iter+1, iterCkptDir, filepath.Base(bestCkpt))
		trainStart := time.Now()
		if err := trainNNSubprocessTo(samplesDir, iterCkptDir, iter+1, bestCkpt); err != nil {
			log.Fatalf("train: %v", err)
		}
		log.Printf("[az iter %d] trained in %.1f min", iter+1, time.Since(trainStart).Minutes())

		// Find produced ckpt (round-001-accXX.json) and rename to iter-NNN.json
		newCkpt := filepath.Join(*ckptDir, fmt.Sprintf("iter-%03d.json", iter+1))
		producedCkpt, err := findLatestCkpt(iterCkptDir)
		if err != nil {
			log.Fatalf("find produced ckpt: %v", err)
		}
		if err := os.Rename(producedCkpt, newCkpt); err != nil {
			log.Fatalf("rename ckpt: %v", err)
		}
		log.Printf("[az iter %d] ckpt at %s", iter+1, newCkpt)

		// Step 3: duel new vs best
		if bestCkpt == "" {
			log.Printf("[az iter %d] no current best, promote new directly", iter+1)
			bestCkpt = newCkpt
			continue
		}
		// Special case: if best is older format (outDim mismatch), auto-promote new
		// (otherwise warm-start chain stuck at old format forever)
		if newOut, oldOut := readCkptOutDim(newCkpt), readCkptOutDim(bestCkpt); newOut != oldOut {
			log.Printf("[az iter %d] outDim mismatch (new=%d vs best=%d), AUTO-PROMOTE for warm-start chain",
				iter+1, newOut, oldOut)
			bestCkpt = newCkpt
			if err := ofc.LoadWeightsFromFile(bestCkpt); err != nil {
				log.Printf("WARN: failed to load auto-promoted ckpt: %v", err)
			}
			continue
		}
		log.Printf("[az iter %d] duel new vs best (%d games)...", iter+1, *duelGames)
		ds := duelTwo(newCkpt, bestCkpt, *duelGames, *mctsSims, &cfg, *jokers, *phantomOpp, *workers, rng)

		// Bench testcases (new ckpt). best 用缓存值 bestTestPass.
		newTestPass := -1
		if len(testCases) > 0 {
			t0 := time.Now()
			newTestPass = benchTestcases(newCkpt, testCases, *jokers, &cfg, rng, *workers)
			log.Printf("[az iter %d] testcase: new=%d/%d  best=%d/%d (%.1fs)",
				iter+1, newTestPass, len(testCases), bestTestPass, len(testCases), time.Since(t0).Seconds())
		}

		log.Printf("[az iter %d] duel: new fan=%d/%d (%.1f%%) score=%.1f foul=%d | best fan=%d/%d (%.1f%%) score=%.1f foul=%d",
			iter+1,
			ds.fanA, ds.games, float64(ds.fanA)/float64(ds.games)*100, ds.scoreA, ds.foulA,
			ds.fanB, ds.games, float64(ds.fanB)/float64(ds.games)*100, ds.scoreB, ds.foulB)

		// 三独立 PROMOTE 规则 (任一触发 → PROMOTE):
		//   1. testcase 通过率 ↑                   → PROMOTE
		//   2. 进范数 ↑                           → PROMOTE
		//   3. 最终分 ↑ AND 进范数 = AND testcase= → PROMOTE  (其他必须 stable)
		// Safety guard: testcase 跌 > testcase-drop-limit → 强制 DISCARD (盖过 1/2/3)
		testcaseAvail := newTestPass >= 0 && bestTestPass >= 0
		testcaseUp := testcaseAvail && newTestPass > bestTestPass
		testcaseEqual := !testcaseAvail || newTestPass == bestTestPass
		testcaseCrash := testcaseAvail && *testcaseDropLimit > 0 &&
			bestTestPass-newTestPass > *testcaseDropLimit
		fantasyUp := ds.fanA > ds.fanB
		fantasyEqual := ds.fanA == ds.fanB
		scoreUp := ds.scoreA > ds.scoreB

		promote := false
		var reasons []string
		if testcaseUp {
			promote = true
			reasons = append(reasons, fmt.Sprintf("testcase↑ (%d→%d)", bestTestPass, newTestPass))
		}
		if fantasyUp {
			promote = true
			reasons = append(reasons, fmt.Sprintf("fantasy↑ (%d→%d)", ds.fanB, ds.fanA))
		}
		if scoreUp && fantasyEqual && testcaseEqual {
			promote = true
			reasons = append(reasons, fmt.Sprintf("score↑ (%.1f→%.1f) [其他稳定]", ds.scoreB, ds.scoreA))
		}
		// Safety guard overrides everything
		if testcaseCrash {
			promote = false
			reasons = []string{fmt.Sprintf("testcase 崩 (%d→%d, 跌 %d > limit %d) 强制 DISCARD",
				bestTestPass, newTestPass, bestTestPass-newTestPass, *testcaseDropLimit)}
		}
		reason := strings.Join(reasons, " + ")

		if promote {
			log.Printf("[az iter %d] ✓ PROMOTE new → best (%s)", iter+1, reason)
			bestCkpt = newCkpt
			if newTestPass >= 0 {
				bestTestPass = newTestPass // 更新缓存
			}
			if err := ofc.LoadWeightsFromFile(bestCkpt); err != nil {
				log.Printf("WARN: failed to load promoted ckpt: %v", err)
			}
		} else {
			log.Printf("[az iter %d] ✗ DISCARD new (testcase/fantasy/score 均未严格优于 best)", iter+1)
		}
	}

	log.Printf("\n[az] Training complete. Best ckpt: %s", bestCkpt)
}

// selfPlayCollect — 跑 N games, 每 game MCTS at each decision, 收 samples
// 用 workers 个 goroutine 并行 (每 worker 独立 RNG seed). MCTS 跟 TrainedEval 都 thread-safe
// (每次 alloc 自己的 buffer, 只读 defaultTrainedNet pointer).
func selfPlayCollect(numGames, mctsSims, valueRollouts int, cfg *ofc.RolloutConfig, jokers, phantomOpp, workers int, rng *rand.Rand) []sampleRecord {
	if workers <= 1 {
		out := make([]sampleRecord, 0, numGames*80)
		for g := 0; g < numGames; g++ {
			out = append(out, selfPlayOneGame(mctsSims, valueRollouts, cfg, jokers, phantomOpp, rng)...)
		}
		return out
	}
	results := make([][]sampleRecord, numGames)
	jobs := make(chan int, numGames)
	for i := 0; i < numGames; i++ {
		jobs <- i
	}
	close(jobs)

	var done atomic.Int32
	progressTicker := time.NewTicker(60 * time.Second)
	defer progressTicker.Stop()
	startT := time.Now()
	go func() {
		for range progressTicker.C {
			d := done.Load()
			if int(d) >= numGames {
				return
			}
			elapsed := time.Since(startT).Minutes()
			rate := float64(d) / elapsed
			etaMin := float64(numGames-int(d)) / rate
			log.Printf("  [self-play progress] %d/%d games (%.0f g/min, ETA %.1f min)",
				d, numGames, rate, etaMin)
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		// per-worker RNG, derived from main rng
		seed := rng.Int63() ^ int64(uint64(w)*0x9E3779B97F4A7C15)
		go func(workerSeed int64) {
			defer wg.Done()
			workerRng := rand.New(rand.NewSource(workerSeed))
			for idx := range jobs {
				results[idx] = selfPlayOneGame(mctsSims, valueRollouts, cfg, jokers, phantomOpp, workerRng)
				done.Add(1)
			}
		}(seed)
	}
	wg.Wait()

	totalLen := 0
	for _, r := range results {
		totalLen += len(r)
	}
	out := make([]sampleRecord, 0, totalLen)
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

type sampleRecord struct {
	Features     []float32
	McScore      float32
	FanRate      float32
	FoulRate     float32
	PolicyTarget float32
	Round        int
}

// selfPlayOneGame — 1 game self-play, 每 round 同时跑:
//   (1) MCTS (mctsSims) → 提供 policy target (visit_share per candidate)
//   (2) 每候选独立 N rollout (valueRollouts) → 干净 per-candidate value label (跟 v14 silver-label 同质)
// 这样 value/policy 标签都是 per-candidate 正确, 不再 share. valueRollouts=0 表示退化用 MCTS Q (噪声大).
func selfPlayOneGame(mctsSims, valueRollouts int, cfg *ofc.RolloutConfig, jokers, phantomOpp int, rng *rand.Rand) []sampleRecord {
	state := ofc.NewGameState(jokers)
	deck := ofc.MakeDeck(jokers)
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}

	opp := 0
	slot := 0
	if phantomOpp > 0 {
		opp = rng.Intn(phantomOpp + 1)
		if opp > 0 {
			slot = rng.Intn(opp + 1)
		}
	}
	maxPhantom := phantomCountFor(5, slot, opp)
	if len(deck)-maxPhantom < 17 {
		maxPhantom = len(deck) - 17
		if maxPhantom < 0 {
			maxPhantom = 0
		}
	}
	phantomReserveStart := len(deck) - maxPhantom
	myCards := deck[:17]
	phantomAdded := 0

	// 每候选 sample: features + policyTarget (MCTS visit_share) + 独立 rollout 的 mcScore/fanRate/foulRate.
	type pendingSample struct {
		features     []float32
		policyTarget float32
		mcScore      float32 // 独立 N rollout 的 mean score (含 fan bonus / -foul cost)
		fanRate      float32 // 独立 rollout 的 fan 比率
		foulRate     float32 // 独立 rollout 的 foul 比率
		round        int
	}
	var pending []pendingSample
	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}

	for round := 1; round <= 5; round++ {
		state.Round = round
		want := phantomCountFor(round, slot, opp)
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

		// MCTS to choose action + collect visit distribution
		mctsCfg := ofc.MCTSConfig{
			Sims:       mctsSims,
			CPuct:      1.5,
			UseValue:   true,
			RolloutCfg: cfg,
			Rng:        rng,
		}
		bestAction, stats := ofc.MCTSSearch(state, dealt, round, mctsCfg)

		// 记录每个 candidate 的 sample
		totalVisits := 0
		for _, s := range stats {
			totalVisits += s.Visits[0]
		}
		if totalVisits == 0 {
			totalVisits = 1
		}
		for _, s := range stats {
			tmp := state.Clone()
			ofc.ApplyMCTSAction(tmp, dealt, s.Action)

			// 独立 N rollout 算 per-candidate value/fan/foul (v14 silver-label 风格)
			// fall back 到 MCTS Q 如果 valueRollouts=0
			var mcScore, fanRate, foulRate float32
			if valueRollouts > 0 {
				var sumScore float32
				sumFan, sumFoul := 0, 0
				for k := 0; k < valueRollouts; k++ {
					rolloutState := tmp.Clone()
					sc := er.QuickRollout(rolloutState, round)
					sumScore += sc
					if er.LastResult.IsFantasy {
						sumFan++
					}
					if er.LastResult.IsFoul {
						sumFoul++
					}
				}
				mcScore = sumScore / float32(valueRollouts)
				fanRate = float32(sumFan) / float32(valueRollouts)
				foulRate = float32(sumFoul) / float32(valueRollouts)
			} else {
				if s.Visits[0] == 0 {
					continue // skip 0-visit candidates (Q meaningless)
				}
				mcScore = s.Q[0]
				fanRate = 0
				foulRate = 0
			}

			pending = append(pending, pendingSample{
				features:     ofc.BuildFeatures(tmp, *inDim),
				policyTarget: float32(s.Visits[0]) / float32(totalVisits),
				mcScore:      mcScore,
				fanRate:      fanRate,
				foulRate:     foulRate,
				round:        round,
			})
		}

		// Apply best to state
		ofc.ApplyMCTSAction(state, dealt, bestAction)
	}

	// 不需要 game-end eventualScore — 每个 sample 已经有 per-candidate rollout-mean.
	// pending 里的 mcScore/fanRate/foulRate 都是 per-candidate 干净 label.
	out := make([]sampleRecord, 0, len(pending))
	for _, p := range pending {
		out = append(out, sampleRecord{
			Features:     p.features,
			McScore:      p.mcScore,
			FanRate:      p.fanRate,
			FoulRate:     p.foulRate,
			PolicyTarget: p.policyTarget,
			Round:        p.round,
		})
	}
	return out
}

// classifyFanBonus 已弃 (2026-05-19, cap-down 误算). 用 ofc.FantasyBonusFromBoard 替代.
// 函数保留但不再被调用, 防止反向引用 break.
func classifyFanBonus_DEPRECATED(top []ofc.Card, cfg *ofc.RolloutConfig) float32 {
	var realCnt [13]int
	jokerCnt := 0
	for _, c := range top {
		if c.IsJoker() {
			jokerCnt++
		} else {
			realCnt[c.Rank()]++
		}
	}
	realMax := 0
	for _, v := range realCnt {
		if v > realMax {
			realMax = v
		}
	}
	effMax := realMax + jokerCnt
	if effMax >= 3 {
		return cfg.TripsFanBonus
	}
	pairR := -1
	if jokerCnt >= 1 {
		for r := 12; r >= 0; r-- {
			if realCnt[r] > 0 {
				pairR = r
				break
			}
		}
		if pairR < 0 && jokerCnt >= 2 {
			pairR = 12
		}
	} else {
		for r := 12; r >= 0; r-- {
			if realCnt[r] >= 2 {
				pairR = r
				break
			}
		}
	}
	if pairR >= 12 {
		return cfg.AAFanBonus
	}
	if pairR >= 11 {
		return cfg.KKFanBonus
	}
	return cfg.QQFanBonus
}

// trainNNSubprocessTo — 调用 ofc-train, ckpt 保存到 ckptDir.
// initFromCkpt 非空时, train MLP 从该 ckpt warm-start (跨 iter 累积).
// initFromCkpt outDim 必须跟 train (-outdim 4) 匹配, 否则 train 自动 fallback 到 NewMLP.
func trainNNSubprocessTo(samplesDir, ckptDir string, iter int, initFromCkpt string) error {
	args := []string{
		"-dataset-dir", samplesDir,
		"-hours", "0.5",
		"-round-min", "60",
		"-outdim", "4",
		"-h1", fmt.Sprintf("%d", *hiddenH1),
		"-h2", fmt.Sprintf("%d", *hiddenH2),
		"-h3", fmt.Sprintf("%d", *hiddenH3),
		"-indim", fmt.Sprintf("%d", *inDim),
		"-fan-w", fmt.Sprintf("%.2f", *fanW),
		"-foul-w", fmt.Sprintf("%.2f", *foulW),
		"-policy-w", fmt.Sprintf("%.2f", *policyW),
		"-epochs", fmt.Sprintf("%d", *trainEpochs),
		"-lr", fmt.Sprintf("%.5f", *trainLR),
		"-warm-lr-mult", fmt.Sprintf("%.2f", *trainWarmLRMult),
		"-ckpt-dir", ckptDir,
		"-policy", fmt.Sprintf("%s-iter%d", *policyVer, iter),
	}
	if initFromCkpt != "" {
		args = append(args, "-init-from-ckpt", initFromCkpt)
	}
	cmd := exec.Command(*trainBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findLatestCkpt — 找到 dir 下最新的 round-NNN-accXX.json
func findLatestCkpt(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var latest string
	var latestMod time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !filepathMatchRoundCkpt(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latest = filepath.Join(dir, name)
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no round-*.json ckpt in %s", dir)
	}
	return latest, nil
}

// readCkptOutDim — 读 ckpt JSON 的 outDim 字段 (为 auto-promote 用)
func readCkptOutDim(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var meta struct {
		OutDim int `json:"outDim"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0
	}
	if meta.OutDim == 0 {
		return 1 // legacy single-head
	}
	return meta.OutDim
}

func filepathMatchRoundCkpt(name string) bool {
	if len(name) < 11 {
		return false
	}
	return name[:6] == "round-" && filepath.Ext(name) == ".json"
}

// saveSamplesJSONL — 写 sample 到 JSONL.gz
func saveSamplesJSONL(path string, samples []sampleRecord) error {
	// 复用 train.go Sample 格式: features, mcScore, fanRate, foulRate, policyTarget
	// 用 round1 子目录 (跟 train -dataset-dir 期望格式兼容)
	dir := filepath.Dir(path)
	// 把 samples 按 round 分目录写, 跟 gen-oracle-dataset format 兼容
	byRound := make(map[int][]sampleRecord)
	for _, s := range samples {
		byRound[s.Round] = append(byRound[s.Round], s)
	}
	for r, rs := range byRound {
		roundDir := filepath.Join(dir, fmt.Sprintf("round%d", r))
		if err := os.MkdirAll(roundDir, 0755); err != nil {
			return err
		}
		shardPath := filepath.Join(roundDir, "shard-00001.jsonl.gz")
		if err := writeShard(shardPath, rs); err != nil {
			return err
		}
	}
	return nil
}

// === testcase bench (跟 test-cases.js 等价 logic, in-process) ===

type testCase struct {
	Name  string   `json:"name"`
	Round int      `json:"round"`
	Dealt []string `json:"dealt"`
	State struct {
		Top       []string `json:"top"`
		Middle    []string `json:"middle"`
		Bottom    []string `json:"bottom"`
		UsedCards []string `json:"usedCards"`
	} `json:"state"`
	Expecteds []struct {
		Top    []string `json:"top"`
		Middle []string `json:"middle"`
		Bottom []string `json:"bottom"`
	} `json:"expecteds"`
}

func loadTestCases(path string) ([]testCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// 跟 test-cases.js normCard 一致: joker → "X" (忽略 jid)
func normCardStr(c string) string {
	if c == "X" || strings.HasPrefix(c, "X") {
		return "X"
	}
	return c
}

func sortKeyStrs(cards []string) string {
	norm := make([]string, len(cards))
	for i, c := range cards {
		norm[i] = normCardStr(c)
	}
	sort.Strings(norm)
	return strings.Join(norm, ",")
}

// benchOneCase — 单 case 测试, 返回是否 pass. 并发安全 (per-call ExpertRollout + 独立 rng).
func benchOneCase(c testCase, jokers int, cfg *ofc.RolloutConfig, rng *rand.Rand) bool {
	state := ofc.NewGameState(jokers)
	state.Round = c.Round
	for _, cs := range c.State.Top {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowTop)
	}
	for _, cs := range c.State.Middle {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowMiddle)
	}
	for _, cs := range c.State.Bottom {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowBottom)
	}
	for _, cid := range c.State.UsedCards {
		state.UsedCards[cid] = true
	}

	dealt := make([]ofc.Card, 0, len(c.Dealt))
	for _, ds := range c.Dealt {
		card, ok := ofc.ParseCard(ds)
		if !ok {
			continue
		}
		dealt = append(dealt, card)
	}

	beforeTop := append([]ofc.Card(nil), state.Top...)
	beforeMid := append([]ofc.Card(nil), state.Middle...)
	beforeBot := append([]ofc.Card(nil), state.Bottom...)

	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
	if c.Round == 1 || len(dealt) == 5 {
		er.ExpertPlace5(state, dealt)
	} else {
		er.ExpertPlace3(state, dealt)
	}

	addedTop := diffCardsAZ(beforeTop, state.Top)
	addedMid := diffCardsAZ(beforeMid, state.Middle)
	addedBot := diffCardsAZ(beforeBot, state.Bottom)

	aiTop := cardSlicetoStrs(addedTop)
	aiMid := cardSlicetoStrs(addedMid)
	aiBot := cardSlicetoStrs(addedBot)

	for _, exp := range c.Expecteds {
		if sortKeyStrs(aiTop) == sortKeyStrs(exp.Top) &&
			sortKeyStrs(aiMid) == sortKeyStrs(exp.Middle) &&
			sortKeyStrs(aiBot) == sortKeyStrs(exp.Bottom) {
			return true
		}
	}
	return false
}

// benchTestcases — 并行跑 cases, 返回 pass 数. 跟 test-cases.js / ofc-go server 同 layout-match 语义.
// 走 ExpertPlace5/3 (跟生产 server 默认 path 一致), 不走 MCTSSearch (AZ-PUCT 太弱).
// 单 ckpt LoadWeightsFromFile 一次, 之后 63 case 并行 (per-worker rng + per-case ExpertRollout 实例).
// TrainedNet 全局只读 + Go 内存模型保证多 reader 安全.
func benchTestcases(ckpt string, cases []testCase, jokers int, cfg *ofc.RolloutConfig, rng *rand.Rand, workers int) int {
	if err := ofc.LoadWeightsFromFile(ckpt); err != nil {
		log.Printf("[bench] load %s: %v", ckpt, err)
		return 0
	}
	if workers <= 1 {
		workers = 1
	}
	if workers > len(cases) {
		workers = len(cases)
	}

	var pass atomic.Int32
	jobs := make(chan int, len(cases))
	for i := 0; i < len(cases); i++ {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		seed := rng.Int63() ^ int64(uint64(w)*0x9E3779B97F4A7C15)
		go func(workerSeed int64) {
			defer wg.Done()
			workerRng := rand.New(rand.NewSource(workerSeed))
			for idx := range jobs {
				if benchOneCase(cases[idx], jokers, cfg, workerRng) {
					pass.Add(1)
				}
			}
		}(seed)
	}
	wg.Wait()
	return int(pass.Load())
}

func diffCardsAZ(before, after []ofc.Card) []ofc.Card {
	beforeSet := make(map[string]int)
	for _, c := range before {
		beforeSet[c.ID()]++
	}
	out := make([]ofc.Card, 0)
	for _, c := range after {
		if beforeSet[c.ID()] > 0 {
			beforeSet[c.ID()]--
		} else {
			out = append(out, c)
		}
	}
	return out
}

func cardSlicetoStrs(cs []ofc.Card) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}

// duelResult — 一场 duel game 的结果
type duelResult struct {
	score   float32
	fantasy bool
	foul    bool
}

// duelStats — N 场 duel 聚合统计
type duelStats struct {
	games          int
	fanA, fanB     int     // 进范局数
	scoreA, scoreB float32 // 总分 (royalty + fan_bonus - foul_cost 累加)
	foulA, foulB   int     // foul 局数
}

// duelTwo — 跑 duelGames 场 same-hand duel, 返回聚合统计.
// 调用方按 lexicographic (fantasy 优先, score tiebreak) 决定 PROMOTE / DISCARD.
func duelTwo(ckptA, ckptB string, numGames, mctsSims int, cfg *ofc.RolloutConfig, jokers, phantomOpp, workers int, rng *rand.Rand) duelStats {
	// Pre-gen game configs
	type gs struct {
		deck []ofc.Card
		opp  int
		slot int
	}
	games := make([]gs, numGames)
	for i := 0; i < numGames; i++ {
		deck := ofc.MakeDeck(jokers)
		for j := len(deck) - 1; j > 0; j-- {
			k := rng.Intn(j + 1)
			deck[j], deck[k] = deck[k], deck[j]
		}
		opp := 0
		slot := 0
		if phantomOpp > 0 {
			opp = rng.Intn(phantomOpp + 1)
			if opp > 0 {
				slot = rng.Intn(opp + 1)
			}
		}
		games[i] = gs{deck, opp, slot}
	}

	runOne := func(ckpt string) []duelResult {
		if err := ofc.LoadWeightsFromFile(ckpt); err != nil {
			log.Fatalf("duel: load %s: %v", ckpt, err)
		}
		results := make([]duelResult, numGames)
		if workers <= 1 {
			for i, g := range games {
				results[i] = duelPlayGame(g.deck, g.opp, g.slot, cfg, mctsSims, jokers, rng)
			}
			return results
		}
		jobs := make(chan int, numGames)
		for i := 0; i < numGames; i++ {
			jobs <- i
		}
		close(jobs)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			seed := rng.Int63() ^ int64(uint64(w)*0x9E3779B97F4A7C15)
			go func(workerSeed int64) {
				defer wg.Done()
				workerRng := rand.New(rand.NewSource(workerSeed))
				for idx := range jobs {
					g := games[idx]
					results[idx] = duelPlayGame(g.deck, g.opp, g.slot, cfg, mctsSims, jokers, workerRng)
				}
			}(seed)
		}
		wg.Wait()
		return results
	}

	resultsA := runOne(ckptA)
	resultsB := runOne(ckptB)

	stats := duelStats{games: numGames}
	for i := 0; i < numGames; i++ {
		if resultsA[i].fantasy {
			stats.fanA++
		}
		if resultsB[i].fantasy {
			stats.fanB++
		}
		if resultsA[i].foul {
			stats.foulA++
		}
		if resultsB[i].foul {
			stats.foulB++
		}
		stats.scoreA += resultsA[i].score
		stats.scoreB += resultsB[i].score
	}
	return stats
}

func duelPlayGame(deck []ofc.Card, opp, slot int, cfg *ofc.RolloutConfig, mctsSims, jokers int, rng *rand.Rand) duelResult {
	state := ofc.NewGameState(jokers)
	maxPhantom := phantomCountFor(5, slot, opp)
	if len(deck)-maxPhantom < 17 {
		maxPhantom = len(deck) - 17
		if maxPhantom < 0 {
			maxPhantom = 0
		}
	}
	phantomReserveStart := len(deck) - maxPhantom
	myCards := deck[:17]
	phantomAdded := 0

	for round := 1; round <= 5; round++ {
		state.Round = round
		want := phantomCountFor(round, slot, opp)
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
		mctsCfg := ofc.MCTSConfig{
			Sims: mctsSims, CPuct: 1.5, UseValue: true, RolloutCfg: cfg, Rng: rng,
		}
		action, _ := ofc.MCTSSearch(state, dealt, round, mctsCfg)
		ofc.ApplyMCTSAction(state, dealt, action)
	}

	if !state.IsComplete() {
		return duelResult{score: -cfg.FoulCost, foul: true}
	}
	score := state.Score()
	if score.Foul {
		return duelResult{score: -cfg.FoulCost, foul: true}
	}
	raw := float32(score.Royalties)
	if score.Fantasy {
		// 2026-05-19: cap-chain aware fan bonus (替代旧 classifyFanBonus cap-down 误算).
		fb, _ := ofc.FantasyBonusFromBoard(state.Top, state.Middle, state.Bottom,
			cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
		raw += fb
		return duelResult{score: raw, fantasy: true}
	}
	return duelResult{score: raw}
}

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

// writeShard — 写 sample 到 JSONL.gz (复用 gen-oracle-dataset 格式)
func writeShard(path string, samples []sampleRecord) error {
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Round < samples[j].Round })
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	enc := json.NewEncoder(gz)
	for _, s := range samples {
		// 写跟 train.go Sample 一致 schema
		jsonSample := map[string]interface{}{
			"features":     s.Features,
			"mcScore":      s.McScore,
			"fanRate":      s.FanRate,
			"foulRate":     s.FoulRate,
			"policyTarget": s.PolicyTarget,
			"round":        s.Round,
		}
		if err := enc.Encode(jsonSample); err != nil {
			return err
		}
	}
	return nil
}
