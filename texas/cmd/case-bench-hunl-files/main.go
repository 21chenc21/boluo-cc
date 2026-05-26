// case-bench-hunl-files — run HUNL push/fold case-bench using JSON files for
// both candidate and reference (instead of re-training each time).
//
// Used to compare NN-distilled strategy against tabular blueprint on the
// 38 curated cases — the "real POC metric" for distillation quality.
//
//	go run ./cmd/case-bench-hunl-files \
//	    -candidate distill/models/hunl-nn-strategy.json \
//	    -reference blueprints/hunl-pushfold-10bb.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/boluo/texas/engine/nlhe"
)

var (
	candPath = flag.String("candidate", "distill/models/hunl-nn-strategy.json", "candidate strategy JSON")
	refPath  = flag.String("reference", "blueprints/hunl-pushfold-10bb.json", "reference strategy JSON")
	stackBBs = flag.Int("stack", 10, "stack in BB units")
	tol      = flag.Float64("tol", 0.10, "tolerance per action probability")
)

type rec struct {
	ID    uint64    `json:"id"`
	Label string    `json:"label"`
	Probs []float64 `json:"probs"`
}

type bp struct {
	Strategy []rec `json:"strategy"`
}

func loadStrat(path string) map[uint64][]float64 {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var b bp
	if err := json.NewDecoder(f).Decode(&b); err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
	out := make(map[uint64][]float64, len(b.Strategy))
	for _, r := range b.Strategy {
		out[r.ID] = r.Probs
	}
	return out
}

type curatedCase struct {
	ID    int
	Label string
	HoleA, HoleB string
	Position int // 0=SB open, 1=BB facing shove
}

// Same 38 cases as cmd/case-bench/hunl.go.
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

	// 2026-05-24 追加: 小对 BB facing shove — abstraction 失败常发. 22/33/44 在 K=20
	// bucket 9-14 跟弱 unpaired 同桶, abstract Nash 错 fold.
	{90, "22 BB shove", "2c", "2d", 1}, {91, "33 BB shove", "3c", "3d", 1},
	{92, "44 BB shove", "4c", "4d", 1}, {93, "66 BB shove", "6c", "6d", 1},
	{94, "88 BB shove", "8c", "8d", 1}, {95, "99 BB shove", "9c", "9d", 1},
	// 弱 Ax SB - 也常被 abstraction 误 bucket
	{100, "A3o SB", "As", "3h", 0}, {101, "A4o SB", "As", "4h", 0},
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	cand := loadStrat(*candPath)
	ref := loadStrat(*refPath)
	fmt.Printf("=== HUNL push/fold case-bench (file mode) ===\n")
	fmt.Printf("candidate: %s  (%d infosets)\n", *candPath, len(cand))
	fmt.Printf("reference: %s  (%d infosets)\n", *refPath, len(ref))
	fmt.Printf("tolerance: ±%.2f per action prob\n\n", *tol)

	cfg := nlhe.PushFoldConfig(*stackBBs)

	pass := 0
	fmt.Printf("%-3s  %-30s  %-7s  %s\n", "ID", "case", "maxgap", "result")
	fmt.Println("-----------------------------------------------------------")
	for _, c := range cases {
		s := buildState(cfg, c)
		if s == nil || s.Terminal {
			fmt.Printf("%-3d  %-30s  --      ✗ (build failed)\n", c.ID, c.Label)
			continue
		}
		id := s.InfosetID()
		candP, candOK := cand[id]
		refP, refOK := ref[id]
		if !candOK || !refOK {
			fmt.Printf("%-3d  %-30s  --      ✗ (missing in strat)\n", c.ID, c.Label)
			continue
		}
		gap := 0.0
		for i := 0; i < len(candP) && i < len(refP); i++ {
			d := math.Abs(candP[i] - refP[i])
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
				if i >= len(candP) || i >= len(refP) {
					break
				}
				fmt.Printf("       %-9s  candidate=%.4f  ref=%.4f  Δ=%+.4f\n",
					name, candP[i], refP[i], candP[i]-refP[i])
			}
		}
	}
	fmt.Println("-----------------------------------------------------------")
	pct := float64(pass) / float64(len(cases)) * 100
	fmt.Printf("PASS: %d / %d  (%.1f%%)\n", pass, len(cases), pct)
	if pass == len(cases) {
		fmt.Println("🎯 all cases pass")
	} else if pct >= 95 {
		fmt.Println("✓ ≥95% pass")
	} else {
		fmt.Println("⚠ <95% pass")
		os.Exit(1)
	}
}

func buildState(cfg *nlhe.GameConfig, c curatedCase) *nlhe.State {
	h1 := nlhe.ParseCard(c.HoleA)
	h2 := nlhe.ParseCard(c.HoleB)
	if h1 == nlhe.NoCard || h2 == nlhe.NoCard {
		return nil
	}
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
