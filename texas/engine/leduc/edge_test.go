package leduc

import (
	"fmt"
	"testing"
)

// ──────────────────────── NewState invariants ────────────────────────

func TestNewStateInvariants(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 1))
	if s.Round != 0 {
		t.Errorf("Round=%d want 0", s.Round)
	}
	if s.Cur != P0 {
		t.Errorf("Cur=%d want P0", s.Cur)
	}
	if s.Contrib[P0] != Ante || s.Contrib[P1] != Ante {
		t.Errorf("Contrib=%v want [%d,%d]", s.Contrib, Ante, Ante)
	}
	if s.NumRaises != 0 {
		t.Errorf("NumRaises=%d want 0", s.NumRaises)
	}
	if s.ToCall != 0 {
		t.Errorf("ToCall=%d want 0", s.ToCall)
	}
	if s.HasPub {
		t.Errorf("HasPub=true on fresh state")
	}
	if s.Terminal {
		t.Errorf("Terminal=true on fresh state")
	}
	if s.FoldedBy != NoPlayer {
		t.Errorf("FoldedBy=%d want NoPlayer", s.FoldedBy)
	}
	if len(s.Hist[0]) != 0 || len(s.Hist[1]) != 0 {
		t.Errorf("Hist=%v want empty", s.Hist)
	}
}

// ──────────────────────── fold semantics ────────────────────────

func TestFreeFoldP0LosesAnte(t *testing.T) {
	// P0 folds immediately (ToCall=0, legal but dominated).
	s := NewState(MakeCard(2, 0), MakeCard(0, 0))
	s.Apply(ActionFold)
	if !s.Terminal {
		t.Fatal("not terminal")
	}
	if s.FoldedBy != P0 {
		t.Errorf("FoldedBy=%d want P0", s.FoldedBy)
	}
	if s.Payoff(P0) != -1 || s.Payoff(P1) != +1 {
		t.Errorf("free fold: P0=%v P1=%v want -1/+1", s.Payoff(P0), s.Payoff(P1))
	}
}

func TestFreeFoldP1LosesAnte(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionCheckCall) // P0 check, hand to P1
	s.Apply(ActionFold)      // P1 fold (still ToCall=0)
	if s.FoldedBy != P1 {
		t.Errorf("FoldedBy=%d want P1", s.FoldedBy)
	}
	if s.Payoff(P0) != +1 || s.Payoff(P1) != -1 {
		t.Errorf("P1 free fold: P0=%v P1=%v want +1/-1", s.Payoff(P0), s.Payoff(P1))
	}
}

func TestRound2BetFold(t *testing.T) {
	// Round 1 check-check, round 2 P0 bet (size 4), P1 fold. P1 loses 1 (ante only).
	s := play(t, MakeCard(2, 0), MakeCard(0, 0), MakeCard(1, 0),
		ActionCheckCall, ActionCheckCall, // round 1
		ActionBetRaise, ActionFold) // round 2: P0 bet, P1 fold
	if !s.Terminal || s.FoldedBy != P1 {
		t.Fatalf("expected P1 fold terminal, got terminal=%v foldedBy=%d", s.Terminal, s.FoldedBy)
	}
	// P0 invested ante(1)+bet(4)=5, P1 invested ante(1)=1.
	// P1 forfeits 1; P0 wins +1 (= P1's contrib).
	if s.Contrib[P0] != Ante+BetSize2 || s.Contrib[P1] != Ante {
		t.Errorf("Contrib=%v want [%d, %d]", s.Contrib, Ante+BetSize2, Ante)
	}
	if s.Payoff(P0) != +1 || s.Payoff(P1) != -1 {
		t.Errorf("R2 P1 fold: payoffs %v/%v want +1/-1", s.Payoff(P0), s.Payoff(P1))
	}
}

func TestRound2RaiseFold(t *testing.T) {
	// Round 1 check-check, R2: P0 bet, P1 raise, P0 fold. P0 invested ante+4=5; P1 invested ante+4+4=9.
	// P0 forfeits 5; P1 wins +5.
	s := play(t, MakeCard(2, 0), MakeCard(0, 0), MakeCard(1, 0),
		ActionCheckCall, ActionCheckCall,
		ActionBetRaise, ActionBetRaise, ActionFold)
	if s.FoldedBy != P0 {
		t.Errorf("FoldedBy=%d want P0", s.FoldedBy)
	}
	if s.Contrib[P0] != Ante+BetSize2 || s.Contrib[P1] != Ante+2*BetSize2 {
		t.Errorf("Contrib=%v want [%d,%d]", s.Contrib, Ante+BetSize2, Ante+2*BetSize2)
	}
	if s.Payoff(P0) != -5 || s.Payoff(P1) != +5 {
		t.Errorf("R2 raise-fold: payoffs %v/%v want -5/+5", s.Payoff(P0), s.Payoff(P1))
	}
}

// ──────────────────────── cross-round contrib accumulation ────────────────────────

func TestCrossRoundContribFullCap(t *testing.T) {
	// Both rounds cap to 2 raises + showdown. Max pot.
	// Round 1: P0 bet, P1 raise, P0 call. Contrib both = ante+2+2 = 5.
	// Round 2: P0 bet, P1 raise, P0 call. Contrib both = 5+4+4 = 13.
	// P0=K, P1=J, pub=Q → P0 wins +13.
	s := play(t, MakeCard(2, 0), MakeCard(0, 0), MakeCard(1, 0),
		ActionBetRaise, ActionBetRaise, ActionCheckCall, // round 1
		ActionBetRaise, ActionBetRaise, ActionCheckCall) // round 2
	if !s.Terminal {
		t.Fatal("not terminal")
	}
	const wantContrib = Ante + 2*BetSize1 + 2*BetSize2 // 1+4+8=13
	if s.Contrib[P0] != wantContrib || s.Contrib[P1] != wantContrib {
		t.Errorf("Contrib=%v want [%d,%d]", s.Contrib, wantContrib, wantContrib)
	}
	if s.Payoff(P0) != +wantContrib {
		t.Errorf("P0 win payoff=%v want +%d", s.Payoff(P0), wantContrib)
	}
}

func TestNumRaisesResetAcrossRounds(t *testing.T) {
	// Round 1: full cap (bet, raise). Round 2: must allow new bet/raise.
	s := play(t, MakeCard(0, 0), MakeCard(1, 0), MakeCard(2, 0),
		ActionBetRaise, ActionBetRaise, ActionCheckCall)
	if s.Round != 1 {
		t.Fatalf("Round=%d want 1", s.Round)
	}
	if s.NumRaises != 0 {
		t.Errorf("Round-2 start NumRaises=%d want 0 (reset)", s.NumRaises)
	}
	if !s.IsLegal(ActionBetRaise) {
		t.Errorf("BetRaise must be legal at round-2 start")
	}
	if s.Cur != P0 {
		t.Errorf("Round-2 first actor=%d want P0", s.Cur)
	}
}

func TestCheckThenRaiseLegality(t *testing.T) {
	// After P0 check, P1 should have all 3 actions legal (incl BetRaise).
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall) // P0 check
	la := s.LegalActions()
	if len(la) != 3 {
		t.Errorf("after P0 check, P1 legal=%v want 3", la)
	}
	// P1 raises → back to P0, ToCall=2, NumRaises=1. P0 can call/raise/fold (all 3).
	s.Apply(ActionBetRaise)
	la = s.LegalActions()
	if len(la) != 3 {
		t.Errorf("after P0 check + P1 bet, P0 legal=%v want 3", la)
	}
}

// ──────────────────────── panics ────────────────────────

func mustPanic(t *testing.T, label string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", label)
		}
	}()
	fn()
}

func TestApplyOnTerminalPanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionFold)
	mustPanic(t, "Apply on terminal", func() { s.Apply(ActionCheckCall) })
}

func TestApplyIllegalBetRaisePanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionBetRaise)
	s.Apply(ActionBetRaise) // NumRaises=2 (cap)
	mustPanic(t, "BetRaise at cap", func() { s.Apply(ActionBetRaise) })
}

func TestSetPublicCollisionPanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall)
	s.Apply(ActionCheckCall) // round transitions to 2
	if !s.NeedsPublicCard() {
		t.Fatal("expected NeedsPublicCard")
	}
	mustPanic(t, "SetPublic collision with priv0", func() { s.SetPublic(MakeCard(0, 0)) })
	mustPanic(t, "SetPublic collision with priv1", func() { s.SetPublic(MakeCard(1, 0)) })
}

func TestSetPublicTwicePanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall)
	s.Apply(ActionCheckCall)
	s.SetPublic(MakeCard(2, 0))
	mustPanic(t, "SetPublic twice", func() { s.SetPublic(MakeCard(2, 1)) })
}

func TestPayoffOnNonTerminalPanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	mustPanic(t, "Payoff on non-terminal", func() { s.Payoff(P0) })
}

func TestLegalActionsOnTerminalEmpty(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionFold)
	if la := s.LegalActions(); la != nil {
		t.Errorf("LegalActions on terminal=%v want nil", la)
	}
	if s.IsLegal(ActionCheckCall) {
		t.Errorf("IsLegal on terminal should be false")
	}
}

func TestActionCharUnknownValue(t *testing.T) {
	if got := Action(99).Char(); got != '?' {
		t.Errorf("unknown action char=%c want '?'", got)
	}
}

func TestApplyBeforeSetPublicPanics(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall)
	s.Apply(ActionCheckCall) // NeedsPublicCard now true
	mustPanic(t, "Apply before SetPublic", func() { s.Apply(ActionCheckCall) })
}

// ──────────────────────── encoding ────────────────────────

func TestActionCharRoundTrip(t *testing.T) {
	cases := []struct {
		a    Action
		want byte
	}{
		{ActionFold, 'f'},
		{ActionCheckCall, 'c'},
		{ActionBetRaise, 'r'},
	}
	for _, c := range cases {
		if c.a.Char() != c.want {
			t.Errorf("Action(%d).Char()=%c want %c", c.a, c.a.Char(), c.want)
		}
	}
}

func TestCardString(t *testing.T) {
	cases := []struct {
		c    Card
		want string
	}{
		{MakeCard(0, 0), "J0"},
		{MakeCard(0, 1), "J1"},
		{MakeCard(1, 0), "Q0"},
		{MakeCard(2, 1), "K1"},
		{NoCard, "?"},
	}
	for _, c := range cases {
		if got := c.c.String(); got != c.want {
			t.Errorf("Card(%d).String()=%q want %q", c.c, got, c.want)
		}
	}
}

// ──────────────────────── infoset enumeration ────────────────────────

// enumerateInfosets walks the entire Leduc game tree and collects all distinct
// infoset keys reachable at decision points. Used for cross-check vs OpenSpiel.
func enumerateInfosets() map[string]struct{} {
	out := make(map[string]struct{})
	var dfs func(s *State)
	dfs = func(s *State) {
		if s.Terminal {
			return
		}
		if s.NeedsPublicCard() {
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl)
			}
			return
		}
		out[s.InfosetKey()] = struct{}{}
		for _, a := range s.LegalActions() {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl)
		}
	}
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(NewState(p0, p1))
		}
	}
	return out
}

func TestInfosetEnumeration(t *testing.T) {
	infosets := enumerateInfosets()
	// Decompose by structure for inspection.
	r1 := 0 // round-1 infosets (no public yet)
	r2 := 0 // round-2 infosets
	for k := range infosets {
		// Key format: "<priv>/<pub|?>/<r1>/<r2>". Round-1 keys have '?' at pub position.
		if len(k) >= 3 && k[2] == '?' {
			r1++
		} else {
			r2++
		}
	}
	t.Logf("total Leduc infosets (current-player view, suit-collapsed): %d (R1=%d, R2=%d)",
		len(infosets), r1, r2)
	// Lock the number — should match OpenSpiel's Leduc info-set count.
	// We don't assert a specific number yet (cross-check vs OpenSpiel Day 2);
	// just record and ensure >0 and reasonable.
	if len(infosets) == 0 {
		t.Fatal("no infosets found")
	}
	if len(infosets) > 10000 {
		t.Errorf("infosets count %d unreasonably large for Leduc", len(infosets))
	}
}

// ──────────────────────── full-tree EV walker ────────────────────────

// walkAllHistories enumerates the entire game tree (private deals, public deal, action sequences)
// and returns total chip flow + count of terminal nodes. Used for engine sanity + lays
// foundation for Day 2 brute-force EV / best-response.
//
// Returns: (totalPayoffP0_summed_over_all_chance, total terminal count, max contrib seen).
func walkAllTerminals(t *testing.T, actionStrategy func(s *State) []float64) (sumP0 float64, terminals int, maxContrib int) {
	t.Helper()
	var dfs func(s *State, reach float64)
	dfs = func(s *State, reach float64) {
		if s.Terminal {
			terminals++
			sumP0 += reach * s.Payoff(P0)
			if s.Contrib[P0] > maxContrib {
				maxContrib = s.Contrib[P0]
			}
			if s.Contrib[P1] > maxContrib {
				maxContrib = s.Contrib[P1]
			}
			return
		}
		if s.NeedsPublicCard() {
			n := 0
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				n++
			}
			share := reach / float64(n)
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl, share)
			}
			return
		}
		legal := s.LegalActions()
		probs := actionStrategy(s)
		if len(probs) != len(legal) {
			t.Fatalf("strategy returned %d probs, want %d legal actions", len(probs), len(legal))
		}
		for i, a := range legal {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl, reach*probs[i])
		}
	}
	// Chance: 30 ordered (priv0, priv1) deals.
	nDeals := 0
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 != p1 {
				nDeals++
			}
		}
	}
	share := 1.0 / float64(nDeals)
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(NewState(p0, p1), share)
		}
	}
	return
}

// Uniform-random strategy: equal prob over legal actions.
func uniformStrategy(s *State) []float64 {
	la := s.LegalActions()
	p := 1.0 / float64(len(la))
	out := make([]float64, len(la))
	for i := range out {
		out[i] = p
	}
	return out
}

func TestUniformStrategyEV(t *testing.T) {
	// Both players uniform random. Symmetric strategies but P0 acts first, so EV may be slightly nonzero.
	ev, terms, maxC := walkAllTerminals(t, uniformStrategy)
	t.Logf("uniform-uniform EV(P0)=%.6f, terminals=%d, maxContrib=%d", ev, terms, maxC)
	// Max contrib sanity: round1(ante+bet+raise=5) + round2(bet+raise=8) = 13.
	const wantMaxContrib = Ante + 2*BetSize1 + 2*BetSize2
	if maxC != wantMaxContrib {
		t.Errorf("maxContrib=%d want %d", maxC, wantMaxContrib)
	}
	// EV should be bounded (not NaN, not exploding); magnitude < 13.
	if ev != ev { // NaN
		t.Errorf("EV is NaN")
	}
	if ev < -13 || ev > 13 {
		t.Errorf("EV=%v out of bounds", ev)
	}
}

// Always-call strategy: never fold, never raise (CheckCall on every decision).
// Reduces to "every hand goes to showdown" — EV should be 0 by symmetry of dealing.
func alwaysCheckCallStrategy(s *State) []float64 {
	la := s.LegalActions()
	out := make([]float64, len(la))
	for i, a := range la {
		if a == ActionCheckCall {
			out[i] = 1
		}
	}
	return out
}

func TestAlwaysCheckCallEV(t *testing.T) {
	// Pure check-check-check-check → every hand goes to showdown. By symmetry of deals,
	// EV(P0) should be exactly 0.
	ev, _, _ := walkAllTerminals(t, alwaysCheckCallStrategy)
	if ev > 1e-9 || ev < -1e-9 {
		t.Errorf("check-call-only EV(P0)=%v want 0 (symmetric)", ev)
	}
}

// ──────────────────────── debug helper ────────────────────────

func TestStateStringDebug(t *testing.T) {
	// Just ensure no panic on String-like dumps; useful for debug.
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionBetRaise)
	_ = fmt.Sprintf("%+v", s)
	_ = s.InfosetKey()
}
