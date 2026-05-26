package abstraction

import (
	"sort"

	"github.com/boluo/texas/engine/nlhe"
)

// Flop canonicalization — reduce 22100 unordered flop combinations (C(52,3))
// to the unique equivalence class under suit-renaming.
//
// Suit-renaming symmetry: poker hand strength is invariant under any permutation
// of the 4 suits. E.g. a flop "AcKc2c" is strategically equivalent to "AhKh2h".
//
// Canonical form: sort cards by rank descending, then rename suits so that
// the first occurrence is suit 0, second new suit is 1, etc.
//
//	AcKc2c → AcKc2c → "AaKa2a" (all suit 0, becomes suit a)
//	AhKc2c → after rank-sort same, rename: A's suit "h" → 0, K&2's suit "c" → 1
//	         → "AaKb2b"
//
// We also need to include hole cards in canonicalization (since they constrain
// suit identity). So full canonical = canonical_with_hole(hole, board).

// CanonicalFlopID — canonical numeric identifier for (board0, board1, board2)
// when paired with given hole cards. Captures suit equivalence classes
// considering hole + board jointly.
//
// Encoding: rank pattern + suit grouping pattern. Returns a uint32 ID + bool
// indicating success.
type CanonicalFlopID uint32

// canonicalSuitMap — given 5 cards (2 hole + 3 board), return suit-renumbering
// map such that the first NEW suit encountered gets 0, second gets 1, etc.
// Returns the renumbered cards.
func canonicalSuitMap(cards []nlhe.Card) []nlhe.Card {
	var suitRemap [nlhe.NumSuits]uint8
	for i := range suitRemap {
		suitRemap[i] = 255
	}
	nextNew := uint8(0)
	out := make([]nlhe.Card, len(cards))
	for i, c := range cards {
		su := c.Suit()
		if suitRemap[su] == 255 {
			suitRemap[su] = nextNew
			nextNew++
		}
		out[i] = nlhe.MakeCard(c.Rank(), suitRemap[su])
	}
	return out
}

// CanonicalHoleFlop — canonical representation of (hole0, hole1, flop0, flop1, flop2).
//
// Input ordering doesn't matter; output is deterministic canonical form.
//
// Algorithm:
//  1. Sort hole [high, low] by rank desc.
//  2. Sort flop [high, mid, low] by rank desc.
//  3. Apply suit canonicalization across all 5 cards (hole first, then board).
//
// Returns 5 canonical cards: hole[0], hole[1], board[0], board[1], board[2].
func CanonicalHoleFlop(h0, h1, b0, b1, b2 nlhe.Card) [5]nlhe.Card {
	// Sort hole by rank descending.
	hole := []nlhe.Card{h0, h1}
	sort.Slice(hole, func(i, j int) bool { return hole[i].Rank() > hole[j].Rank() })

	// Sort board by rank descending.
	board := []nlhe.Card{b0, b1, b2}
	sort.Slice(board, func(i, j int) bool { return board[i].Rank() > board[j].Rank() })

	// Apply suit canonicalization across all 5 cards (hole then board).
	all := []nlhe.Card{hole[0], hole[1], board[0], board[1], board[2]}
	mapped := canonicalSuitMap(all)
	return [5]nlhe.Card{mapped[0], mapped[1], mapped[2], mapped[3], mapped[4]}
}

// CanonicalHoleFlopKey — string key for the canonical (hole, flop) class.
// Used as map key during bucket construction. Compact: 5 chars (suit) + 5 chars (rank).
func CanonicalHoleFlopKey(h0, h1, b0, b1, b2 nlhe.Card) string {
	canon := CanonicalHoleFlop(h0, h1, b0, b1, b2)
	var buf [10]byte
	for i, c := range canon {
		buf[i*2] = rankChar(c.Rank())
		buf[i*2+1] = 'a' + c.Suit()
	}
	return string(buf[:])
}

// CanonicalHoleBoard — generalized form for any board length 0/3/4/5 (preflop/flop/turn/river).
// Sort hole desc by rank, sort board desc by rank, then suit-canonicalize across all.
// Returns canonical card slice (len = 2 + len(board)).
func CanonicalHoleBoard(hole [2]nlhe.Card, board []nlhe.Card) []nlhe.Card {
	h := []nlhe.Card{hole[0], hole[1]}
	sort.Slice(h, func(i, j int) bool { return h[i].Rank() > h[j].Rank() })
	bd := make([]nlhe.Card, len(board))
	copy(bd, board)
	sort.Slice(bd, func(i, j int) bool { return bd[i].Rank() > bd[j].Rank() })
	all := append(h, bd...)
	return canonicalSuitMap(all)
}

// CanonicalHoleBoardKey — string key for any (hole, board) class.
// Length = 2 * (2 + len(board)) bytes.
func CanonicalHoleBoardKey(hole [2]nlhe.Card, board []nlhe.Card) string {
	canon := CanonicalHoleBoard(hole, board)
	buf := make([]byte, 2*len(canon))
	for i, c := range canon {
		buf[i*2] = rankChar(c.Rank())
		buf[i*2+1] = 'a' + c.Suit()
	}
	return string(buf)
}
