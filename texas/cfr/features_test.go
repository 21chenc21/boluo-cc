package cfr

import (
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

// TestFeatureVecOpening — at start of game with P0 holding K, expect:
//   - priv K bit set
//   - pub none bit set
//   - round=0
//   - all history zeros
//   - legal mask = {Fold, CheckCall, BetRaise} all set
func TestFeatureVecOpening(t *testing.T) {
	s := leduc.NewState(leduc.MakeCard(2, 0), leduc.MakeCard(0, 0)) // P0=K
	f := FeatureVec(s)

	// Priv rank K (idx 2)
	if f[0] != 0 || f[1] != 0 || f[2] != 1 {
		t.Errorf("priv K: f[0:3]=%v want [0,0,1]", f[0:3])
	}
	// Pub none (idx 6)
	if f[3] != 0 || f[4] != 0 || f[5] != 0 || f[6] != 1 {
		t.Errorf("pub none: f[3:7]=%v want [0,0,0,1]", f[3:7])
	}
	// Round 0
	if f[7] != 0 {
		t.Errorf("round: f[7]=%v want 0", f[7])
	}
	// History zeros.
	for i := 8; i < 32; i++ {
		if f[i] != 0 {
			t.Errorf("hist f[%d]=%v want 0", i, f[i])
		}
	}
	// Legal mask all 1 (3 actions).
	if f[32] != 1 || f[33] != 1 || f[34] != 1 {
		t.Errorf("legal mask: f[32:35]=%v want [1,1,1]", f[32:35])
	}
}

// TestFeatureVecRound2 — after check-check, P0 facing public K with their J.
func TestFeatureVecRound2(t *testing.T) {
	s := leduc.NewState(leduc.MakeCard(0, 0), leduc.MakeCard(1, 0))
	s.Apply(leduc.ActionCheckCall)
	s.Apply(leduc.ActionCheckCall)
	s.SetPublic(leduc.MakeCard(2, 0))
	f := FeatureVec(s)
	if f[0] != 1 {
		t.Errorf("priv J: f[0]=%v want 1", f[0])
	}
	if f[5] != 1 || f[6] != 0 {
		t.Errorf("pub K: f[3:7]=%v want pub K (slot 5=1, 6=0)", f[3:7])
	}
	if f[7] != 1 {
		t.Errorf("round=1: f[7]=%v want 1", f[7])
	}
	// R1 history: pos 0 = check (ActionCheckCall=1), pos 1 = check.
	// f[8+0*3+1] and f[8+1*3+1] should be 1.
	if f[8+1] != 1 || f[8+3+1] != 1 {
		t.Errorf("r1 history check-check: f[8:14]=%v", f[8:14])
	}
}

// TestFeatureVecSuitInvariance — same rank, different suits → same feature.
func TestFeatureVecSuitInvariance(t *testing.T) {
	for su := uint8(0); su < leduc.NumSuits; su++ {
		s1 := leduc.NewState(leduc.MakeCard(1, su), leduc.MakeCard(0, 0))
		s2 := leduc.NewState(leduc.MakeCard(1, su^1), leduc.MakeCard(0, 0))
		f1 := FeatureVec(s1)
		f2 := FeatureVec(s2)
		if f1 != f2 {
			t.Errorf("suit %d: features differ across same-rank private", su)
		}
	}
}

// TestFeatureLegalMaskAtCap — at raise cap, BetRaise bit should be 0.
func TestFeatureLegalMaskAtCap(t *testing.T) {
	s := leduc.NewState(leduc.MakeCard(0, 0), leduc.MakeCard(2, 0))
	s.Apply(leduc.ActionBetRaise)
	s.Apply(leduc.ActionBetRaise) // NumRaises=2 cap
	f := FeatureVec(s)
	if f[32] != 1 || f[33] != 1 {
		t.Errorf("Fold + CheckCall should be legal: f[32:34]=%v", f[32:34])
	}
	if f[34] != 0 {
		t.Errorf("BetRaise should NOT be legal at cap: f[34]=%v want 0", f[34])
	}
}
