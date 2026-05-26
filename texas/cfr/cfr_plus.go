package cfr

import (
	"github.com/boluo/texas/engine/leduc"
)

// CFRPlus — CFR+ per Tammelin 2014, matching OpenSpiel's algorithm exactly:
//   - Alternating updates (each iter does P0 walk then P1 walk; each walk
//     updates ONLY that player's regret/strategy).
//   - Regret deltas accumulated during walks WITHOUT clamping.
//   - End-of-iter floor: clamp all regrets to ≥ 0 in one pass.
//     (My earlier per-delta clamping was the bug — over-aggressive clamping
//      within an iter lost information across multiple visits to the same infoset.)
//   - Linear averaging: stratSum += t × π_i × σ.
//
// Convergence: should reach expl < 1e-3 in ~500 iter on Leduc (vs vanilla CFR's
// ~50000 iter for same target).
type CFRPlus struct {
	regret   map[uint64][]float64
	strategy map[uint64][]float64
	sigmaBuf map[uint64][]float64
	iters    int
}

func NewPlus() *CFRPlus {
	return &CFRPlus{
		regret:   make(map[uint64][]float64),
		strategy: make(map[uint64][]float64),
		sigmaBuf: make(map[uint64][]float64),
	}
}

func (c *CFRPlus) Iters() int       { return c.iters }
func (c *CFRPlus) NumInfosets() int { return len(c.regret) }

func (c *CFRPlus) ensure(id uint64, n int) (r, s, sigma []float64) {
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

// Iter — one CFR+ iteration matching OpenSpiel exactly:
//
//  1. Increment iter counter.
//  2. For EACH player (both):
//     a. Pre-compute σ = RM+(R) for ALL infosets — frozen for this walk.
//     b. Walk tree updating only this player's regret/strategy.
//     c. RM+ clamp regret to ≥ 0 (immediately after this player's walk).
//
// CRITICAL: σ is cached at start of each player's walk and NOT recomputed
// mid-walk. OpenSpiel does this via `_update_current_policy`; we do it via
// `c.sigmaBuf` snapshot. Recomputing σ on-the-fly (as my pre-fix version did)
// causes RM+ to over-react within an iter — convergence 100x slower.
func (c *CFRPlus) Iter() {
	c.iters++
	for _, trav := range [...]leduc.Player{leduc.P0, leduc.P1} {
		// Pre-compute σ for all known infosets (cached for the whole walk).
		for id, r := range c.regret {
			sigma := c.sigmaBuf[id]
			regretMatching(r, sigma)
		}
		c.runTraverser(trav)
		// RM+ clamp after THIS player's walk.
		for _, r := range c.regret {
			for i := range r {
				if r[i] < 0 {
					r[i] = 0
				}
			}
		}
	}
}

func (c *CFRPlus) runTraverser(trav leduc.Player) {
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

func (c *CFRPlus) walk(s *leduc.State, trav leduc.Player, reachTrav, reachOther float64) float64 {
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
	// For NEWLY-discovered infosets (sigma still zero), compute σ on-the-fly.
	// For known ones, σ was pre-computed at start of this player's walk.
	if isZero(sigma) {
		regretMatching(regret, sigma)
	}

	if s.Cur == trav {
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
		// Accumulate regret deltas WITHOUT clamping (RM+ clamp happens after the walk).
		for i := range legal {
			regret[i] += reachOther * (utils[i] - nodeUtil)
		}
		// Linear averaging: weight = current iter × π_i × σ (OpenSpiel cfr.py:357-359).
		t := float64(c.iters)
		for i := range legal {
			stratSum[i] += t * reachTrav * sigma[i]
		}
		return nodeUtil
	}

	// Opponent's turn (no regret/strategy update for opp during this traversal).
	var nodeUtil float64
	for i, a := range legal {
		snap := s.Snapshot()
		s.Apply(a)
		nodeUtil += sigma[i] * c.walk(s, trav, reachTrav, reachOther*sigma[i])
		s.Restore(snap)
	}
	return nodeUtil
}

func isZero(s []float64) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

func (c *CFRPlus) AverageStrategy() Strategy {
	out := make(Strategy, len(c.strategy))
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
