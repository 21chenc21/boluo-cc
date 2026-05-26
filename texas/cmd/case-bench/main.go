// case-bench — Reference-based case validator.
//
// Methodology (per user 2026-05-24): not OFC-style hardcoded "AA must shove >0.9".
// HUNL Nash is mixed-strategy; hardcoded thresholds inject human bias and may
// reject correct Nash strategies. Instead:
//
//   1. Reference solver = my own MCCFR/CFR at very high iter count
//      (independently validated against OpenSpiel CFR+ — see project_texas_cfr_plus_bug).
//   2. For each curated infoset case, compute reference freq AND candidate freq.
//   3. PASS if max |candidate - reference| < tolerance (default 0.10).
//
// Sub-modes:
//   case-bench leduc <candidate.json>             # uses leduc-vanilla-30k.json as ref
//   case-bench pushfold <stack-bbs> <iters> <ref-iters>   # train both, compare
//
//	go run ./cmd/case-bench leduc blueprints/leduc-vanilla-30k.json
//	go run ./cmd/case-bench pushfold 10 100000 500000
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "leduc":
		runLeduc(os.Args[2:])
	case "pushfold":
		runPushFold(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: case-bench leduc <candidate.json>")
	fmt.Fprintln(os.Stderr, "       case-bench pushfold <stack-bbs> [candidate-iters] [ref-iters]")
	os.Exit(2)
}

// caseResult — per-case comparison outcome.
type caseResult struct {
	id        int
	label     string
	actions   []string
	candidate []float64
	reference []float64
	maxGap    float64
	pass      bool
}

// summarize — print final pass/fail counts.
func summarize(results []caseResult, tolerance float64) {
	fmt.Println()
	fmt.Printf("Tolerance: ±%.2f per action probability\n", tolerance)
	fmt.Println("=================================================================")
	fmt.Printf("%-3s  %-44s  %-7s  %s\n", "ID", "case", "maxgap", "result")
	fmt.Println("-----------------------------------------------------------------")
	passes := 0
	for _, r := range results {
		mark := "✗"
		if r.pass {
			mark = "✓"
			passes++
		}
		fmt.Printf("%-3d  %-44s  %.4f   %s\n", r.id, r.label, r.maxGap, mark)
		if !r.pass {
			// Detail line showing candidate vs reference per action.
			for i, a := range r.actions {
				fmt.Printf("       %-9s  candidate=%.4f  ref=%.4f  Δ=%+.4f\n",
					a, r.candidate[i], r.reference[i], r.candidate[i]-r.reference[i])
			}
		}
	}
	fmt.Println("-----------------------------------------------------------------")
	fmt.Printf("PASS: %d / %d  (%.1f%%)\n", passes, len(results),
		float64(passes)/float64(len(results))*100)
	if passes == len(results) {
		fmt.Println("🎯 all cases pass")
	} else if float64(passes)/float64(len(results)) >= 0.95 {
		fmt.Println("✓ ≥95% pass (per methodology: model basically usable)")
	} else {
		fmt.Println("⚠ <95% pass — model questionable, see failures above")
		os.Exit(1)
	}
}

func maxAbsGap(cand, ref []float64) float64 {
	gap := 0.0
	n := len(cand)
	if len(ref) < n {
		n = len(ref)
	}
	for i := 0; i < n; i++ {
		d := math.Abs(cand[i] - ref[i])
		if d > gap {
			gap = d
		}
	}
	return gap
}

// parseInt — flexible CLI int parsing with default.
func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("expected int, got %q: %v", s, err)
	}
	return v
}
