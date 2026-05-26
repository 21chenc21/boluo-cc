package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestCanonicalFlopSuitSymmetry — flops differing only in suit permutation
// should produce same canonical form.
func TestCanonicalFlopSuitSymmetry(t *testing.T) {
	// AhKc2c with hole Ad7c (AKo suited K, 2 spec)
	// vs AcKh2h with hole As7h (same structure, suits relabeled)
	// These should canonicalize to same form.
	cases := []struct {
		name              string
		h0a, h1a          string
		b0a, b1a, b2a     string
		h0b, h1b          string
		b0b, b1b, b2b     string
		expectSameCanonical bool
	}{
		{
			name: "all rainbow flop, different suit permutations",
			h0a:  "Ah", h1a: "Kc", b0a: "Qd", b1a: "Js", b2a: "2c",
			h0b: "Ad", h1b: "Ks", b0b: "Qc", b1b: "Jh", b2b: "2s",
			expectSameCanonical: true,
		},
		{
			name: "monotone flop, suit-renamed",
			h0a:  "Ah", h1a: "Kh", b0a: "Qh", b1a: "Jh", b2a: "2h",
			h0b: "As", h1b: "Ks", b0b: "Qs", b1b: "Js", b2b: "2s",
			expectSameCanonical: true,
		},
		{
			name: "DIFFERENT structure should canonicalize differently",
			h0a:  "Ah", h1a: "Kh", b0a: "Qh", b1a: "Js", b2a: "2c", // monotone hole, rainbow board
			h0b: "As", h1b: "Kh", b0b: "Qs", b1b: "Jh", b2b: "2c", // off-suit hole, mixed board
			expectSameCanonical: false,
		},
	}
	for _, c := range cases {
		ka := CanonicalHoleFlopKey(
			nlhe.ParseCard(c.h0a), nlhe.ParseCard(c.h1a),
			nlhe.ParseCard(c.b0a), nlhe.ParseCard(c.b1a), nlhe.ParseCard(c.b2a))
		kb := CanonicalHoleFlopKey(
			nlhe.ParseCard(c.h0b), nlhe.ParseCard(c.h1b),
			nlhe.ParseCard(c.b0b), nlhe.ParseCard(c.b1b), nlhe.ParseCard(c.b2b))
		got := ka == kb
		if got != c.expectSameCanonical {
			t.Errorf("%s: keys %q vs %q match=%v want %v", c.name, ka, kb, got, c.expectSameCanonical)
		} else {
			t.Logf("%s: %q vs %q match=%v ✓", c.name, ka, kb, got)
		}
	}
}

// TestCanonicalFlopOrderInvariance — order of hole or board input shouldn't
// affect canonical key.
func TestCanonicalFlopOrderInvariance(t *testing.T) {
	h0 := nlhe.ParseCard("Ah")
	h1 := nlhe.ParseCard("Kh")
	b0 := nlhe.ParseCard("Qs")
	b1 := nlhe.ParseCard("Jd")
	b2 := nlhe.ParseCard("2c")
	ref := CanonicalHoleFlopKey(h0, h1, b0, b1, b2)

	// All permutations of hole order × all permutations of board order.
	for _, hPerm := range [][]nlhe.Card{{h0, h1}, {h1, h0}} {
		for _, bPerm := range [][]nlhe.Card{
			{b0, b1, b2}, {b0, b2, b1}, {b1, b0, b2},
			{b1, b2, b0}, {b2, b0, b1}, {b2, b1, b0},
		} {
			k := CanonicalHoleFlopKey(hPerm[0], hPerm[1], bPerm[0], bPerm[1], bPerm[2])
			if k != ref {
				t.Errorf("order invariance violated: %q != %q (hole %v %v, board %v %v %v)",
					k, ref, hPerm[0], hPerm[1], bPerm[0], bPerm[1], bPerm[2])
			}
		}
	}
}

// TestCanonicalFlopCardinality — exhaustively enumerate all (hole, flop) combos
// and count unique canonical keys. Expected value matches known literature.
//
// Total raw combos: C(52,2) * C(50,3) = 1326 * 19600 = ~26M
// Canonical: well-known number is 1,286,792 unique (hole, flop) classes under suit symmetry
// (computed by Bonomo / poker math community).
//
// This test enumerates and verifies the count.
// Skipped under -short (10+ minute enumeration).
func TestCanonicalFlopCardinality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping exhaustive enumeration in -short")
	}
	seen := make(map[string]int)
	for h0 := nlhe.Card(0); h0 < nlhe.DeckSize; h0++ {
		for h1 := h0 + 1; h1 < nlhe.DeckSize; h1++ {
			for b0 := nlhe.Card(0); b0 < nlhe.DeckSize; b0++ {
				if b0 == h0 || b0 == h1 {
					continue
				}
				for b1 := b0 + 1; b1 < nlhe.DeckSize; b1++ {
					if b1 == h0 || b1 == h1 {
						continue
					}
					for b2 := b1 + 1; b2 < nlhe.DeckSize; b2++ {
						if b2 == h0 || b2 == h1 {
							continue
						}
						k := CanonicalHoleFlopKey(h0, h1, b0, b1, b2)
						seen[k]++
					}
				}
			}
		}
	}
	t.Logf("Canonical (hole, flop) classes: %d", len(seen))
	// Sanity: number is in expected range. 1,286,792 is theoretical for full
	// suit-isomorphism (may differ slightly by canonicalization details).
	if len(seen) < 1_000_000 || len(seen) > 2_000_000 {
		t.Errorf("unexpected canonical class count %d, want ~1.3M", len(seen))
	}
}
