package leduc

import (
	"testing"
)

// TestSnapshotRestoreRoundTrip — Snapshot/Restore round-trips through random play.
// At each decision, snapshot, apply, recurse 1 step, restore — must match pre-action.
func TestSnapshotRestoreRoundTrip(t *testing.T) {
	scenarios := [][]Action{
		{ActionCheckCall, ActionCheckCall, ActionCheckCall, ActionCheckCall},
		{ActionBetRaise, ActionCheckCall, ActionBetRaise, ActionCheckCall},
		{ActionBetRaise, ActionBetRaise, ActionCheckCall, ActionBetRaise, ActionBetRaise, ActionCheckCall},
		{ActionCheckCall, ActionBetRaise, ActionFold},
	}
	for _, seq := range scenarios {
		s := NewState(MakeCard(0, 0), MakeCard(2, 0))
		pub := MakeCard(1, 0)
		for _, a := range seq {
			if s.Terminal {
				break
			}
			if s.NeedsPublicCard() {
				s.SetPublic(pub)
			}
			// Snapshot, apply, immediately restore — state should be exactly back.
			snap := s.Snapshot()
			before := s.Clone()
			s.Apply(a)
			s.Restore(snap)
			if !statesEqual(s, before) {
				t.Errorf("seq=%v action=%d: restore did not roundtrip", seq, a)
			}
			// Now apply for real and continue.
			s.Apply(a)
		}
	}
}

// TestSnapshotRestoreOverSetPublic — Snapshot+SetPublic+Restore must unset public.
func TestSnapshotRestoreOverSetPublic(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall)
	s.Apply(ActionCheckCall) // round 1 ended, needs public
	snap := s.Snapshot()
	s.SetPublic(MakeCard(2, 0))
	if !s.HasPub {
		t.Fatal("setpub failed")
	}
	s.Restore(snap)
	if s.HasPub {
		t.Errorf("Restore did not unset HasPub")
	}
	if s.Pub != NoCard {
		t.Errorf("Restore did not unset Pub: %v", s.Pub)
	}
}

func statesEqual(a, b *State) bool {
	if a.Round != b.Round || a.NumRaises != b.NumRaises || a.ToCall != b.ToCall || a.Cur != b.Cur {
		return false
	}
	if a.Contrib != b.Contrib {
		return false
	}
	if a.Terminal != b.Terminal || a.FoldedBy != b.FoldedBy {
		return false
	}
	if a.Pub != b.Pub || a.HasPub != b.HasPub {
		return false
	}
	if len(a.Hist[0]) != len(b.Hist[0]) || len(a.Hist[1]) != len(b.Hist[1]) {
		return false
	}
	for r := 0; r < 2; r++ {
		for i := range a.Hist[r] {
			if a.Hist[r][i] != b.Hist[r][i] {
				return false
			}
		}
	}
	return true
}

// TestInfosetIDUnique — every distinct (priv, pub, hist) infoset gets a unique ID.
// Verifies the bit-packing is injective.
func TestInfosetIDUnique(t *testing.T) {
	idToKey := make(map[uint64]string)
	collisions := 0

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
		id := s.InfosetID()
		key := s.InfosetKey()
		if prevKey, seen := idToKey[id]; seen {
			if prevKey != key {
				t.Errorf("ID collision: id=%d key1=%q key2=%q", id, prevKey, key)
				collisions++
			}
		} else {
			idToKey[id] = key
		}
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
	t.Logf("InfosetID: %d unique IDs covered (want 288 = Leduc infoset count)", len(idToKey))
	if len(idToKey) != 288 {
		t.Errorf("InfosetID coverage: got %d unique want 288", len(idToKey))
	}
}

// TestInfosetIDStable — same state visited via different paths gives same ID.
// e.g. suit-symmetric privates with same rank should collapse.
func TestInfosetIDStable(t *testing.T) {
	// P0 with J♠ vs P0 with J♥ should give same ID at start of game.
	a := NewState(MakeCard(0, 0), MakeCard(2, 0))
	b := NewState(MakeCard(0, 1), MakeCard(2, 0))
	if a.InfosetID() != b.InfosetID() {
		t.Errorf("ID not suit-invariant: %d vs %d", a.InfosetID(), b.InfosetID())
	}
}
