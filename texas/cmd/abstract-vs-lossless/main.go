// abstract-vs-lossless — compare card-abstraction MCCFR vs lossless MCCFR on
// HUNL push/fold. Measures:
//
//  1. Infoset count: K*2 buckets vs 2652 lossless
//  2. Convergence speed: case-bench pass rate at varying iter counts
//  3. Final quality: case-bench pass at high iter
//
//	go run ./cmd/abstract-vs-lossless -k-buckets 20 -iters 100000 \
//	    -buckets blueprints/preflop-buckets-K20.json \
//	    -tabular blueprints/hunl-pushfold-10bb.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	bucketsPath = flag.String("buckets", "blueprints/preflop-buckets-K20.json", "preflop bucket file")
	tabularPath = flag.String("tabular", "blueprints/hunl-pushfold-10bb.json", "tabular blueprint (3M iter ref)")
	iters       = flag.Int("iters", 100000, "abstract MCCFR iterations")
	stackBBs    = flag.Int("stack", 10, "stack BB")
	seed        = flag.Int64("seed", 42, "RNG seed")
	tol         = flag.Float64("tol", 0.10, "case-bench tolerance")
)

type curatedCase struct {
	ID    int
	Label string
	HoleA, HoleB string
	Position int
}

// Same 38 cases as cmd/case-bench-hunl-files.
var cases = []curatedCase{
	{1, "AA SB", "Ac", "Ad", 0}, {2, "KK SB", "Kc", "Kd", 0}, {3, "QQ SB", "Qc", "Qd", 0},
	{4, "JJ SB", "Jc", "Jd", 0}, {5, "TT SB", "Tc", "Td", 0},
	{6, "AKs SB", "As", "Ks", 0}, {7, "AKo SB", "As", "Kh", 0},
	{8, "AQs SB", "As", "Qs", 0}, {9, "AQo SB", "As", "Qh", 0}, {10, "AJs SB", "As", "Js", 0},
	{20, "22 SB", "2c", "2d", 0}, {21, "55 SB", "5c", "5d", 0}, {22, "A5s SB", "As", "5s", 0},
	{23, "A2o SB", "As", "2h", 0}, {24, "K9s SB", "Ks", "9s", 0}, {25, "QJs SB", "Qs", "Js", 0},
	{26, "T9s SB", "Ts", "9s", 0},
	{30, "72o SB", "7c", "2d", 0}, {31, "32o SB", "3c", "2d", 0}, {32, "82o SB", "8c", "2d", 0},
	{33, "94o SB", "9c", "4d", 0}, {34, "T2o SB", "Tc", "2d", 0},
	{50, "AA BB shove", "Ac", "Ad", 1}, {51, "KK BB shove", "Kc", "Kd", 1},
	{52, "QQ BB shove", "Qc", "Qd", 1}, {53, "JJ BB shove", "Jc", "Jd", 1},
	{54, "TT BB shove", "Tc", "Td", 1}, {55, "AKs BB shove", "As", "Ks", 1},
	{56, "AKo BB shove", "As", "Kh", 1}, {57, "AQs BB shove", "As", "Qs", 1},
	{70, "55 BB shove", "5c", "5d", 1}, {71, "A9s BB shove", "As", "9s", 1},
	{72, "KQs BB shove", "Ks", "Qs", 1},
	{80, "72o BB shove", "7c", "2d", 1}, {81, "32o BB shove", "3c", "2d", 1},
	{82, "82o BB shove", "8c", "2d", 1}, {83, "94o BB shove", "9c", "4d", 1}, {84, "T2o BB shove", "Tc", "2d", 1},
}

type bpRec struct {
	ID    uint64    `json:"id"`
	Probs []float64 `json:"probs"`
}
type bpFile struct {
	Strategy []bpRec `json:"strategy"`
}

func loadStrat(path string) map[uint64][]float64 {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var b bpFile
	if err := json.NewDecoder(f).Decode(&b); err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
	out := make(map[uint64][]float64, len(b.Strategy))
	for _, r := range b.Strategy {
		out[r.ID] = r.Probs
	}
	return out
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	// Load preflop buckets.
	buckets, err := abstraction.LoadPreflopBuckets(*bucketsPath)
	if err != nil {
		log.Fatalf("load buckets: %v", err)
	}
	log.Printf("[setup] loaded K=%d buckets from %s", buckets.K, *bucketsPath)
	log.Printf("[setup] expected abstract infoset count: %d (= K*2 positions)", buckets.K*2)

	// Load tabular reference (3M iter MCCFR).
	tabStrat := loadStrat(*tabularPath)
	log.Printf("[setup] loaded tabular reference: %d infosets", len(tabStrat))

	// Train abstract MCCFR.
	log.Printf("\n[train] starting abstract MCCFR (K=%d, iters=%d)...", buckets.K, *iters)
	cfg := nlhe.PushFoldConfig(*stackBBs)
	absM := nlhe.NewMCCFR(cfg, *seed).WithIDFn(buckets.PreflopID)
	t0 := time.Now()
	logEvery := *iters / 10
	if logEvery < 1 {
		logEvery = 1
	}
	for i := 1; i <= *iters; i++ {
		absM.Iter()
		if i%logEvery == 0 {
			log.Printf("[train] iter %d/%d  %.1fs  infosets=%d",
				i, *iters, time.Since(t0).Seconds(), absM.NumInfosets())
		}
	}
	absStrat := absM.AverageStrategy()
	log.Printf("[train] done in %.1fs, %d abstract infosets", time.Since(t0).Seconds(), len(absStrat))

	// Compare on 38 curated cases.
	fmt.Println()
	fmt.Println("=== Case-bench: abstract (K-bucket) vs tabular lossless (3M iter) ===")
	fmt.Printf("%-3s  %-30s  %-7s  %s\n", "ID", "case", "maxgap", "result")
	fmt.Println("-----------------------------------------------------------------")

	pass := 0
	for _, c := range cases {
		s := buildState(cfg, c)
		if s == nil {
			fmt.Printf("%-3d  %-30s  --      ✗ (build)\n", c.ID, c.Label)
			continue
		}
		lossID := s.InfosetID()
		absID := buckets.PreflopID(s)
		lossP, lossOK := tabStrat[lossID]
		absP, absOK := absStrat[absID]
		if !lossOK || !absOK {
			fmt.Printf("%-3d  %-30s  --      ✗ (missing) lossOK=%v absOK=%v\n",
				c.ID, c.Label, lossOK, absOK)
			continue
		}
		gap := 0.0
		for i := 0; i < len(lossP) && i < len(absP); i++ {
			d := math.Abs(lossP[i] - absP[i])
			if d > gap {
				gap = d
			}
		}
		ok := gap < *tol
		mark := "✗"
		if ok {
			mark = "✓"
			pass++
		}
		fmt.Printf("%-3d  %-30s  %.4f   %s\n", c.ID, c.Label, gap, mark)
		if !ok {
			legal := s.LegalActions()
			for i, a := range legal {
				name := "?"
				switch a.Kind {
				case nlhe.ActionFold:
					name = "fold"
				case nlhe.ActionCheckCall:
					name = "call"
				case nlhe.ActionAllIn:
					name = "allin"
				}
				if i >= len(lossP) || i >= len(absP) {
					break
				}
				fmt.Printf("       %-9s  loss=%.4f  abs=%.4f  Δ=%+.4f\n",
					name, lossP[i], absP[i], absP[i]-lossP[i])
			}
		}
	}
	fmt.Println("-----------------------------------------------------------------")
	pct := float64(pass) / float64(len(cases)) * 100
	fmt.Printf("PASS: %d / %d  (%.1f%%)\n\n", pass, len(cases), pct)

	// Summary.
	fmt.Println("=== Summary ===")
	fmt.Printf("Lossless:  2652 infosets, trained 3M iter (~5 min reference)\n")
	fmt.Printf("Abstract:  %d infosets (= K=%d × 2 positions), trained %d iter (%.1fs)\n",
		len(absStrat), buckets.K, *iters, time.Since(t0).Seconds())
	fmt.Printf("Compression: %.0fx fewer infosets\n", float64(2652)/float64(len(absStrat)))
	fmt.Printf("Case-bench: %.1f%% pass (tolerance ±%.2f)\n", pct, *tol)
	if pass == len(cases) {
		fmt.Println("🎯 abstraction is essentially lossless for push/fold")
	} else if pct >= 95 {
		fmt.Println("✓ abstraction acceptable for push/fold")
	} else {
		fmt.Println("⚠ abstraction lossy — see failures above. Increase K or use OCHS.")
	}
}

func buildState(cfg *nlhe.GameConfig, c curatedCase) *nlhe.State {
	h1 := nlhe.ParseCard(c.HoleA)
	h2 := nlhe.ParseCard(c.HoleB)
	oppA, oppB := pickOpp(h1, h2)
	s := nlhe.NewState(cfg)
	if c.Position == 0 {
		s.SetHole(nlhe.P0, h1, h2)
		s.SetHole(nlhe.P1, oppA, oppB)
	} else {
		s.SetHole(nlhe.P0, oppA, oppB)
		s.SetHole(nlhe.P1, h1, h2)
		s.Apply(nlhe.Action{Kind: nlhe.ActionAllIn})
	}
	return s
}

func pickOpp(c1, c2 nlhe.Card) (nlhe.Card, nlhe.Card) {
	var pick [2]nlhe.Card
	n := 0
	for cd := nlhe.Card(0); cd < nlhe.DeckSize && n < 2; cd++ {
		if cd != c1 && cd != c2 && (n == 0 || cd != pick[0]) {
			pick[n] = cd
			n++
		}
	}
	return pick[0], pick[1]
}
