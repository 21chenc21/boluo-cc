package nlhe6

import (
	"hash/fnv"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

// MultiStreetID — abstract infoset key for a 6-max state using a card bucket
// bundle (preflop + 3 postflop). Mirrors abstraction.MultiStreetBuckets.ID
// for HUNL but accommodates up to 6 seats (3-bit position).
//
// Layout (uint64):
//   bits  0-1   street (2b)
//   bits  2-4   actor seat (3b, 0-5; HU uses 0/1)
//   bits  5-12  preflop bucket (8b)
//   bits 13-20  flop bucket
//   bits 21-28  turn bucket
//   bits 29-36  river bucket
//   bits 37-63  history hash (27b)
//
// Differences vs abstraction.MultiStreetBuckets.ID:
//  - position 1b → 3b (cost: hist hash 29b → 27b, collision rate ~1/2^27 still
//    safe for ~10^7 infosets per cell)
//
// Action history hash: per-street action sequence with separator.
// Implicit seat-of-action recovered at replay by engine rotation rules.
func MultiStreetID(b *abstraction.MultiStreetBuckets, s *State) uint64 {
	cur := s.Cur
	c0, c1 := s.Hole[cur][0], s.Hole[cur][1]
	pre := b.Preflop.For(c0, c1)
	if pre < 0 || pre > 255 {
		panic("MultiStreetID: preflop bucket out of range (K>256?)")
	}

	hole := [2]nlhe.Card{c0, c1}
	var flop, turn, river int
	mcS, mcSeed := b.MCSamplesFallback, b.FallbackSeed
	if s.NumBoard >= 3 && b.Flop != nil {
		flop = lookupStreet(b.Flop, hole, boardSlice(s, 3), mcS, mcSeed)
	}
	if s.NumBoard >= 4 && b.Turn != nil {
		turn = lookupStreet(b.Turn, hole, boardSlice(s, 4), mcS, mcSeed)
	}
	if s.NumBoard >= 5 && b.River != nil {
		river = lookupStreet(b.River, hole, boardSlice(s, 5), mcS, mcSeed)
	}
	if flop < 0 || flop > 255 || turn < 0 || turn > 255 || river < 0 || river > 255 {
		panic("MultiStreetID: postflop bucket out of range (K>256?)")
	}

	h := fnv.New32a()
	var hbuf [3]byte
	for st := 0; st < 4; st++ {
		for _, e := range s.Hist[st] {
			hbuf[0] = byte(e.Action.Kind)
			hbuf[1] = e.Action.SizeIdx
			hbuf[2] = byte(e.Seat)
			h.Write(hbuf[:3])
		}
		h.Write([]byte{0xfe})
	}
	histHash := uint64(h.Sum32() & 0x7FFFFFF) // 27 bits

	var id uint64
	id |= uint64(s.Street) & 0x3
	id |= uint64(cur) << 2
	id |= uint64(pre) << 5
	id |= uint64(flop) << 13
	id |= uint64(turn) << 21
	id |= uint64(river) << 29
	id |= histHash << 37
	return id
}

func boardSlice(s *State, n int) []nlhe.Card {
	out := make([]nlhe.Card, n)
	for i := 0; i < n; i++ {
		out[i] = s.Board[i]
	}
	return out
}

func lookupStreet(sb abstraction.StreetBucketer, hole [2]nlhe.Card, board []nlhe.Card, mcSamples int, seed int64) int {
	if mcSamples > 0 {
		return sb.ForOrFallback(hole, board, mcSamples, seed)
	}
	bid := sb.For(hole, board)
	if bid < 0 {
		return 0
	}
	return bid
}

// MultiStreetIDFn — returns a closure suitable for MCCFR.WithIDFn.
func MultiStreetIDFn(b *abstraction.MultiStreetBuckets) func(*State) uint64 {
	return func(s *State) uint64 { return MultiStreetID(b, s) }
}
