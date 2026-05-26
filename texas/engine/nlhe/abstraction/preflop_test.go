package abstraction

import (
	"path/filepath"
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestBuildSmallBuckets — quick smoke build with K=5 + low MC samples.
// Verifies the full pipeline works and produces sensible bucket assignments.
func TestBuildSmallBuckets(t *testing.T) {
	bp := Build(5, 5000, 42)
	if len(bp.Buckets) != NumPreflopHandTypes {
		t.Fatalf("buckets len=%d", len(bp.Buckets))
	}
	if len(bp.Centers) != 5 {
		t.Fatalf("centers len=%d, want 5", len(bp.Centers))
	}
	// Centers must be ascending.
	for i := 1; i < len(bp.Centers); i++ {
		if bp.Centers[i] <= bp.Centers[i-1] {
			t.Errorf("centers not ascending: %v", bp.Centers)
			break
		}
	}
	// AA should land in top bucket (highest equity).
	aaBucket := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Ah"))
	if aaBucket != 4 {
		t.Errorf("AA in bucket %d, want 4 (top of K=5)", aaBucket)
	}
	// 72o should land in bottom bucket.
	trashBucket := bp.For(nlhe.ParseCard("7c"), nlhe.ParseCard("2d"))
	if trashBucket != 0 {
		t.Errorf("72o in bucket %d, want 0 (bottom of K=5)", trashBucket)
	}
	// Ranking holds: AA bucket > AKs bucket > 22 bucket > 72o bucket
	aksBucket := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Ks"))
	twoBucket := bp.For(nlhe.ParseCard("2c"), nlhe.ParseCard("2d"))
	if !(aaBucket >= aksBucket && aksBucket > twoBucket) {
		t.Errorf("ranking AA(%d) >= AKs(%d) > 22(%d) violated",
			aaBucket, aksBucket, twoBucket)
	}
}

// TestSaveLoadRoundTrip — write/read JSON preserves all data.
func TestSaveLoadRoundTrip(t *testing.T) {
	bp1 := Build(8, 5000, 7)
	dir := t.TempDir()
	path := filepath.Join(dir, "buckets.json")
	if err := bp1.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	bp2, err := LoadPreflopBuckets(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if bp2.K != bp1.K {
		t.Errorf("K %d != %d", bp2.K, bp1.K)
	}
	for i := 0; i < NumPreflopHandTypes; i++ {
		if bp2.Buckets[i] != bp1.Buckets[i] {
			t.Errorf("bucket[%d] %d != %d", i, bp2.Buckets[i], bp1.Buckets[i])
		}
	}
}

// TestSuitInvariance — same hand-type with different suit picks → same bucket.
func TestSuitInvariance(t *testing.T) {
	bp := Build(10, 5000, 42)
	// AKs in different suits.
	b1 := bp.For(nlhe.ParseCard("As"), nlhe.ParseCard("Ks"))
	b2 := bp.For(nlhe.ParseCard("Ah"), nlhe.ParseCard("Kh"))
	b3 := bp.For(nlhe.ParseCard("Ac"), nlhe.ParseCard("Kc"))
	if b1 != b2 || b2 != b3 {
		t.Errorf("AKs different suits got buckets %d %d %d", b1, b2, b3)
	}
	// 72o different suits.
	t1 := bp.For(nlhe.ParseCard("7c"), nlhe.ParseCard("2d"))
	t2 := bp.For(nlhe.ParseCard("7h"), nlhe.ParseCard("2s"))
	if t1 != t2 {
		t.Errorf("72o different suits got buckets %d %d", t1, t2)
	}
}
