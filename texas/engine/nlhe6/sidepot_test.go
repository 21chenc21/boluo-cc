package nlhe6

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestComputeSidePotsTrivial — 2 players, equal wagered → 1 pot.
func TestComputeSidePotsTrivial(t *testing.T) {
	wagered := []int{50, 50}
	folded := []bool{false, false}
	pots := ComputeSidePots(wagered, folded)
	if len(pots) != 1 {
		t.Fatalf("want 1 pot, got %d", len(pots))
	}
	if pots[0].Amount != 100 {
		t.Errorf("pot 0 amount %d want 100", pots[0].Amount)
	}
	if len(pots[0].Eligible) != 2 {
		t.Errorf("pot 0 eligible len %d want 2", len(pots[0].Eligible))
	}
}

// TestComputeSidePotsFolded — folded player contributes but isn't eligible.
func TestComputeSidePotsFolded(t *testing.T) {
	wagered := []int{20, 50, 50}
	folded := []bool{true, false, false}
	pots := ComputeSidePots(wagered, folded)
	// Non-folded levels: 50. Single pot at level 50.
	if len(pots) != 1 {
		t.Fatalf("want 1 pot, got %d", len(pots))
	}
	// Pot amount: 20 (folded) + 50 + 50 = 120.
	if pots[0].Amount != 120 {
		t.Errorf("pot 0 amount %d want 120", pots[0].Amount)
	}
	// Eligible: seats 1 and 2 (folded seat 0 excluded).
	if len(pots[0].Eligible) != 2 {
		t.Errorf("pot 0 eligible len %d want 2", len(pots[0].Eligible))
	}
}

// TestComputeSidePotsAllInLayered — 3 players, all-in at different levels.
//
// Seat A: 30 chips (all-in for 30).
// Seat B: 100 (all-in for 100).
// Seat C: 100 (all-in for 100, or stack > 100).
//
// Expected:
//  - Main pot (level 30): 30+30+30 = 90, eligible A, B, C.
//  - Side pot (level 100): 0+70+70 = 140, eligible B, C.
func TestComputeSidePotsAllInLayered(t *testing.T) {
	wagered := []int{30, 100, 100}
	folded := []bool{false, false, false}
	pots := ComputeSidePots(wagered, folded)
	if len(pots) != 2 {
		t.Fatalf("want 2 pots, got %d", len(pots))
	}
	if pots[0].Amount != 90 || len(pots[0].Eligible) != 3 {
		t.Errorf("main pot: amount=%d eligible=%d, want 90/3", pots[0].Amount, len(pots[0].Eligible))
	}
	if pots[1].Amount != 140 || len(pots[1].Eligible) != 2 {
		t.Errorf("side pot: amount=%d eligible=%d, want 140/2", pots[1].Amount, len(pots[1].Eligible))
	}
}

// TestComputeSidePotsThreeLevels — 4 players, 3 distinct all-in levels.
func TestComputeSidePotsThreeLevels(t *testing.T) {
	wagered := []int{20, 50, 100, 100}
	folded := []bool{false, false, false, false}
	pots := ComputeSidePots(wagered, folded)
	if len(pots) != 3 {
		t.Fatalf("want 3 pots, got %d", len(pots))
	}
	// Level 20: 20×4 = 80, eligible all 4.
	if pots[0].Amount != 80 || len(pots[0].Eligible) != 4 {
		t.Errorf("pot 0 (level 20): amount=%d eligible=%d want 80/4", pots[0].Amount, len(pots[0].Eligible))
	}
	// Level 50: (50-20)×3 = 90, eligible seats 1,2,3.
	if pots[1].Amount != 90 || len(pots[1].Eligible) != 3 {
		t.Errorf("pot 1 (level 50): amount=%d eligible=%d want 90/3", pots[1].Amount, len(pots[1].Eligible))
	}
	// Level 100: (100-50)×2 = 100, eligible seats 2,3.
	if pots[2].Amount != 100 || len(pots[2].Eligible) != 2 {
		t.Errorf("pot 2 (level 100): amount=%d eligible=%d want 100/2", pots[2].Amount, len(pots[2].Eligible))
	}
	// Sum: 80+90+100 = 270 = total wagered.
	totalWagered := 20 + 50 + 100 + 100
	totalPots := pots[0].Amount + pots[1].Amount + pots[2].Amount
	if totalPots != totalWagered {
		t.Errorf("total pots %d != total wagered %d", totalPots, totalWagered)
	}
}

// TestPayoffFoldWin — fold-win pays out blinds correctly.
func TestPayoffFoldWin(t *testing.T) {
	cfg := DefaultConfigN(2)
	s := NewState(cfg)
	setupHoles(s)
	s.Apply(Action{Kind: ActionFold}) // SB fold
	// BB wins SB chips (1).
	if v := s.Payoff(0); v != -1 {
		t.Errorf("SB payoff %d want -1", v)
	}
	if v := s.Payoff(1); v != 1 {
		t.Errorf("BB payoff %d want +1", v)
	}
}

// TestPayoffShowdownHU — HU heads-up: AA vs 22 on AKQ32 board. AA wins.
func TestPayoffShowdownHU(t *testing.T) {
	cfg := DefaultConfigN(2)
	s := NewState(cfg)
	// AA (seat 0) vs 22 (seat 1).
	s.SetHole(0, nlhe.ParseCard("Ac"), nlhe.ParseCard("Ad"))
	s.SetHole(1, nlhe.ParseCard("2c"), nlhe.ParseCard("2d"))
	// SB call, BB check → flop. Flop check-check, turn check-check, river check-check.
	s.Apply(Action{Kind: ActionCheckCall}) // SB call
	s.Apply(Action{Kind: ActionCheckCall}) // BB check → flop
	// Fill flop manually.
	s.Board[0] = nlhe.ParseCard("Kh")
	s.Board[1] = nlhe.ParseCard("Qh")
	s.Board[2] = nlhe.ParseCard("3s")
	s.NumBoard = 3
	s.Apply(Action{Kind: ActionCheckCall}) // BB check
	s.Apply(Action{Kind: ActionCheckCall}) // SB check → turn
	s.Board[3] = nlhe.ParseCard("4s")
	s.NumBoard = 4
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	s.Board[4] = nlhe.ParseCard("9d")
	s.NumBoard = 5
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	if !s.Terminal {
		t.Fatalf("after river check-check should be terminal")
	}
	// AA pair > 22 pair (high pair). Each player wagered 2 (BB called preflop).
	// Pot 4 → AA wins 2 (= BB's wagered).
	p0 := s.Payoff(0)
	p1 := s.Payoff(1)
	if p0 != 2 {
		t.Errorf("AA payoff %d want +2", p0)
	}
	if p1 != -2 {
		t.Errorf("22 payoff %d want -2", p1)
	}
	if p0+p1 != 0 {
		t.Errorf("zero-sum violated: %d + %d = %d", p0, p1, p0+p1)
	}
}

// TestPayoffSidePot3Way — 3 players, 2 all-in at different levels, 1 active.
// A: wagered 20 (AA, all-in), B: wagered 50 (22, all-in), C: wagered 50 (KK).
// Main pot 60, eligible A/B/C → AA wins 60. A net = 60 - 20 = +40.
// Side pot (50-20)*2 = 60, eligible B/C → KK wins 60. C net = 60 - 50 = +10.
// B net = -50.
// Sum: +40 +10 -50 = 0 ✓.
func TestPayoffSidePot3Way(t *testing.T) {
	cfg := DefaultConfigN(3)
	cfg.StartStack = 200
	s := &State{
		Cfg:        cfg,
		FoldWinner: NoSeat,
		Terminal:   true,
		NumBoard:   5,
	}
	// Manual setup (skip Apply flow for this isolated test).
	s.SetHole(0, nlhe.ParseCard("Ac"), nlhe.ParseCard("Ad")) // AA
	s.SetHole(1, nlhe.ParseCard("2c"), nlhe.ParseCard("2d")) // 22
	s.SetHole(2, nlhe.ParseCard("Kc"), nlhe.ParseCard("Kd")) // KK
	s.Board[0] = nlhe.ParseCard("3s")
	s.Board[1] = nlhe.ParseCard("4s")
	s.Board[2] = nlhe.ParseCard("9d")
	s.Board[3] = nlhe.ParseCard("7h")
	s.Board[4] = nlhe.ParseCard("Js")
	s.Wagered[0] = 20
	s.Wagered[1] = 50
	s.Wagered[2] = 50

	p0 := s.Payoff(0)
	p1 := s.Payoff(1)
	p2 := s.Payoff(2)
	t.Logf("payoffs: A(AA)=%d, B(22)=%d, C(KK)=%d", p0, p1, p2)
	if p0 != 40 {
		t.Errorf("AA (all-in for 20, wins main pot) payoff %d want +40", p0)
	}
	if p1 != -50 {
		t.Errorf("22 (lost both pots) payoff %d want -50", p1)
	}
	if p2 != 10 {
		t.Errorf("KK (wins side pot only) payoff %d want +10", p2)
	}
	if p0+p1+p2 != 0 {
		t.Errorf("zero-sum violated: %d", p0+p1+p2)
	}
}
