package nlhe

import (
	"math/rand"
	"testing"
)

// ──────────────────────── helpers ────────────────────────

// playSeq applies a sequence of actions in order, dealing default board cards
// when the engine requests them. Returns the state. Aborts on terminal mid-seq.
func playSeq(t *testing.T, s *State, actions ...Action) {
	t.Helper()
	// Default board: T♠ J♠ Q♠ K♦ A♥ — gives diverse hand combos.
	board := []Card{
		ParseCard("Ts"), ParseCard("Jc"), ParseCard("Qd"),
		ParseCard("Kh"), ParseCard("Ah"),
	}
	for i, a := range actions {
		if s.Terminal {
			t.Fatalf("seq idx %d: state already terminal", i)
		}
		n, needs := s.NeedsBoard()
		_ = n
		_ = needs
		s.Apply(a)
		// After applying, if state moved to a new street, the engine just resets
		// BetThisStreet — it does NOT deal board. Caller deals.
	}
	// Deal board on demand AFTER walking actions.
	n, needs := s.NeedsBoard()
	if needs {
		// Skip cards already dealt.
		used := map[Card]bool{
			s.Hole[P0][0]: true, s.Hole[P0][1]: true,
			s.Hole[P1][0]: true, s.Hole[P1][1]: true,
		}
		for i := uint8(0); i < s.NumBoard; i++ {
			used[s.Board[i]] = true
		}
		var toDeal []Card
		for _, c := range board {
			if used[c] || len(toDeal) >= n {
				continue
			}
			used[c] = true
			toDeal = append(toDeal, c)
		}
		if len(toDeal) < n {
			t.Fatalf("playSeq: cannot find %d non-conflicting cards", n)
		}
		s.SetBoard(toDeal...)
	}
}

func mkHUNL(t *testing.T) *State {
	t.Helper()
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))
	return s
}

// ──────────────────────── preflop progression ────────────────────────

func TestPreflopRaiseCallToFlop(t *testing.T) {
	s := mkHUNL(t)
	// SB raises pot (Bet sizeIdx=1 = 1pot from default {0.5,1,2}).
	// After SB raise, BB calls → flop.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.Street != StreetPreflop {
		t.Fatalf("Street=%d want preflop", s.Street)
	}
	if s.Cur != P1 {
		t.Fatalf("after SB raise: Cur=%d want P1", s.Cur)
	}
	if s.BetThisStreet[P0] == s.BetThisStreet[P1] {
		t.Errorf("after raise, bets should not match: %v", s.BetThisStreet)
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB calls
	if s.Street != StreetFlop {
		t.Errorf("after BB call: Street=%d want Flop", s.Street)
	}
	n, needs := s.NeedsBoard()
	if !needs || n != 3 {
		t.Errorf("NeedsBoard=%d %v want 3 true", n, needs)
	}
	if s.BetThisStreet[P0] != 0 || s.BetThisStreet[P1] != 0 {
		t.Errorf("after street advance: BetThisStreet=%v want [0,0]", s.BetThisStreet)
	}
	if s.Cur != P1 {
		t.Errorf("flop first actor=%d want P1 (BB OOP postflop)", s.Cur)
	}
}

func TestPreflopRaiseRaiseCallToFlop(t *testing.T) {
	s := mkHUNL(t)
	// SB raise (1pot), BB re-raise (1pot), SB call.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.Cur != P0 {
		t.Fatalf("after BB raise: Cur=%d want P0", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Street != StreetFlop {
		t.Errorf("after SB call: Street=%d want Flop", s.Street)
	}
	if s.Wagered[P0] != s.Wagered[P1] {
		t.Errorf("after call: wagered should match got %v", s.Wagered)
	}
}

// ──────────────────────── multi-street ────────────────────────

func TestFullFourStreetCheckCheckToShowdown(t *testing.T) {
	// SB limps (call BB), BB checks → flop.
	// Flop check check → turn.
	// Turn check check → river.
	// River check check → showdown.
	s := mkHUNL(t)
	playSeq(t, s,
		Action{Kind: ActionCheckCall}, // SB calls BB
		Action{Kind: ActionCheckCall}, // BB checks → flop
	)
	if s.Street != StreetFlop {
		t.Fatalf("Street=%d want flop", s.Street)
	}
	// Flop check check.
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Cur != P0 {
		t.Errorf("flop after BB check: Cur=%d want P0", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Street != StreetTurn {
		t.Fatalf("Street=%d want turn", s.Street)
	}
	// Need 1 card for turn.
	n, needs := s.NeedsBoard()
	if !needs || n != 1 {
		t.Errorf("turn NeedsBoard=%d %v", n, needs)
	}
	s.SetBoard(ParseCard("Kh"))
	// Turn check check → river.
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Street != StreetRiver {
		t.Fatalf("Street=%d want river", s.Street)
	}
	s.SetBoard(ParseCard("Ah"))
	// River check check → showdown.
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	if !s.Terminal {
		t.Fatal("river check-check should be terminal")
	}
	if s.FoldedBy != NoPlayer {
		t.Errorf("showdown but FoldedBy=%d", s.FoldedBy)
	}
	// SB has AK on TJQKA → straight to A. BB has 72 on TJQKA → straight to A.
	// They tie? Or SB wins? Actually wait — 7c2d does NOT make straight (need 3,4,5,6,7,8 etc).
	// Board is TJQKA (broadway). SB has A → wins with broadway straight to A.
	// BB has 7,2,T,J,Q,K,A → A-K-Q-J-T = broadway too! Both have broadway from board.
	// → split pot (tie). Payoff = 0 each.
	if s.Payoff(P0) != 0 {
		t.Errorf("both broadway from board → tie. P0 payoff=%d want 0", s.Payoff(P0))
	}
}

func TestFlopBetCallToTurn(t *testing.T) {
	s := mkHUNL(t)
	playSeq(t, s,
		Action{Kind: ActionCheckCall}, // SB limp
		Action{Kind: ActionCheckCall}, // BB check → flop
	)
	if s.Street != StreetFlop {
		t.Fatalf("not flop: %d", s.Street)
	}
	// BB bets pot, SB calls.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	if s.Cur != P0 {
		t.Fatalf("after BB bet flop: Cur=%d want P0", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall})
	if s.Street != StreetTurn {
		t.Errorf("after SB call: Street=%d want turn", s.Street)
	}
}

// ──────────────────────── all-in corner cases ────────────────────────

func TestAllInPreflopRaiseCall(t *testing.T) {
	s := mkHUNL(t)
	s.Apply(Action{Kind: ActionAllIn})
	if s.Cur != P1 {
		t.Fatalf("after SB all-in: Cur=%d want P1", s.Cur)
	}
	if !s.AllIn[P0] {
		t.Errorf("P0 should be all-in")
	}
	if s.AllIn[P1] {
		t.Errorf("P1 should NOT be all-in yet")
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB calls all-in
	if !s.Terminal {
		t.Errorf("both all-in should be terminal")
	}
	n, needs := s.NeedsBoard()
	if !needs || n != 5 {
		t.Errorf("after both all-in preflop: NeedsBoard=%d %v want 5 true", n, needs)
	}
}

func TestAllInOnFlop(t *testing.T) {
	s := mkHUNL(t)
	playSeq(t, s,
		Action{Kind: ActionCheckCall}, // SB limp
		Action{Kind: ActionCheckCall}, // BB check → flop
	)
	// On flop, BB (P1) acts first (HUNL postflop convention).
	if s.Cur != P1 {
		t.Fatalf("flop first actor = %d, want P1", s.Cur)
	}
	s.Apply(Action{Kind: ActionAllIn}) // BB shoves
	if !s.AllIn[P1] {
		t.Fatal("BB should be all-in after shove")
	}
	if s.AllIn[P0] {
		t.Fatal("SB should not be all-in yet")
	}
	s.Apply(Action{Kind: ActionCheckCall}) // SB calls
	if !s.Terminal {
		t.Errorf("after flop allin+call should be terminal")
	}
	// Need 2 more cards (turn + river).
	n, needs := s.NeedsBoard()
	if !needs || n != 2 {
		t.Errorf("after flop allin: NeedsBoard=%d %v want 2 true", n, needs)
	}
}

// ──────────────────────── min-raise enforcement ────────────────────────

func TestMinRaiseSize(t *testing.T) {
	s := mkHUNL(t)
	// SB raises 0.5pot. Then BB tries to raise smaller than minimum.
	// Min raise = previous raise size = at least BB.
	// Sequence: SB bet sizeIdx=0 (0.5pot). pot was 3 (SB1+BB2). 0.5pot raise = 1.5 → call 1 + raise 1.5 = total 2.5
	s.Apply(Action{Kind: ActionBet, SizeIdx: 0})
	la := s.LegalActions()
	// All bet sizes BB would pick must satisfy minRaise. Verify no Bet with smaller-than-min raise.
	for _, a := range la {
		if a.Kind != ActionBet {
			continue
		}
		// Compute raise size implied by this action.
		frac := s.Cfg.BetSizes[a.SizeIdx]
		toCall := s.ToCall()
		pot := s.Pot() + toCall
		raiseTo := s.BetThisStreet[s.Cur] + toCall + int(float64(pot)*frac)
		// raiseTo must be ≥ minRaise.
		minRaise := s.minRaiseTo()
		if raiseTo < minRaise {
			t.Errorf("LegalActions contains under-minraise: %v raiseTo=%d minRaise=%d",
				a, raiseTo, minRaise)
		}
	}
}

// ──────────────────────── conservation invariants ────────────────────────

func TestChipsConservation(t *testing.T) {
	// At any state, Stacks[P0] + Stacks[P1] + Pot() == 2 * StartStack.
	cfg := DefaultConfig()
	scenarios := [][]Action{
		{Action{Kind: ActionFold}},
		{Action{Kind: ActionCheckCall}, Action{Kind: ActionCheckCall}},
		{Action{Kind: ActionBet, SizeIdx: 1}, Action{Kind: ActionCheckCall}},
		{Action{Kind: ActionBet, SizeIdx: 1}, Action{Kind: ActionBet, SizeIdx: 1}, Action{Kind: ActionCheckCall}},
		{Action{Kind: ActionAllIn}, Action{Kind: ActionCheckCall}},
		{Action{Kind: ActionAllIn}, Action{Kind: ActionFold}},
	}
	for i, seq := range scenarios {
		s := NewState(cfg)
		s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
		s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))
		for _, a := range seq {
			if s.Terminal {
				break
			}
			s.Apply(a)
		}
		total := s.Stacks[P0] + s.Stacks[P1] + s.Pot()
		want := 2 * cfg.StartStack
		if total != want {
			t.Errorf("seq %d: conservation violated total=%d want %d (stacks=%v pot=%d)",
				i, total, want, s.Stacks, s.Pot())
		}
	}
}

func TestPayoffZeroSum(t *testing.T) {
	cfg := DefaultConfig()
	scenarios := [][]Action{
		{Action{Kind: ActionFold}},
		{Action{Kind: ActionBet, SizeIdx: 1}, Action{Kind: ActionFold}},
		{Action{Kind: ActionBet, SizeIdx: 1}, Action{Kind: ActionCheckCall}},
		{Action{Kind: ActionAllIn}, Action{Kind: ActionFold}},
	}
	for i, seq := range scenarios {
		s := NewState(cfg)
		s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
		s.SetHole(P1, ParseCard("7c"), ParseCard("2d"))
		for _, a := range seq {
			if s.Terminal {
				break
			}
			s.Apply(a)
		}
		if !s.Terminal {
			continue
		}
		// Skip showdown scenarios (need board)
		if s.FoldedBy == NoPlayer {
			continue
		}
		if s.Payoff(P0)+s.Payoff(P1) != 0 {
			t.Errorf("seq %d zero-sum: %d + %d != 0", i, s.Payoff(P0), s.Payoff(P1))
		}
	}
}

// ──────────────────────── snapshot restore stress ────────────────────────

func TestSnapshotRestoreAcrossStreets(t *testing.T) {
	s := mkHUNL(t)
	snap0 := s.Snapshot()
	// Walk to river.
	playSeq(t, s,
		Action{Kind: ActionCheckCall},
		Action{Kind: ActionCheckCall},
	)
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall}) // flop done
	s.SetBoard(ParseCard("Kh"))
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall}) // turn done
	if s.Street != StreetRiver {
		t.Fatalf("not river: %d", s.Street)
	}
	// Restore from preflop.
	s.Restore(snap0)
	if s.Street != StreetPreflop || s.NumBoard != 0 || s.Terminal {
		t.Errorf("Restore failed: street=%d numboard=%d terminal=%v",
			s.Street, s.NumBoard, s.Terminal)
	}
	if s.Stacks[P0] != cfg().StartStack-1 || s.Stacks[P1] != cfg().StartStack-2 {
		t.Errorf("Restore left wrong stacks: %v", s.Stacks)
	}
}

func cfg() *GameConfig { return DefaultConfig() }

// ──────────────────────── random play stress ────────────────────────

func TestRandomPlayNoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	const trials = 5000
	for trial := 0; trial < trials; trial++ {
		s := NewState(DefaultConfig())
		// Deal 4 distinct cards.
		var used [DeckSize]bool
		var picked [4]Card
		for i := 0; i < 4; i++ {
			for {
				c := Card(rng.Intn(DeckSize))
				if !used[c] {
					picked[i] = c
					used[c] = true
					break
				}
			}
		}
		s.SetHole(P0, picked[0], picked[1])
		s.SetHole(P1, picked[2], picked[3])

		// Walk randomly until terminal, dealing board when needed.
		steps := 0
		for !s.Terminal {
			if n, needs := s.NeedsBoard(); needs {
				dealt := 0
				for c := Card(0); c < DeckSize && dealt < n; c++ {
					if !used[c] {
						s.Board[s.NumBoard] = c
						s.NumBoard++
						used[c] = true
						dealt++
					}
				}
				continue
			}
			la := s.LegalActions()
			if len(la) == 0 {
				t.Fatalf("trial %d step %d: no legal actions, not terminal. %s", trial, steps, s.summary())
			}
			a := la[rng.Intn(len(la))]
			s.Apply(a)
			steps++
			if steps > 200 {
				t.Fatalf("trial %d: too many steps without termination, hist=%v", trial, s.Hist)
			}
		}
		// Conservation invariant.
		if s.FoldedBy != NoPlayer || s.NumBoard == 5 {
			total := s.Stacks[P0] + s.Stacks[P1] + s.Pot()
			want := 2 * s.Cfg.StartStack
			if total != want {
				t.Errorf("trial %d: conservation violated total=%d want %d", trial, total, want)
			}
			// Zero-sum payoff if showdown completable.
			if s.FoldedBy != NoPlayer || s.NumBoard == 5 {
				p0 := s.Payoff(P0)
				p1 := s.Payoff(P1)
				if p0+p1 != 0 {
					t.Errorf("trial %d: payoff zero-sum: %d+%d", trial, p0, p1)
				}
			}
		}
	}
}
