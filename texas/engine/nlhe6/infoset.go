package nlhe6

import (
	"hash/fnv"
)

// InfosetID — lossless 64-bit identifier of Cur's view of the game.
//
// Includes: Cur's hole cards (private), public board, Cur's seat position,
// full per-street betting history (visible to all).
// Excludes: other players' hole cards (hidden).
//
// FNV-1a 64-bit hash of canonical byte encoding. For 6-max with ~10^10 - 10^14
// states, collision rate is negligible.
//
// Caller MUST NOT be at chance node — NumBoard ambiguity would break the hash.
func (s *State) InfosetID() uint64 {
	h := fnv.New64a()
	var buf [16]byte

	// Hero hole (canonicalized by sort).
	c0, c1 := s.Hole[s.Cur][0], s.Hole[s.Cur][1]
	if c0 > c1 {
		c0, c1 = c1, c0
	}
	buf[0] = byte(c0)
	buf[1] = byte(c1)
	h.Write(buf[:2])

	// Public board.
	for i := uint8(0); i < s.NumBoard; i++ {
		buf[0] = byte(s.Board[i])
		h.Write(buf[:1])
	}
	h.Write([]byte{0xff}) // sentinel between board and rest

	// Position: seat + button (encodes "where in rotation is Cur"). Use
	// PositionFor for canonical label so equivalent rotations collapse.
	buf[0] = byte(s.Cur)
	buf[1] = byte(s.Button)
	buf[2] = byte(s.Cfg.NumPlayers)
	h.Write(buf[:3])

	// Per-street action history with separators. Seat-of-action inferred at
	// replay by engine's rotation rules — same Hist bytes → same game tree
	// position regardless of who acted.
	for st := 0; st < 4; st++ {
		for _, e := range s.Hist[st] {
			buf[0] = byte(e.Action.Kind)
			buf[1] = e.Action.SizeIdx
			buf[2] = byte(e.Seat)
			h.Write(buf[:3])
		}
		h.Write([]byte{0xfe})
	}

	return h.Sum64()
}
