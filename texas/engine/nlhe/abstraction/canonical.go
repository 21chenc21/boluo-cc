package abstraction

import (
	"fmt"

	"github.com/boluo/texas/engine/nlhe"
)

// 169 canonical preflop hand types in standard ordering:
//   - Indices 0..12:   pocket pairs AA(0=A)...22(12=2) — by descending rank
//   - Indices 13..90:  suited hands  (78 = C(13,2))
//   - Indices 91..168: offsuit hands (78)
//
// Suited/offsuit ordering: for ranks (high, low) with high > low, lexicographic
// by (high desc, low desc). AKs at 13, AQs at 14, ..., 32s at 90.
// AKo at 91, ..., 32o at 168.
const NumPreflopHandTypes = 169

// HandTypeIdx returns the canonical preflop hand type index for two hole cards.
// Suit-collapsed: input ordering doesn't matter.
func HandTypeIdx(c1, c2 nlhe.Card) int {
	r1, r2 := c1.Rank(), c2.Rank()
	if r1 < r2 {
		r1, r2 = r2, r1 // r1 = higher rank
	}
	if c1.Rank() == c2.Rank() {
		// Pair: ranks 0..12 in card encoding (2..A), but we want AA=0 KK=1 .. 22=12.
		return int(nlhe.NumRanks - 1 - r1) // 12-r1: A(12)→0, K(11)→1, ..., 2(0)→12
	}
	suited := c1.Suit() == c2.Suit()
	// pairIdx maps (high, low) → 0..77 with high > low.
	// e.g. (A=12,K=11)=0, (A=12,Q=10)=1, ..., (A=12,2=0)=11
	//      (K=11,Q=10)=12, ...
	pairIdx := nonPairIdx(r1, r2)
	if suited {
		return 13 + pairIdx
	}
	return 13 + 78 + pairIdx
}

// nonPairIdx — maps (high rank, low rank) with high > low to a 0..77 index.
// Layout: for high=A: 0..11 (12 non-pair low ranks K..2)
//         for high=K: 12..22 (11 non-pair low ranks Q..2)
//         ...
//         for high=3: 77 (only low=2)
//
// Formula: skip = sum over j from 12 down to high+1 of j
//            (j is the count of pairs with high=j+0; pair count for high level k is k)
// Equivalently: skip = (12-high) * (12+(high+1)) / 2  for descending high
func nonPairIdx(high, low uint8) int {
	if high <= low {
		panic(fmt.Sprintf("nonPairIdx: high=%d <= low=%d", high, low))
	}
	// Number of non-pair combos with strictly higher high-rank:
	// for h in {high+1, ..., 12}: there are h slots for the low rank.
	// So skip = sum_{h=high+1}^{12} h.
	skip := 0
	for h := int(high) + 1; h <= int(nlhe.NumRanks-1); h++ {
		skip += h
	}
	// Within this high rank, ranks 0..high-1 are valid low ranks, in DESCENDING order.
	// (high-1) → 0, (high-2) → 1, ..., 0 → high-1
	return skip + int(high-1-low)
}

// HandTypeLabel returns a readable name for hand type idx (e.g. "AKs", "TT", "72o").
func HandTypeLabel(idx int) string {
	if idx < 0 || idx >= NumPreflopHandTypes {
		return "?"
	}
	if idx < 13 {
		// Pair: idx 0=A, 1=K, ..., 12=2
		r := 12 - idx
		return string([]byte{rankChar(uint8(r)), rankChar(uint8(r))})
	}
	suited := idx < 13+78
	off := idx - 13
	if !suited {
		off -= 78
	}
	// Reverse nonPairIdx: find (high, low) such that nonPairIdx(high,low) == off.
	for high := uint8(nlhe.NumRanks - 1); high >= 1; high-- {
		// Number of low-ranks for this high = high (since low ∈ 0..high-1)
		if off < int(high) {
			low := uint8(int(high) - 1 - off)
			suffix := byte('s')
			if !suited {
				suffix = 'o'
			}
			return string([]byte{rankChar(high), rankChar(low), suffix})
		}
		off -= int(high)
	}
	return "?"
}

func rankChar(r uint8) byte {
	return []byte{'2', '3', '4', '5', '6', '7', '8', '9', 'T', 'J', 'Q', 'K', 'A'}[r]
}
