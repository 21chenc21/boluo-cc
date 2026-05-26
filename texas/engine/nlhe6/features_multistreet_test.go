package nlhe6

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestFeatureDim — schema 288-d.
func TestFeatureDim(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	f := FeatureVecMultiStreet(s)
	if len(f) != 288 {
		t.Errorf("dim=%d want 288", len(f))
	}
}

// TestFeature6MaxPosition — UTG (seat 3 at button=0) sets slot [117+UTG] = 1.
func TestFeature6MaxPosition(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// Default button=0, so SB=1, BB=2, UTG=3 acts first.
	if s.Cur != 3 {
		t.Fatalf("expected Cur=3, got %d", s.Cur)
	}
	f := FeatureVecMultiStreet(s)
	// UTG = PosUTG = 3. Slot index = 117 + 3 = 120.
	if f[120] != 1 {
		t.Errorf("UTG position slot f[120] = %v want 1", f[120])
	}
	// SB / BB slots should be 0.
	if f[118] != 0 || f[119] != 0 {
		t.Errorf("non-hero position slots should be 0")
	}
}

// TestFeature6MaxOppSlots — 6-max fills 5 opp slots clockwise from hero+1.
func TestFeature6MaxOppSlots(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// Hero = UTG (seat 3). Clockwise opps: 4 (MP), 5 (CO), 0 (BTN), 1 (SB), 2 (BB).
	f := FeatureVecMultiStreet(s)
	// Each opp slot is 4 dims at base 127 + i*4.
	// Slot 0 (= seat 4): should have stack ratio (startStack-0)/startStack = 1.0
	if f[127] < 0.99 {
		t.Errorf("opp slot 0 stack ratio=%v want ~1", f[127])
	}
	// Slot 3 = seat 1 = SB, has bet 1, stack 199. Bet ratio = 1/200 = 0.005.
	if v := f[127+3*4+1]; v < 0.001 || v > 0.01 {
		t.Errorf("opp slot 3 (SB) bet ratio=%v want ~0.005", v)
	}
	// Slot 4 = seat 2 = BB, bet 2 → 0.01.
	if v := f[127+4*4+1]; v < 0.005 || v > 0.015 {
		t.Errorf("opp slot 4 (BB) bet ratio=%v want ~0.01", v)
	}
}

// TestFeature6MaxActionHistory — action history block records actor seat + action.
func TestFeature6MaxActionHistory(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	setupHoles(s)
	// UTG (seat 3) folds. Hist[0][0] = (seat=3, action=Fold).
	s.Apply(Action{Kind: ActionFold})
	f := FeatureVecMultiStreet(s)
	const histBase = 160
	// Slot 0: actor seat 3 relative to button 0, normalized = 3/6 = 0.5
	if v := f[histBase]; v < 0.49 || v > 0.51 {
		t.Errorf("history slot 0 actor=%v want 0.5", v)
	}
	// dim 1 = Fold one-hot.
	if f[histBase+1] != 1 {
		t.Errorf("history slot 0 Fold dim=%v want 1", f[histBase+1])
	}
	// dim 7 = exists.
	if f[histBase+7] != 1 {
		t.Errorf("history slot 0 exists=%v want 1", f[histBase+7])
	}
	// Slot 1 empty.
	if f[histBase+8+7] != 0 {
		t.Errorf("history slot 1 exists should be 0")
	}
}

// TestFeature6MaxBoardEncoding — flop dealt → board encoded sorted desc.
func TestFeature6MaxBoardEncoding(t *testing.T) {
	cfg := DefaultConfig6()
	s := NewState(cfg)
	// Set hole cards from rank-12+ to avoid board conflict.
	s.SetHole(0, nlhe.ParseCard("As"), nlhe.ParseCard("Ah"))
	s.SetHole(1, nlhe.ParseCard("Ac"), nlhe.ParseCard("Ad"))
	s.SetHole(2, nlhe.ParseCard("Ks"), nlhe.ParseCard("Kh"))
	s.SetHole(3, nlhe.ParseCard("Kc"), nlhe.ParseCard("Kd"))
	s.SetHole(4, nlhe.ParseCard("Qs"), nlhe.ParseCard("Qh"))
	s.SetHole(5, nlhe.ParseCard("Qc"), nlhe.ParseCard("Qd"))
	// All fold to BB, then deal flop. Use proper game flow:
	// UTG=3 fold, MP=4 fold, CO=5 fold, BTN=0 fold, SB=1 fold → BB wins.
	// But we want a flop scenario — call/limp instead.
	cfg.BetSizes = []float64{1.0}
	s2 := NewState(cfg)
	for i := 0; i < 6; i++ {
		s2.SetHole(Seat(i), nlhe.Card(2*i), nlhe.Card(2*i+1))
	}
	for i := 0; i < 6; i++ {
		s2.Apply(Action{Kind: ActionCheckCall})
	}
	// Now Street should be Flop, NeedsBoard 3.
	if s2.Street != StreetFlop {
		t.Fatalf("expected Street=Flop, got %v", s2.Street)
	}
	// Deal flop 2-7-K.
	s2.Board[0] = nlhe.ParseCard("2c")
	s2.Board[1] = nlhe.ParseCard("7d")
	s2.Board[2] = nlhe.ParseCard("Kh")
	s2.NumBoard = 3
	f := FeatureVecMultiStreet(s2)
	// Slot 0 = K (rank 11). Base 28+11.
	if f[28+11] != 1 {
		t.Errorf("flop slot 0 K not set, f[39]=%v", f[28+11])
	}
	// Street one-hot: flop = 114.
	if f[114] != 1 {
		t.Errorf("street flop one-hot not set")
	}
}
