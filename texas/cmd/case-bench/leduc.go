package main

import (
	"fmt"
	"log"
	"time"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/engine/leduc"
)

// LeducInfosetCase — curated Leduc infoset (no expected freq — we use reference).
type LeducInfosetCase struct {
	ID    int
	Label string
	// Specify private card RANK (suit doesn't matter for infoset).
	PrivRank uint8 // 0=J, 1=Q, 2=K
	// Public card rank (-1 if no public).
	PubRank int
	// History: round-1 history + round-2 history as action sequences.
	R1Hist []leduc.Action
	R2Hist []leduc.Action
}

// curatedLeducCases — 30+ infosets covering diverse game situations.
//
// "Curated" means we picked specific infosets that exercise different decision
// types. We DON'T specify expected frequencies — those come from the reference.
var curatedLeducCases = []LeducInfosetCase{
	// R1 opening (no public, empty history)
	{1, "K open", 2, -1, nil, nil},
	{2, "Q open", 1, -1, nil, nil},
	{3, "J open", 0, -1, nil, nil},
	// R1 facing P0 check
	{4, "K facing check", 2, -1, []leduc.Action{leduc.ActionCheckCall}, nil},
	{5, "Q facing check", 1, -1, []leduc.Action{leduc.ActionCheckCall}, nil},
	{6, "J facing check", 0, -1, []leduc.Action{leduc.ActionCheckCall}, nil},
	// R1 facing P0 bet
	{7, "K facing bet", 2, -1, []leduc.Action{leduc.ActionBetRaise}, nil},
	{8, "Q facing bet", 1, -1, []leduc.Action{leduc.ActionBetRaise}, nil},
	{9, "J facing bet", 0, -1, []leduc.Action{leduc.ActionBetRaise}, nil},
	// R1 P0 facing check-raise
	{10, "K facing P1 raise", 2, -1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionBetRaise}, nil},
	{11, "Q facing P1 raise", 1, -1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionBetRaise}, nil},
	{12, "J facing P1 raise", 0, -1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionBetRaise}, nil},
	// R2 paired board
	{20, "K paired (KK on board)", 2, 2, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	{21, "Q paired (QQ)", 1, 1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	{22, "J paired (JJ)", 0, 0, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	// R2 non-paired premium
	{30, "K, public Q", 2, 1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	{31, "K, public J", 2, 0, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	// R2 weakest
	{32, "J, public K (opp could have pair K)", 0, 2, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	{33, "J, public Q (opp could have pair Q)", 0, 1, []leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall}, nil},
	// R2 facing bet
	{40, "K KK-paired, facing P0 R2 bet", 2, 2,
		[]leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall},
		[]leduc.Action{leduc.ActionBetRaise}},
	{41, "J KK-paired, facing P0 R2 bet (must fold)", 0, 2,
		[]leduc.Action{leduc.ActionCheckCall, leduc.ActionCheckCall},
		[]leduc.Action{leduc.ActionBetRaise}},
}

func runLeduc(args []string) {
	candidatePath := "blueprints/leduc-vanilla-30k.json"
	if len(args) > 0 {
		candidatePath = args[0]
	}

	fmt.Printf("=== Leduc case-bench ===\n")
	fmt.Printf("candidate: %s\n", candidatePath)

	// Load candidate.
	cand, _, err := cfr.LoadBlueprint(candidatePath)
	if err != nil {
		log.Fatalf("load candidate: %v", err)
	}

	// Build/load reference: my own CFR at high iter (already validated via Week 1 gate).
	// Use the 30k blueprint as reference IF candidate is something else.
	// If candidate IS leduc-vanilla-30k, train a fresh 20k iter ref to compare against itself.
	refPath := "blueprints/leduc-vanilla-30k.json"
	if candidatePath == refPath {
		fmt.Println("candidate == default reference; training fresh 20k iter reference (CFR+) for non-trivial comparison")
		ref := trainLeducRef(20000)
		runLeducCases(cand, ref)
		return
	}
	fmt.Printf("reference: %s\n", refPath)
	ref, _, err := cfr.LoadBlueprint(refPath)
	if err != nil {
		log.Fatalf("load reference: %v", err)
	}
	runLeducCases(cand, ref)
}

func trainLeducRef(iters int) cfr.Strategy {
	t0 := time.Now()
	c := cfr.NewPlus()
	for i := 0; i < iters; i++ {
		c.Iter()
	}
	fmt.Printf("[ref] CFR+ %d iters in %.1fs, expl=%.6f\n",
		iters, time.Since(t0).Seconds(), cfr.Exploitability(c.AverageStrategy()))
	return c.AverageStrategy()
}

func runLeducCases(cand, ref cfr.Strategy) {
	const tolerance = 0.10
	results := make([]caseResult, 0, len(curatedLeducCases))

	for _, c := range curatedLeducCases {
		// Build state to get InfosetID + LegalActions order.
		s, ok := buildLeducState(c)
		if !ok {
			results = append(results, caseResult{c.ID, c.Label + " (unreachable)", nil, nil, nil, 1.0, false})
			continue
		}
		id := s.InfosetID()
		legal := s.LegalActions()
		actNames := make([]string, len(legal))
		for i, a := range legal {
			switch a {
			case leduc.ActionFold:
				actNames[i] = "fold"
			case leduc.ActionCheckCall:
				if s.ToCall == 0 {
					actNames[i] = "check"
				} else {
					actNames[i] = "call"
				}
			case leduc.ActionBetRaise:
				if s.ToCall == 0 {
					actNames[i] = "bet"
				} else {
					actNames[i] = "raise"
				}
			}
		}
		candProbs, candOK := cand[id]
		refProbs, refOK := ref[id]
		if !candOK || !refOK {
			results = append(results, caseResult{c.ID, c.Label + " (missing in strategy)", actNames, candProbs, refProbs, 1.0, false})
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

// buildLeducState — construct State at the curated infoset.
func buildLeducState(c LeducInfosetCase) (*leduc.State, bool) {
	// Pick opp private rank ≠ c.PrivRank, and pick public if needed.
	oppRank := (c.PrivRank + 1) % leduc.NumRanks
	// Use suit 0 for actor, suit 1 for opp.
	actorCard := leduc.MakeCard(c.PrivRank, 0)
	oppCard := leduc.MakeCard(oppRank, 1)
	if oppCard == actorCard {
		oppCard = leduc.MakeCard(oppRank, 0)
	}
	s := leduc.NewState(actorCard, oppCard)

	// Replay R1 history.
	for _, a := range c.R1Hist {
		if s.Terminal {
			return nil, false
		}
		s.Apply(a)
	}
	// Public deal if specified.
	if c.PubRank >= 0 {
		if !s.NeedsPublicCard() {
			return nil, false
		}
		// Pick a suit not conflicting.
		var pub leduc.Card
		for su := uint8(0); su < leduc.NumSuits; su++ {
			cand := leduc.MakeCard(uint8(c.PubRank), su)
			if cand != actorCard && cand != oppCard {
				pub = cand
				break
			}
		}
		s.SetPublic(pub)
		for _, a := range c.R2Hist {
			if s.Terminal {
				return nil, false
			}
			s.Apply(a)
		}
	}
	if s.Terminal {
		return nil, false
	}
	return s, true
}
