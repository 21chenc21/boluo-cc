// compare-blueprints — Week 2 POC validator.
//
// Loads two BlueprintFile-format JSON files (tabular vs distilled-NN), computes:
//   - Exploitability for each (should both be small)
//   - Per-infoset KL divergence (POC threshold: < 0.05)
//   - Game-value gap (POC threshold: < 5% relative)
//
// Run:
//
//	go run ./cmd/compare-blueprints \
//	    -tabular blueprints/leduc-vanilla-30k.json \
//	    -nn distill/models/leduc-nn-strategy.json
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/engine/leduc"
)

var (
	tabularPath = flag.String("tabular", "blueprints/leduc-vanilla-30k.json", "tabular blueprint JSON")
	nnPath      = flag.String("nn", "distill/models/leduc-nn-strategy.json", "NN-distilled strategy JSON (same format)")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	tab, _, err := cfr.LoadBlueprint(*tabularPath)
	if err != nil {
		log.Fatalf("load tabular: %v", err)
	}
	nn, _, err := cfr.LoadBlueprint(*nnPath)
	if err != nil {
		log.Fatalf("load nn: %v", err)
	}
	log.Printf("[cmp] tabular: %d infosets   nn: %d infosets", len(tab), len(nn))

	// Coverage check.
	if len(tab) != 288 || len(nn) != 288 {
		log.Fatalf("expected 288 infosets each; got tab=%d nn=%d", len(tab), len(nn))
	}

	// 1. Exploitability.
	tabExpl := cfr.Exploitability(tab)
	nnExpl := cfr.Exploitability(nn)
	tabGv := cfr.GameValue(tab, leduc.P0)
	nnGv := cfr.GameValue(nn, leduc.P0)

	fmt.Println()
	fmt.Println("============================================")
	fmt.Println("Week 2 POC — distillation validation report")
	fmt.Println("============================================")
	fmt.Printf("Tabular  expl=%.6f  gv(P0)=%+.6f\n", tabExpl, tabGv)
	fmt.Printf("NN       expl=%.6f  gv(P0)=%+.6f\n", nnExpl, nnGv)
	fmt.Println()

	// 2. KL divergence per-infoset.
	var maxKL, sumKL float64
	var nKL int
	worstKL := struct {
		id   uint64
		kl   float64
		tab  []float64
		nn   []float64
	}{}
	for id, tProbs := range tab {
		nProbs, ok := nn[id]
		if !ok {
			log.Fatalf("nn missing infoset %d", id)
		}
		if len(tProbs) != len(nProbs) {
			log.Fatalf("infoset %d: tab len=%d nn len=%d mismatch", id, len(tProbs), len(nProbs))
		}
		kl := klDiv(tProbs, nProbs)
		sumKL += kl
		nKL++
		if kl > maxKL {
			maxKL = kl
			worstKL.id = id
			worstKL.kl = kl
			worstKL.tab = tProbs
			worstKL.nn = nProbs
		}
	}
	avgKL := sumKL / float64(nKL)
	fmt.Printf("KL(tabular || nn): avg=%.6f  max=%.6f  (POC threshold avg < 0.05)\n", avgKL, maxKL)
	fmt.Printf("  worst infoset id=%d  tab=%v  nn=%v\n", worstKL.id, worstKL.tab, worstKL.nn)
	fmt.Println()

	// 3. Game-value gap.
	gvGap := math.Abs(nnGv - tabGv)
	relGap := gvGap / math.Abs(tabGv)
	fmt.Printf("game-value gap |nn-tab| = %.6f  (relative %.2f%%, POC threshold < 5%%)\n", gvGap, relGap*100)
	fmt.Println()

	// Summary table.
	fmt.Println("POC criteria check:")
	criteria := []struct {
		name    string
		val     float64
		thresh  float64
		op      string
		passing bool
	}{
		{"#1  tabular expl < 0.01", tabExpl, 0.01, "<", tabExpl < 0.01},
		{"#2  KL avg < 0.05", avgKL, 0.05, "<", avgKL < 0.05},
		{"#3  EV gap < 5%", relGap * 100, 5.0, "<", relGap*100 < 5.0},
		{"    NN expl reasonable", nnExpl, 0.05, "<", nnExpl < 0.05},
	}
	allPass := true
	for _, c := range criteria {
		mark := "✓"
		if !c.passing {
			mark = "✗"
			allPass = false
		}
		fmt.Printf("  %s %-30s  val=%.6f  thresh%s%.6f\n", mark, c.name, c.val, c.op, c.thresh)
	}
	fmt.Println()
	if allPass {
		fmt.Println("🎯 POC PASS — proceed to 6-max NLHE engine.")
	} else {
		fmt.Println("⚠  POC FAIL — see distill/README.md failure-recovery section.")
		os.Exit(1)
	}
}

// klDiv computes KL(p || q). Handles 0 in p (skip), small q (avoid log(0)).
func klDiv(p, q []float64) float64 {
	var sum float64
	for i := range p {
		if p[i] <= 0 {
			continue
		}
		qi := q[i]
		if qi < 1e-12 {
			qi = 1e-12
		}
		sum += p[i] * math.Log(p[i]/qi)
	}
	return sum
}
