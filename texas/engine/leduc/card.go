package leduc

import (
	"fmt"
	"math/rand"
)

// Leduc Hold'em: 2 suits × 3 ranks (J, Q, K) = 6 cards.
// Cards 0..5 encode (rank, suit) as: rank = id/2, suit = id%2.
const (
	NumPlayers = 2
	NumRanks   = 3
	NumSuits   = 2
	DeckSize   = NumRanks * NumSuits
)

type Card uint8

const NoCard Card = 255

func MakeCard(rank, suit uint8) Card { return Card(rank*NumSuits + suit) }
func (c Card) Rank() uint8           { return uint8(c) / NumSuits }
func (c Card) Suit() uint8           { return uint8(c) % NumSuits }

var rankSym = [NumRanks]byte{'J', 'Q', 'K'}

func (c Card) String() string {
	if c == NoCard {
		return "?"
	}
	return fmt.Sprintf("%c%d", rankSym[c.Rank()], c.Suit())
}

// Deck — small deck used for dealing private + public cards.
type Deck struct {
	cards []Card
}

func NewDeck() *Deck {
	d := &Deck{cards: make([]Card, DeckSize)}
	for i := range d.cards {
		d.cards[i] = Card(i)
	}
	return d
}

func (d *Deck) Shuffle(rng *rand.Rand) {
	rng.Shuffle(len(d.cards), func(i, j int) {
		d.cards[i], d.cards[j] = d.cards[j], d.cards[i]
	})
}

func (d *Deck) Draw() Card {
	c := d.cards[0]
	d.cards = d.cards[1:]
	return c
}

func (d *Deck) Remaining() int { return len(d.cards) }
