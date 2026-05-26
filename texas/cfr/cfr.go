// Package cfr — tabular vanilla CFR for small games (Leduc).
//
// Implements vanilla CFR per Zinkevich 2007 with alternating-traverser updates.
// Convergence rate: O(1/√T). For Leduc (~288 infosets), expl < 0.05 in ~1000 iter,
// < 0.02 in ~5000.
//
// Performance: uses uint64 InfosetID as map key (avoid string alloc) + Snapshot/Restore
// for zero-alloc recursive walks. CFR+ (RM+ + linear avg) coming next; bug under
// investigation.
//
// Usage:
//
//	c := cfr.New()
//	for i := 0; i < 1000; i++ { c.Iter() }
//	strat := c.AverageStrategy()
//	expl := cfr.Exploitability(strat)
package cfr

import (
	"github.com/boluo/texas/engine/leduc"
)

// CFR — tabular regret + strategy accumulators, keyed by uint64 InfosetID.
type CFR struct {
	regret   map[uint64][]float64
	strategy map[uint64][]float64
	sigmaBuf map[uint64][]float64
	iters    int
}

func New() *CFR {
	return &CFR{
		regret:   make(map[uint64][]float64),
		strategy: make(map[uint64][]float64),
		sigmaBuf: make(map[uint64][]float64),
	}
}

func (c *CFR) ensure(id uint64, n int) (r, s, sigma []float64) {
	r, ok := c.regret[id]
	if !ok {
		r = make([]float64, n)
		c.regret[id] = r
		c.strategy[id] = make([]float64, n)
		c.sigmaBuf[id] = make([]float64, n)
	}
	s = c.strategy[id]
	sigma = c.sigmaBuf[id]
	return
}

// Iter — one full CFR iteration (alternates traverser P0, P1).
func (c *CFR) Iter() {
	c.iters++
	// Reuse two persistent State objects across all 60 deals × 2 traversers.
	// Snapshot/Restore inside walk keeps these zero-alloc per node.
	c.runTraverser(leduc.P0)
	c.runTraverser(leduc.P1)
}

func (c *CFR) Iters() int { return c.iters }

func (c *CFR) runTraverser(trav leduc.Player) {
	const nDeals = leduc.DeckSize * (leduc.DeckSize - 1)
	chanceProb := 1.0 / float64(nDeals)
	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			s := leduc.NewState(p0, p1)
			c.walk(s, trav, 1.0, chanceProb)
		}
	}
}

// walk — CFR recursion. Returns expected utility for `trav`.
//   - reachTrav: π_trav along path
//   - reachOther: π_{-trav} × π_chance along path
//
// Uses Snapshot/Restore on the same State pointer to avoid Clone alloc.
func (c *CFR) walk(s *leduc.State, trav leduc.Player, reachTrav, reachOther float64) float64 {
	if s.Terminal {
		return s.Payoff(trav)
	}
	if s.NeedsPublicCard() {
		nRemain := 0
		for card := leduc.Card(0); card < leduc.DeckSize; card++ {
			if card != s.Priv[0] && card != s.Priv[1] {
				nRemain++
			}
		}
		probEach := 1.0 / float64(nRemain)
		var sum float64
		for card := leduc.Card(0); card < leduc.DeckSize; card++ {
			if card == s.Priv[0] || card == s.Priv[1] {
				continue
			}
			snap := s.Snapshot()
			s.SetPublic(card)
			sum += probEach * c.walk(s, trav, reachTrav, reachOther*probEach)
			s.Restore(snap)
		}
		return sum
	}

	id := s.InfosetID()
	legal := s.LegalActions()
	n := len(legal)
	regret, stratSum, sigma := c.ensure(id, n)
	regretMatching(regret, sigma)

	if s.Cur == trav {
		// Stack-allocated small utils array (n ≤ 3 for Leduc).
		var utilsArr [4]float64
		utils := utilsArr[:n]
		var nodeUtil float64
		for i, a := range legal {
			snap := s.Snapshot()
			s.Apply(a)
			utils[i] = c.walk(s, trav, reachTrav*sigma[i], reachOther)
			s.Restore(snap)
			nodeUtil += sigma[i] * utils[i]
		}
		for i := range legal {
			regret[i] += reachOther * (utils[i] - nodeUtil)
		}
		for i := range legal {
			stratSum[i] += reachTrav * sigma[i]
		}
		return nodeUtil
	}

	// Opponent's turn.
	var nodeUtil float64
	for i, a := range legal {
		snap := s.Snapshot()
		s.Apply(a)
		nodeUtil += sigma[i] * c.walk(s, trav, reachTrav, reachOther*sigma[i])
		s.Restore(snap)
	}
	return nodeUtil
}

// AverageStrategy — normalized cumulative strategy per infoset (the policy that converges to Nash).
func (c *CFR) AverageStrategy() map[uint64][]float64 {
	out := make(map[uint64][]float64, len(c.strategy))
	for k, ss := range c.strategy {
		var sum float64
		for _, v := range ss {
			sum += v
		}
		probs := make([]float64, len(ss))
		if sum > 0 {
			for i, v := range ss {
				probs[i] = v / sum
			}
		} else {
			u := 1.0 / float64(len(ss))
			for i := range probs {
				probs[i] = u
			}
		}
		out[k] = probs
	}
	return out
}

// CurrentStrategy — regret-matched σ from latest iteration (per-infoset).
func (c *CFR) CurrentStrategy() map[uint64][]float64 {
	out := make(map[uint64][]float64, len(c.regret))
	for k, r := range c.regret {
		n := len(r)
		probs := make([]float64, n)
		regretMatching(r, probs)
		out[k] = probs
	}
	return out
}

func (c *CFR) NumInfosets() int { return len(c.regret) }
