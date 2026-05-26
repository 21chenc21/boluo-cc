package nlhe

import (
	"testing"
)

// ──────────────────────── bet sizing chain edges ────────────────────────

// TestThreeBetPreflop — SB bet 1pot, BB raise 1pot, SB call.
// Verifies LastRaiseSize tracking and chip flow.
func TestThreeBetPreflop(t *testing.T) {
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))

	// SB bet sizeIdx=1 (1pot). pot=3, ToCall=1.
	// raiseTo = 1 + 1 + 4*1 = 6. SB stack 199→194. BetThisStreet[SB]=6.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.BetThisStreet[P0] != 6 {
		t.Errorf("SB BetThisStreet after 1pot bet = %d, want 6", s.BetThisStreet[P0])
	}
	if s.Stacks[P0] != 194 {
		t.Errorf("SB stack after 1pot bet = %d, want 194", s.Stacks[P0])
	}
	if s.LastRaiseSize != 4 { // 6 - 2 = 4
		t.Errorf("LastRaiseSize after SB bet = %d, want 4", s.LastRaiseSize)
	}

	// BB raise sizeIdx=1 (1pot). ToCall=4. pot AFTER call = 8+4=12.
	// raiseTo = BetThisStreet[BB] + toCall + pot*1 = 2 + 4 + 12 = 18.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.BetThisStreet[P1] != 18 {
		t.Errorf("BB 3-bet BetThisStreet = %d, want 18", s.BetThisStreet[P1])
	}
	if s.LastRaiseSize != 12 { // 18 - 6 = 12
		t.Errorf("LastRaiseSize after BB 3-bet = %d, want 12", s.LastRaiseSize)
	}

	// SB call. ToCall=12. SB BetThisStreet=18. Then street transition → reset BetThisStreet.
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Street != StreetFlop {
		t.Errorf("after call: Street=%d want flop", s.Street)
	}
	if s.BetThisStreet[P0] != 0 || s.BetThisStreet[P1] != 0 {
		t.Errorf("post-street BetThisStreet should reset: %v", s.BetThisStreet)
	}
	// Wagered: SB=1+5+12=18, BB=2+16=18.
	if s.Wagered[P0] != 18 || s.Wagered[P1] != 18 {
		t.Errorf("Wagered after 3-bet call = %v, want [18,18]", s.Wagered)
	}
	if s.Stacks[P0] != 182 || s.Stacks[P1] != 182 {
		t.Errorf("stacks after 3-bet call = %v, want [182,182]", s.Stacks)
	}
}

// TestFourBetPreflop — SB bet 1pot, BB raise 1pot, SB raise 1pot, BB call.
func TestFourBetPreflop(t *testing.T) {
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))

	s.Apply(Action{Kind: ActionBet, SizeIdx: 1}) // SB bet → 6
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1}) // BB 3-bet → 18
	// SB 4-bet sizeIdx=1: ToCall=12, pot after call=(6+18)+12=36, raiseTo=6+12+36=54.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.BetThisStreet[P0] != 54 {
		t.Errorf("4-bet BetThisStreet[P0]=%d, want 54", s.BetThisStreet[P0])
	}
	if s.LastRaiseSize != 36 { // 54-18
		t.Errorf("4-bet LastRaiseSize=%d, want 36", s.LastRaiseSize)
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB call
	if s.Street != StreetFlop {
		t.Errorf("after BB 4-bet call: not on flop")
	}
}

// ──────────────────────── under-stack edge cases ────────────────────────

// TestCheckCallWithInsufficientStack — what happens when stack < toCall.
// In real poker: implicit all-in for whatever's left.
// In my engine: ❓ — let's see.
func TestCheckCallWithInsufficientStack(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))
	// Hack: shrink BB stack to be very small.
	s.Stacks[P1] = 1 // BB has only 1 chip after the BB blind.

	// SB raises pot (sizeIdx=1) → raiseTo = 6. Now BB has BetThisStreet=2, stack=1, ToCall=4.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.Cur != P1 {
		t.Fatalf("not BB's turn: %d", s.Cur)
	}
	toCall := s.ToCall()
	t.Logf("BB facing ToCall=%d with stack=%d", toCall, s.Stacks[P1])

	// LegalActions: what does engine offer?
	la := s.LegalActions()
	t.Logf("BB LegalActions with insufficient stack: %v", la)

	// Try CheckCall — if engine has bug, stack goes negative.
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Stacks[P1] < 0 {
		t.Errorf("🚨 BUG: BB stack went NEGATIVE after CheckCall with insufficient stack: %d",
			s.Stacks[P1])
	}
	if !s.AllIn[P1] && s.Stacks[P1] == 0 {
		t.Errorf("BB stack=0 but AllIn not set")
	}
	t.Logf("BB stack after CheckCall: %d, AllIn=%v", s.Stacks[P1], s.AllIn[P1])
}

// TestUnderCallAllIn — BB all-in for less than SB's bet should NOT reopen action.
// SB bets 100, BB shoves for 50 (all-in but less than SB's bet).
// Standard poker rule: SB cannot raise again; hand goes to showdown.
func TestUnderCallAllIn(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))
	// Set up: BB starts with very small stack (5 chips after BB blind).
	s.Stacks[P1] = 5

	// SB raises to a large amount (sizeIdx=2, 2pot): raiseTo = 1+1+4*2 = 10.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 2})
	if s.BetThisStreet[P0] != 10 {
		t.Fatalf("SB bet ended at %d, want 10", s.BetThisStreet[P0])
	}

	// BB all-in for 5 (current 2 + remaining 5 = 7 total BetThisStreet, less than SB's 10).
	s.Apply(Action{Kind: ActionAllIn})
	if !s.AllIn[P1] {
		t.Fatal("BB should be all-in")
	}
	if s.BetThisStreet[P1] != 7 {
		t.Errorf("BB BetThisStreet = %d, want 7 (2 blind + 5 all-in)", s.BetThisStreet[P1])
	}

	// Engine behavior: SB's BetThisStreet > BB's. Standard rule says hand goes to
	// showdown (no reopen), BUT my engine routes to completeStreetOrAdvanceTurn
	// which checks betsMatched=false → toggles turn to SB → SB gets to act again.
	// This is INCORRECT per poker rules.
	t.Logf("After BB under-call allin: Terminal=%v Cur=%d BetThisStreet=%v",
		s.Terminal, s.Cur, s.BetThisStreet)
	if !s.Terminal {
		t.Errorf("🚨 BUG: BB all-in for less should end action (advance to showdown), engine has Cur=%d not terminal",
			s.Cur)
	}
}
