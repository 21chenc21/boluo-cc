package nlhe

import "sort"

// Hand category, higher = better.
type HandCategory uint8

const (
	HighCard HandCategory = iota
	Pair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

// HandRank — total ordering for showdown. Higher value beats lower.
// Bit layout (high-to-low):
//
//	category (4 bits) | 5 rank tiebreakers (each 4 bits, high-to-low significance) = 24 bits used.
//
// Two players' HandRank compared via integer compare.
type HandRank uint32

func makeRank(cat HandCategory, tiebreak [5]uint8) HandRank {
	r := uint32(cat) << 20
	for i, t := range tiebreak {
		r |= uint32(t&0xF) << uint(16-i*4)
	}
	return HandRank(r)
}

// Evaluate7 — evaluate the best 5-card poker hand from 7 cards.
// Pure Go implementation; correct but not optimized for billion-call paths
// (TODO: cgo OMPEval for production). Sufficient for engine validation.
func Evaluate7(cards [7]Card) HandRank {
	// Bucket by rank and by suit.
	var rankCount [NumRanks]uint8
	var suitCount [NumSuits]uint8
	var suitedRanks [NumSuits][NumRanks]bool
	for _, c := range cards {
		rankCount[c.Rank()]++
		suitCount[c.Suit()]++
		suitedRanks[c.Suit()][c.Rank()] = true
	}

	// 1. Check flush + straight flush (highest priority).
	var flushSuit int = -1
	for s, n := range suitCount {
		if n >= 5 {
			flushSuit = s
			break
		}
	}
	if flushSuit >= 0 {
		// Check straight flush within the suited set.
		sf := bestStraight(suitedRanks[flushSuit][:])
		if sf >= 0 {
			return makeRank(StraightFlush, [5]uint8{uint8(sf), 0, 0, 0, 0})
		}
	}

	// 2. Sort ranks by count desc, then rank desc; identify pair/trips/quads.
	type rankBin struct {
		count uint8
		rank  uint8
	}
	var bins []rankBin
	for r := NumRanks - 1; r >= 0; r-- {
		if rankCount[r] > 0 {
			bins = append(bins, rankBin{rankCount[r], uint8(r)})
		}
	}
	sort.SliceStable(bins, func(i, j int) bool {
		if bins[i].count != bins[j].count {
			return bins[i].count > bins[j].count
		}
		return bins[i].rank > bins[j].rank
	})

	// Quads.
	if bins[0].count == 4 {
		var kicker uint8
		for _, b := range bins {
			if b.count != 4 {
				if b.rank > kicker {
					kicker = b.rank
				}
			}
		}
		return makeRank(FourOfAKind, [5]uint8{bins[0].rank, kicker, 0, 0, 0})
	}

	// Full house: a triple AND any pair (or another triple).
	if bins[0].count == 3 {
		for _, b := range bins[1:] {
			if b.count >= 2 {
				return makeRank(FullHouse, [5]uint8{bins[0].rank, b.rank, 0, 0, 0})
			}
		}
	}

	// Flush (not straight flush — handled above).
	if flushSuit >= 0 {
		// Top 5 ranks within flush suit.
		var top5 [5]uint8
		n := 0
		for r := NumRanks - 1; r >= 0 && n < 5; r-- {
			if suitedRanks[flushSuit][r] {
				top5[n] = uint8(r)
				n++
			}
		}
		return makeRank(Flush, top5)
	}

	// Straight.
	var rankPresent [NumRanks]bool
	for r, c := range rankCount {
		if c > 0 {
			rankPresent[r] = true
		}
	}
	if s := bestStraight(rankPresent[:]); s >= 0 {
		return makeRank(Straight, [5]uint8{uint8(s), 0, 0, 0, 0})
	}

	// Trips (no full house at this point).
	if bins[0].count == 3 {
		var k1, k2 uint8
		got := 0
		for _, b := range bins[1:] {
			if b.count == 1 {
				if got == 0 {
					k1 = b.rank
				} else if got == 1 {
					k2 = b.rank
				}
				got++
				if got == 2 {
					break
				}
			}
		}
		return makeRank(ThreeOfAKind, [5]uint8{bins[0].rank, k1, k2, 0, 0})
	}

	// Two pair.
	if bins[0].count == 2 && len(bins) >= 2 && bins[1].count == 2 {
		var kicker uint8
		for _, b := range bins[2:] {
			if b.rank > kicker {
				kicker = b.rank
			}
		}
		return makeRank(TwoPair, [5]uint8{bins[0].rank, bins[1].rank, kicker, 0, 0})
	}

	// One pair.
	if bins[0].count == 2 {
		var k [3]uint8
		got := 0
		for _, b := range bins[1:] {
			if b.count == 1 {
				k[got] = b.rank
				got++
				if got == 3 {
					break
				}
			}
		}
		return makeRank(Pair, [5]uint8{bins[0].rank, k[0], k[1], k[2], 0})
	}

	// High card.
	var k [5]uint8
	for i := 0; i < 5; i++ {
		k[i] = bins[i].rank
	}
	return makeRank(HighCard, k)
}

// bestStraight — given a 13-elem rank-presence array, return the top rank of
// the highest 5-card straight, or -1 if none. Wheel (A-2-3-4-5) returns 3 (= 5).
func bestStraight(present []bool) int {
	if len(present) != NumRanks {
		return -1
	}
	// Check from A-high down. Need 5 consecutive ranks: top, top-1, ..., top-4.
	// So top must be ≥ 4. Wheel (A-2-3-4-5) handled separately below.
	for top := NumRanks - 1; top >= 4; top-- {
		ok := true
		for i := 0; i < 5; i++ {
			if !present[top-i] {
				ok = false
				break
			}
		}
		if ok {
			return top
		}
	}
	// Wheel: A,2,3,4,5 — A is rank 12, 5 is rank 3.
	if present[12] && present[0] && present[1] && present[2] && present[3] {
		return 3
	}
	return -1
}

// Category — extract the category from a HandRank for debugging.
func (r HandRank) Category() HandCategory { return HandCategory(r >> 20) }
