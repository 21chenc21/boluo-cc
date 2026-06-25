// fantasy-calibrate v2 — 按 v0-dev 真实 fantasy 规则校 bonus
//
// 关键规则 (game.js:1113 getFantasyDealCount):
//   QQ      → 14 张 (弃 1)
//   KK      → 15 张 (弃 2)
//   AA      → 16 张 (弃 3)
//   trips   → 17 张 (弃 4)
//   re-fan  → 17 张 (弃 4)
//
// Placer: ExpertPlaceFantasyBeam(dealt, discardCount, 10)
// (跟 JS solver.js:923 expertPlaceFantasy beamWidth=10 一致)
//
// 递归 bonus 结构:
//   1. 先算 bonus_refan: 17 张 re-fan 场景的 fixed-point
//      bonus_refan = avg_roy(17) + refan_rate(17) × bonus_refan
//   2. 各 trigger 用 (card_count, discardCount) + bonus_refan:
//      bonus[t] = avg_roy(cards[t]) + refan_rate(cards[t]) × bonus_refan
//
// 用法: ./fantasy-calibrate -n 5000
package main

import (
	"flag"
	"fmt"
	"math/rand"

	"github.com/boluo/v0-server/ofc"
)

type stats struct {
	name       string
	nSamples   int
	nFoul      int
	nRefan     int
	sumRoyalty int
}

func (s *stats) avgRoyalty() float64 {
	nValid := s.nSamples - s.nFoul
	if nValid == 0 {
		return 0
	}
	return float64(s.sumRoyalty) / float64(nValid)
}

func (s *stats) refanRate() float64 {
	nValid := s.nSamples - s.nFoul
	if nValid == 0 {
		return 0
	}
	return float64(s.nRefan) / float64(nValid)
}

func (s *stats) foulRate() float64 {
	if s.nSamples == 0 {
		return 0
	}
	return float64(s.nFoul) / float64(s.nSamples)
}

func removeCard(deck []ofc.Card, rank, suit uint8) []ofc.Card {
	target := ofc.MakeCard(rank, suit)
	out := make([]ofc.Card, 0, len(deck))
	for _, c := range deck {
		if c == target {
			continue
		}
		out = append(out, c)
	}
	return out
}

func prepareDeck(jokers int, removeRanks []uint8) []ofc.Card {
	deck := ofc.MakeDeck(jokers)
	for _, r := range removeRanks {
		for s := uint8(0); s < 4; s++ {
			before := len(deck)
			deck = removeCard(deck, r, s)
			if len(deck) < before {
				break
			}
		}
	}
	return deck
}

func placeWithRecover(dealt []ofc.Card, discardCount, beamWidth int) (r *ofc.FantasyResult, panicked bool) {
	defer func() {
		if rec := recover(); rec != nil {
			panicked = true
		}
	}()
	_ = beamWidth
	r = ofc.ExpertPlaceFantasy(dealt, discardCount)
	return
}

func simulate(label string, baseDeck []ofc.Card, cardCount, discardCount, beamWidth, n int, rng *rand.Rand) *stats {
	s := &stats{name: label, nSamples: n}
	for i := 0; i < n; i++ {
		deck := make([]ofc.Card, len(baseDeck))
		copy(deck, baseDeck)
		for j := len(deck) - 1; j > 0; j-- {
			k := rng.Intn(j + 1)
			deck[j], deck[k] = deck[k], deck[j]
		}
		dealt := deck[:cardCount]
		r, panicked := placeWithRecover(dealt, discardCount, beamWidth)
		if panicked || r == nil || r.Sc.Foul {
			s.nFoul++
			continue
		}
		s.sumRoyalty += r.Royalty
		if r.Sc.TopEval.Type >= ofc.TypeThreeOfAKind || r.Sc.BotEval.Type >= ofc.TypeFourOfAKind {
			s.nRefan++
		}
	}
	return s
}

func fixedPoint(avgRoy, refanRate float64, iters int) float64 {
	bonus := 0.0
	for i := 0; i < iters; i++ {
		bonus = avgRoy + refanRate*bonus
	}
	return bonus
}

func main() {
	n := flag.Int("n", 5000, "samples per scenario")
	jokers := flag.Int("jokers", 2, "deck jokers")
	beam := flag.Int("beam", 10, "ExpertPlaceFantasyBeam beam width")
	iters := flag.Int("iters", 15, "fixed-point iterations")
	flag.Parse()

	rng := rand.New(rand.NewSource(42))

	fmt.Printf("=== Fantasy Calibration v2 ===\n")
	fmt.Printf("samples: %d/scenario, jokers: %d, beam: %d, FP iters: %d\n\n", *n, *jokers, *beam, *iters)

	// Re-fan 牌数 = 当初 trigger 牌数 (不是统一 17). 每 trigger 自 fixed-point.
	scenarios := []struct {
		label        string
		removed      []uint8
		cardCount    int
		discardCount int
	}{
		{"QQ trigger", []uint8{ofc.RankQ, ofc.RankQ}, 14, 1},
		{"KK trigger", []uint8{ofc.RankK, ofc.RankK}, 15, 2},
		{"AA trigger", []uint8{ofc.RankA, ofc.RankA}, 16, 3},
		{"Trips Q", []uint8{ofc.RankQ, ofc.RankQ, ofc.RankQ}, 17, 4},
		{"Trips K", []uint8{ofc.RankK, ofc.RankK, ofc.RankK}, 17, 4},
		{"Trips A", []uint8{ofc.RankA, ofc.RankA, ofc.RankA}, 17, 4},
		{"Trips 2", []uint8{ofc.Rank2, ofc.Rank2, ofc.Rank2}, 17, 4},
	}

	fmt.Printf("%-15s %5s %4s %9s %9s %8s %10s\n", "trigger", "cards", "disc", "avg_roy", "refan%", "foul%", "bonus")
	fmt.Println("---------------------------------------------------------------------------")

	for _, sc := range scenarios {
		deck := prepareDeck(*jokers, sc.removed)
		s := simulate(sc.label, deck, sc.cardCount, sc.discardCount, *beam, *n, rng)
		// bonus = avg_roy + refan_rate × bonus (递归同 trigger, fixed-point)
		bonus := fixedPoint(s.avgRoyalty(), s.refanRate(), *iters)
		fmt.Printf("%-15s %5d %4d %9.2f %8.2f%% %7.2f%% %10.2f\n",
			s.name, sc.cardCount, sc.discardCount,
			s.avgRoyalty(), s.refanRate()*100, s.foulRate()*100, bonus)
	}
	fmt.Println()
	fmt.Println("注: bonus = avg_roy + refan_rate × bonus (re-fan 同 trigger 牌数 递归 fixed-point)")
}
