package nlhe

import "testing"

// TestFeatureMultiStreetDim — dimension matches constant.
func TestFeatureMultiStreetDim(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	f := FeatureVecMultiStreet(s)
	if len(f) != FeatureDimMultiStreet {
		t.Errorf("dim=%d want %d", len(f), FeatureDimMultiStreet)
	}
}

// TestFeatureMultiStreetPreflop — preflop state encodes hole correctly, board is zero.
func TestFeatureMultiStreetPreflop(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	f := FeatureVecMultiStreet(s)
	// AKo: rank A=12, rank K=11. Low=11, high=12.
	if f[11] != 1 {
		t.Errorf("low rank (K=11) bit not set: f[11]=%v", f[11])
	}
	if f[13+12] != 1 {
		t.Errorf("high rank (A=12) bit not set: f[25]=%v", f[13+12])
	}
	if f[26] != 0 {
		t.Errorf("pair bit set for AK: f[26]=%v", f[26])
	}
	if f[27] != 0 {
		t.Errorf("suited bit set for offsuit AK: f[27]=%v", f[27])
	}
	// Board zero in slots 28..113.
	for i := 28; i < 113; i++ {
		if f[i] != 0 {
			t.Errorf("preflop board feature[%d]=%v not zero", i, f[i])
		}
	}
	// Street preflop.
	if f[113] != 1 {
		t.Errorf("street preflop bit not set: f[113]=%v", f[113])
	}
}

// TestFeatureMultiStreetBoard — flop state encodes board cards in sorted-desc order.
func TestFeatureMultiStreetBoard(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	// Deal flop 2-7-K rainbow (K=12, 7=5, 2=0).
	s.Board[0] = ParseCard("2c")
	s.Board[1] = ParseCard("7d")
	s.Board[2] = ParseCard("Kh")
	s.NumBoard = 3
	f := FeatureVecMultiStreet(s)
	// Sorted desc: K, 7, 2 → slot0=K, slot1=7, slot2=2.
	// Rank: 2=0, 3=1, ..., 7=5, T=8, J=9, Q=10, K=11, A=12.
	// slot0: base=28, rank=11 (K)
	if f[28+11] != 1 {
		t.Errorf("slot0 rank K not set, f[39]=%v", f[28+11])
	}
	// slot1: base=45, rank=5 (7)
	if f[45+5] != 1 {
		t.Errorf("slot1 rank 7 not set, f[50]=%v", f[45+5])
	}
	// slot2: base=62, rank=0 (2)
	if f[62+0] != 1 {
		t.Errorf("slot2 rank 2 not set, f[62]=%v", f[62+0])
	}
	// slot3/4 zero.
	for i := 79; i < 113; i++ {
		if f[i] != 0 {
			t.Errorf("unfilled slot3/4 feature[%d]=%v not zero", i, f[i])
		}
	}
	// Street flop.
	if f[114] != 1 {
		t.Errorf("street flop bit not set")
	}
	// Pair count zero (no pair on 2-7-K).
	if f[153] != 0 {
		t.Errorf("pair count nonzero on rainbow flop: f[153]=%v", f[153])
	}
	// Distinct ranks 3 → 3/5 = 0.6.
	if d := f[156]; d < 0.59 || d > 0.61 {
		t.Errorf("distinct rank ratio f[156]=%v want ~0.6", d)
	}
}

// TestFeatureMultiStreetBoardStructure — paired board sets pair count.
func TestFeatureMultiStreetBoardStructure(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	// K-K-2 paired board.
	s.Board[0] = ParseCard("Ks")
	s.Board[1] = ParseCard("Kc")
	s.Board[2] = ParseCard("2d")
	s.NumBoard = 3
	f := FeatureVecMultiStreet(s)
	if f[153] != 0.5 {
		t.Errorf("paired board f[153]=%v want 0.5 (1 pair / 2)", f[153])
	}
}

// TestFeatureMultiStreetPositionOneHot — hero position lives in slots [117:123].
// HU sets slot 0 (SB / P0) or slot 1 (BB / P1); others stay zero.
func TestFeatureMultiStreetPositionOneHot(t *testing.T) {
	cfg := DefaultConfig()
	sP0 := NewState(cfg)
	sP0.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	sP0.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	fP0 := FeatureVecMultiStreet(sP0)
	if fP0[117] != 1 {
		t.Errorf("P0 → slot 0 (SB) should be 1, got %v", fP0[117])
	}
	if fP0[118] != 0 {
		t.Errorf("P0 → slot 1 (BB) should be 0, got %v", fP0[118])
	}
	for i := 119; i < 123; i++ {
		if fP0[i] != 0 {
			t.Errorf("HU should leave 6-max position slots [119:123] zero, f[%d]=%v", i, fP0[i])
		}
	}

	// After P0 calls, P1 is to act.
	sP1 := NewState(cfg)
	sP1.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	sP1.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	sP1.Apply(Action{Kind: ActionCheckCall})
	fP1 := FeatureVecMultiStreet(sP1)
	if fP1[117] != 0 {
		t.Errorf("P1 to act: slot 0 (SB) should be 0, got %v", fP1[117])
	}
	if fP1[118] != 1 {
		t.Errorf("P1 to act: slot 1 (BB) should be 1, got %v", fP1[118])
	}
}

// TestFeatureMultiStreetOppSlot — opp slot 0 encodes the 1 HU opponent.
// 6-max migration: slots 1-4 also populated; HU keeps them zero.
func TestFeatureMultiStreetOppSlot(t *testing.T) {
	cfg := DefaultConfig() // StartStack=200
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	// P0 to act. Opp = P1. Initial: Stacks[P1]=198 (BB posted), BetThisStreet[P1]=2.
	f := FeatureVecMultiStreet(s)
	const oppBase = 127
	// opp stack ratio = 198 / 200 = 0.99
	if v := f[oppBase+0]; v < 0.98 || v > 1.0 {
		t.Errorf("opp stack ratio f[%d]=%v want ~0.99", oppBase, v)
	}
	// opp bet-this-street ratio = 2 / 200 = 0.01
	if v := f[oppBase+1]; v < 0.005 || v > 0.015 {
		t.Errorf("opp bet ratio f[%d]=%v want ~0.01", oppBase+1, v)
	}
	// opp position offset = 1.0 (HU)
	if v := f[oppBase+2]; v != 1.0 {
		t.Errorf("opp position offset f[%d]=%v want 1.0", oppBase+2, v)
	}
	// opp not all-in
	if v := f[oppBase+3]; v != 0 {
		t.Errorf("opp all-in flag f[%d]=%v want 0", oppBase+3, v)
	}
	// Slots 1-4 (positions 131-146): all zero in HU.
	for i := oppBase + 4; i < oppBase+20; i++ {
		if f[i] != 0 {
			t.Errorf("HU should leave opp slots 1-4 zero, f[%d]=%v", i, f[i])
		}
	}
}

// TestFeatureMultiStreetHeroStateRatios — hero stack/bet/pot/raise ratios.
func TestFeatureMultiStreetHeroStateRatios(t *testing.T) {
	cfg := DefaultConfig() // StartStack=200, SB=1, BB=2
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	f := FeatureVecMultiStreet(s)
	// hero (P0/SB) stack: 200-1 = 199 → ratio 199/200 = 0.995
	if v := f[123]; v < 0.99 || v > 1.0 {
		t.Errorf("hero stack ratio f[123]=%v want ~0.995", v)
	}
	// hero bet-this-street: 1 (SB blind) → 1/200 = 0.005
	if v := f[124]; v < 0.0 || v > 0.01 {
		t.Errorf("hero bet ratio f[124]=%v want ~0.005", v)
	}
	// pot: 3 (SB+BB blinds) → 3/(2*200) = 0.0075
	if v := f[125]; v < 0.005 || v > 0.01 {
		t.Errorf("pot ratio f[125]=%v want ~0.0075", v)
	}
}

// TestFeatureMultiStreetDerivedScalars — pot odds, SPR, effective stack.
func TestFeatureMultiStreetDerivedScalars(t *testing.T) {
	cfg := DefaultConfig() // StartStack=200
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	f := FeatureVecMultiStreet(s)
	// Preflop opening for P0/SB: opp.bet=2 (BB), hero.bet=1 (SB). to_call=1, pot=3.
	// pot_odds = 1/(1+3) = 0.25
	if v := f[157]; v < 0.24 || v > 0.26 {
		t.Errorf("pot odds f[157]=%v want ~0.25", v)
	}
	// SPR = hero_stack(199) / pot(3) = 66.3 → clamped to 10 → /10 = 1.0
	if v := f[158]; v < 0.99 || v > 1.01 {
		t.Errorf("SPR f[158]=%v want 1.0 (clamped)", v)
	}
	// Effective stack = min(199, 198) / 200 = 0.99
	if v := f[159]; v < 0.98 || v > 1.0 {
		t.Errorf("eff stack f[159]=%v want ~0.99", v)
	}
}

// TestFeatureMultiStreetActionHistory — action history block records sequence.
func TestFeatureMultiStreetActionHistory(t *testing.T) {
	cfg := DefaultConfig()
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	// Apply: SB calls, BB checks → preflop closes.
	s.Apply(Action{Kind: ActionCheckCall})
	s.Apply(Action{Kind: ActionCheckCall})
	// After applying, history has 2 entries on preflop.
	f := FeatureVecMultiStreet(s)
	// History block at 160. Preflop = street 0. Slot 0 starts at 160.
	const histBase = 160
	// Slot 0: SB called. actor=P0(0). action CheckCall = dim 2. exists = dim 7.
	if v := f[histBase+0]; v != 0 {
		t.Errorf("preflop slot 0 actor want P0 (0), got %v", v)
	}
	if v := f[histBase+2]; v != 1 {
		t.Errorf("preflop slot 0 CheckCall one-hot want 1, got %v", v)
	}
	if v := f[histBase+7]; v != 1 {
		t.Errorf("preflop slot 0 exists flag want 1, got %v", v)
	}
	// Slot 1: BB checked. actor=P1(1). action CheckCall. exists=1.
	if v := f[histBase+8]; v != 1 {
		t.Errorf("preflop slot 1 actor want P1 (1), got %v", v)
	}
	if v := f[histBase+8+2]; v != 1 {
		t.Errorf("preflop slot 1 CheckCall one-hot want 1, got %v", v)
	}
	if v := f[histBase+8+7]; v != 1 {
		t.Errorf("preflop slot 1 exists flag want 1, got %v", v)
	}
	// Slots 2/3: empty. exists=0.
	if v := f[histBase+16+7]; v != 0 {
		t.Errorf("preflop slot 2 exists should be 0, got %v", v)
	}
	// Flop / turn / river slots: empty.
	for st := 1; st < 4; st++ {
		streetBase := histBase + st*32
		for i := 0; i < 4; i++ {
			if v := f[streetBase+i*8+7]; v != 0 {
				t.Errorf("street %d slot %d exists should be 0, got %v", st, i, v)
			}
		}
	}
}

// TestFeatureMultiStreetActionHistorySequenceDistinguishes — different action
// sequences with same pot should differ in features (the whole point of
// adding history block).
func TestFeatureMultiStreetActionHistorySequenceDistinguishes(t *testing.T) {
	cfg := DefaultConfig()
	// Sequence A: SB raise → BB call (preflop: bet, call).
	sA := NewState(cfg)
	sA.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	sA.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	sA.Apply(Action{Kind: ActionBet, SizeIdx: 1}) // SB bet 1.0 pot
	sA.Apply(Action{Kind: ActionCheckCall})        // BB call
	// Now flop. Need P1 to act first postflop.

	// Sequence B: SB limp → BB raise → SB call (preflop: call, bet, call).
	sB := NewState(cfg)
	sB.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	sB.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	sB.Apply(Action{Kind: ActionCheckCall})
	sB.Apply(Action{Kind: ActionBet, SizeIdx: 1})
	sB.Apply(Action{Kind: ActionCheckCall})

	fA := FeatureVecMultiStreet(sA)
	fB := FeatureVecMultiStreet(sB)

	// Check that the action history blocks differ.
	diff := false
	for i := 160; i < 288; i++ {
		if fA[i] != fB[i] {
			diff = true
			break
		}
	}
	if !diff {
		t.Errorf("two different preflop sequences produced identical action history blocks")
	}
}
