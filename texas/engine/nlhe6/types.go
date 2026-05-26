// Package nlhe6 — multi-player (2-6) NLHE state machine.
//
// HUNL is the special case NumPlayers=2. Card/eval/abstraction are reused
// from engine/nlhe via type aliases. See README.md for design rationale.
package nlhe6

import (
	"github.com/boluo/texas/engine/nlhe"
)

// --- Reused types from engine/nlhe ---
//
// Card encoding, suit/rank decoding, and hand evaluation are player-count
// agnostic. We alias the types so 6-max code can use them directly.

type Card = nlhe.Card

const (
	DeckSize = nlhe.DeckSize
	NumRanks = nlhe.NumRanks
	NumSuits = nlhe.NumSuits
)

// ParseCard / Evaluate7 are reused via direct call to nlhe package.
// (Not aliased as vars to avoid var-init order issues.)

// --- 6-max specific ---

// MaxPlayers — hard upper bound on table size. Pluribus uses 6.
// Increasing requires bigger seat-relative position encoding and re-checking
// snapshot/restore field sizes.
const MaxPlayers = 6

// Seat — index into State.Seats. 0..NumPlayers-1.
type Seat uint8

// NoSeat — sentinel for "no seat" (e.g. FoldedBy when nobody folded).
const NoSeat Seat = 255

// Street — same ordinal as engine/nlhe; reproduced here so importers don't
// need both packages.
type Street uint8

const (
	StreetPreflop Street = 0
	StreetFlop    Street = 1
	StreetTurn    Street = 2
	StreetRiver   Street = 3
)

func (s Street) String() string {
	switch s {
	case StreetPreflop:
		return "preflop"
	case StreetFlop:
		return "flop"
	case StreetTurn:
		return "turn"
	case StreetRiver:
		return "river"
	}
	return "?"
}

// Position — canonical 6-max position relative to button.
//
//	BTN+0 = BTN, BTN+1 = SB, BTN+2 = BB, BTN+3 = UTG, BTN+4 = MP, BTN+5 = CO.
//
// For HU (N=2): BTN+0 = SB (button is on SB by HUNL convention),
// BTN+1 = BB. Acts preflop SB first then BB, postflop BB first.
//
// PositionFor(seat, button, n) computes a seat's canonical position.
type Position uint8

const (
	PosBTN Position = 0
	PosSB  Position = 1
	PosBB  Position = 2
	PosUTG Position = 3
	PosMP  Position = 4
	PosCO  Position = 5
)

func (p Position) String() string {
	switch p {
	case PosBTN:
		return "BTN"
	case PosSB:
		return "SB"
	case PosBB:
		return "BB"
	case PosUTG:
		return "UTG"
	case PosMP:
		return "MP"
	case PosCO:
		return "CO"
	}
	return "?"
}

// PositionFor — canonical position label for seat given button position and
// player count. Caller responsible for n in [2, MaxPlayers].
//
// For HU (n=2): the dealer button is on SB by convention, so PositionFor
// returns PosSB for the button seat and PosBB for the other.
func PositionFor(seat, button Seat, n int) Position {
	offset := (int(seat) - int(button) + n) % n
	if n == 2 {
		// HU convention: button = SB, other = BB.
		if offset == 0 {
			return PosSB
		}
		return PosBB
	}
	// 3-6 player: standard offset → position.
	switch offset {
	case 0:
		return PosBTN
	case 1:
		return PosSB
	case 2:
		return PosBB
	case 3:
		return PosUTG
	case 4:
		return PosMP
	case 5:
		return PosCO
	}
	return PosBTN
}

// FirstToActPreflop — seat that acts first preflop.
//
//	HU (n=2): SB (= button seat).
//	3+: UTG (= button + 3 mod n). For n<6, wraps to next-available seat
//	    (BTN+3 might exceed n-1 for short tables, then wraps to BTN+3 mod n).
func FirstToActPreflop(button Seat, n int) Seat {
	if n == 2 {
		return button // SB = button in HU; SB acts first preflop
	}
	return Seat((int(button) + 3) % n)
}

// FirstToActPostflop — seat that acts first on flop/turn/river.
//
//	HU (n=2): BB (= button + 1).
//	3+: SB (= button + 1 mod n).
func FirstToActPostflop(button Seat, n int) Seat {
	return Seat((int(button) + 1) % n)
}

// NextSeat — clockwise next seat (regardless of fold/all-in status; caller
// filters).
func NextSeat(s Seat, n int) Seat {
	return Seat((int(s) + 1) % n)
}
