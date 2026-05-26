// multistreet-smoke — multi-street HUNL MCCFR with abstraction.
//
// Loads preflop + flop + turn + river bucket files, runs MCCFR with
// MultiStreetBuckets.ID as the abstract infoset key on the full 4-street game.
// Reports timing + infoset count + per-street visit distribution.
//
//	go run ./cmd/multistreet-smoke -iters 100000 -stack 20 \
//	    -preflop blueprints/preflop-buckets-K20.json \
//	    -flop blueprints/flop-buckets-K50.json \
//	    -turn blueprints/turn-buckets-K50.json \
//	    -river blueprints/river-buckets-K50.json
package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	iters       = flag.Int("iters", 100000, "MCCFR iterations")
	stackBBs    = flag.Int("stack", 20, "starting stack in BB units")
	seed        = flag.Int64("seed", 42, "RNG seed")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket JSON")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket JSON")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket JSON")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket JSON")
	fallbackMC  = flag.Int("fallback-mc", 0, "MC samples for unseen postflop class (0 = coalesce to bucket 0)")
	betFracs    = flag.String("bet-frac", "1.0", "comma-separated bet sizes as fractions of pot (e.g. \"0.5,1.0,2.0\")")
)

func parseBetSizes(s string) ([]float64, error) {
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("bet size %q: %w", p, err)
		}
		out = append(out, f)
	}
	return out, nil
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	pre, err := abstraction.LoadPreflopBuckets(*preflopPath)
	if err != nil {
		log.Fatalf("load preflop: %v", err)
	}
	flop, err := abstraction.LoadStreetBuckets(*flopPath)
	if err != nil {
		log.Fatalf("load flop: %v", err)
	}
	turn, err := abstraction.LoadStreetBuckets(*turnPath)
	if err != nil {
		log.Fatalf("load turn: %v", err)
	}
	river, err := abstraction.LoadStreetBuckets(*riverPath)
	if err != nil {
		log.Fatalf("load river: %v", err)
	}
	log.Printf("[smoke] buckets: preflop K=%d / flop K=%d (cov %.1f%%) / turn K=%d (cov %.2f%%) / river K=%d (cov %.2f%%)",
		pre.K, flop.K, flop.CoveragePct(), turn.K, turn.CoveragePct(), river.K, river.CoveragePct())

	b := &abstraction.MultiStreetBuckets{
		Preflop:           pre,
		Flop:              flop,
		Turn:              turn,
		River:             river,
		MCSamplesFallback: *fallbackMC,
		FallbackSeed:      *seed,
	}

	betSizes, err := parseBetSizes(*betFracs)
	if err != nil {
		log.Fatalf("parse bet-frac: %v", err)
	}
	cfg := &nlhe.GameConfig{
		SmallBlind:   1,
		BigBlind:     2,
		StartStack:   2 * (*stackBBs),
		BetSizes:     betSizes,
		PushFoldOnly: false,
	}
	log.Printf("[smoke] HUNL multi-street, stack=%dbb, bet-sizes=%v, iters=%d, fallback-mc=%d",
		*stackBBs, betSizes, *iters, *fallbackMC)

	m := nlhe.NewMCCFR(cfg, *seed)
	var visits [4]int64
	m.WithIDFn(func(s *nlhe.State) uint64 {
		visits[s.Street]++
		return b.ID(s)
	})

	t0 := time.Now()
	checkpoints := []int{1000, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000}
	next := 0
	for c := 0; c < *iters; c++ {
		m.Iter()
		if next < len(checkpoints) && m.Iters() == checkpoints[next] {
			log.Printf("[smoke] iter %d  %.1fs  infosets=%d  preflop=%d flop=%d turn=%d river=%d",
				m.Iters(), time.Since(t0).Seconds(), m.NumInfosets(),
				visits[0], visits[1], visits[2], visits[3])
			next++
		}
	}
	elapsed := time.Since(t0)
	log.Printf("[smoke] done in %.1fs, %d infosets, %.0f iter/s",
		elapsed.Seconds(), m.NumInfosets(), float64(*iters)/elapsed.Seconds())

	totalVisits := visits[0] + visits[1] + visits[2] + visits[3]
	fmt.Println()
	fmt.Println("=== Per-street visit distribution ===")
	streetNames := []string{"preflop", "flop", "turn", "river"}
	for i, v := range visits {
		fmt.Printf("  %-7s  %12d  (%.1f%%)\n", streetNames[i], v, 100*float64(v)/float64(totalVisits))
	}
	fmt.Printf("  total    %12d\n", totalVisits)

	// Sample strategy: AA preflop infoset.
	fmt.Println()
	fmt.Println("=== Sample strategy: AA preflop opening ===")
	s := nlhe.NewState(cfg)
	s.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Ah"))
	s.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
	id := b.ID(s)
	avg := m.AverageStrategy()
	if probs, ok := avg[id]; ok {
		legal := s.LegalActions()
		for i, a := range legal {
			fmt.Printf("  %-8s  %.3f\n", a.String(), probs[i])
		}
	} else {
		fmt.Println("  AA infoset not seen during MCCFR")
	}
}
