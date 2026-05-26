package main

import (
	"fmt"
	"log"
	"time"

	"github.com/boluo/texas/engine/nlhe"
)

// HUNLPushFoldCase — curated infoset for HUNL push/fold.
type HUNLPushFoldCase struct {
	ID    int
	Label string
	// Actor's two hole cards (codes like "As" "Kh").
	HoleA, HoleB string
	// Position: 0 = SB opening, 1 = BB facing SB shove.
	Position int
}

var curatedHUNLCases = []HUNLPushFoldCase{
	// SB opening — premium
	{1, "AA SB", "Ac", "Ad", 0},
	{2, "KK SB", "Kc", "Kd", 0},
	{3, "QQ SB", "Qc", "Qd", 0},
	{4, "JJ SB", "Jc", "Jd", 0},
	{5, "TT SB", "Tc", "Td", 0},
	{6, "AKs SB", "As", "Ks", 0},
	{7, "AKo SB", "As", "Kh", 0},
	{8, "AQs SB", "As", "Qs", 0},
	{9, "AQo SB", "As", "Qh", 0},
	{10, "AJs SB", "As", "Js", 0},
	// SB opening — middle
	{20, "22 SB", "2c", "2d", 0},
	{21, "55 SB", "5c", "5d", 0},
	{22, "A5s SB", "As", "5s", 0},
	{23, "A2o SB", "As", "2h", 0},
	{24, "K9s SB", "Ks", "9s", 0},
	{25, "QJs SB", "Qs", "Js", 0},
	{26, "T9s SB", "Ts", "9s", 0},
	// SB opening — trash
	{30, "72o SB", "7c", "2d", 0},
	{31, "32o SB", "3c", "2d", 0},
	{32, "82o SB", "8c", "2d", 0},
	{33, "94o SB", "9c", "4d", 0},
	{34, "T2o SB", "Tc", "2d", 0},

	// BB facing shove — premium (must call)
	{50, "AA BB facing shove", "Ac", "Ad", 1},
	{51, "KK BB facing shove", "Kc", "Kd", 1},
	{52, "QQ BB facing shove", "Qc", "Qd", 1},
	{53, "JJ BB facing shove", "Jc", "Jd", 1},
	{54, "TT BB facing shove", "Tc", "Td", 1},
	{55, "AKs BB facing shove", "As", "Ks", 1},
	{56, "AKo BB facing shove", "As", "Kh", 1},
	{57, "AQs BB facing shove", "As", "Qs", 1},
	// BB facing shove — middle
	{70, "55 BB facing shove", "5c", "5d", 1},
	{71, "A9s BB facing shove", "As", "9s", 1},
	{72, "KQs BB facing shove", "Ks", "Qs", 1},
	// BB facing shove — trash (must fold)
	{80, "72o BB facing shove", "7c", "2d", 1},
	{81, "32o BB facing shove", "3c", "2d", 1},
	{82, "82o BB facing shove", "8c", "2d", 1},
	{83, "94o BB facing shove", "9c", "4d", 1},
	{84, "T2o BB facing shove", "Tc", "2d", 1},
}

func runPushFold(args []string) {
	stackBBs := parseInt(getArg(args, 0), 10)
	candIters := parseInt(getArg(args, 1), 100000)
	refIters := parseInt(getArg(args, 2), 1000000)

	fmt.Printf("=== HUNL push/fold case-bench ===\n")
	fmt.Printf("stack=%dbb, candidate iters=%d, reference iters=%d\n", stackBBs, candIters, refIters)

	cfg := nlhe.PushFoldConfig(stackBBs)

	// Train CANDIDATE.
	fmt.Println("\n[1/2] training candidate model...")
	tCand := time.Now()
	candM := nlhe.NewMCCFR(cfg, 42)
	for i := 0; i < candIters; i++ {
		candM.Iter()
	}
	candStrat := candM.AverageStrategy()
	fmt.Printf("[1/2] done in %.1fs, %d infosets\n", time.Since(tCand).Seconds(), candM.NumInfosets())

	// Train REFERENCE (different seed, more iters).
	fmt.Println("\n[2/2] training reference model (high iter, different seed)...")
	tRef := time.Now()
	refM := nlhe.NewMCCFR(cfg, 12345)
	for i := 0; i < refIters; i++ {
		refM.Iter()
	}
	refStrat := refM.AverageStrategy()
	fmt.Printf("[2/2] done in %.1fs, %d infosets\n", time.Since(tRef).Seconds(), refM.NumInfosets())

	runHUNLCases(cfg, candStrat, refStrat)
}

func getArg(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func runHUNLCases(cfg *nlhe.GameConfig, cand, ref map[uint64][]float64) {
	const tolerance = 0.10
	results := make([]caseResult, 0, len(curatedHUNLCases))

	for _, c := range curatedHUNLCases {
		s, ok := buildHUNLState(cfg, c)
		if !ok {
			results = append(results, caseResult{c.ID, c.Label + " (build failed)", nil, nil, nil, 1.0, false})
			continue
		}
		id := s.InfosetID()
		legal := s.LegalActions()

		// Translate actions to readable names per push/fold mode.
		actNames := make([]string, len(legal))
		for i, a := range legal {
			switch a.Kind {
			case nlhe.ActionFold:
				actNames[i] = "fold"
			case nlhe.ActionCheckCall:
				actNames[i] = "call"
			case nlhe.ActionAllIn:
				actNames[i] = "allin"
			}
		}

		candProbs, candOK := cand[id]
		refProbs, refOK := ref[id]
		if !candOK || !refOK {
			results = append(results, caseResult{c.ID, c.Label + " (missing)", actNames, candProbs, refProbs, 1.0, false})
			continue
		}
		gap := maxAbsGap(candProbs, refProbs)
		results = append(results, caseResult{
			id: c.ID, label: c.Label, actions: actNames,
			candidate: candProbs, reference: refProbs,
			maxGap: gap, pass: gap < tolerance,
		})
	}

	summarize(results, tolerance)
}

// buildHUNLState — set up state at the curated infoset.
func buildHUNLState(cfg *nlhe.GameConfig, c HUNLPushFoldCase) (*nlhe.State, bool) {
	h1 := nlhe.ParseCard(c.HoleA)
	h2 := nlhe.ParseCard(c.HoleB)
	if h1 == nlhe.NoCard || h2 == nlhe.NoCard {
		log.Printf("ParseCard failed for %q/%q", c.HoleA, c.HoleB)
		return nil, false
	}
	// Pick arbitrary opp cards not conflicting.
	oppA, oppB := pickNonConflicting(h1, h2)
	if c.Position == 0 {
		// SB opening.
		s := nlhe.NewState(cfg)
		s.SetHole(nlhe.P0, h1, h2)
		s.SetHole(nlhe.P1, oppA, oppB)
		return s, true
	}
	// BB facing SB shove.
	s := nlhe.NewState(cfg)
	s.SetHole(nlhe.P0, oppA, oppB)
	s.SetHole(nlhe.P1, h1, h2)
	s.Apply(nlhe.Action{Kind: nlhe.ActionAllIn})
	if s.Terminal {
		return nil, false
	}
	return s, true
}

func pickNonConflicting(c1, c2 nlhe.Card) (nlhe.Card, nlhe.Card) {
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
