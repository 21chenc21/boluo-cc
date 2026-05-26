package abstraction

import "github.com/boluo/texas/engine/nlhe"

// PreflopID — bucket-based infoset key for HUNL push/fold preflop.
//
// Layout (single uint64, mostly low bits):
//
//	bits  0-4   bucket id (0..K-1, supports K up to 32)
//	bits    5   position (0 = SB / P0, 1 = BB / P1)
//	bits    6   facing-shove flag (1 = opp.AllIn=true)
//
// Total: 7 bits used. Up to 128 distinct abstract infosets (typical K=20 → 80).
//
// For full HUNL preflop with multiple bet sizes / streets, history bits would
// extend this. Push/fold doesn't need history (position + facing-shove uniquely
// identifies decision point).
func (b *PreflopBuckets) PreflopID(s *nlhe.State) uint64 {
	bucket := b.For(s.Hole[s.Cur][0], s.Hole[s.Cur][1])
	var id uint64
	id |= uint64(bucket)
	id |= uint64(s.Cur) << 5
	if s.AllIn[s.Cur.Other()] {
		id |= 1 << 6
	}
	return id
}
