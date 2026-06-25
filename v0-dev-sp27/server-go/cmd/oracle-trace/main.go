// oracle-trace — 人类可读的 oracle decision trace, 用于 dataset quality 验证.
// 跟 gen-oracle-dataset 同 logic, 但 stdout 打印每个 round 的:
//   - state (top/mid/bot)
//   - dealt cards
//   - top-N candidates with oracle scores
//   - oracle's choice (highlighted)
// 不写 JSONL, 只打印.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	numGames    = flag.Int("num-games", 3, "games to trace")
	seed        = flag.Int64("seed", 0, "RNG seed (0=time-based)")
	jokers      = flag.Int("jokers", 2, "joker count")
	r1MultiK    = flag.Int("r1-multi-k", 4, "R1 multi-future K")
	phantomOpp  = flag.Int("phantom-opponents", 2, "max opponents (random per game)")
	topN        = flag.Int("top-n", 5, "show top-N candidates per decision")

	foulCost      = flag.Float64("foul-cost", 6, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 80, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 90, "")
)

func main() {
	flag.Parse()
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

	fmt.Printf("=== oracle-trace: %d games, seed=%d ===\n", *numGames, *seed)
	fmt.Printf("knobs: foul=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f\n",
		cfg.FoulCost, cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
	fmt.Println()

	for g := 0; g < *numGames; g++ {
		fmt.Printf("\n══════════════════════════════════════════════════════════════\n")
		fmt.Printf(" GAME %d\n", g+1)
		fmt.Printf("══════════════════════════════════════════════════════════════\n")
		traceOneGame(rng, &cfg)
	}
}

func traceOneGame(rng *rand.Rand, cfg *ofc.RolloutConfig) {
	state := ofc.NewGameState(*jokers)
	deck := ofc.MakeDeck(*jokers)
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}

	// Phantom
	opponents := 0
	slot := 0
	if *phantomOpp > 0 {
		opponents = rng.Intn(*phantomOpp + 1)
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
	futureTailPool := deck[5:phantomReserveStart]

	fmt.Printf("Phantom: opponents=%d slot=%d (R5 phantom=%d)\n", opponents, slot, maxPhantom)
	fmt.Printf("My 17 cards:\n")
	fmt.Printf("  R1 dealt: %s\n", cardsStr(myCards[0:5]))
	fmt.Printf("  R2 dealt: %s\n", cardsStr(myCards[5:8]))
	fmt.Printf("  R3 dealt: %s\n", cardsStr(myCards[8:11]))
	fmt.Printf("  R4 dealt: %s\n", cardsStr(myCards[11:14]))
	fmt.Printf("  R5 dealt: %s\n", cardsStr(myCards[14:17]))
	if maxPhantom > 0 {
		fmt.Printf("Phantom (objections cards in deck): %s\n", cardsStr(deck[phantomReserveStart:]))
	}

	phantomAdded := 0

	for round := 1; round <= 5; round++ {
		state.Round = round

		// Inject phantom
		want := phantomCountFor(round, slot, opponents)
		if want > maxPhantom {
			want = maxPhantom
		}
		for phantomAdded < want {
			state.UsedCards[deck[phantomReserveStart+phantomAdded].ID()] = true
			phantomAdded++
		}

		fmt.Printf("\n────── Round %d ──────\n", round)
		fmt.Printf("State:  top=%-12s mid=%-20s bot=%-20s\n",
			cardsStr(state.Top), cardsStr(state.Middle), cardsStr(state.Bottom))
		curPhantom := want
		if curPhantom > 0 {
			fmt.Printf("Phantom in usedCards now: %d cards\n", curPhantom)
		}

		var dealt []ofc.Card
		if round == 1 {
			dealt = myCards[0:5]
		} else {
			start := 5 + (round-2)*3
			dealt = myCards[start : start+3]
		}
		fmt.Printf("Dealt:  %s\n", cardsStr(dealt))

		var bestAction *actionInfo
		bestScore := float32(-1e9)

		type candResult struct {
			desc      string
			score     float32
			action    *actionInfo
		}
		results := []candResult{}

		if round == 1 {
			// R1 K-future
			placements := ofc.GenerateRound1Actions(dealt, state)
			seen := map[string]bool{}
			for _, p := range placements {
				tmp := state.Clone()
				for i, c := range dealt {
					tmp.PlaceCard(c, p[i])
				}
				k := stateKey(tmp)
				if seen[k] {
					continue
				}
				seen[k] = true
				pCopy := make([]ofc.Row, len(p))
				copy(pCopy, p)

				var sumScore float32
				K := *r1MultiK
				for kk := 0; kk < K; kk++ {
					tail := sampleTail(futureTailPool, 12, rng)
					futureRounds := [][]ofc.Card{tail[0:3], tail[3:6], tail[6:9], tail[9:12]}
					sumScore += ofc.OracleSolve(tmp, futureRounds, cfg)
				}
				avg := sumScore / float32(K)

				desc := describeR1Placement(dealt, pCopy)
				results = append(results, candResult{
					desc: desc, score: avg,
					action: &actionInfo{round: round, placement: pCopy},
				})
				if avg > bestScore {
					bestScore = avg
					bestAction = &actionInfo{round: round, placement: pCopy}
				}
			}
		} else {
			// R2-R5 single-future
			actions := ofc.GenerateRoundNActions(dealt, state)
			seen := map[string]bool{}
			var futureRounds [][]ofc.Card
			for r2 := round + 1; r2 <= 5; r2++ {
				start := 5 + (r2-2)*3
				futureRounds = append(futureRounds, myCards[start:start+3])
			}
			for i := range actions {
				a := &actions[i]
				tmp := state.Clone()
				tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
				for k, c := range a.Kept {
					tmp.PlaceCard(c, a.Placement[k])
				}
				k := dealt[a.DiscardIdx].ID() + "|" + stateKey(tmp)
				if seen[k] {
					continue
				}
				seen[k] = true
				keptCopy := make([]ofc.Card, len(a.Kept))
				copy(keptCopy, a.Kept)
				placeCopy := make([]ofc.Row, len(a.Placement))
				copy(placeCopy, a.Placement)
				score := ofc.OracleSolve(tmp, futureRounds, cfg)

				desc := describeRNAction(dealt[a.DiscardIdx], keptCopy, placeCopy)
				results = append(results, candResult{
					desc: desc, score: score,
					action: &actionInfo{
						round: round, placement: placeCopy,
						discardCard: dealt[a.DiscardIdx], kept: keptCopy,
					},
				})
				if score > bestScore {
					bestScore = score
					bestAction = &actionInfo{
						round: round, placement: placeCopy,
						discardCard: dealt[a.DiscardIdx], kept: keptCopy,
					}
				}
			}
		}

		// Sort + print top N
		sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
		fmt.Printf("Total %d unique candidates. Top %d by oracle score:\n", len(results), *topN)
		for i, r := range results {
			if i >= *topN {
				break
			}
			marker := "  "
			if i == 0 {
				marker = "★ "
			}
			fmt.Printf("  %s%6.2f  %s\n", marker, r.score, r.desc)
		}

		// Apply best
		if bestAction == nil {
			fmt.Println("(no valid action, breaking)")
			break
		}
		if round == 1 {
			for i, c := range dealt {
				state.PlaceCard(c, bestAction.placement[i])
			}
		} else {
			state.UsedCards[bestAction.discardCard.ID()] = true
			for k, c := range bestAction.kept {
				state.PlaceCard(c, bestAction.placement[k])
			}
		}
	}

	// Final state
	fmt.Printf("\n────── Final ──────\n")
	fmt.Printf("Final: top=%-12s mid=%-20s bot=%-20s\n",
		cardsStr(state.Top), cardsStr(state.Middle), cardsStr(state.Bottom))
	if state.IsComplete() {
		score := state.Score()
		fmt.Printf("Score: %d royalty (top=%d mid=%d bot=%d), foul=%v, fantasy=%v\n",
			score.Royalties, score.TopRoyalty, score.MidRoyalty, score.BotRoyalty,
			score.Foul, score.Fantasy)
	} else {
		fmt.Println("Game incomplete")
	}
}

type actionInfo struct {
	round       int
	placement   []ofc.Row
	discardCard ofc.Card
	kept        []ofc.Card
}

func describeR1Placement(dealt []ofc.Card, placement []ofc.Row) string {
	var top, mid, bot []ofc.Card
	for i, c := range dealt {
		switch placement[i] {
		case ofc.RowTop:
			top = append(top, c)
		case ofc.RowMiddle:
			mid = append(mid, c)
		default:
			bot = append(bot, c)
		}
	}
	return fmt.Sprintf("top=%s mid=%s bot=%s",
		cardsStr(top), cardsStr(mid), cardsStr(bot))
}

func describeRNAction(discard ofc.Card, kept []ofc.Card, placement []ofc.Row) string {
	var parts []string
	for i, c := range kept {
		parts = append(parts, fmt.Sprintf("%s→%s", c.String(), rowStr(placement[i])))
	}
	return fmt.Sprintf("discard %s, %s", discard.String(), strings.Join(parts, " "))
}

func rowStr(r ofc.Row) string {
	switch r {
	case ofc.RowTop:
		return "top"
	case ofc.RowMiddle:
		return "mid"
	default:
		return "bot"
	}
}

func cardsStr(cards []ofc.Card) string {
	if len(cards) == 0 {
		return "[]"
	}
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = c.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func sampleTail(pool []ofc.Card, n int, rng *rand.Rand) []ofc.Card {
	if n > len(pool) {
		n = len(pool)
	}
	cp := make([]ofc.Card, len(pool))
	copy(cp, pool)
	for i := 0; i < n; i++ {
		j := i + rng.Intn(len(cp)-i)
		cp[i], cp[j] = cp[j], cp[i]
	}
	return cp[:n]
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
