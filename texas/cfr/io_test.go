package cfr

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/boluo/texas/engine/leduc"
)

func TestBlueprintRoundTrip(t *testing.T) {
	c := New()
	for i := 0; i < 500; i++ {
		c.Iter()
	}
	orig := c.AverageStrategy()
	origExpl := Exploitability(orig)
	origGv := GameValue(orig, leduc.P0)

	dir := t.TempDir()
	path := filepath.Join(dir, "bp.json")
	if err := SaveBlueprint(orig, path, c.Iters()); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, meta, err := LoadBlueprint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if meta.NumInfosets != 288 {
		t.Errorf("loaded meta NumInfosets=%d want 288", meta.NumInfosets)
	}
	if math.Abs(meta.Exploitability-origExpl) > 1e-9 {
		t.Errorf("meta expl=%v want %v", meta.Exploitability, origExpl)
	}
	if math.Abs(meta.GameValueP0-origGv) > 1e-9 {
		t.Errorf("meta gv=%v want %v", meta.GameValueP0, origGv)
	}
	if err := sanityCheckBlueprint(loaded); err != nil {
		t.Fatalf("sanity: %v", err)
	}

	// Loaded strategy must give identical metrics.
	if math.Abs(Exploitability(loaded)-origExpl) > 1e-9 {
		t.Errorf("loaded expl differs from saved")
	}
	if math.Abs(GameValue(loaded, leduc.P0)-origGv) > 1e-9 {
		t.Errorf("loaded gv differs from saved")
	}
}

func TestBlueprintLabelsAttached(t *testing.T) {
	c := New()
	for i := 0; i < 100; i++ {
		c.Iter()
	}
	sigma := c.AverageStrategy()
	dir := t.TempDir()
	path := filepath.Join(dir, "bp.json")
	if err := SaveBlueprint(sigma, path, c.Iters()); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, meta, err := LoadBlueprint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Spot-check a known label.
	found := false
	for _, e := range meta.Strategy {
		if e.Label == "K/?//" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected K/?// label in saved blueprint")
	}
}

// TestBlueprintSanityNoCorruption — uses a non-temp path for visibility, then cleans.
func TestBlueprintSanityNoCorruption(t *testing.T) {
	c := New()
	for i := 0; i < 50; i++ {
		c.Iter()
	}
	sigma := c.AverageStrategy()
	dir := t.TempDir()
	path := filepath.Join(dir, "bp.json")
	if err := SaveBlueprint(sigma, path, c.Iters()); err != nil {
		t.Fatalf("save: %v", err)
	}
	st, _ := os.Stat(path)
	if st.Size() < 1000 {
		t.Errorf("blueprint suspiciously small: %d bytes", st.Size())
	}
}
