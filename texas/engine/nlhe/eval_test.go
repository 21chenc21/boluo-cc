package nlhe

import (
	"testing"
)

func mustCard(t *testing.T, s string) Card {
	t.Helper()
	c := ParseCard(s)
	if c == NoCard {
		t.Fatalf("ParseCard(%q) failed", s)
	}
	return c
}

// ──────────────────────── card encoding ────────────────────────

func TestCardParseStringRoundTrip(t *testing.T) {
	cases := []string{"2c", "9d", "Th", "As", "Kc", "Qs"}
	for _, s := range cases {
		c := ParseCard(s)
		if c == NoCard {
			t.Errorf("ParseCard(%q) failed", s)
			continue
		}
		got := c.String()
		if got != s {
			t.Errorf("%q → %v → %q", s, c, got)
		}
	}
}

func TestDeckCoverage(t *testing.T) {
	seen := make(map[Card]bool)
	for r := uint8(0); r < NumRanks; r++ {
		for s := uint8(0); s < NumSuits; s++ {
			c := MakeCard(r, s)
			if seen[c] {
				t.Errorf("dup card r=%d s=%d → %v", r, s, c)
			}
			seen[c] = true
			if c.Rank() != r || c.Suit() != s {
				t.Errorf("MakeCard(%d,%d)=%v r/s=%d,%d", r, s, c, c.Rank(), c.Suit())
			}
		}
	}
	if len(seen) != DeckSize {
		t.Errorf("deck %d, want %d", len(seen), DeckSize)
	}
}

// ──────────────────────── hand evaluation ────────────────────────

// Helper: evaluate a 7-card hand specified by 2-char codes.
func eval(t *testing.T, cards ...string) HandRank {
	t.Helper()
	if len(cards) != 7 {
		t.Fatalf("eval: need 7 cards, got %d", len(cards))
	}
	var arr [7]Card
	for i, s := range cards {
		arr[i] = mustCard(t, s)
	}
	return Evaluate7(arr)
}

func TestEvalCategoryHighCard(t *testing.T) {
	r := eval(t, "2c", "5d", "7h", "9s", "Jc", "Kh", "Ad")
	if r.Category() != HighCard {
		t.Errorf("HighCard expected, got %v", r.Category())
	}
}

func TestEvalCategoryPair(t *testing.T) {
	r := eval(t, "Ac", "Ad", "2h", "5s", "7c", "Th", "Js")
	if r.Category() != Pair {
		t.Errorf("Pair expected, got %v", r.Category())
	}
}

func TestEvalCategoryTwoPair(t *testing.T) {
	r := eval(t, "Ac", "Ad", "Kh", "Ks", "2c", "5h", "7s")
	if r.Category() != TwoPair {
		t.Errorf("TwoPair expected, got %v", r.Category())
	}
}

func TestEvalCategoryTrips(t *testing.T) {
	r := eval(t, "Ac", "Ad", "Ah", "2s", "5c", "7h", "Js")
	if r.Category() != ThreeOfAKind {
		t.Errorf("Trips expected, got %v", r.Category())
	}
}

func TestEvalCategoryStraight(t *testing.T) {
	r := eval(t, "5c", "6d", "7h", "8s", "9c", "2h", "Js")
	if r.Category() != Straight {
		t.Errorf("Straight expected, got %v", r.Category())
	}
}

func TestEvalCategoryWheel(t *testing.T) {
	// A,2,3,4,5 = wheel straight, lowest straight.
	r := eval(t, "Ac", "2d", "3h", "4s", "5c", "Kh", "Js")
	if r.Category() != Straight {
		t.Errorf("Wheel straight expected, got %v", r.Category())
	}
}

func TestEvalCategoryFlush(t *testing.T) {
	r := eval(t, "2c", "5c", "7c", "9c", "Jc", "Kh", "Ad")
	if r.Category() != Flush {
		t.Errorf("Flush expected, got %v", r.Category())
	}
}

func TestEvalCategoryFullHouse(t *testing.T) {
	r := eval(t, "Ac", "Ad", "Ah", "Kc", "Kd", "2h", "5s")
	if r.Category() != FullHouse {
		t.Errorf("FullHouse expected, got %v", r.Category())
	}
}

func TestEvalCategoryQuads(t *testing.T) {
	r := eval(t, "Ac", "Ad", "Ah", "As", "Kc", "2d", "5h")
	if r.Category() != FourOfAKind {
		t.Errorf("Quads expected, got %v", r.Category())
	}
}

func TestEvalCategoryStraightFlush(t *testing.T) {
	r := eval(t, "5c", "6c", "7c", "8c", "9c", "Ah", "Kd")
	if r.Category() != StraightFlush {
		t.Errorf("StraightFlush expected, got %v", r.Category())
	}
}

func TestEvalCategoryRoyalFlush(t *testing.T) {
	// Royal flush = straight flush A high.
	r := eval(t, "Tc", "Jc", "Qc", "Kc", "Ac", "2h", "5d")
	if r.Category() != StraightFlush {
		t.Errorf("Royal (StraightFlush) expected, got %v", r.Category())
	}
}

// ──────────────────────── ordering ────────────────────────

func TestEvalOrdering(t *testing.T) {
	// All categories in increasing strength must compare correctly.
	hands := []struct {
		name  string
		cards []string
	}{
		{"high card", []string{"2c", "5d", "7h", "9s", "Jc", "Kh", "Ad"}},
		{"pair AA", []string{"Ac", "Ad", "2h", "5s", "7c", "Th", "Js"}},
		{"two pair AAKK", []string{"Ac", "Ad", "Kh", "Ks", "2c", "5h", "7s"}},
		{"trips AAA", []string{"Ac", "Ad", "Ah", "2s", "5c", "7h", "Js"}},
		{"straight 9-high", []string{"5c", "6d", "7h", "8s", "9c", "2h", "Js"}},
		{"flush K-high", []string{"2c", "5c", "7c", "9c", "Kc", "Th", "Ad"}},
		{"full AAA KK", []string{"Ac", "Ad", "Ah", "Kc", "Kd", "2h", "5s"}},
		{"quads AAAA", []string{"Ac", "Ad", "Ah", "As", "Kc", "2d", "5h"}},
		{"straight flush 9-high", []string{"5c", "6c", "7c", "8c", "9c", "Ah", "Kd"}},
		{"royal", []string{"Tc", "Jc", "Qc", "Kc", "Ac", "2h", "5d"}},
	}
	for i := 1; i < len(hands); i++ {
		prev := eval(t, hands[i-1].cards...)
		curr := eval(t, hands[i].cards...)
		if curr <= prev {
			t.Errorf("ordering violation: %s (%v) <= %s (%v)",
				hands[i].name, curr, hands[i-1].name, prev)
		}
	}
}

func TestEvalHigherFlushBeatsLowerFlush(t *testing.T) {
	// A-high flush vs K-high flush, same suit.
	ah := eval(t, "Ac", "2c", "5c", "7c", "9c", "Th", "Jd")
	kh := eval(t, "Kc", "2c", "5c", "7c", "9c", "Th", "Jd")
	if ah <= kh {
		t.Errorf("A-flush %v should beat K-flush %v", ah, kh)
	}
}

func TestEvalKickerBreaksTie(t *testing.T) {
	// AA + K kicker vs AA + Q kicker.
	ak := eval(t, "Ac", "Ad", "Kh", "2s", "5c", "7h", "9s")
	aq := eval(t, "Ac", "Ad", "Qh", "2s", "5c", "7h", "9s")
	if ak <= aq {
		t.Errorf("AA-K kicker %v should beat AA-Q kicker %v", ak, aq)
	}
}

func TestEvalRoyalBeatsLowSF(t *testing.T) {
	royal := eval(t, "Tc", "Jc", "Qc", "Kc", "Ac", "2h", "5d")
	low := eval(t, "5c", "6c", "7c", "8c", "9c", "Ah", "Kd")
	if royal <= low {
		t.Errorf("royal %v should beat 5-9 SF %v", royal, low)
	}
}

// Wheel-vs-not test: 5-high wheel beats high card, loses to 6-high straight.
func TestEvalWheelOrdering(t *testing.T) {
	wheel := eval(t, "Ac", "2d", "3h", "4s", "5c", "Kh", "Js")
	highCard := eval(t, "2c", "5d", "7h", "9s", "Jc", "Kh", "Ad")
	straight6 := eval(t, "2c", "3d", "4h", "5s", "6c", "Kh", "Ad")
	if wheel <= highCard {
		t.Errorf("wheel %v should beat high card %v", wheel, highCard)
	}
	if straight6 <= wheel {
		t.Errorf("6-straight %v should beat wheel %v", straight6, wheel)
	}
}
