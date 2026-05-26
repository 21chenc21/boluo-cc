package abstraction

import (
	"hash/fnv"

	"github.com/boluo/texas/engine/nlhe"
)

// StreetBucketer — interface implemented by both EHS-based StreetBuckets and
// OCHS-based StreetOCHSBuckets, so MultiStreetBuckets can mix-and-match by
// street (e.g. EHS flop + OCHS turn/river for cheap-then-precise).
type StreetBucketer interface {
	For(hole [2]nlhe.Card, board []nlhe.Card) int
	ForOrFallback(hole [2]nlhe.Card, board []nlhe.Card, mcSamples int, seed int64) int
}

// MultiStreetBuckets — preflop + postflop bucket bundle for full-game MCCFR.
//
// Preflop is required. Postflop streets are optional via the StreetBucketer
// interface (EHS or OCHS).
type MultiStreetBuckets struct {
	Preflop *PreflopBuckets
	Flop    StreetBucketer // street=3 (EHS *StreetBuckets or OCHS *StreetOCHSBuckets)
	Turn    StreetBucketer // street=4
	River   StreetBucketer // street=5

	// MCSamplesFallback — when a postflop (hole, board) class wasn't seen at
	// build time, ForOrFallback runs this many MC equity samples + picks the
	// nearest bucket center. Cost: ~50µs * samples per fallback lookup.
	MCSamplesFallback int
	// FallbackSeed — seed for fallback MC. Determinism within a process.
	FallbackSeed int64
}

// ID — abstract infoset key for any street.
//
// Layout (uint64):
//
//	bits  0-1   street (0=preflop, 1=flop, 2=turn, 3=river)
//	bit     2   position (0 = P0/SB, 1 = P1/BB)
//	bits  3-10  preflop bucket (8 bits, K up to 256)
//	bits 11-18  flop bucket (0 if street<flop)
//	bits 19-26  turn bucket (0 if street<turn)
//	bits 27-34  river bucket (0 if street<river)
//	bits 35-63  bet-history FNV hash (29 bits, collisions ~1/2^29 per
//	            (bucket, street, position) cell)
//
// Assumes K ≤ 256 per street (8 bits each).
func (b *MultiStreetBuckets) ID(s *nlhe.State) uint64 {
	// Preflop bucket.
	pre := b.Preflop.For(s.Hole[s.Cur][0], s.Hole[s.Cur][1])
	if pre < 0 || pre > 255 {
		panic("MultiStreetBuckets.ID: preflop bucket out of range (K>256?)")
	}

	hole := s.Hole[s.Cur]
	var flop, turn, river int
	if s.NumBoard >= 3 && b.Flop != nil {
		flop = b.lookup(b.Flop, hole, s.Board[:3])
	}
	if s.NumBoard >= 4 && b.Turn != nil {
		turn = b.lookup(b.Turn, hole, s.Board[:4])
	}
	if s.NumBoard >= 5 && b.River != nil {
		river = b.lookup(b.River, hole, s.Board[:5])
	}

	if flop < 0 || flop > 255 || turn < 0 || turn > 255 || river < 0 || river > 255 {
		panic("MultiStreetBuckets.ID: postflop bucket out of range (K>256?)")
	}

	// Bet-history hash: per-street action sequences with street separator.
	h := fnv.New32a()
	var hbuf [2]byte
	for st := 0; st < 4; st++ {
		for _, a := range s.Hist[st] {
			hbuf[0] = byte(a.Kind)
			hbuf[1] = a.SizeIdx
			h.Write(hbuf[:2])
		}
		h.Write([]byte{0xfe})
	}
	histHash := uint64(h.Sum32() & 0x1FFFFFFF) // 29 bits

	var id uint64
	id |= uint64(s.Street) & 0x3
	id |= uint64(s.Cur) << 2
	id |= uint64(pre) << 3
	id |= uint64(flop) << 11
	id |= uint64(turn) << 19
	id |= uint64(river) << 27
	id |= histHash << 35
	return id
}

// lookup — postflop bucket lookup with fallback.
//
// If MCSamplesFallback > 0 and class is unseen, fall back to runtime MC equity.
// Otherwise, unseen → bucket 0 (matches "low equity" assumption).
func (b *MultiStreetBuckets) lookup(sb StreetBucketer, hole [2]nlhe.Card, board []nlhe.Card) int {
	if b.MCSamplesFallback > 0 {
		return sb.ForOrFallback(hole, board, b.MCSamplesFallback, b.FallbackSeed)
	}
	bid := sb.For(hole, board)
	if bid < 0 {
		return 0
	}
	return bid
}
