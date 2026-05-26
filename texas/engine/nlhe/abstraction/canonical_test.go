package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestHandTypeIdxRoundTripCoverage — verify that all 1326 hole-card pairs map
// to exactly 169 distinct hand types, each accounted for.
func TestHandTypeIdxRoundTripCoverage(t *testing.T) {
	counts := make([]int, NumPreflopHandTypes)
	for c1 := nlhe.Card(0); c1 < nlhe.DeckSize; c1++ {
		for c2 := c1 + 1; c2 < nlhe.DeckSize; c2++ {
			idx := HandTypeIdx(c1, c2)
			if idx < 0 || idx >= NumPreflopHandTypes {
				t.Errorf("HandTypeIdx(%v,%v)=%d out of range", c1, c2, idx)
			}
			counts[idx]++
		}
	}
	// Expected count distribution:
	//   pairs:    13 * C(4,2) = 13 * 6 = 78 combos / 13 pairs = 6 each
	//   suited:   78 * 4 (one per suit) = 312 combos / 78 types = 4 each
	//   offsuit:  78 * C(4,2) for diff-suit = 78 * 12 (4 hi-suit * 3 lo-suit, but each pair counted once) = 936
	//     Actually: 4 * 3 = 12 ordered (hi-suit, lo-suit) per type, so 12 / 1 = 12 unordered
	//     Wait, hi has 4 suit choices, lo has 4 suit choices, exclude same-suit (4 cases), = 12. ✓
	//   Total: 78 + 312 + 936 = 1326 ✓
	wantPair := 6
	wantSuited := 4
	wantOffsuit := 12
	for i := 0; i < 13; i++ {
		if counts[i] != wantPair {
			t.Errorf("pair idx %d (%s): count=%d want %d", i, HandTypeLabel(i), counts[i], wantPair)
		}
	}
	for i := 13; i < 13+78; i++ {
		if counts[i] != wantSuited {
			t.Errorf("suited idx %d (%s): count=%d want %d", i, HandTypeLabel(i), counts[i], wantSuited)
		}
	}
	for i := 13 + 78; i < 169; i++ {
		if counts[i] != wantOffsuit {
			t.Errorf("offsuit idx %d (%s): count=%d want %d", i, HandTypeLabel(i), counts[i], wantOffsuit)
		}
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != 1326 {
		t.Errorf("total hole pairs %d want 1326", total)
	}
}

// TestHandTypeIdxKnownLabels — spot-check specific hand mappings.
func TestHandTypeIdxKnownLabels(t *testing.T) {
	cases := []struct {
		c1, c2 string
		want   string
	}{
		{"As", "Ah", "AA"},
		{"Kc", "Kd", "KK"},
		{"2c", "2d", "22"},
		{"As", "Ks", "AKs"},
		{"As", "Kh", "AKo"},
		{"Tc", "Td", "TT"},
		{"7c", "2d", "72o"},
		{"3c", "2c", "32s"},
		{"3c", "2d", "32o"},
		{"Qc", "Js", "QJo"},
		{"Qs", "Js", "QJs"},
	}
	for _, c := range cases {
		c1 := nlhe.ParseCard(c.c1)
		c2 := nlhe.ParseCard(c.c2)
		idx := HandTypeIdx(c1, c2)
		got := HandTypeLabel(idx)
		if got != c.want {
			t.Errorf("HandTypeIdx(%s,%s)=%d (%q), want %q", c.c1, c.c2, idx, got, c.want)
		}
	}
}

// TestHandTypeLabelUnique — all 169 labels distinct.
func TestHandTypeLabelUnique(t *testing.T) {
	seen := make(map[string]int)
	for i := 0; i < NumPreflopHandTypes; i++ {
		lbl := HandTypeLabel(i)
		if prev, dup := seen[lbl]; dup {
			t.Errorf("dup label %q at idx %d (prev idx %d)", lbl, i, prev)
		}
		seen[lbl] = i
	}
	if len(seen) != NumPreflopHandTypes {
		t.Errorf("unique labels %d want %d", len(seen), NumPreflopHandTypes)
	}
}

// TestHandTypeIdxOrderInvariant — (As, Kh) and (Kh, As) → same idx.
func TestHandTypeIdxOrderInvariant(t *testing.T) {
	for c1 := nlhe.Card(0); c1 < nlhe.DeckSize; c1++ {
		for c2 := c1 + 1; c2 < nlhe.DeckSize; c2++ {
			a := HandTypeIdx(c1, c2)
			b := HandTypeIdx(c2, c1)
			if a != b {
				t.Errorf("order matters: (%v,%v)=%d (%v,%v)=%d", c1, c2, a, c2, c1, b)
			}
		}
	}
}
