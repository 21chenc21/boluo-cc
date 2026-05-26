// bench-3metric — 2-ckpt duel bench (fantasy / score / foul), 同 hand 公平对比.
//
// 用法:
//   ./bench-3metric -new <new.json> -best <best.json> -games 200
//   exit 0 = new 上 (PROMOTE), 1 = new 下 (DISCARD)
//
// 输出 (stdout, bash 可 parse):
//   NEW_FAN=N
//   BEST_FAN=N
//   NEW_SCORE=N.NN
//   BEST_SCORE=N.NN
//   NEW_FOUL=N
//   BEST_FOUL=N
//   GAMES=N
//
// 跟 alphazero-train 的 duelTwo 同逻辑, 但用 ExpertPlace5/3 (生产 inference path),
// 不走 MCTSSearch. 跟 DISABLE_MCTS=1 server inference 等价.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"

	"github.com/boluo/v0-server/ofc"
)

type gameResult struct {
	score   float32
	fantasy bool
	foul    bool
}

func main() {
	// Default: disable MCTS rollout (bench production-like 纯 MLP inference).
	// ENABLE_MCTS=1 可显式打开 rollout (调试用), 但慢 50-100×.
	ofc.MctsDisabled = true
	if os.Getenv("ENABLE_MCTS") == "1" {
		ofc.MctsDisabled = false
	}

	newCkpt := flag.String("new", "", "new ckpt path")
	bestCkpt := flag.String("best", "", "best ckpt path (current) — 用 same hand 对比")
	topkNew := flag.Int("topk-new", 0, "new 端 R1 top-K sample (0=top-1 deterministic, 2=top-2 随机)")
	topkBest := flag.Int("topk-best", 0, "best 端 R1 top-K sample")
	topkNewRN := flag.Int("topk-new-rn", 0, "new 端 R2-R5 top-K sample (0=top-1)")
	topkBestRN := flag.Int("topk-best-rn", 0, "best 端 R2-R5 top-K sample")
	numGames := flag.Int("games", 200, "number of duel games")
	seed := flag.Int64("seed", 42, "rng seed")
	jokers := flag.Int("jokers", 2, "deck jokers")
	phantomOpp := flag.Int("phantom-opponents", 2, "max phantom opponents 0-2")
	workers := flag.Int("workers", 0, "parallel workers (0=NumCPU)")
	foulCost := flag.Float64("foul-cost", 6, "foul penalty (label)")
	fanQQ := flag.Float64("fan-bonus-qq", 20, "")
	fanKK := flag.Float64("fan-bonus-kk", 40, "")
	fanAA := flag.Float64("fan-bonus-aa", 80, "")
	fanTrips := flag.Float64("fan-bonus-trips", 90, "")
	flag.Parse()

	if *newCkpt == "" || *bestCkpt == "" {
		log.Fatalf("usage: -new <ckpt> -best <ckpt> -games N")
	}

	cfg := ofc.RolloutConfig{
		FoulCost:      float32(*foulCost),
		QQFanBonus:    float32(*fanQQ),
		KKFanBonus:    float32(*fanKK),
		AAFanBonus:    float32(*fanAA),
		TripsFanBonus: float32(*fanTrips),
	}

	rng := rand.New(rand.NewSource(*seed))

	// Pre-gen N decks + opp/slot configs (same for both ckpts)
	type gconf struct {
		deck []ofc.Card
		opp  int
		slot int
	}
	games := make([]gconf, *numGames)
	for i := 0; i < *numGames; i++ {
		deck := ofc.MakeDeck(*jokers)
		for j := len(deck) - 1; j > 0; j-- {
			k := rng.Intn(j + 1)
			deck[j], deck[k] = deck[k], deck[j]
		}
		opp := 0
		slot := 0
		if *phantomOpp > 0 {
			opp = rng.Intn(*phantomOpp + 1)
			if opp > 0 {
				slot = rng.Intn(opp + 1)
			}
		}
		games[i] = gconf{deck, opp, slot}
	}

	runOne := func(ckpt string, topkR1, topkRN int) []gameResult {
		if err := ofc.LoadWeightsFromFile(ckpt); err != nil {
			log.Fatalf("bench: load %s: %v", ckpt, err)
		}
		ofc.MctsTopKSample = topkR1
		ofc.MctsTopKSampleRN = topkRN
		results := make([]gameResult, *numGames)
		if *workers <= 1 {
			for i, g := range games {
				results[i] = playOne(g.deck, g.opp, g.slot, &cfg, *jokers, rand.New(rand.NewSource(int64(i)+1)))
			}
			return results
		}
		jobs := make(chan int, *numGames)
		for i := 0; i < *numGames; i++ {
			jobs <- i
		}
		close(jobs)
		var wg sync.WaitGroup
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			wseed := rng.Int63() ^ int64(uint64(w)*0x9E3779B97F4A7C15)
			go func(s int64) {
				defer wg.Done()
				wrng := rand.New(rand.NewSource(s))
				for idx := range jobs {
					g := games[idx]
					results[idx] = playOne(g.deck, g.opp, g.slot, &cfg, *jokers, wrng)
				}
			}(wseed)
		}
		wg.Wait()
		return results
	}

	newResults := runOne(*newCkpt, *topkNew, *topkNewRN)
	bestResults := runOne(*bestCkpt, *topkBest, *topkBestRN)

	newFan, bestFan := 0, 0
	newFoul, bestFoul := 0, 0
	var newScore, bestScore float32
	for _, r := range newResults {
		if r.fantasy {
			newFan++
		}
		if r.foul {
			newFoul++
		}
		newScore += r.score
	}
	for _, r := range bestResults {
		if r.fantasy {
			bestFan++
		}
		if r.foul {
			bestFoul++
		}
		bestScore += r.score
	}

	fmt.Printf("NEW_FAN=%d\n", newFan)
	fmt.Printf("BEST_FAN=%d\n", bestFan)
	fmt.Printf("NEW_SCORE=%.2f\n", newScore)
	fmt.Printf("BEST_SCORE=%.2f\n", bestScore)
	fmt.Printf("NEW_FOUL=%d\n", newFoul)
	fmt.Printf("BEST_FOUL=%d\n", bestFoul)
	fmt.Printf("GAMES=%d\n", *numGames)

	// 始终 exit 0. 脚本通过 stdout (NEW_FAN/SCORE/FOUL) 决定 PROMOTE.
	// 2026-05-19 fix: 旧版 exit 1 在 new<best 时触发 set -e 杀脚本, 直接终止 iter loop.
	_ = os.Args // silence unused if Args
}

// playOne — 单场 game 模拟: 5 rounds, 每轮 ExpertPlace5/3 决策.
func playOne(deck []ofc.Card, opp, slot int, cfg *ofc.RolloutConfig, jokers int, rng *rand.Rand) gameResult {
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
			er.ExpertPlace5(state, dealt)
		} else {
			start := 5 + (round-2)*3
			dealt = myCards[start : start+3]
			er.ExpertPlace3(state, dealt)
		}
	}

	if !state.IsComplete() {
		return gameResult{score: -cfg.FoulCost, foul: true}
	}
	score := state.Score()
	if score.Foul {
		return gameResult{score: -cfg.FoulCost, foul: true}
	}
	raw := float32(score.Royalties)
	if score.Fantasy {
		// cap-chain aware fan bonus (跟 game.js 2026-05-18 修复一致, 避免 cap-down 误算)
		fb, _ := ofc.FantasyBonusFromBoard(state.Top, state.Middle, state.Bottom,
			cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
		raw += fb
		return gameResult{score: raw, fantasy: true}
	}
	return gameResult{score: raw}
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

// 注: 弃旧 classifyFanBonus (cap 错). 直接调 ofc.FantasyBonusFromBoard.
