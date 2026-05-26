package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// tinyMultiStreetBuckets — fast bucket build for tests. ~0.5s total.
//
// Tiny coverage means most postflop (hole, board) classes are unseen, so we
// enable MCSamplesFallback to get real bucket assignment via nearest-center
// at lookup time (matches production path for unseen classes).
func tinyMultiStreetBuckets(t *testing.T) *MultiStreetBuckets {
	t.Helper()
	return &MultiStreetBuckets{
		Preflop:           Build(10, 2000, 42),
		Flop:              BuildStreet(3, 10, 2000, 50, 42),
		Turn:              BuildStreet(4, 10, 2000, 50, 42),
		River:             BuildStreet(5, 10, 2000, 50, 42),
		MCSamplesFallback: 100,
		FallbackSeed:      42,
	}
}

// TestMultiStreetIDDeterministic — same state → same ID.
func TestMultiStreetIDDeterministic(t *testing.T) {
	b := tinyMultiStreetBuckets(t)
	cfg := nlhe.DefaultConfig()
	s := nlhe.NewState(cfg)
	s.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
	s.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
	id1 := b.ID(s)
	id2 := b.ID(s)
	if id1 != id2 {
		t.Errorf("ID not deterministic: %d vs %d", id1, id2)
	}
}

// TestMultiStreetIDDifferentStreetsDiffer — same hole+history, different street
// (with board) should yield different ID.
func TestMultiStreetIDDifferentStreetsDiffer(t *testing.T) {
	b := tinyMultiStreetBuckets(t)
	cfg := nlhe.DefaultConfig()
	mkState := func(numBoard int) *nlhe.State {
		s := nlhe.NewState(cfg)
		s.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
		s.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
		if numBoard >= 3 {
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Board[0] = nlhe.ParseCard("Qd")
			s.Board[1] = nlhe.ParseCard("Jc")
			s.Board[2] = nlhe.ParseCard("Th")
			s.NumBoard = 3
		}
		if numBoard >= 4 {
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Board[3] = nlhe.ParseCard("5s")
			s.NumBoard = 4
		}
		if numBoard >= 5 {
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
			s.Board[4] = nlhe.ParseCard("7d")
			s.NumBoard = 5
		}
		return s
	}
	idPre := b.ID(mkState(0))
	idFlop := b.ID(mkState(3))
	idTurn := b.ID(mkState(4))
	idRiver := b.ID(mkState(5))
	ids := []uint64{idPre, idFlop, idTurn, idRiver}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] == ids[j] {
				t.Errorf("ids[%d]==ids[%d]=%d (different streets should differ)", i, j, ids[i])
			}
		}
	}
}

// TestMultiStreetIDDifferentBoardsCanDiffer — same hole, different flop with
// drastically different equity (top set vs air) should produce different
// flop buckets → different IDs. (Not guaranteed for all board pairs but a
// strong test for clear equity gaps.)
func TestMultiStreetIDDifferentBoardsCanDiffer(t *testing.T) {
	b := tinyMultiStreetBuckets(t)
	cfg := nlhe.DefaultConfig()
	mk := func(b0, b1, b2 string) *nlhe.State {
		s := nlhe.NewState(cfg)
		s.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Ah"))
		s.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
		s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
		s.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall})
		s.Board[0] = nlhe.ParseCard(b0)
		s.Board[1] = nlhe.ParseCard(b1)
		s.Board[2] = nlhe.ParseCard(b2)
		s.NumBoard = 3
		return s
	}
	// AA on dry low: super strong overpair.
	id1 := b.ID(mk("2c", "7d", "3h"))
	// AA on co-ordinated highs: AA dominated by sets potential.
	id2 := b.ID(mk("Tc", "Td", "Jh")) // wait — Td conflicts with hole? Ah only.
	// Use no-conflict board.
	id2 = b.ID(mk("Tc", "Jd", "Qh"))
	if id1 == id2 {
		t.Errorf("AA on 273 vs AA on TJQ: same ID %d — flop bucket didn't differentiate", id1)
	}
}

// TestMultiStreetIDPositionMatters — same state but different actor → different ID.
func TestMultiStreetIDPositionMatters(t *testing.T) {
	b := tinyMultiStreetBuckets(t)
	cfg := nlhe.DefaultConfig()
	s := nlhe.NewState(cfg)
	s.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
	s.SetHole(nlhe.P1, nlhe.ParseCard("As"), nlhe.ParseCard("Kh")) // same buckets
	id0 := b.ID(s) // s.Cur = P0
	s.Cur = nlhe.P1
	id1 := b.ID(s)
	if id0 == id1 {
		t.Errorf("position bit not encoded: P0 and P1 both yielded %d", id0)
	}
}

// TestMultiStreetIDHistoryMatters — different bet history → different ID.
func TestMultiStreetIDHistoryMatters(t *testing.T) {
	b := tinyMultiStreetBuckets(t)
	cfg := nlhe.DefaultConfig()
	s1 := nlhe.NewState(cfg)
	s1.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
	s1.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
	s1.Apply(nlhe.Action{Kind: nlhe.ActionCheckCall}) // SB call
	id1 := b.ID(s1)

	s2 := nlhe.NewState(cfg)
	s2.SetHole(nlhe.P0, nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
	s2.SetHole(nlhe.P1, nlhe.ParseCard("2c"), nlhe.ParseCard("3d"))
	s2.Apply(nlhe.Action{Kind: nlhe.ActionBet, SizeIdx: 0}) // SB bet
	id2 := b.ID(s2)
	if id1 == id2 {
		t.Errorf("call vs bet yielded same ID %d — history hash broken", id1)
	}
}
