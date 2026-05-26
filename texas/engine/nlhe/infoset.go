package nlhe

import (
	"hash/fnv"
)

// InfosetID — lossless 64-bit identifier of the current actor's view.
//
// Includes: actor's hole cards, public board, position, full betting history.
// Excludes opponent's hole cards (hidden info).
//
// Implementation: FNV-1a 64-bit hash of canonical byte encoding. Collision
// rate ~ 1/2^64; for HUNL with ~10^14 infosets, expected ~5 per 10^14 = safe.
//
// Caller MUST NOT be at a chance node (NumBoard implicit "needs more cards"
// state would be ambiguous).
func (s *State) InfosetID() uint64 {
	h := fnv.New64a()
	var buf [16]byte

	// Actor's hole cards (canonicalized by sort — suits matter for flush draws
	// but order shouldn't).
	c0, c1 := s.Hole[s.Cur][0], s.Hole[s.Cur][1]
	if c0 > c1 {
		c0, c1 = c1, c0
	}
	buf[0] = byte(c0)
	buf[1] = byte(c1)
	h.Write(buf[:2])

	// Public board (already ordered by deal, treat as sequence).
	for i := uint8(0); i < s.NumBoard; i++ {
		buf[0] = byte(s.Board[i])
		h.Write(buf[:1])
	}
	// Sentinel to disambiguate "0 board cards" from "no separator".
	h.Write([]byte{0xff})

	// Position.
	h.Write([]byte{byte(s.Cur)})

	// History per street + separator.
	for st := 0; st < 4; st++ {
		for _, a := range s.Hist[st] {
			buf[0] = byte(a.Kind)
			buf[1] = a.SizeIdx
			h.Write(buf[:2])
		}
		h.Write([]byte{0xfe})
	}

	return h.Sum64()
}

// InfosetLabel — human-readable summary (for debugging only; NOT used as map key).
func (s *State) InfosetLabel() string {
	c0, c1 := s.Hole[s.Cur][0], s.Hole[s.Cur][1]
	if c0 > c1 {
		c0, c1 = c1, c0
	}
	out := c0.String() + c1.String() + "/"
	for i := uint8(0); i < s.NumBoard; i++ {
		out += s.Board[i].String()
	}
	out += "/P" + string(rune('0'+s.Cur)) + "/"
	for st := 0; st < 4; st++ {
		for _, a := range s.Hist[st] {
			out += a.String()
		}
		out += "."
	}
	return out
}
