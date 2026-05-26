package nlhe

import (
	"testing"
)

// ──────────────────────── initial state ────────────────────────

func TestNewStateInitialBlinds(t *testing.T) {
	s := NewState(DefaultConfig())
	if s.Wagered[P0] != 1 || s.Wagered[P1] != 2 {
		t.Errorf("Wagered=%v want [1,2]", s.Wagered)
	}
	if s.Stacks[P0] != 199 || s.Stacks[P1] != 198 {
		t.Errorf("Stacks=%v want [199,198]", s.Stacks)
	}
	if s.Pot() != 3 {
		t.Errorf("Pot=%d want 3 (SB+BB)", s.Pot())
	}
	if s.Cur != P0 {
		t.Errorf("Cur=%d want P0 (SB acts first preflop)", s.Cur)
	}
	if s.ToCall() != 1 {
		t.Errorf("ToCall=%d want 1 (BB - SB)", s.ToCall())
	}
}

// ──────────────────────── push/fold smoke ────────────────────────

func TestPushFoldSBFolds(t *testing.T) {
	s := NewState(PushFoldConfig(10)) // 10 BB stacks
	s.SetHole(P0, ParseCard("2c"), ParseCard("7d"))
	s.SetHole(P1, ParseCard("As"), ParseCard("Ks"))
	if s.Cur != P0 {
		t.Fatalf("Cur=%d want P0", s.Cur)
	}
	s.Apply(Action{Kind: ActionFold})
	if !s.Terminal {
		t.Fatal("not terminal")
	}
	if s.Payoff(P0) != -1 { // SB loses just the small blind
		t.Errorf("SB fold payoff = %d, want -1", s.Payoff(P0))
	}
	if s.Payoff(P1) != +1 {
		t.Errorf("BB on SB fold = %d, want +1", s.Payoff(P1))
	}
}

func TestPushFoldShoveCallShowdown(t *testing.T) {
	// SB shoves, BB calls, showdown with AA vs 72o on dry board.
	cfg := PushFoldConfig(10)
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))

	s.Apply(Action{Kind: ActionAllIn})
	if s.Cur != P1 {
		t.Fatalf("after SB shove: Cur=%d want P1", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB calls

	// Both all-in: state requires full board to showdown.
	if !s.Terminal {
		t.Fatalf("expected terminal after BB call (both all-in)")
	}
	n, needs := s.NeedsBoard()
	if !needs || n != 5 {
		t.Fatalf("NeedsBoard=%d %v, want 5 true", n, needs)
	}
	// Deal random board (no AA bricks though).
	s.SetBoard(
		ParseCard("3c"), ParseCard("8d"), ParseCard("Th"),
		ParseCard("Jc"), ParseCard("Qd"),
	)
	// AA should win.
	if s.Payoff(P0) <= 0 {
		t.Errorf("AA payoff=%d want > 0", s.Payoff(P0))
	}
	if s.Payoff(P0)+s.Payoff(P1) != 0 {
		t.Errorf("zero-sum violated")
	}
}

func TestPushFoldShoveFoldNoBoardNeeded(t *testing.T) {
	cfg := PushFoldConfig(10)
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))

	s.Apply(Action{Kind: ActionAllIn})
	s.Apply(Action{Kind: ActionFold})
	if !s.Terminal || s.FoldedBy != P1 {
		t.Fatalf("expected P1 fold terminal")
	}
	if s.Payoff(P0) != 2 { // SB wins BB's blind (2 chips)
		t.Errorf("SB on BB fold payoff = %d, want +2", s.Payoff(P0))
	}
}

// ──────────────────────── legal action enumeration ────────────────────────

func TestLegalActionsOpening(t *testing.T) {
	s := NewState(DefaultConfig())
	la := s.LegalActions()
	// At opening: Fold, CheckCall (= call BB), and bet sizes + AllIn.
	// 1 fold + 1 call + 3 bet sizes + 1 allin = 6.
	if len(la) < 4 {
		t.Errorf("opening LegalActions=%d, want >= 4", len(la))
	}
	// Must contain Fold and CheckCall.
	var foundFold, foundCall, foundAllIn bool
	for _, a := range la {
		if a.Kind == ActionFold {
			foundFold = true
		}
		if a.Kind == ActionCheckCall {
			foundCall = true
		}
		if a.Kind == ActionAllIn {
			foundAllIn = true
		}
	}
	if !foundFold || !foundCall {
		t.Errorf("missing core actions in %v", la)
	}
	if !foundAllIn {
		t.Errorf("AllIn should always be in legal opening: %v", la)
	}
}

func TestLegalActionsPushFoldOnly(t *testing.T) {
	s := NewState(PushFoldConfig(10))
	la := s.LegalActions()
	// Push/fold mode: only Fold, CheckCall, AllIn (no Bet sizes).
	for _, a := range la {
		if a.Kind == ActionBet {
			t.Errorf("push-fold mode should not have Bet, got %v", a)
		}
	}
	// Must have AllIn.
	hasAllIn := false
	for _, a := range la {
		if a.Kind == ActionAllIn {
			hasAllIn = true
		}
	}
	if !hasAllIn {
		t.Errorf("push-fold mode missing AllIn: %v", la)
	}
}

// ──────────────────────── street transition ────────────────────────

func TestPreflopCallToFlop(t *testing.T) {
	// SB limps (call BB), BB checks → flop.
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("Ac"), ParseCard("Kd"))
	s.SetHole(P1, ParseCard("2s"), ParseCard("7h"))
	s.Apply(Action{Kind: ActionCheckCall}) // SB calls
	s.Apply(Action{Kind: ActionCheckCall}) // BB checks → flop

	if s.Street != StreetFlop {
		t.Errorf("Street=%d want Flop=1", s.Street)
	}
	n, needs := s.NeedsBoard()
	if !needs || n != 3 {
		t.Errorf("NeedsBoard=%d,%v want 3,true", n, needs)
	}
	if s.Cur != P1 { // postflop BB acts first
		t.Errorf("postflop first actor = %d, want P1 (BB)", s.Cur)
	}
	if s.Wagered[P0] != 2 || s.Wagered[P1] != 2 {
		t.Errorf("Wagered=%v want [2,2]", s.Wagered)
	}
}
