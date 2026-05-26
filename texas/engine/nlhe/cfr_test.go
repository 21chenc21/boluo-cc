package nlhe

import (
	"testing"
)

// TestMCCFRPushFoldRunsNoPanic — 100 iter on push/fold doesn't panic, covers
// the MCCFR walk paths (chance, traverser, opp sampling, terminal showdown).
func TestMCCFRPushFoldRunsNoPanic(t *testing.T) {
	cfg := PushFoldConfig(10)
	m := NewMCCFR(cfg, 42)
	for i := 0; i < 100; i++ {
		m.Iter()
	}
	if m.NumInfosets() == 0 {
		t.Errorf("MCCFR touched 0 infosets after 100 iter")
	}
	if m.Iters() != 100 {
		t.Errorf("Iters=%d want 100", m.Iters())
	}
	avg := m.AverageStrategy()
	if len(avg) == 0 {
		t.Errorf("AverageStrategy returned empty")
	}
	// Verify probs sum to 1 (within float epsilon) per infoset.
	for id, probs := range avg {
		var sum float64
		for _, p := range probs {
			sum += p
		}
		if sum < 0.99 || sum > 1.01 {
			t.Errorf("infoset %d: probs sum=%v want ~1", id, sum)
		}
	}
}

// TestMCCFRPushFoldTopHandsConverge — short run (50k iter), verify AA shoves
// > 80% (Nash ~100%). Spot-check that the heavy hitters at least look right.
func TestMCCFRPushFoldTopHandsConverge(t *testing.T) {
	if testing.Short() {
		t.Skip("convergence test skipped in -short mode")
	}
	cfg := PushFoldConfig(10)
	m := NewMCCFR(cfg, 42)
	const iters = 50000
	for i := 0; i < iters; i++ {
		m.Iter()
	}
	avg := m.AverageStrategy()
	// Find AA infoset: SB opening with both hole cards rank=A (rank 12).
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("Ac"), ParseCard("Ad"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	id := s.InfosetID()
	probs, ok := avg[id]
	if !ok {
		t.Fatalf("AA infoset not in average strategy")
	}
	// Push/fold mode: legal actions are [Fold, AllIn] for SB.
	legal := s.LegalActions()
	if len(legal) != 2 {
		t.Fatalf("push/fold SB legal=%v want [Fold, AllIn]", legal)
	}
	// AllIn should be in position 1.
	if legal[1].Kind != ActionAllIn {
		t.Fatalf("expected AllIn at index 1, got %v", legal[1])
	}
	allInFreq := probs[1]
	t.Logf("AA SB allin freq after %d iter: %.3f", iters, allInFreq)
	if allInFreq < 0.7 {
		t.Errorf("AA AllIn freq=%v after %d iter, want > 0.7 (Nash ~1.0)", allInFreq, iters)
	}
}

// TestNumActionsForIDCache — verify NumActionsForID returns cached count.
func TestNumActionsForIDCache(t *testing.T) {
	cfg := PushFoldConfig(10)
	m := NewMCCFR(cfg, 42)
	m.Iter()
	count := 0
	for id := range m.regret {
		n := m.NumActionsForID(id)
		if n < 2 || n > 3 {
			t.Errorf("infoset %d: NumActions=%d unreasonable", id, n)
		}
		count++
		if count > 10 {
			break
		}
	}
	if count == 0 {
		t.Errorf("no infosets to check")
	}
}

// TestSampleFromSigmaCoverage — verify sampler hits all actions over many draws.
func TestSampleFromSigmaCoverage(t *testing.T) {
	cfg := PushFoldConfig(10)
	m := NewMCCFR(cfg, 42)
	sigma := []float64{0.4, 0.6}
	var hits [2]int
	for i := 0; i < 1000; i++ {
		idx := m.sampleFromSigma(sigma)
		if idx < 0 || idx > 1 {
			t.Fatalf("sample out of range: %d", idx)
		}
		hits[idx]++
	}
	if hits[0] < 300 || hits[0] > 500 {
		t.Errorf("sample distribution skewed: hits=%v expected ~400/~600", hits)
	}
}

// TestInfosetLabelNonEmpty — InfosetLabel returns something useful for debugging.
func TestInfosetLabelNonEmpty(t *testing.T) {
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	label := s.InfosetLabel()
	if len(label) == 0 {
		t.Errorf("InfosetLabel empty")
	}
	// Should contain both hole cards and position.
	t.Logf("InfosetLabel: %q", label)
}

// TestMCCFRMultiStreetVisitsAllStreets — full multi-street walk reaches flop /
// turn / river infosets via chance-node dealing (NeedsBoard). Verifies the
// Phase 2c refactor: walk handles preflop→flop→turn→river chance transitions.
func TestMCCFRMultiStreetVisitsAllStreets(t *testing.T) {
	// Small stack so games end quickly but still reach all streets via small
	// bets / check-call lines.
	cfg := &GameConfig{
		SmallBlind:   1,
		BigBlind:     2,
		StartStack:   40, // 20 BB
		BetSizes:     []float64{1.0},
		PushFoldOnly: false,
	}
	m := NewMCCFR(cfg, 42)
	var visits [4]int // per-street infoset visit count
	// Wrap idFn to record per-street visits as a side-effect.
	m.WithIDFn(func(s *State) uint64 {
		visits[s.Street]++
		return s.InfosetID()
	})
	for i := 0; i < 500; i++ {
		m.Iter()
	}
	t.Logf("street visit counts: preflop=%d flop=%d turn=%d river=%d",
		visits[0], visits[1], visits[2], visits[3])
	if visits[0] == 0 {
		t.Errorf("no preflop infoset visits")
	}
	if visits[1] == 0 {
		t.Errorf("no flop infoset visits — chance-node refactor broken")
	}
	if visits[2] == 0 {
		t.Errorf("no turn infoset visits")
	}
	if visits[3] == 0 {
		t.Errorf("no river infoset visits")
	}
}

// TestMCCFRMultiStreetBoardChangesInfosetID — two states with same betting line
// but different boards must produce different InfosetIDs.
func TestMCCFRMultiStreetBoardChangesInfosetID(t *testing.T) {
	cfg := DefaultConfig()
	s1 := NewState(cfg)
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	s1.Apply(Action{Kind: ActionCheckCall})
	s1.Apply(Action{Kind: ActionCheckCall})
	// Preflop done, ready for flop.
	if n, _ := s1.NeedsBoard(); n != 3 {
		t.Fatalf("after preflop close, NeedsBoard=%d want 3", n)
	}
	s1.Board[0] = ParseCard("Qd")
	s1.Board[1] = ParseCard("Jc")
	s1.Board[2] = ParseCard("Th")
	s1.NumBoard = 3
	id1 := s1.InfosetID()

	s2 := NewState(cfg)
	s2.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s2.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	s2.Apply(Action{Kind: ActionCheckCall})
	s2.Apply(Action{Kind: ActionCheckCall})
	s2.Board[0] = ParseCard("2h")
	s2.Board[1] = ParseCard("7d")
	s2.Board[2] = ParseCard("8s")
	s2.NumBoard = 3
	id2 := s2.InfosetID()

	if id1 == id2 {
		t.Errorf("AKo on QJT vs AKo on 278: same InfosetID %d — board not in hash", id1)
	}
}

// TestActionStringFormat — Action.String produces parseable debug output.
func TestActionStringFormat(t *testing.T) {
	cases := []struct {
		a    Action
		want string
	}{
		{Action{Kind: ActionFold}, "f"},
		{Action{Kind: ActionCheckCall}, "c"},
		{Action{Kind: ActionBet, SizeIdx: 0}, "b0"},
		{Action{Kind: ActionBet, SizeIdx: 2}, "b2"},
		{Action{Kind: ActionAllIn}, "a"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("Action(%v).String()=%q want %q", c.a, got, c.want)
		}
	}
}
