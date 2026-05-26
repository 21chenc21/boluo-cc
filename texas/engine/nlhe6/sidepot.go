package nlhe6

import (
	"sort"

	"github.com/boluo/texas/engine/nlhe"
)

// SubPot — one pot in side-pot decomposition. Eligible seats compete for this
// pot's chips at showdown.
type SubPot struct {
	Amount   int
	Eligible []Seat
}

// ComputeSidePots — decompose total wagered chips into side pots based on
// all-in / wagered levels among non-folded players. Folded players'
// contributions are pooled into the pots they covered (chips don't "return").
//
// Algorithm:
//  1. Distinct wagered amounts of non-folded players define pot tier levels.
//  2. For each level (ascending), pot at this level = sum over all players of
//     min(wagered_i, level) - prev_level (clamped at 0).
//  3. Eligible at this level = non-folded players with wagered_i ≥ level.
//
// Pot decomposition is exact in chips (no fractional). Total over all SubPots
// == sum of all wagered. Empty list if all wagered are zero (rare; usually no
// hand reached this point).
func ComputeSidePots(wagered []int, folded []bool) []SubPot {
	n := len(wagered)
	// Levels = distinct non-folded wagered values > 0.
	seen := make(map[int]bool)
	for i := 0; i < n; i++ {
		if !folded[i] && wagered[i] > 0 {
			seen[wagered[i]] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	levels := make([]int, 0, len(seen))
	for l := range seen {
		levels = append(levels, l)
	}
	sort.Ints(levels)

	pots := make([]SubPot, 0, len(levels))
	prev := 0
	for _, lvl := range levels {
		var amount int
		var eligible []Seat
		for i := 0; i < n; i++ {
			contrib := wagered[i]
			if contrib > lvl {
				contrib = lvl
			}
			contrib -= prev
			if contrib > 0 {
				amount += contrib
			}
			if !folded[i] && wagered[i] >= lvl {
				eligible = append(eligible, Seat(i))
			}
		}
		pots = append(pots, SubPot{Amount: amount, Eligible: eligible})
		prev = lvl
	}
	return pots
}

// Payoff — net chip delta for `seat` at terminal state. Sum over all seats
// equals 0 (modulo chip-remainder on split pots, which is currently dropped).
func (s *State) Payoff(seat Seat) int {
	if !s.Terminal {
		panic("Payoff on non-terminal")
	}
	// FoldWin: sole survivor takes all wagered chips.
	if s.FoldWinner != NoSeat {
		if seat == s.FoldWinner {
			var sum int
			for i := 0; i < s.Cfg.NumPlayers; i++ {
				if Seat(i) != seat {
					sum += s.Wagered[i]
				}
			}
			return sum
		}
		return -s.Wagered[seat]
	}
	// Showdown — must have full board.
	if s.NumBoard < 5 {
		panic("Payoff: showdown but NumBoard < 5")
	}
	pots := ComputeSidePots(s.Wagered[:s.Cfg.NumPlayers], s.Folded[:s.Cfg.NumPlayers])

	// Winnings per seat (gross, before subtracting own wagered).
	winnings := make([]int, s.Cfg.NumPlayers)
	for _, pot := range pots {
		switch len(pot.Eligible) {
		case 0:
			// Shouldn't normally happen (folded contributions create at least
			// one eligible at the lowest level). Skip.
		case 1:
			winnings[pot.Eligible[0]] += pot.Amount
		default:
			// Find best hand rank among eligible.
			var bestRank nlhe.HandRank
			var winners []Seat
			for j, p := range pot.Eligible {
				r := s.handRank(p)
				if j == 0 || r > bestRank {
					bestRank = r
					winners = winners[:0]
					winners = append(winners, p)
				} else if r == bestRank {
					winners = append(winners, p)
				}
			}
			if len(winners) == 0 {
				continue
			}
			share := pot.Amount / len(winners)
			for _, w := range winners {
				winnings[w] += share
			}
			// Chip remainder (pot.Amount % len(winners)) is currently dropped.
			// In real poker rules, awarded to first eligible left of button —
			// not implementing here; total payoff sum may differ from 0 by at
			// most NumPlayers - 1 chips per hand.
		}
	}
	return winnings[seat] - s.Wagered[seat]
}
