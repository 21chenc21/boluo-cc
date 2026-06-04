// duel — 两个 ckpt 对抗 N 场, 输出胜率. 用于 AlphaZero promotion gate.
//
// 同 hand duel: 每场发同 17 张牌, A 用 ckpt1+MCTS 摆, B 用 ckpt2+MCTS 摆.
// 比较 final royalty, 高者胜. 排除 deck-luck variance.
//
// 用法:
//   ./duel -ckpt1 path/a.json -ckpt2 path/b.json -games 100 -mcts-sims 200
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	ckpt1     = flag.String("ckpt1", "", "ckpt path A / seat0 (required)")
	ckpt2     = flag.String("ckpt2", "", "ckpt path B / seat1 (required)")
	ckpt3     = flag.String("ckpt3", "", "ckpt path C / seat2 (可选; 设了→3 玩家真实对局, 共享一副牌 + 配对 OFC 计分)")
	numGames  = flag.Int("games", 100, "duel games (每 seed)")
	numSeeds  = flag.Int("seeds", 1, "跑多少个 base seed (聚合 + 报每 seed), 总局数 = games × seeds")
	mctsSims  = flag.Int("mcts-sims", 200, "MCTS sims per decision (0=use ExpertPlace, no MCTS). 当 -sims2 ≥ 0 时只用于 A.")
	mctsSims2 = flag.Int("sims2", -1, "B 的 MCTS sims (单独控制, -1=跟 A 同). 2026-05-23 加, 用于 sims 强度对比.")
	jokers    = flag.Int("jokers", 2, "deck joker count")
	phantomOpp = flag.Int("phantom-opponents", 2, "max phantom opponents (0..N uniform per game)")
	seed      = flag.Int64("seed", 0, "RNG seed (0=time)")
	workers   = flag.Int("workers", 0, "parallel workers (0=NumCPU). 2026-06-04 加, ~Ncore 加速.")
	verbose   = flag.Bool("v", false, "verbose per-game scores")

	// Fan/foul knob (跟 train 一致)
	foulCost      = flag.Float64("foul-cost", 6, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "")
)

func main() {
	flag.Parse()
	if *ckpt1 == "" || *ckpt2 == "" {
		fmt.Fprintln(os.Stderr, "usage: duel -ckpt1 a.json -ckpt2 b.json [-games N] [-mcts-sims N]")
		os.Exit(1)
	}

	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(*seed))

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanBonusQQ)
	cfg.KKFanBonus = float32(*fanBonusKK)
	cfg.AAFanBonus = float32(*fanBonusAA)
	cfg.TripsFanBonus = float32(*fanBonusTrips)

	sims2 := *mctsSims
	if *mctsSims2 >= 0 {
		sims2 = *mctsSims2
	}

	// 3 玩家真实对局模式 (ckpt3 设了)
	if *ckpt3 != "" {
		run3Player(&cfg, *mctsSims, *seed)
		return
	}

	log.Printf("[duel] %s vs %s — %d games, A sims=%d, B sims=%d, jokers=%d, phantom-opp-max=%d",
		shortName(*ckpt1), shortName(*ckpt2), *numGames, *mctsSims, sims2, *jokers, *phantomOpp)

	// Pre-generate game configs (deck+phantom) so both ckpts see SAME hands
	games := make([]gameSetup, *numGames)
	for i := 0; i < *numGames; i++ {
		deck := ofc.MakeDeck(*jokers)
		shuffleDeck(deck, rng)
		opp := 0
		slot := 0
		if *phantomOpp > 0 {
			opp = rng.Intn(*phantomOpp + 1)
			if opp > 0 {
				slot = rng.Intn(opp + 1)
			}
		}
		games[i] = gameSetup{deck, opp, slot}
	}

	// Run ckpt1 on all games
	log.Printf("[duel] running %s on %d games...", shortName(*ckpt1), *numGames)
	if err := ofc.LoadWeightsFromFile(*ckpt1); err != nil {
		log.Fatalf("load ckpt1: %v", err)
	}
	scores1, tiers1 := runAllGames(games, &cfg, *mctsSims, *seed, *workers)

	// Run ckpt2
	log.Printf("[duel] running %s on %d games (sims=%d)...", shortName(*ckpt2), *numGames, sims2)
	if err := ofc.LoadWeightsFromFile(*ckpt2); err != nil {
		log.Fatalf("load ckpt2: %v", err)
	}
	scores2, tiers2 := runAllGames(games, &cfg, sims2, *seed, *workers)

	// Tally
	w1, w2, draws := 0, 0, 0
	totalDelta := float64(0)
	for i := 0; i < *numGames; i++ {
		s1 := scores1[i]
		s2 := scores2[i]
		totalDelta += float64(s1 - s2)
		if s1 > s2 {
			w1++
		} else if s2 > s1 {
			w2++
		} else {
			draws++
		}
		if *verbose {
			fmt.Printf("game %d: %s=%.0f  %s=%.0f  Δ=%+.0f\n",
				i+1, shortName(*ckpt1), s1, shortName(*ckpt2), s2, s1-s2)
		}
	}

	rate1 := float64(w1) / float64(*numGames) * 100
	rate2 := float64(w2) / float64(*numGames) * 100
	avgDelta := totalDelta / float64(*numGames)

	fmt.Printf("\n=== Duel Result ===\n")
	fmt.Printf("%s wins: %d (%.1f%%)\n", shortName(*ckpt1), w1, rate1)
	fmt.Printf("%s wins: %d (%.1f%%)\n", shortName(*ckpt2), w2, rate2)
	fmt.Printf("Draws:   %d (%.1f%%)\n", draws, float64(draws)/float64(*numGames)*100)
	fmt.Printf("Avg score Δ (ckpt1 - ckpt2): %+.2f\n", avgDelta)

	// 范特西分档统计 (per side)
	tally := func(tiers []string) (qq, kk, aa, trips, foul int) {
		for _, t := range tiers {
			switch t {
			case "QQ":
				qq++
			case "KK":
				kk++
			case "AA":
				aa++
			case "trips":
				trips++
			case "foul":
				foul++
			}
		}
		return
	}
	q1, k1, a1, t1, f1 := tally(tiers1)
	q2, k2, a2, t2, f2 := tally(tiers2)
	fanTot := func(q, k, a, t int) int { return q + k + a + t }
	fmt.Printf("\n=== Fantasy 分档 (次数 / %d 局) ===\n", *numGames)
	fmt.Printf("%-22s  QQ=%-4d KK=%-4d AA=%-4d 三条=%-4d  范合计=%-4d  foul=%d\n",
		shortName(*ckpt1), q1, k1, a1, t1, fanTot(q1, k1, a1, t1), f1)
	fmt.Printf("%-22s  QQ=%-4d KK=%-4d AA=%-4d 三条=%-4d  范合计=%-4d  foul=%d\n",
		shortName(*ckpt2), q2, k2, a2, t2, fanTot(q2, k2, a2, t2), f2)

	// AlphaZero promotion gate: rate1 >= 55% means ckpt1 strictly better
	gate := 55.0
	rateNotDraw := float64(w1) / float64(w1+w2) * 100
	fmt.Printf("Win-rate of ckpt1 excl draws: %.1f%% (gate %.0f%% for promotion)\n", rateNotDraw, gate)
	if rateNotDraw >= gate {
		fmt.Printf("✓ ckpt1 PASSES gate vs ckpt2\n")
	} else {
		fmt.Printf("✗ ckpt1 FAILS gate (need ≥%.0f%% to promote)\n", gate)
	}
}

type gameSetup struct {
	deck      []ofc.Card
	opponents int
	slot      int
}

// runAllGames — 2026-06-04: 多核并行 (workers 个 goroutine), 每局独立 rng (seed^idx) 保可复现.
// 返回 scores + 每局 fan tier ("" / "QQ" / "KK" / "AA" / "trips" / "foul").
// 安全性: TrainedEval 每次 newEvalBuffers, 只读全局 net → 并发读安全 (同 bench-cases).
func runAllGames(games []gameSetup, cfg *ofc.RolloutConfig, sims int, baseSeed int64, workers int) ([]float32, []string) {
	scores := make([]float32, len(games))
	tiers := make([]string, len(games))
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	// 2026-06-04: sims==0 → pureMLP (生产路径, ExpertPlace5/3 走 top-1 prerank, ~5-50ms).
	// 否则 ExpertPlace5 内部仍跑 MCTS rollout (~秒级/R1, 旧版 1000 局 28min 的真凶).
	cfgLocal := *cfg
	cfgLocal.PureMLP = (sims == 0)
	cfg = &cfgLocal
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := range games {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			rng := rand.New(rand.NewSource(baseSeed ^ int64(uint64(idx)*0x9E3779B97F4A7C15)))
			scores[idx], tiers[idx] = playOneGame(games[idx].deck, games[idx].opponents, games[idx].slot, cfg, sims, rng)
		}(i)
	}
	wg.Wait()
	return scores, tiers
}

// fanTierOf — cap-aware 分类最终 board 的范特西档 (QQ/KK/AA/trips), 非范返回 "".
func fanTierOf(top, mid, bot []ofc.Card) string {
	if len(top) != 3 || len(mid) != 5 || len(bot) != 5 {
		return ""
	}
	be := ofc.Evaluate5JokerCap(bot, nil)
	me := ofc.Evaluate5JokerCap(mid, &be)
	te := ofc.Evaluate3JokerCap(top, &me)
	if be.Type < 0 || me.Type < 0 || te.Type < 0 {
		return ""
	}
	if ofc.HandExceeds5(me, be) || ofc.TopExceedsMid(te, me) {
		return ""
	}
	if te.Type == ofc.TypeThreeOfAKind {
		return "trips"
	}
	if te.Type == ofc.TypePair {
		switch (te.Value - 1000000) / 15 {
		case 12:
			return "AA"
		case 11:
			return "KK"
		case 10:
			return "QQ"
		}
	}
	return ""
}

func playOneGame(deck []ofc.Card, opponents, slot int, cfg *ofc.RolloutConfig, mctsSims int, rng *rand.Rand) (float32, string) {
	state := ofc.NewGameState(0)
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

	for round := 1; round <= 5; round++ {
		state.Round = round
		// Phantom 渐进注入
		want := phantomCountFor(round, slot, opponents)
		if want > maxPhantom {
			want = maxPhantom
		}
		for phantomAdded < want {
			state.UsedCards[deck[phantomReserveStart+phantomAdded].ID()] = true
			phantomAdded++
		}

		// 取 dealt
		var dealt []ofc.Card
		if round == 1 {
			dealt = myCards[0:5]
		} else {
			start := 5 + (round-2)*3
			dealt = myCards[start : start+3]
		}

		// MCTS 或 ExpertPlace 选 action
		if mctsSims > 0 {
			mctsCfg := ofc.MCTSConfig{
				Sims:       mctsSims,
				CPuct:      1.5,
				UseValue:   true,
				RolloutCfg: cfg,
				Rng:        rng,
			}
			action, _ := ofc.MCTSSearch(state, dealt, round, mctsCfg)
			ofc.ApplyMCTSAction(state, dealt, action)
		} else {
			er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
			if round == 1 {
				er.ExpertPlace5(state, dealt)
			} else {
				er.ExpertPlace3(state, dealt)
			}
		}
	}

	if !state.IsComplete() {
		return -cfg.FoulCost, "foul"
	}
	score := state.Score()
	if score.Foul {
		return -cfg.FoulCost, "foul"
	}
	raw := float32(score.Royalties)
	tier := ""
	if score.Fantasy {
		// 2026-05-19: cap-chain aware fan bonus.
		fb, _ := ofc.FantasyBonusFromBoard(state.Top, state.Middle, state.Bottom,
			cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
		raw += fb
		tier = fanTierOf(state.Top, state.Middle, state.Bottom)
	}
	return raw, tier
}

// ============================================================
// 3 玩家真实对局 (2026-06-04): 3 ckpt = 3 seat, 共享一副牌 (无重叠), 配对 OFC 计分.
// 简化: 每 seat 独立摆自己 17 张 (只看自己的牌, 不建模对手可见牌), 摆完配对算分.
// ckpt 全局加载冲突 → 按 seat 顺序 load+并行摆所有局, 三 seat 都摆完再计分.
// ============================================================

type seatRes struct {
	sc            ofc.ScoreResult
	top, mid, bot []ofc.Card
}

// playSeatBoard — 摆一个 seat 的 17 张 (R1=5, R2-5=3), 无 phantom, 返回最终 board.
func playSeatBoard(my []ofc.Card, cfg *ofc.RolloutConfig, sims int, rng *rand.Rand) seatRes {
	state := ofc.NewGameState(0)
	for round := 1; round <= 5; round++ {
		state.Round = round
		var dealt []ofc.Card
		if round == 1 {
			dealt = my[0:5]
		} else {
			start := 5 + (round-2)*3
			dealt = my[start : start+3]
		}
		if sims > 0 {
			mctsCfg := ofc.MCTSConfig{Sims: sims, CPuct: 1.5, UseValue: true, RolloutCfg: cfg, Rng: rng}
			action, _ := ofc.MCTSSearch(state, dealt, round, mctsCfg)
			ofc.ApplyMCTSAction(state, dealt, action)
		} else {
			er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
			if round == 1 {
				er.ExpertPlace5(state, dealt)
			} else {
				er.ExpertPlace3(state, dealt)
			}
		}
	}
	sc := ofc.ScoreHand(state.Top, state.Middle, state.Bottom)
	return seatRes{sc, state.Top, state.Middle, state.Bottom}
}

func cmpEval(a, b ofc.HandValue) int {
	if a.Type != b.Type {
		if a.Type > b.Type {
			return 1
		}
		return -1
	}
	if a.Value > b.Value {
		return 1
	}
	if a.Value < b.Value {
		return -1
	}
	return 0
}

// ofcPairNet — a 相对 b 的净分 (1-6 计分: 每行 ±1, 横扫 +3, + royalty 差). b 的净分 = -返回值.
func ofcPairNet(a, b ofc.ScoreResult) int {
	if a.Foul && b.Foul {
		return 0
	}
	if a.Foul {
		return -(6 + b.Royalties) // a 犯规: 输 6 (3 行 + 横扫) + b 的 royalty
	}
	if b.Foul {
		return 6 + a.Royalties
	}
	wins := cmpEval(a.TopEval, b.TopEval) + cmpEval(a.MidEval, b.MidEval) + cmpEval(a.BotEval, b.BotEval)
	scoop := 0
	if wins == 3 {
		scoop = 3
	} else if wins == -3 {
		scoop = -3
	}
	return wins + scoop + (a.Royalties - b.Royalties)
}

func run3Player(cfg *ofc.RolloutConfig, sims int, baseSeed int64) {
	seats := []string{*ckpt1, *ckpt2, *ckpt3}
	workersN := *workers
	if workersN <= 0 {
		workersN = runtime.NumCPU()
	}
	cfgLocal := *cfg
	cfgLocal.PureMLP = (sims == 0)

	log.Printf("[duel-3p] seat0=%s seat1=%s seat2=%s — %d games × %d seeds, sims=%d, jokers=%d",
		shortName(seats[0]), shortName(seats[1]), shortName(seats[2]), *numGames, *numSeeds, sims, *jokers)

	var totPts [3]float64
	tierCnt := [3]map[string]int{{}, {}, {}}
	foulCnt := [3]int{}
	totalGames := 0
	perSeed := make([]string, 0, *numSeeds)

	for s := 0; s < *numSeeds; s++ {
		seedBase := baseSeed + int64(s)
		rng := rand.New(rand.NewSource(seedBase))
		decks := make([][]ofc.Card, *numGames)
		for g := range decks {
			d := ofc.MakeDeck(*jokers)
			shuffleDeck(d, rng)
			decks[g] = d
		}
		boards := make([][3]seatRes, *numGames)
		// 按 seat load ckpt + 并行摆该 seat 所有局
		for seat := 0; seat < 3; seat++ {
			if err := ofc.LoadWeightsFromFile(seats[seat]); err != nil {
				log.Fatalf("load seat%d %s: %v", seat, seats[seat], err)
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, workersN)
			for g := 0; g < *numGames; g++ {
				wg.Add(1)
				sem <- struct{}{}
				go func(gg, st int) {
					defer wg.Done()
					defer func() { <-sem }()
					rg := rand.New(rand.NewSource(seedBase ^ int64(uint64(gg*3+st)*0x9E3779B97F4A7C15)))
					my := decks[gg][st*17 : st*17+17]
					boards[gg][st] = playSeatBoard(my, &cfgLocal, sims, rg)
				}(g, seat)
			}
			wg.Wait()
		}
		// 配对计分
		var seedPts [3]float64
		for g := 0; g < *numGames; g++ {
			b := boards[g]
			n0 := ofcPairNet(b[0].sc, b[1].sc) + ofcPairNet(b[0].sc, b[2].sc)
			n1 := ofcPairNet(b[1].sc, b[0].sc) + ofcPairNet(b[1].sc, b[2].sc)
			n2 := ofcPairNet(b[2].sc, b[0].sc) + ofcPairNet(b[2].sc, b[1].sc)
			seedPts[0] += float64(n0)
			seedPts[1] += float64(n1)
			seedPts[2] += float64(n2)
			for st := 0; st < 3; st++ {
				if b[st].sc.Foul {
					foulCnt[st]++
				} else if b[st].sc.Fantasy {
					tierCnt[st][fanTierOf(b[st].top, b[st].mid, b[st].bot)]++
				}
			}
		}
		for st := 0; st < 3; st++ {
			totPts[st] += seedPts[st]
		}
		totalGames += *numGames
		perSeed = append(perSeed, fmt.Sprintf("  seed %d: seat0=%+.2f seat1=%+.2f seat2=%+.2f (pts/局)",
			seedBase, seedPts[0]/float64(*numGames), seedPts[1]/float64(*numGames), seedPts[2]/float64(*numGames)))
	}

	fmt.Printf("\n=== 3-Player Duel (%d 局 = %d games × %d seeds, sims=%d) ===\n", totalGames, *numGames, *numSeeds, sims)
	names := []string{shortName(seats[0]), shortName(seats[1]), shortName(seats[2])}
	for st := 0; st < 3; st++ {
		t := tierCnt[st]
		fanTot := t["QQ"] + t["KK"] + t["AA"] + t["trips"]
		fmt.Printf("seat%d %-22s  平均 %+6.3f pts/局  | 范: QQ=%d KK=%d AA=%d 三条=%d (合计 %d, %.1f%%)  foul=%d (%.1f%%)\n",
			st, names[st], totPts[st]/float64(totalGames),
			t["QQ"], t["KK"], t["AA"], t["trips"], fanTot, 100*float64(fanTot)/float64(totalGames),
			foulCnt[st], 100*float64(foulCnt[st])/float64(totalGames))
	}
	if *numSeeds > 1 {
		fmt.Println("--- 每 seed ---")
		for _, l := range perSeed {
			fmt.Println(l)
		}
	}
	fmt.Println("注: 每 seat 独立摆自己 17 张 (不建模对手可见牌); 配对 OFC 1-6 计分 (行±1 + 横扫±3 + royalty 差).")
}

// classifyFanBonus DEPRECATED (2026-05-19) — cap-down 误算. 用 ofc.FantasyBonusFromBoard 替代.
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

func shuffleDeck(deck []ofc.Card, rng *rand.Rand) {
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
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

func shortName(p string) string {
	// 取文件名
	i := len(p)
	for i > 0 && p[i-1] != '/' {
		i--
	}
	return p[i:]
}
