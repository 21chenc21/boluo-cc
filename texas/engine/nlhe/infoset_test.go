package nlhe

import (
	"testing"
)

func TestInfosetIDSuitInvariance(t *testing.T) {
	// Same hole rank, different suits ordering → same canonical hole pair, but
	// suit identities differ, so IDs CAN differ (suits matter for flush draws).
	// Just verify two states with IDENTICAL holes give same ID.
	s1 := NewState(DefaultConfig())
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	s2 := NewState(DefaultConfig())
	s2.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s2.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	if s1.InfosetID() != s2.InfosetID() {
		t.Errorf("identical states → different IDs: %d vs %d", s1.InfosetID(), s2.InfosetID())
	}
}

func TestInfosetIDHoleOrderInvariance(t *testing.T) {
	// AsKh vs KhAs — same hand, just different deal order. ID must be same.
	s1 := NewState(DefaultConfig())
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	s2 := NewState(DefaultConfig())
	s2.SetHole(P0, ParseCard("Kh"), ParseCard("As"))
	s2.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	if s1.InfosetID() != s2.InfosetID() {
		t.Errorf("hole order should not matter: %d vs %d", s1.InfosetID(), s2.InfosetID())
	}
}

func TestInfosetIDOpponentHoleNotLeaked(t *testing.T) {
	// At P0's decision, P0's view should NOT depend on P1's hole.
	s1 := NewState(DefaultConfig())
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	s2 := NewState(DefaultConfig())
	s2.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s2.SetHole(P1, ParseCard("Ts"), ParseCard("Jh"))

	if s1.InfosetID() != s2.InfosetID() {
		t.Errorf("opponent hole leaked into actor's infoset: %d vs %d",
			s1.InfosetID(), s2.InfosetID())
	}
}

func TestInfosetIDChangesWithHistory(t *testing.T) {
	// After actions, ID must differ from start state.
	s1 := NewState(DefaultConfig())
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("7d"))
	id1 := s1.InfosetID()

	s1.Apply(Action{Kind: ActionCheckCall}) // SB calls
	// Now Cur = P1, infoset key includes P1's view + history "c"
	id2 := s1.InfosetID()
	if id1 == id2 {
		t.Errorf("ID didn't change after action")
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	s := NewState(DefaultConfig())
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("7d"))

	snap := s.Snapshot()
	id1 := s.InfosetID()

	s.Apply(Action{Kind: ActionCheckCall}) // SB call
	s.Apply(Action{Kind: ActionCheckCall}) // BB check → flop
	s.SetBoard(ParseCard("Th"), ParseCard("Jc"), ParseCard("Qd"))
	idMid := s.InfosetID()

	if idMid == id1 {
		t.Error("ID unchanged after street advance")
	}

	s.Restore(snap)
	id2 := s.InfosetID()
	if id1 != id2 {
		t.Errorf("Restore failed to restore ID: %d → %d", id1, id2)
	}
	if s.Terminal {
		t.Error("Restore left state terminal")
	}
	if s.NumBoard != 0 {
		t.Errorf("Restore left NumBoard=%d, want 0", s.NumBoard)
	}
	if s.Street != StreetPreflop {
		t.Errorf("Restore left Street=%d, want preflop", s.Street)
	}
}
