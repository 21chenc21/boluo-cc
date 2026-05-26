// pushfold-smoke — HUNL push/fold CFR smoke test.
//
// Runs NLHE MCCFR in push/fold-only mode at configurable stack depth, then
// reports the learned SB shove range and BB call range against known Nash
// approximations. Validates the full pipeline:
//
//	NLHE State + Snapshot/Restore + InfosetID + MCCFR + showdown
//
//	go run ./cmd/pushfold-smoke -iters 200000 -stack 10
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"time"

	"github.com/boluo/texas/engine/nlhe"
)

var (
	iters     = flag.Int("iters", 100000, "MCCFR iterations")
	stackBBs  = flag.Int("stack", 10, "starting stack in BB units")
	seed      = flag.Int64("seed", 42, "RNG seed")
	verbose   = flag.Bool("v", false, "print every-checkpoint progress")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	cfg := nlhe.PushFoldConfig(*stackBBs)
	m := nlhe.NewMCCFR(cfg, *seed)
	log.Printf("[smoke] HUNL push/fold, stack=%dbb, iters=%d", *stackBBs, *iters)

	t0 := time.Now()
	checkpoints := []int{1000, 10000, 50000, 100000, 200000, 500000, 1000000}
	next := 0
	for c := 0; c < *iters; c++ {
		m.Iter()
		if next < len(checkpoints) && m.Iters() == checkpoints[next] {
			log.Printf("[smoke] iter %d  %.1fs  infosets=%d",
				m.Iters(), time.Since(t0).Seconds(), m.NumInfosets())
			next++
		}
	}
	log.Printf("[smoke] done in %.1fs, %d infosets touched", time.Since(t0).Seconds(), m.NumInfosets())

	// Compute SB shove range and BB call range by replaying every starting hand.
	avg := m.AverageStrategy()
	reportRanges(cfg, *seed, avg)
}

// reportRanges — for each starting hole-card pair, simulate the engine's
// initial state and look up the average strategy at the SB opening infoset.
// Then sample BB perspective after SB shoves.
//
// Aggregates by canonical 169 hand types (e.g. AKs / AKo / AA / 22).
func reportRanges(cfg *nlhe.GameConfig, seed int64, avg map[uint64][]float64) {
	type handStats struct {
		label    string
		sbShove  float64 // P0 P(AllIn) at opening
		bbCall   float64 // P1 P(CheckCall) when facing shove
		nSeen    int
	}
	canonical := make(map[string]*handStats)
	rng := rand.New(rand.NewSource(seed))

	// Enumerate ALL 1326 starting hands for the actor's perspective by
	// constructing states with each (c1, c2) hole. Opp hole sampled to avoid
	// hole conflict (only 50 valid opp cards for a given P0 hole; we just need
	// a representative state for ID lookup).
	for c1 := 0; c1 < nlhe.DeckSize; c1++ {
		for c2 := c1 + 1; c2 < nlhe.DeckSize; c2++ {
			h1 := nlhe.Card(c1)
			h2 := nlhe.Card(c2)
			label := canonHand(h1, h2)
			st := canonical[label]
			if st == nil {
				st = &handStats{label: label}
				canonical[label] = st
			}

			// SB shove range — state at opening with SB = (h1,h2), arbitrary BB.
			oppA, oppB := pickOpp(rng, h1, h2)
			s := nlhe.NewState(cfg)
			s.SetHole(nlhe.P0, h1, h2)
			s.SetHole(nlhe.P1, oppA, oppB)
			id := s.InfosetID()
			probs, ok := avg[id]
			if ok {
				legal := s.LegalActions()
				for i, a := range legal {
					if a.Kind == nlhe.ActionAllIn {
						st.sbShove += probs[i]
						break
					}
				}
				st.nSeen++
			}

			// BB call range — state after SB shoves, BB has (h1,h2).
			oppA, oppB = pickOpp(rng, h1, h2)
			s2 := nlhe.NewState(cfg)
			s2.SetHole(nlhe.P0, oppA, oppB)
			s2.SetHole(nlhe.P1, h1, h2)
			s2.Apply(nlhe.Action{Kind: nlhe.ActionAllIn})
			id2 := s2.InfosetID()
			probs2, ok := avg[id2]
			if ok {
				legal := s2.LegalActions()
				for i, a := range legal {
					if a.Kind == nlhe.ActionCheckCall {
						st.bbCall += probs2[i]
						break
					}
				}
			}
		}
	}

	// Average and print.
	var rows []*handStats
	for _, st := range canonical {
		if st.nSeen > 0 {
			st.sbShove /= float64(st.nSeen)
			st.bbCall /= float64(st.nSeen)
		}
		rows = append(rows, st)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].sbShove > rows[j].sbShove
	})

	fmt.Println()
	fmt.Printf("=== Learned SB shove + BB call frequencies (canonical 169 hand types) ===\n")
	fmt.Printf("%-6s  %10s  %10s\n", "hand", "SB_shove", "BB_call")
	for _, r := range rows {
		fmt.Printf("%-6s  %10.3f  %10.3f\n", r.label, r.sbShove, r.bbCall)
	}

	// Sklansky-Chubukov / Nash push-fold reference for 10bb:
	// (rough — exact tables vary by source)
	//   SB shove: ~ 60% of 169 types (AA-22, AKo-A2o, AKs-A2s, KQs-T9s, KQo-KJo, ...)
	//   BB call:  ~ 20-30% of 169 types (AA-22, AKs-AJs, AKo-AQo, KQs-...)
	fmt.Println()
	var sbWideCount, bbCallCount int
	for _, r := range rows {
		if r.sbShove > 0.5 {
			sbWideCount++
		}
		if r.bbCall > 0.5 {
			bbCallCount++
		}
	}
	fmt.Printf("Summary (threshold 50%%): SB shoves %d/169 hand types, BB calls %d/169\n",
		sbWideCount, bbCallCount)
	fmt.Println("Known Nash @ 10bb (approx): SB ~95/169 (very wide), BB ~50/169 (tighter)")
	fmt.Println("→ engine + MCCFR working if numbers in same ballpark")
}

func canonHand(a, b nlhe.Card) string {
	r1, r2 := a.Rank(), b.Rank()
	if r1 < r2 {
		r1, r2 = r2, r1
	}
	const ranks = "23456789TJQKA"
	if a.Rank() == b.Rank() {
		return string([]byte{ranks[r1], ranks[r1]})
	}
	suited := a.Suit() == b.Suit()
	suffix := "o"
	if suited {
		suffix = "s"
	}
	return string([]byte{ranks[r1], ranks[r2]}) + suffix
}

// pickOpp — pick 2 random cards distinct from {h1, h2}.
func pickOpp(rng *rand.Rand, h1, h2 nlhe.Card) (nlhe.Card, nlhe.Card) {
	var pick [2]nlhe.Card
	for i := 0; i < 2; i++ {
		for {
			c := nlhe.Card(rng.Intn(nlhe.DeckSize))
			if c != h1 && c != h2 && (i == 0 || c != pick[0]) {
				pick[i] = c
				break
			}
		}
	}
	return pick[0], pick[1]
}
