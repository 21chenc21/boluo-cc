package nlhe6

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// helper: set hole cards for all seats, distinct cards.
func setupHoles(s *State) {
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		c1 := nlhe.Card(2*i + 0)
		c2 := nlhe.Card(2*i + 1)
		s.SetHole(Seat(i), c1, c2)
	}
}

// TestPostBlindsHU — HU: button = SB, other = BB.
func TestPostBlindsHU(t *testing.T) {
	cfg := DefaultConfigN(2)
	s := NewState(cfg)
	setupHoles(s)
	// Button = 0; SB = seat 0, BB = seat 1.
	if s.BetThisStreet[0] != cfg.SmallBlind {
		t.Errorf("seat 0 (SB) bet %d want %d", s.BetThisStreet[0], cfg.SmallBlind)
	}
	if s.BetThisStreet[1] != cfg.BigBlind {
		t.Errorf("seat 1 (BB) bet %d want %d", s.BetThisStreet[1], cfg.BigBlind)
	}
	if s.LastBetAmount != cfg.BigBlind {
		t.Errorf("LastBetAmount=%d want %d", s.LastBetAmount, cfg.BigBlind)
	}
	// HU: SB acts first preflop.
	if s.Cur != 0 {
		t.Errorf("Cur=%d want 0 (SB)", s.Cur)
	}
	// BB has option → HasActed[1] = false.
	if s.HasActed[1] {
		t.Errorf("BB shouldn't have HasActed=true after blind posting")
	}
}

// TestPostBlinds6Max — 6-max: button=0, SB=1, BB=2, UTG=3 acts first.
func TestPostBlinds6Max(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	if s.BetThisStreet[1] != cfg.SmallBlind {
		t.Errorf("seat 1 (SB) bet %d want %d", s.BetThisStreet[1], cfg.SmallBlind)
	}
	if s.BetThisStreet[2] != cfg.BigBlind {
		t.Errorf("seat 2 (BB) bet %d want %d", s.BetThisStreet[2], cfg.BigBlind)
	}
	if s.Cur != 3 {
		t.Errorf("Cur=%d want 3 (UTG)", s.Cur)
	}
	if s.HasActed[2] {
		t.Errorf("BB shouldn't have HasActed=true after blind posting")
	}
}

// TestHUFoldImmediately — SB folds preflop, BB wins SB chips.
func TestHUFoldImmediately(t *testing.T) {
	cfg := DefaultConfigN(2)
	s := NewState(cfg)
	setupHoles(s)
	s.Apply(Action{Kind: ActionFold})
	if !s.Terminal {
		t.Fatalf("HU SB fold should terminate")
	}
	if s.FoldWinner != 1 {
		t.Errorf("FoldWinner=%d want 1 (BB)", s.FoldWinner)
	}
	if s.Wagered[0] != cfg.SmallBlind {
		t.Errorf("SB wagered=%d want %d", s.Wagered[0], cfg.SmallBlind)
	}
	if s.Wagered[1] != cfg.BigBlind {
		t.Errorf("BB wagered=%d want %d", s.Wagered[1], cfg.BigBlind)
	}
}

// TestHUCallCheckPreflop — SB call, BB check → flop transition.
func TestHUCallCheckPreflop(t *testing.T) {
	cfg := DefaultConfigN(2)
	s := NewState(cfg)
	setupHoles(s)
	s.Apply(Action{Kind: ActionCheckCall}) // SB call
	if s.Terminal {
		t.Fatalf("HU after SB call should not be terminal")
	}
	if s.Cur != 1 {
		t.Errorf("Cur=%d want 1 (BB option)", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB check
	// Preflop should close → advance to flop.
	if n, needs := s.NeedsBoard(); !needs || n != 3 {
		t.Errorf("after BB check expect NeedsBoard=(3, true), got (%d, %v)", n, needs)
	}
	if s.Street != StreetFlop {
		t.Errorf("Street=%v want StreetFlop", s.Street)
	}
	// Postflop BB (= seat 1) acts first; should still be Cur=1.
	if s.Cur != 1 {
		t.Errorf("Cur=%d want 1 (postflop BB first)", s.Cur)
	}
}

// Test6MaxFoldAroundToBB — UTG/MP/CO/BTN/SB all fold; BB wins.
func Test6MaxFoldAroundToBB(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// UTG=3, MP=4, CO=5, BTN=0, SB=1, BB=2.
	expected := []Seat{3, 4, 5, 0, 1}
	for i, exp := range expected {
		if s.Cur != exp {
			t.Errorf("step %d: Cur=%d want %d", i, s.Cur, exp)
		}
		s.Apply(Action{Kind: ActionFold})
	}
	if !s.Terminal {
		t.Fatalf("after 5 folds should be terminal")
	}
	if s.FoldWinner != 2 {
		t.Errorf("FoldWinner=%d want 2 (BB)", s.FoldWinner)
	}
}

// Test6MaxLimpedRound — everyone limps preflop, BB checks option.
func Test6MaxLimpedRound(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// UTG/MP/CO/BTN/SB call (limp). BB has option, checks.
	for i := 0; i < 5; i++ {
		s.Apply(Action{Kind: ActionCheckCall})
	}
	// Now Cur = BB (seat 2). BB option.
	if s.Cur != 2 {
		t.Errorf("Cur=%d want 2 (BB option)", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall}) // BB check
	// Round closes → advance to flop.
	if s.Terminal {
		t.Fatalf("after limped preflop should not be terminal")
	}
	if s.Street != StreetFlop {
		t.Errorf("Street=%v want StreetFlop", s.Street)
	}
	// All 6 players each contributed BB chips.
	for i := 0; i < 6; i++ {
		if s.Wagered[i] != cfg.BigBlind {
			t.Errorf("seat %d wagered %d, want %d", i, s.Wagered[i], cfg.BigBlind)
		}
	}
	// Postflop SB (seat 1) acts first.
	if s.Cur != 1 {
		t.Errorf("postflop Cur=%d want 1 (SB)", s.Cur)
	}
}

// Test6MaxRaiseFoldedToRaiser — UTG raises, all fold; UTG wins blinds.
func Test6MaxRaiseFoldedToRaiser(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// UTG raises 1.0 pot.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	// MP/CO/BTN/SB/BB fold.
	for i := 0; i < 5; i++ {
		s.Apply(Action{Kind: ActionFold})
	}
	if !s.Terminal {
		t.Fatalf("after UTG raise + 5 folds should terminate")
	}
	if s.FoldWinner != 3 {
		t.Errorf("FoldWinner=%d want 3 (UTG)", s.FoldWinner)
	}
}

// TestThreeWayPreflopRaiseCallCall — 3-handed: UTG raises, BB calls, SB calls → flop.
func TestThreeWayPreflopRaiseCallCall(t *testing.T) {
	cfg := DefaultConfigN(3)
	s := NewState(cfg)
	setupHoles(s)
	// n=3: button=0(BTN), SB=1, BB=2. First-to-act preflop = BTN+3 mod 3 = 0 (BTN).
	if s.Cur != 0 {
		t.Errorf("3-handed Cur=%d want 0 (BTN/UTG)", s.Cur)
	}
	// BTN raises.
	s.Apply(Action{Kind: ActionBet, SizeIdx: 0}) // 0.5 pot
	// SB calls.
	if s.Cur != 1 {
		t.Errorf("Cur=%d want 1 (SB)", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall})
	// BB calls.
	if s.Cur != 2 {
		t.Errorf("Cur=%d want 2 (BB)", s.Cur)
	}
	s.Apply(Action{Kind: ActionCheckCall})
	// Preflop closes, advance to flop.
	if s.Street != StreetFlop {
		t.Errorf("Street=%v want StreetFlop", s.Street)
	}
}

// Test6MaxAllInPreflopAllElseFold — UTG all-in, all fold → UTG wins.
func Test6MaxAllInPreflopAllElseFold(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	s.Apply(Action{Kind: ActionAllIn}) // UTG all-in
	if !s.AllIn[3] {
		t.Fatalf("UTG should be AllIn after AllIn action")
	}
	for i := 0; i < 5; i++ {
		s.Apply(Action{Kind: ActionFold})
	}
	if !s.Terminal {
		t.Fatalf("UTG all-in + 5 folds should terminate")
	}
	if s.FoldWinner != 3 {
		t.Errorf("FoldWinner=%d want 3 (UTG)", s.FoldWinner)
	}
}

// Test6MaxAllInPreflopOneCall — UTG all-in, BB calls, rest fold → showdown.
func Test6MaxAllInPreflopOneCall(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	s.Apply(Action{Kind: ActionAllIn}) // UTG all-in
	s.Apply(Action{Kind: ActionFold})  // MP fold
	s.Apply(Action{Kind: ActionFold})  // CO fold
	s.Apply(Action{Kind: ActionFold})  // BTN fold
	s.Apply(Action{Kind: ActionFold})  // SB fold
	s.Apply(Action{Kind: ActionCheckCall}) // BB call (all-in if needed)
	if !s.Terminal {
		t.Fatalf("UTG all-in + 4 folds + BB call should terminate (both all-in, board to deal)")
	}
	// Board still needs filling.
	if n, needs := s.NeedsBoard(); !needs || n != 5 {
		t.Errorf("expected NeedsBoard=(5, true), got (%d, %v)", n, needs)
	}
}
