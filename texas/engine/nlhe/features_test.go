package nlhe

import "testing"

func TestFeatureVecPushFoldSBOpen(t *testing.T) {
	cfg := PushFoldConfig(10)
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s.SetHole(P1, ParseCard("2c"), ParseCard("3d"))

	f := FeatureVecPushFold(s)

	// AKo: ranks 12 (A) and 11 (K), unpaired, offsuit, P0, not facing shove.
	if f[11] != 1 {
		t.Errorf("low rank K (idx 11)=%v want 1", f[11])
	}
	if f[13+12] != 1 {
		t.Errorf("high rank A (idx 25)=%v want 1", f[13+12])
	}
	if f[26] != 0 {
		t.Errorf("pair indicator=%v want 0 (AKo)", f[26])
	}
	if f[27] != 0 {
		t.Errorf("suited indicator=%v want 0 (offsuit)", f[27])
	}
	if f[28] != 0 {
		t.Errorf("position=%v want 0 (SB)", f[28])
	}
	if f[29] != 0 {
		t.Errorf("facing shove=%v want 0 (start)", f[29])
	}
	// Legal mask: Fold + AllIn only for SB opening in push/fold (no CheckCall).
	if f[30] != 1 || f[31] != 0 || f[32] != 1 {
		t.Errorf("legal mask=[%v,%v,%v] want [1,0,1]", f[30], f[31], f[32])
	}
}

func TestFeatureVecPushFoldBBFacingShove(t *testing.T) {
	cfg := PushFoldConfig(10)
	s := NewState(cfg)
	s.SetHole(P0, ParseCard("As"), ParseCard("Ah"))
	s.SetHole(P1, ParseCard("Tc"), ParseCard("Td"))
	s.Apply(Action{Kind: ActionAllIn})
	if s.Cur != P1 {
		t.Fatalf("Cur=%d want P1", s.Cur)
	}

	f := FeatureVecPushFold(s)

	// TT (pair, rank 8). Position=1 (BB). Facing shove=1.
	if f[8] != 1 || f[13+8] != 1 {
		t.Errorf("TT ranks: low=%v high=%v want both 1", f[8], f[13+8])
	}
	if f[26] != 1 {
		t.Errorf("pair indicator=%v want 1", f[26])
	}
	if f[28] != 1 {
		t.Errorf("position=%v want 1 (BB)", f[28])
	}
	if f[29] != 1 {
		t.Errorf("facing shove=%v want 1", f[29])
	}
	// Legal: Fold + CheckCall (no AllIn since opp already all-in and BB stack just calls).
	if f[30] != 1 || f[31] != 1 {
		t.Errorf("legal mask Fold+CheckCall: %v %v want [1,1]", f[30], f[31])
	}
}

func TestFeatureVecPushFoldSuitInvariance(t *testing.T) {
	cfg := PushFoldConfig(10)
	// Two states differing only in suit identity. Rank-based features should match.
	s1 := NewState(cfg)
	s1.SetHole(P0, ParseCard("As"), ParseCard("Kh"))
	s1.SetHole(P1, ParseCard("2c"), ParseCard("3d"))
	s2 := NewState(cfg)
	s2.SetHole(P0, ParseCard("Ac"), ParseCard("Kd")) // different suits, AKo same
	s2.SetHole(P1, ParseCard("2h"), ParseCard("3s"))
	f1 := FeatureVecPushFold(s1)
	f2 := FeatureVecPushFold(s2)
	if f1 != f2 {
		t.Errorf("AKo with different suits → different features")
	}
}
