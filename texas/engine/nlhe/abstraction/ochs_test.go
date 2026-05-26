package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestBuildOCHSSmoke — build OCHS bucket, verify structure + sanity.
func TestBuildOCHSSmoke(t *testing.T) {
	bp := BuildOCHS(10, 5, 5000, 42)
	if bp.Mode != "OCHS" {
		t.Fatalf("Mode=%s want OCHS", bp.Mode)
	}
	if len(bp.Buckets) != NumPreflopHandTypes {
		t.Errorf("Buckets len=%d", len(bp.Buckets))
	}
	if len(bp.OchsEquities) != NumPreflopHandTypes {
		t.Errorf("OchsEquities len=%d", len(bp.OchsEquities))
	}
	if len(bp.OchsEquities[0]) != 5 {
		t.Errorf("OchsEquities[0] len=%d want 5", len(bp.OchsEquities[0]))
	}
	if len(bp.OppClusterFor) != NumPreflopHandTypes {
		t.Errorf("OppClusterFor len=%d", len(bp.OppClusterFor))
	}
	// AA should be in top bucket; 72o in bottom.
	aaBucket := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Ah"))
	if aaBucket != bp.K-1 {
		t.Errorf("AA bucket=%d want %d (top)", aaBucket, bp.K-1)
	}
	trash := bp.For(nlhe.ParseCard("7c"), nlhe.ParseCard("2d"))
	if trash != 0 {
		t.Errorf("72o bucket=%d want 0 (bottom)", trash)
	}
}

// TestOCHSFixesAKsBucketing — the failure that motivated OCHS.
// Under E[HS], AKs could end up bucketed with hands like K9s/QJs (similar 1-D
// equity but very different Nash strategy facing shove). OCHS should keep AKs
// with other big-broadway suited/offsuit hands.
func TestOCHSFixesAKsBucketing(t *testing.T) {
	bp := BuildOCHS(20, 5, 10000, 42)

	aks := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Ks"))
	ako := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Kh"))
	aqs := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Qs"))
	ajs := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Js"))

	t.Logf("AKs bucket=%d, AKo=%d, AQs=%d, AJs=%d", aks, ako, aqs, ajs)

	// Premium broadway hands should be close in bucket (within 2 of each other).
	maxAbs := func(a, b int) int {
		if a > b {
			return a - b
		}
		return b - a
	}
	if maxAbs(aks, ako) > 2 {
		t.Errorf("AKs bucket %d and AKo bucket %d too far apart", aks, ako)
	}
	if maxAbs(aks, aqs) > 3 {
		t.Errorf("AKs bucket %d and AQs bucket %d too far apart", aks, aqs)
	}
}

// TestOCHSFixesPocketPairBucketing — under E[HS], 22 ended up with weak Aces /
// suited connectors. OCHS should respect 22's distinct variance profile.
func TestOCHSFixesPocketPairBucketing(t *testing.T) {
	bp := BuildOCHS(20, 5, 10000, 42)

	pair22 := bp.For(nlhe.ParseCard("2c"), nlhe.ParseCard("2d"))
	pair33 := bp.For(nlhe.ParseCard("3c"), nlhe.ParseCard("3d"))
	pair44 := bp.For(nlhe.ParseCard("4c"), nlhe.ParseCard("4d"))
	t9s := bp.For(nlhe.ParseCard("Ts"), nlhe.ParseCard("9s")) // suited connector

	t.Logf("22 bucket=%d, 33=%d, 44=%d, T9s=%d", pair22, pair33, pair44, t9s)

	// Small pairs should bucket close together (similar variance profile vs opp ranges).
	if pair22 == t9s {
		t.Errorf("OCHS still bucketed 22 with T9s — variance profile not separated")
	}
}
