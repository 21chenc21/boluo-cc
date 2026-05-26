package abstraction

import (
	"path/filepath"
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestBuildStreetFlop — generic build at street=3 (flop) — should match old FlopBuckets behavior.
func TestBuildStreetFlop(t *testing.T) {
	bp := BuildStreet(3, 20, 5000, 200, 42)
	if bp.Street != 3 || bp.K != 20 {
		t.Fatalf("Street=%d K=%d", bp.Street, bp.K)
	}
	t.Logf("Flop: %d unique classes, coverage %.2f%%", len(bp.Buckets), bp.CoveragePct())
}

// TestBuildStreetTurn — at street=4 (turn).
func TestBuildStreetTurn(t *testing.T) {
	bp := BuildStreet(4, 20, 5000, 200, 42)
	if bp.Street != 4 {
		t.Fatalf("Street=%d", bp.Street)
	}
	t.Logf("Turn: %d unique classes, coverage %.2f%%", len(bp.Buckets), bp.CoveragePct())
	// Turn theoretical ~14M, expect very low coverage at 5k outer.
}

// TestBuildStreetRiver — at street=5 (river).
func TestBuildStreetRiver(t *testing.T) {
	bp := BuildStreet(5, 20, 5000, 100, 42)
	if bp.Street != 5 {
		t.Fatalf("Street=%d", bp.Street)
	}
	t.Logf("River: %d unique classes, coverage %.2f%%", len(bp.Buckets), bp.CoveragePct())
}

// TestStreetBucketStrengthOrderingFlop — same as flop_bucket_test but via generic API.
func TestStreetBucketStrengthOrderingFlop(t *testing.T) {
	bp := BuildStreet(3, 10, 20000, 500, 42)

	flop := []nlhe.Card{nlhe.ParseCard("2s"), nlhe.ParseCard("7d"), nlhe.ParseCard("3h")}
	bAA := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Ah")},
		flop, 500, 99)
	flopAir := []nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Kh"), nlhe.ParseCard("Qc")}
	bAir := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("7c"), nlhe.ParseCard("2d")},
		flopAir, 500, 99)
	t.Logf("AA on 2-7-3: bucket %d", bAA)
	t.Logf("72o on AKQ (air): bucket %d", bAir)
	if bAir >= bAA {
		t.Errorf("air bucket %d should be < AA bucket %d", bAir, bAA)
	}
}

// TestStreetBucketStrengthOrderingTurn — same idea on turn (4 board cards).
func TestStreetBucketStrengthOrderingTurn(t *testing.T) {
	bp := BuildStreet(4, 10, 20000, 500, 42)

	turn := []nlhe.Card{nlhe.ParseCard("2s"), nlhe.ParseCard("7d"), nlhe.ParseCard("3h"), nlhe.ParseCard("8c")}
	bAA := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Ah")},
		turn, 500, 99)
	turnAir := []nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Kh"), nlhe.ParseCard("Qc"), nlhe.ParseCard("Jd")}
	bAir := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("7c"), nlhe.ParseCard("2d")},
		turnAir, 500, 99)
	t.Logf("AA on 2-7-3-8 turn: bucket %d", bAA)
	t.Logf("72o on AKQJ turn (air): bucket %d", bAir)
	if bAir >= bAA {
		t.Errorf("air bucket %d should be < AA bucket %d", bAir, bAA)
	}
}

// TestStreetBucketStrengthOrderingRiver — at river, board fully known.
// AA on full dry board vs 72o air.
func TestStreetBucketStrengthOrderingRiver(t *testing.T) {
	bp := BuildStreet(5, 10, 20000, 200, 42)

	dryBoard := []nlhe.Card{nlhe.ParseCard("2s"), nlhe.ParseCard("7d"), nlhe.ParseCard("3h"),
		nlhe.ParseCard("8c"), nlhe.ParseCard("Kd")}
	bAA := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Ah")},
		dryBoard, 200, 99)
	airBoard := []nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Kh"), nlhe.ParseCard("Qc"),
		nlhe.ParseCard("Jd"), nlhe.ParseCard("Td")}
	bAir := bp.ForOrFallback(
		[2]nlhe.Card{nlhe.ParseCard("3c"), nlhe.ParseCard("2d")}, // 32o on Broadway board: air
		airBoard, 200, 99)
	t.Logf("AA on dry river: bucket %d", bAA)
	t.Logf("32o on Broadway river (air): bucket %d", bAir)
	if bAir >= bAA {
		t.Errorf("air bucket %d should be < AA bucket %d", bAir, bAA)
	}
}

// TestStreetBucketSaveLoadRoundTrip
func TestStreetBucketSaveLoadRoundTrip(t *testing.T) {
	bp1 := BuildStreet(4, 8, 2000, 100, 7)
	dir := t.TempDir()
	path := filepath.Join(dir, "turn.json")
	if err := bp1.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	bp2, err := LoadStreetBuckets(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if bp2.K != bp1.K || bp2.Street != bp1.Street {
		t.Errorf("metadata mismatch")
	}
	for k, b := range bp1.Buckets {
		if bp2.Buckets[k] != b {
			t.Errorf("bucket mismatch for %s", k)
			return
		}
	}
}
