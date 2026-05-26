package nlhe

import (
	"math/rand"
	"sort"
	"testing"
)

// ==============================================================================
// Brute-force "obviously correct" 5-card hand evaluator.
//
// Used to cross-check Evaluate7 (the fast 7-card scorer): for each 7-card hand,
// enumerate all C(7,5)=21 five-card subsets, score each with bruteEval5, take
// max. Compare with Evaluate7 — they MUST agree on category and tiebreakers.
//
// bruteEval5 is intentionally simple/slow but clearly matches the rules.
// ==============================================================================

func bruteEval5(cards [5]Card) HandRank {
	var rankCount [NumRanks]int
	var suitCount [NumSuits]int
	for _, c := range cards {
		rankCount[c.Rank()]++
		suitCount[c.Suit()]++
	}

	flush := false
	for _, n := range suitCount {
		if n == 5 {
			flush = true
		}
	}

	// Straight detection on rank histogram.
	straightTop := -1
	for top := NumRanks - 1; top >= 4; top-- {
		ok := true
		for i := 0; i < 5; i++ {
			if rankCount[top-i] == 0 {
				ok = false
				break
			}
		}
		if ok {
			straightTop = top
			break
		}
	}
	if straightTop < 0 && rankCount[12] > 0 && rankCount[0] > 0 &&
		rankCount[1] > 0 && rankCount[2] > 0 && rankCount[3] > 0 {
		straightTop = 3 // wheel A-2-3-4-5
	}

	if flush && straightTop >= 0 {
		return makeRank(StraightFlush, [5]uint8{uint8(straightTop), 0, 0, 0, 0})
	}

	// Count by frequency.
	type binFreq struct {
		freq int
		rank int
	}
	var bins []binFreq
	for r := NumRanks - 1; r >= 0; r-- {
		if rankCount[r] > 0 {
			bins = append(bins, binFreq{rankCount[r], r})
		}
	}
	sort.SliceStable(bins, func(i, j int) bool {
		if bins[i].freq != bins[j].freq {
			return bins[i].freq > bins[j].freq
		}
		return bins[i].rank > bins[j].rank
	})

	switch {
	case bins[0].freq == 4:
		var kicker uint8
		for _, b := range bins[1:] {
			if uint8(b.rank) > kicker {
				kicker = uint8(b.rank)
			}
		}
		return makeRank(FourOfAKind, [5]uint8{uint8(bins[0].rank), kicker, 0, 0, 0})
	case bins[0].freq == 3 && len(bins) >= 2 && bins[1].freq == 2:
		return makeRank(FullHouse, [5]uint8{uint8(bins[0].rank), uint8(bins[1].rank), 0, 0, 0})
	case flush:
		// Top 5 ranks (5-card flush — all 5 cards).
		var top5 [5]uint8
		for i, c := range cards {
			top5[i] = uint8(c.Rank())
		}
		sort.Slice(top5[:], func(i, j int) bool { return top5[i] > top5[j] })
		return makeRank(Flush, top5)
	case straightTop >= 0:
		return makeRank(Straight, [5]uint8{uint8(straightTop), 0, 0, 0, 0})
	case bins[0].freq == 3:
		var k [2]uint8
		got := 0
		for _, b := range bins[1:] {
			if b.freq == 1 {
				k[got] = uint8(b.rank)
				got++
				if got == 2 {
					break
				}
			}
		}
		return makeRank(ThreeOfAKind, [5]uint8{uint8(bins[0].rank), k[0], k[1], 0, 0})
	case bins[0].freq == 2 && len(bins) >= 2 && bins[1].freq == 2:
		var kicker uint8
		for _, b := range bins[2:] {
			if uint8(b.rank) > kicker {
				kicker = uint8(b.rank)
			}
		}
		return makeRank(TwoPair, [5]uint8{uint8(bins[0].rank), uint8(bins[1].rank), kicker, 0, 0})
	case bins[0].freq == 2:
		var k [3]uint8
		got := 0
		for _, b := range bins[1:] {
			if b.freq == 1 {
				k[got] = uint8(b.rank)
				got++
				if got == 3 {
					break
				}
			}
		}
		return makeRank(Pair, [5]uint8{uint8(bins[0].rank), k[0], k[1], k[2], 0})
	}
	// High card.
	var top5 [5]uint8
	for i, c := range cards {
		top5[i] = uint8(c.Rank())
	}
	sort.Slice(top5[:], func(i, j int) bool { return top5[i] > top5[j] })
	return makeRank(HighCard, top5)
}

// bruteEval7 — for each 7-card hand, enumerate C(7,5)=21 5-card subsets and
// return the max via bruteEval5. This is THE GOLD STANDARD: provably correct
// since it directly applies the rules of poker.
func bruteEval7(cards [7]Card) HandRank {
	var best HandRank
	indices := [5]int{}
	var permute func(start, depth int)
	permute = func(start, depth int) {
		if depth == 5 {
			var subset [5]Card
			for i, idx := range indices {
				subset[i] = cards[idx]
			}
			r := bruteEval5(subset)
			if r > best {
				best = r
			}
			return
		}
		for i := start; i < 7; i++ {
			indices[depth] = i
			permute(i+1, depth+1)
		}
	}
	permute(0, 0)
	return best
}

// TestEvaluate7VsBruteForce — STRONGEST eval correctness evidence.
//
// For 10,000 random 7-card hands, Evaluate7 must equal bruteEval7.
// Any disagreement = bug in Evaluate7 (or bruteEval5/bruteEval7).
//
// Pseudo-deterministic via seed=42 so re-runnable. Use -count=3 to verify.
func TestEvaluate7VsBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	trials := 10000
	if !testing.Short() {
		trials = 100000 // heavy mode
	}
	mismatches := 0
	categoryHits := [10]int{}

	for trial := 0; trial < trials; trial++ {
		// Draw 7 distinct random cards.
		var seen [DeckSize]bool
		var hand [7]Card
		for i := 0; i < 7; i++ {
			for {
				c := Card(rng.Intn(DeckSize))
				if !seen[c] {
					seen[c] = true
					hand[i] = c
					break
				}
			}
		}
		fast := Evaluate7(hand)
		brute := bruteEval7(hand)
		categoryHits[fast.Category()]++
		if fast != brute {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("trial %d: Evaluate7 vs brute disagree", trial)
				t.Logf("  cards: %v %v %v %v %v %v %v",
					hand[0], hand[1], hand[2], hand[3], hand[4], hand[5], hand[6])
				t.Logf("  Evaluate7=%v (cat=%d) vs brute=%v (cat=%d)",
					fast, fast.Category(), brute, brute.Category())
			}
		}
	}

	t.Logf("Evaluate7 vs brute-force: %d trials, %d mismatches", trials, mismatches)
	t.Logf("Category coverage in %d trials:", trials)
	names := [10]string{"high", "pair", "twopair", "trips", "straight",
		"flush", "fullhouse", "quads", "straightflush", "(unused)"}
	for cat, n := range categoryHits {
		if n > 0 {
			t.Logf("  %s: %d (%.2f%%)", names[cat], n, float64(n)/float64(trials)*100)
		}
	}
}

// TestEvaluate7ConsistentUnderShuffle — same 7 cards in different orders → same rank.
func TestEvaluate7ConsistentUnderShuffle(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const trials = 1000
	for trial := 0; trial < trials; trial++ {
		// Random 7-card hand.
		var seen [DeckSize]bool
		var hand [7]Card
		for i := 0; i < 7; i++ {
			for {
				c := Card(rng.Intn(DeckSize))
				if !seen[c] {
					seen[c] = true
					hand[i] = c
					break
				}
			}
		}
		r1 := Evaluate7(hand)

		// Shuffle and re-eval 5 times.
		for shuffle := 0; shuffle < 5; shuffle++ {
			perm := rng.Perm(7)
			var shuffled [7]Card
			for i, p := range perm {
				shuffled[i] = hand[p]
			}
			r2 := Evaluate7(shuffled)
			if r1 != r2 {
				t.Errorf("trial %d shuffle %d: order-dependent eval %v vs %v",
					trial, shuffle, r1, r2)
			}
		}
	}
	t.Logf("Evaluate7 order-invariant verified across %d × 5 shuffles", trials)
}
