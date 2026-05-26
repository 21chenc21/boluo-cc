package ofc

import "testing"

func mustParse(s string) Card {
	c, ok := ParseCard(s)
	if !ok {
		panic("invalid card: " + s)
	}
	return c
}

func parseHand(strs ...string) []Card {
	out := make([]Card, len(strs))
	for i, s := range strs {
		out[i] = mustParse(s)
	}
	return out
}

func TestEvaluate3(t *testing.T) {
	cases := []struct {
		name  string
		cards []string
		typ   int
	}{
		{"high card", []string{"As", "Kh", "9d"}, TypeHighCard},
		{"pair", []string{"Ks", "Kh", "5d"}, TypePair},
		{"trips", []string{"Qs", "Qh", "Qd"}, TypeThreeOfAKind},
		{"low pair", []string{"2s", "2h", "3d"}, TypePair},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := Evaluate3(parseHand(tc.cards...))
			if ev.Type != tc.typ {
				t.Errorf("type: got %d, want %d", ev.Type, tc.typ)
			}
		})
	}
}

func TestEvaluate3OrderingPair(t *testing.T) {
	// KK + 5 should beat KK + 2
	hi := Evaluate3(parseHand("Ks", "Kh", "5d"))
	lo := Evaluate3(parseHand("Ks", "Kh", "2d"))
	if hi.Value <= lo.Value {
		t.Errorf("KK5 (%d) should be > KK2 (%d)", hi.Value, lo.Value)
	}
}

func TestEvaluate5(t *testing.T) {
	cases := []struct {
		name  string
		cards []string
		typ   int
	}{
		{"high card A", []string{"As", "Kh", "9d", "5c", "2s"}, TypeHighCard},
		{"pair", []string{"Ks", "Kh", "9d", "5c", "2s"}, TypePair},
		{"two pair", []string{"Ks", "Kh", "9d", "9c", "2s"}, TypeTwoPair},
		{"trips", []string{"Ks", "Kh", "Kd", "9c", "2s"}, TypeThreeOfAKind},
		{"straight 9-K", []string{"9s", "Th", "Jd", "Qc", "Ks"}, TypeStraight},
		{"wheel A2345", []string{"As", "2h", "3d", "4c", "5s"}, TypeStraight},
		{"flush", []string{"As", "Ks", "9s", "5s", "2s"}, TypeFlush},
		{"full house", []string{"Ks", "Kh", "Kd", "9c", "9s"}, TypeFullHouse},
		{"quads", []string{"Ks", "Kh", "Kd", "Kc", "2s"}, TypeFourOfAKind},
		{"straight flush 9-K", []string{"9s", "Ts", "Js", "Qs", "Ks"}, TypeStraightFlush},
		{"royal flush", []string{"Ts", "Js", "Qs", "Ks", "As"}, TypeRoyalFlush},
		{"wheel SF", []string{"As", "2s", "3s", "4s", "5s"}, TypeStraightFlush},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := Evaluate5(parseHand(tc.cards...))
			if ev.Type != tc.typ {
				t.Errorf("type: got %d, want %d", ev.Type, tc.typ)
			}
		})
	}
}

func TestEvaluate5Ordering(t *testing.T) {
	// AAA > KKK
	a := Evaluate5(parseHand("As", "Ah", "Ad", "5c", "2s"))
	k := Evaluate5(parseHand("Ks", "Kh", "Kd", "5c", "2s"))
	if a.Value <= k.Value {
		t.Errorf("AAA (%d) should beat KKK (%d)", a.Value, k.Value)
	}
	// AAA-K vs AAA-Q (same trips, different kicker)
	ak := Evaluate5(parseHand("As", "Ah", "Ad", "Kc", "2s"))
	aq := Evaluate5(parseHand("As", "Ah", "Ad", "Qc", "2s"))
	if ak.Value <= aq.Value {
		t.Errorf("AAA-K should beat AAA-Q")
	}
	// 9-high straight beats wheel? actually 9-K straight high=K=11, wheel high=3 → 9-K wins
	s9 := Evaluate5(parseHand("9s", "Th", "Jd", "Qc", "Ks"))
	wheel := Evaluate5(parseHand("As", "2h", "3d", "4c", "5s"))
	if s9.Value <= wheel.Value {
		t.Errorf("9-K straight (%d) should beat wheel (%d)", s9.Value, wheel.Value)
	}
}

func TestCardRoundtrip(t *testing.T) {
	for _, s := range []string{"2s", "Th", "Kc", "Ad", "X"} {
		c, ok := ParseCard(s)
		if !ok {
			t.Fatalf("parse %s failed", s)
		}
		if c.String() != s {
			t.Errorf("roundtrip %s → %s", s, c.String())
		}
	}
}
