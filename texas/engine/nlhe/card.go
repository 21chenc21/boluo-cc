package nlhe

import "fmt"

// 52-card standard deck: 13 ranks × 4 suits.
// Cards encoded as uint8 in range [0, 52):
//   rank = id / 4
//   suit = id % 4
//
// Ranks: 0..12 corresponding to 2,3,4,5,6,7,8,9,T,J,Q,K,A
// Suits: 0=c, 1=d, 2=h, 3=s (clubs/diamonds/hearts/spades)
const (
	NumPlayers = 2
	NumRanks   = 13
	NumSuits   = 4
	DeckSize   = NumRanks * NumSuits // 52
)

type Card uint8

const NoCard Card = 255

func MakeCard(rank, suit uint8) Card { return Card(rank*NumSuits + suit) }
func (c Card) Rank() uint8           { return uint8(c) / NumSuits }
func (c Card) Suit() uint8           { return uint8(c) % NumSuits }
func (c Card) IsValid() bool         { return c < DeckSize }

var rankSym = [NumRanks]byte{'2', '3', '4', '5', '6', '7', '8', '9', 'T', 'J', 'Q', 'K', 'A'}
var suitSym = [NumSuits]byte{'c', 'd', 'h', 's'}

func (c Card) String() string {
	if c == NoCard {
		return "?"
	}
	if !c.IsValid() {
		return fmt.Sprintf("Card(%d)", uint8(c))
	}
	return string([]byte{rankSym[c.Rank()], suitSym[c.Suit()]})
}

// ParseCard parses two-char "Ah", "Td", "2c". Returns NoCard on parse failure.
func ParseCard(s string) Card {
	if len(s) != 2 {
		return NoCard
	}
	r := s[0]
	su := s[1]
	var rIdx, sIdx int = -1, -1
	for i, x := range rankSym {
		if r == x {
			rIdx = i
			break
		}
	}
	for i, x := range suitSym {
		if su == x {
			sIdx = i
			break
		}
	}
	if rIdx < 0 || sIdx < 0 {
		return NoCard
	}
	return MakeCard(uint8(rIdx), uint8(sIdx))
}
