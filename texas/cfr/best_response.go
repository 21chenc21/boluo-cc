package cfr

import (
	"github.com/boluo/texas/engine/leduc"
)

// Strategy — keyed by uint64 InfosetID, value is action probability over LegalActions order.
type Strategy = map[uint64][]float64

// BestResponseValue — exact best-response value for player p against opp σ.
//
// Algorithm (OpenSpiel-style):
//  1. Enumerate every state h in every p-owned infoset I, recording π_{-p}(h).
//  2. Memoized recursion: brValue(h) + brAction(I).
//     brAction(I) = argmax_a Σ_{h∈I} π_{-p}(h) × brValue(h·a).
//  3. Return Σ chance × brValue(root_after_deal).
//
// Performance: enumerate phase still clones states (needed for stored members), but
// brValue recursive walk uses Snapshot/Restore for O(tree) zero-alloc evaluation.
func BestResponseValue(sigma Strategy, p leduc.Player) float64 {
	br := newBRSolver(sigma, p)
	br.enumerate()
	return br.totalValue()
}

type infosetMember struct {
	state      *leduc.State // cloned state at p's decision point
	reachOther float64      // π_{-p}(h)
}

type brSolver struct {
	sigma          Strategy
	p              leduc.Player
	infosetMembers map[uint64][]infosetMember
	brActionCache  map[uint64]int // I -> chosen action index
}

func newBRSolver(sigma Strategy, p leduc.Player) *brSolver {
	return &brSolver{
		sigma:          sigma,
		p:              p,
		infosetMembers: make(map[uint64][]infosetMember),
		brActionCache:  make(map[uint64]int),
	}
}

// enumerate — walk the tree, recording (state, π_-p) for each p-owned infoset.
// Uses Snapshot/Restore for non-clone walking; clones only at p-decision recording points.
func (b *brSolver) enumerate() {
	const nDeals = leduc.DeckSize * (leduc.DeckSize - 1)
	chanceProb := 1.0 / float64(nDeals)
	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			s := leduc.NewState(p0, p1)
			b.walkEnum(s, chanceProb)
		}
	}
}

func (b *brSolver) walkEnum(s *leduc.State, reachOther float64) {
	if s.Terminal {
		return
	}
	if s.NeedsPublicCard() {
		nRemain := 0
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c != s.Priv[0] && c != s.Priv[1] {
				nRemain++
			}
		}
		probEach := 1.0 / float64(nRemain)
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c == s.Priv[0] || c == s.Priv[1] {
				continue
			}
			snap := s.Snapshot()
			s.SetPublic(c)
			b.walkEnum(s, reachOther*probEach)
			s.Restore(snap)
		}
		return
	}
	legal := s.LegalActions()
	if s.Cur == b.p {
		id := s.InfosetID()
		// Record member — must Clone since the walking state will be mutated.
		b.infosetMembers[id] = append(b.infosetMembers[id], infosetMember{s.Clone(), reachOther})
		for _, a := range legal {
			snap := s.Snapshot()
			s.Apply(a)
			b.walkEnum(s, reachOther)
			s.Restore(snap)
		}
		return
	}
	probs := sigmaAt(b.sigma, s.InfosetID(), len(legal))
	for i, a := range legal {
		snap := s.Snapshot()
		s.Apply(a)
		b.walkEnum(s, reachOther*probs[i])
		s.Restore(snap)
	}
}

// brAction — argmax_a Σ_{h∈I} π_-p(h) × brValue(h·a). Memoized per infoset.
func (b *brSolver) brAction(id uint64) int {
	if a, ok := b.brActionCache[id]; ok {
		return a
	}
	members := b.infosetMembers[id]
	if len(members) == 0 {
		b.brActionCache[id] = 0
		return 0
	}
	legal := members[0].state.LegalActions()
	bestA, bestQ := 0, -1e30
	for i, a := range legal {
		var q float64
		for _, m := range members {
			snap := m.state.Snapshot()
			m.state.Apply(a)
			q += m.reachOther * b.brValue(m.state)
			m.state.Restore(snap)
		}
		if q > bestQ {
			bestQ = q
			bestA = i
		}
	}
	b.brActionCache[id] = bestA
	return bestA
}

// brValue — value at state s, p plays BR going forward, opp plays σ.
func (b *brSolver) brValue(s *leduc.State) float64 {
	if s.Terminal {
		return s.Payoff(b.p)
	}
	if s.NeedsPublicCard() {
		nRemain := 0
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c != s.Priv[0] && c != s.Priv[1] {
				nRemain++
			}
		}
		probEach := 1.0 / float64(nRemain)
		var sum float64
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c == s.Priv[0] || c == s.Priv[1] {
				continue
			}
			snap := s.Snapshot()
			s.SetPublic(c)
			sum += probEach * b.brValue(s)
			s.Restore(snap)
		}
		return sum
	}
	legal := s.LegalActions()
	if s.Cur == b.p {
		a := b.brAction(s.InfosetID())
		snap := s.Snapshot()
		s.Apply(legal[a])
		v := b.brValue(s)
		s.Restore(snap)
		return v
	}
	probs := sigmaAt(b.sigma, s.InfosetID(), len(legal))
	var sum float64
	for i, a := range legal {
		snap := s.Snapshot()
		s.Apply(a)
		sum += probs[i] * b.brValue(s)
		s.Restore(snap)
	}
	return sum
}

func (b *brSolver) totalValue() float64 {
	const nDeals = leduc.DeckSize * (leduc.DeckSize - 1)
	prob := 1.0 / float64(nDeals)
	var total float64
	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			s := leduc.NewState(p0, p1)
			total += prob * b.brValue(s)
		}
	}
	return total
}

// sigmaAt fetches σ[id]. Falls back to uniform if missing.
func sigmaAt(sigma Strategy, id uint64, n int) []float64 {
	if v, ok := sigma[id]; ok && len(v) == n {
		return v
	}
	u := 1.0 / float64(n)
	out := make([]float64, n)
	for i := range out {
		out[i] = u
	}
	return out
}

// GameValue — expected utility for player p when both players play σ.
// For Nash σ, GameValue(P0) ≈ -0.0856 in canonical Leduc.
func GameValue(sigma Strategy, p leduc.Player) float64 {
	const nDeals = leduc.DeckSize * (leduc.DeckSize - 1)
	prob := 1.0 / float64(nDeals)
	var total float64
	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			s := leduc.NewState(p0, p1)
			total += prob * sigmaWalk(s, p, sigma)
		}
	}
	return total
}

func sigmaWalk(s *leduc.State, p leduc.Player, sigma Strategy) float64 {
	if s.Terminal {
		return s.Payoff(p)
	}
	if s.NeedsPublicCard() {
		nRemain := 0
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c != s.Priv[0] && c != s.Priv[1] {
				nRemain++
			}
		}
		probEach := 1.0 / float64(nRemain)
		var sum float64
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c == s.Priv[0] || c == s.Priv[1] {
				continue
			}
			snap := s.Snapshot()
			s.SetPublic(c)
			sum += probEach * sigmaWalk(s, p, sigma)
			s.Restore(snap)
		}
		return sum
	}
	legal := s.LegalActions()
	probs := sigmaAt(sigma, s.InfosetID(), len(legal))
	var sum float64
	for i, a := range legal {
		snap := s.Snapshot()
		s.Apply(a)
		sum += probs[i] * sigmaWalk(s, p, sigma)
		s.Restore(snap)
	}
	return sum
}

// Exploitability — sum-of-best-responses metric. For Nash σ, expl=0.
func Exploitability(sigma Strategy) float64 {
	return BestResponseValue(sigma, leduc.P0) + BestResponseValue(sigma, leduc.P1)
}
