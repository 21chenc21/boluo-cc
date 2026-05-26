// multistreet-case-bench — directional case-bench for multi-street abstract MCCFR.
//
// Trains MCCFR with MultiStreetBuckets, then evaluates hand-curated multi-street
// scenarios with directional expectations (e.g. "AA preflop should raise+allin
// total ≥ 0.5", "72o preflop should fold ≥ 0.5").
//
// Directional not exact-frequency: no trusted multi-street solver exists for
// reference. The bench documents both what's converged (passing cases) and
// known abstraction flaws (e.g. preflop bucket dragdown on AA → limp).
//
//	go run ./cmd/multistreet-case-bench -iters 1000000 -stack 20 \
//	    -bet-frac "0.5,1.0,2.0"
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
	iters       = flag.Int("iters", 500000, "MCCFR iterations")
	stackBBs    = flag.Int("stack", 20, "starting stack in BB units")
	seed        = flag.Int64("seed", 42, "RNG seed")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket JSON")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket JSON (EHS)")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket JSON (EHS)")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket JSON (EHS)")
	flopOCHS    = flag.String("flop-ochs", "", "flop OCHS bucket JSON (overrides -flop)")
	turnOCHS    = flag.String("turn-ochs", "", "turn OCHS bucket JSON (overrides -turn)")
	riverOCHS   = flag.String("river-ochs", "", "river OCHS bucket JSON (overrides -river)")
	fallbackMC  = flag.Int("fallback-mc", 0, "MC samples for unseen postflop class")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "comma-separated bet sizes")
)

// caseDir — which action sums to compare. Two modes:
//
//	"sum_ge": probability of actions in Kinds (+ optional SizeIdx) must be ≥ Threshold.
//	"sum_le": ... ≤ Threshold.
type caseDir struct {
	Label     string
	HoleA     string
	HoleB     string
	Position  nlhe.Player // P0 (SB) or P1 (BB) — actor at decision point
	Actions   []nlhe.Action // action history to replay (canonical, applied alternately by Cur)
	Board     []string      // board cards as deal sequence
	Mode      string        // "sum_ge" or "sum_le"
	MatchKind []nlhe.ActionKind // which action kinds count toward sum
	Threshold float64       // pass if observed sum >=Threshold (sum_ge) or <= (sum_le)
	Comment   string
}

// All actions are CheckCall/Bet/Fold/AllIn. SizeIdx 0/1/2 = bet-frac index.
var (
	aCall  = nlhe.Action{Kind: nlhe.ActionCheckCall}
	aFold  = nlhe.Action{Kind: nlhe.ActionFold}
	aBet0  = nlhe.Action{Kind: nlhe.ActionBet, SizeIdx: 0}
	aBet1  = nlhe.Action{Kind: nlhe.ActionBet, SizeIdx: 1}
	aBet2  = nlhe.Action{Kind: nlhe.ActionBet, SizeIdx: 2}
	aAllIn = nlhe.Action{Kind: nlhe.ActionAllIn}
)

var cases = []caseDir{
	// === Preflop opening expectations (SB to act) ===
	{
		Label: "AA SB preflop should raise/allin heavy",
		HoleA: "As", HoleB: "Ah", Position: nlhe.P0,
		Mode: "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.5, Comment: "premium pair, must aggression > 0.5",
	},
	{
		Label: "KK SB preflop should raise/allin heavy",
		HoleA: "Ks", HoleB: "Kh", Position: nlhe.P0,
		Mode: "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.5,
	},
	{
		Label: "72o SB preflop should fold heavy",
		HoleA: "7c", HoleB: "2d", Position: nlhe.P0,
		Mode: "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.4, Comment: "worst hand, should fold most",
	},
	{
		Label: "AKs SB preflop should not fold",
		HoleA: "As", HoleB: "Ks", Position: nlhe.P0,
		Mode: "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.1, Comment: "premium broadway",
	},

	// === Preflop BB facing SB raise ===
	{
		Label: "AA BB facing SB pot-bet should not fold",
		HoleA: "As", HoleB: "Ah", Position: nlhe.P1,
		Actions: []nlhe.Action{aBet1}, // SB bets 1.0 pot
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.05, Comment: "AA must not fold to a single bet",
	},
	{
		Label: "32o BB facing SB pot-bet should fold mostly",
		HoleA: "3c", HoleB: "2d", Position: nlhe.P1,
		Actions: []nlhe.Action{aBet1},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.3, Comment: "worst hand vs raise",
	},

	// === Flop scenarios ===
	{
		Label: "AA flop on 2-7-K rainbow after preflop check-check should bet",
		HoleA: "As", HoleB: "Ah", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall}, // SB limp, BB check
		Board:   []string{"2c", "7d", "Kh"},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.3, Comment: "overpair on dry board, c-bet",
	},
	{
		Label: "72o flop on QJ8 after preflop check-check should not bet",
		HoleA: "7c", HoleB: "2d", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"Qd", "Jc", "8h"},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.5, Comment: "complete air on wet board, should mostly check",
	},
	{
		Label: "Top pair AKo flop on K72 after check-check should bet",
		HoleA: "As", HoleB: "Kh", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"Kd", "7c", "2h"},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.3, Comment: "top pair top kicker on dry board",
	},

	// === Turn / river ===
	{
		Label: "AA river on 2-7-K-3-9 rainbow after all check should bet",
		HoleA: "As", HoleB: "Ah", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall, aCall, aCall, aCall, aCall},
		Board:   []string{"2c", "7d", "Kh", "3s", "9c"},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.3, Comment: "value-bet overpair on dry river",
	},
	{
		Label: "32o river on KQJ98 after all check should not bet",
		HoleA: "3c", HoleB: "2d", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall, aCall, aCall, aCall, aCall},
		Board:   []string{"Ks", "Qd", "Jh", "9c", "8s"},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.4, Comment: "plays-board high-card KQJ98, weak vs any pair, should mostly check",
	},

	// === Premium pair preflop opening (dragdown verification) ===
	{
		Label: "QQ SB preflop should raise+allin heavy",
		HoleA: "Qc", HoleB: "Qd", Position: nlhe.P0,
		Mode: "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.4, Comment: "QQ premium pair; tests lossless preflop fix vs K=20 dragdown",
	},
	{
		Label: "JJ SB preflop should raise (not all fold)",
		HoleA: "Jc", HoleB: "Jd", Position: nlhe.P0,
		Mode: "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.3, Comment: "JJ can mix limp/raise but should have some aggression",
	},

	// === BB premium pair defense (must defend vs single bet) ===
	{
		Label: "KK BB facing SB pot-bet should not fold",
		HoleA: "Kc", HoleB: "Kd", Position: nlhe.P1,
		Actions: []nlhe.Action{aBet1},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.05, Comment: "KK premium, never fold to single bet",
	},
	{
		Label: "QQ BB facing SB pot-bet should not fold",
		HoleA: "Qc", HoleB: "Qd", Position: nlhe.P1,
		Actions: []nlhe.Action{aBet1},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionFold},
		Threshold: 0.10, Comment: "QQ premium, defend",
	},

	// === Postflop OCHS-specific patterns ===
	{
		Label: "77 set on K72 flop after check-check should bet heavy",
		HoleA: "7s", HoleB: "7h", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"Kd", "7c", "2h"},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.4, Comment: "middle set on dry board, nuts territory",
	},
	{
		Label: "AKs flush draw on Qh7h2c flop after check-check should bet (semi-bluff)",
		HoleA: "Ah", HoleB: "Kh", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"Qh", "7h", "2c"},
		Mode:    "sum_ge", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.2, Comment: "OCHS-specific: 9 outs to flush + 2 overcards, semi-bluff candidate",
	},
	{
		Label: "55 underpair on JT3 rainbow flop after check-check should not bet",
		HoleA: "5c", HoleB: "5d", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"Js", "Td", "3h"},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.4, Comment: "small pair facing 2 overcards, should mostly check",
	},
	{
		Label: "KK on A72 flop after check-check should not bet heavy (cautious)",
		HoleA: "Kc", HoleB: "Kd", Position: nlhe.P1,
		Actions: []nlhe.Action{aCall, aCall},
		Board:   []string{"As", "7d", "2h"},
		Mode:    "sum_le", MatchKind: []nlhe.ActionKind{nlhe.ActionBet, nlhe.ActionAllIn},
		Threshold: 0.5, Comment: "KK is now underpair to A; should be cautious, mostly check-call",
	},
}

func parseBetSizes(s string) []float64 {
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			log.Fatalf("bet size %q: %v", p, err)
		}
		out = append(out, f)
	}
	return out
}

// applyHistory — sets hole cards (opp gets dummy cards), runs the action sequence
// + deals the board, returns the state ready for the actor's decision.
//
// Action sequence is "canonical": each entry applied by whoever Cur is at the
// time of application. Board cards inserted via NeedsBoard checkpoints between
// streets.
func applyHistory(cfg *nlhe.GameConfig, c caseDir) *nlhe.State {
	s := nlhe.NewState(cfg)
	hA := nlhe.ParseCard(c.HoleA)
	hB := nlhe.ParseCard(c.HoleB)

	// Pick opp hole cards that don't conflict with hero hole or board.
	used := map[nlhe.Card]bool{hA: true, hB: true}
	for _, b := range c.Board {
		used[nlhe.ParseCard(b)] = true
	}
	var oppA, oppB nlhe.Card
	for c := nlhe.Card(0); c < nlhe.DeckSize; c++ {
		if used[c] {
			continue
		}
		if oppA == 0 && c != 0 {
			oppA = c
			used[c] = true
		} else if oppB == 0 && c != 0 {
			oppB = c
			break
		}
	}
	if c.Position == nlhe.P0 {
		s.SetHole(nlhe.P0, hA, hB)
		s.SetHole(nlhe.P1, oppA, oppB)
	} else {
		s.SetHole(nlhe.P0, oppA, oppB)
		s.SetHole(nlhe.P1, hA, hB)
	}

	boardIdx := 0
	boardCards := make([]nlhe.Card, len(c.Board))
	for i, str := range c.Board {
		boardCards[i] = nlhe.ParseCard(str)
	}
	for _, a := range c.Actions {
		s.Apply(a)
		// After Apply, check if board needs filling.
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			if boardIdx+n > len(boardCards) {
				log.Fatalf("case %q: needs %d board cards but only %d in spec",
					c.Label, n, len(boardCards)-boardIdx)
			}
			for i := 0; i < n; i++ {
				s.Board[s.NumBoard] = boardCards[boardIdx+i]
				s.NumBoard++
			}
			boardIdx += n
		}
	}
	return s
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	pre, err := abstraction.LoadPreflopBuckets(*preflopPath)
	if err != nil {
		log.Fatalf("load preflop: %v", err)
	}
	loadStreet := func(street int, ehsPath, ochsPath string) abstraction.StreetBucketer {
		streetName := []string{"", "", "", "flop", "turn", "river"}[street]
		if ochsPath != "" {
			b, err := abstraction.LoadStreetOCHSBuckets(ochsPath)
			if err != nil {
				log.Fatalf("load %s OCHS: %v", streetName, err)
			}
			log.Printf("[bench] %s: OCHS K=%d (opp=%d) from %s", streetName, b.K, b.NumOppClusters, ochsPath)
			return b
		}
		b, err := abstraction.LoadStreetBuckets(ehsPath)
		if err != nil {
			log.Fatalf("load %s EHS: %v", streetName, err)
		}
		log.Printf("[bench] %s: EHS K=%d from %s", streetName, b.K, ehsPath)
		return b
	}
	b := &abstraction.MultiStreetBuckets{
		Preflop:           pre,
		Flop:              loadStreet(3, *flopPath, *flopOCHS),
		Turn:              loadStreet(4, *turnPath, *turnOCHS),
		River:             loadStreet(5, *riverPath, *riverOCHS),
		MCSamplesFallback: *fallbackMC,
		FallbackSeed:      *seed,
	}

	betSizes := parseBetSizes(*betFracs)
	cfg := &nlhe.GameConfig{
		SmallBlind: 1, BigBlind: 2,
		StartStack:   2 * (*stackBBs),
		BetSizes:     betSizes,
		PushFoldOnly: false,
	}
	log.Printf("[bench] %d iter / stack=%dbb / bet-sizes=%v / fallback-mc=%d", *iters, *stackBBs, betSizes, *fallbackMC)

	m := nlhe.NewMCCFR(cfg, *seed).WithIDFn(func(s *nlhe.State) uint64 { return b.ID(s) })
	t0 := time.Now()
	for i := 0; i < *iters; i++ {
		m.Iter()
	}
	log.Printf("[bench] training done in %.1fs, %d infosets", time.Since(t0).Seconds(), m.NumInfosets())

	avg := m.AverageStrategy()

	fmt.Println()
	fmt.Println("=== Multi-street case-bench (directional) ===")
	passed, failed := 0, 0
	for i, c := range cases {
		state := applyHistory(cfg, c)
		if state.Cur != c.Position {
			fmt.Printf("[%2d] SKIP %q — state.Cur=%v but case Position=%v after replay\n",
				i+1, c.Label, state.Cur, c.Position)
			continue
		}
		id := b.ID(state)
		probs, ok := avg[id]
		if !ok {
			fmt.Printf("[%2d] MISS %q — infoset not visited\n", i+1, c.Label)
			failed++
			continue
		}
		legal := state.LegalActions()
		var sum float64
		var matched []string
		for j, a := range legal {
			for _, k := range c.MatchKind {
				if a.Kind == k {
					sum += probs[j]
					matched = append(matched, fmt.Sprintf("%s=%.3f", a.String(), probs[j]))
				}
			}
		}
		pass := false
		switch c.Mode {
		case "sum_ge":
			pass = sum >= c.Threshold
		case "sum_le":
			pass = sum <= c.Threshold
		}
		mark := "FAIL"
		if pass {
			mark = "PASS"
			passed++
		} else {
			failed++
		}
		fmt.Printf("[%2d] %s %s\n     expect %s sum %s %.2f, got %.3f  (%s)\n",
			i+1, mark, c.Label, kindLabels(c.MatchKind), c.Mode, c.Threshold, sum, strings.Join(matched, ", "))
		if c.Comment != "" {
			fmt.Printf("     // %s\n", c.Comment)
		}
	}
	fmt.Printf("\n=== %d/%d PASS (%.1f%%) ===\n",
		passed, passed+failed, 100*float64(passed)/float64(passed+failed))
}

func kindLabels(kinds []nlhe.ActionKind) string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		switch k {
		case nlhe.ActionFold:
			out = append(out, "fold")
		case nlhe.ActionCheckCall:
			out = append(out, "call")
		case nlhe.ActionBet:
			out = append(out, "bet")
		case nlhe.ActionAllIn:
			out = append(out, "allin")
		}
	}
	return "{" + strings.Join(out, "+") + "}"
}
