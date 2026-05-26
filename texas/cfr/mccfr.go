package cfr

import (
	"math/rand"

	"github.com/boluo/texas/engine/leduc"
)

// MCCFR — External-Sampling Monte-Carlo CFR (Lanctot et al. 2009).
//
// Per iteration, for each traverser:
//  1. Sample one (priv0, priv1) chance outcome.
//  2. Walk from there: sample one public card; sample one opp action; expand
//     all traverser actions.
//  3. Update regret/strategy only at traverser-owned infosets visited.
//
// Key vs vanilla CFR:
//   - No reach-probability factor in regret/strategy updates (sampling provides it).
//   - Each iter visits ~10-50 nodes (vs vanilla's 6000+). Per-iter cost drops 50-100x.
//   - Higher variance per iter, so MORE iter needed for same expl. Net wall-time wins.
//
// Convergence: O(1/√T) iter (same asymptotic as vanilla). Linear averaging supported.
type MCCFR struct {
	regret   map[uint64][]float64
	strategy map[uint64][]float64
	sigmaBuf map[uint64][]float64

	rng        *rand.Rand
	iters      int
	linearAvg  bool // multiply strategy increments by iter (faster convergence)
	useRMPlus  bool // floor regrets to 0 at end of iter (RM+)
}

// NewMCCFR — external-sampling MCCFR with RM+ + linear averaging (recommended).
// Seed deterministic for tests; pass time.Now().UnixNano() for fresh randomness.
func NewMCCFR(seed int64) *MCCFR {
	return &MCCFR{
		regret:    make(map[uint64][]float64),
		strategy:  make(map[uint64][]float64),
		sigmaBuf:  make(map[uint64][]float64),
		rng:       rand.New(rand.NewSource(seed)),
		linearAvg: true,
		useRMPlus: true,
	}
}

// NewMCCFRVanilla — plain external-sampling MCCFR (no RM+, no linear avg).
// Slower convergence but simpler theory. Use only for ablation.
func NewMCCFRVanilla(seed int64) *MCCFR {
	m := NewMCCFR(seed)
	m.linearAvg = false
	m.useRMPlus = false
	return m
}

func (m *MCCFR) Iters() int       { return m.iters }
func (m *MCCFR) NumInfosets() int { return len(m.regret) }

func (m *MCCFR) ensure(id uint64, n int) (r, s, sigma []float64) {
	r, ok := m.regret[id]
	if !ok {
		r = make([]float64, n)
		m.regret[id] = r
		m.strategy[id] = make([]float64, n)
		m.sigmaBuf[id] = make([]float64, n)
	}
	s = m.strategy[id]
	sigma = m.sigmaBuf[id]
	return
}

// Iter — one MCCFR iter: P0 walk + P1 walk, each on its own sampled subtree.
func (m *MCCFR) Iter() {
	m.iters++
	m.runTraverser(leduc.P0)
	m.runTraverser(leduc.P1)
	if m.useRMPlus {
		// End-of-iter regret floor.
		for _, r := range m.regret {
			for i := range r {
				if r[i] < 0 {
					r[i] = 0
				}
			}
		}
	}
}

// sampleDeal — sample one ordered (priv0, priv1) from 30 possibilities, uniform.
func (m *MCCFR) sampleDeal() (leduc.Card, leduc.Card) {
	p0 := leduc.Card(m.rng.Intn(leduc.DeckSize))
	p1 := leduc.Card(m.rng.Intn(leduc.DeckSize - 1))
	if p1 >= p0 {
		p1++ // skip p0
	}
	return p0, p1
}

// sampleFromSigma — pick action index proportional to sigma. Sigma must sum to 1.
func (m *MCCFR) sampleFromSigma(sigma []float64) int {
	r := m.rng.Float64()
	var cum float64
	for i, p := range sigma {
		cum += p
		if r < cum {
			return i
		}
	}
	return len(sigma) - 1
}

func (m *MCCFR) runTraverser(trav leduc.Player) {
	p0, p1 := m.sampleDeal()
	s := leduc.NewState(p0, p1)
	m.walk(s, trav)
}

func (m *MCCFR) walk(s *leduc.State, trav leduc.Player) float64 {
	if s.Terminal {
		return s.Payoff(trav)
	}
	if s.NeedsPublicCard() {
		// Sample one remaining card uniform.
		var rem [leduc.DeckSize]leduc.Card
		n := 0
		for c := leduc.Card(0); c < leduc.DeckSize; c++ {
			if c != s.Priv[0] && c != s.Priv[1] {
				rem[n] = c
				n++
			}
		}
		c := rem[m.rng.Intn(n)]
		snap := s.Snapshot()
		s.SetPublic(c)
		v := m.walk(s, trav)
		s.Restore(snap)
		return v
	}

	id := s.InfosetID()
	legal := s.LegalActions()
	n := len(legal)
	regret, stratSum, sigma := m.ensure(id, n)
	regretMatching(regret, sigma)

	if s.Cur == trav {
		// Expand all actions (no sampling at traverser nodes).
		var utilsArr [4]float64
		utils := utilsArr[:n]
		var nodeUtil float64
		for i, a := range legal {
			snap := s.Snapshot()
			s.Apply(a)
			utils[i] = m.walk(s, trav)
			s.Restore(snap)
			nodeUtil += sigma[i] * utils[i]
		}
		// Regret update — NO reach factor; sampling already provides it.
		for i := range legal {
			regret[i] += utils[i] - nodeUtil
		}
		// Strategy update — linear averaging if enabled.
		w := 1.0
		if m.linearAvg {
			w = float64(m.iters)
		}
		for i := range legal {
			stratSum[i] += w * sigma[i]
		}
		return nodeUtil
	}

	// Opponent — sample one action proportional to sigma.
	aIdx := m.sampleFromSigma(sigma)
	snap := s.Snapshot()
	s.Apply(legal[aIdx])
	v := m.walk(s, trav)
	s.Restore(snap)
	return v
}

func (m *MCCFR) AverageStrategy() Strategy {
	out := make(Strategy, len(m.strategy))
	for k, ss := range m.strategy {
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
