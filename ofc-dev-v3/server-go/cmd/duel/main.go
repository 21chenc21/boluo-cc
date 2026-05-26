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
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	ckpt1     = flag.String("ckpt1", "", "ckpt path A (required)")
	ckpt2     = flag.String("ckpt2", "", "ckpt path B (required)")
	numGames  = flag.Int("games", 100, "duel games")
	mctsSims  = flag.Int("mcts-sims", 200, "MCTS sims per decision (0=use ExpertPlace, no MCTS)")
	jokers    = flag.Int("jokers", 2, "deck joker count")
	phantomOpp = flag.Int("phantom-opponents", 2, "max phantom opponents (0..N uniform per game)")
	seed      = flag.Int64("seed", 0, "RNG seed (0=time)")
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

	log.Printf("[duel] %s vs %s — %d games, MCTS sims=%d, jokers=%d, phantom-opp-max=%d",
		shortName(*ckpt1), shortName(*ckpt2), *numGames, *mctsSims, *jokers, *phantomOpp)

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
	scores1 := runAllGames(games, &cfg, *mctsSims, rng)

	// Run ckpt2
	log.Printf("[duel] running %s on %d games...", shortName(*ckpt2), *numGames)
	if err := ofc.LoadWeightsFromFile(*ckpt2); err != nil {
		log.Fatalf("load ckpt2: %v", err)
	}
	scores2 := runAllGames(games, &cfg, *mctsSims, rng)

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

func runAllGames(games []gameSetup, cfg *ofc.RolloutConfig, sims int, rng *rand.Rand) []float32 {
	scores := make([]float32, len(games))
	for i, g := range games {
		scores[i] = playOneGame(g.deck, g.opponents, g.slot, cfg, sims, rng)
	}
	return scores
}

func playOneGame(deck []ofc.Card, opponents, slot int, cfg *ofc.RolloutConfig, mctsSims int, rng *rand.Rand) float32 {
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
		return -cfg.FoulCost
	}
	score := state.Score()
	if score.Foul {
		return -cfg.FoulCost
	}
	raw := float32(score.Royalties)
	if score.Fantasy {
		// 2026-05-19: cap-chain aware fan bonus.
		fb, _ := ofc.FantasyBonusFromBoard(state.Top, state.Middle, state.Bottom,
			cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
		raw += fb
	}
	return raw
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
