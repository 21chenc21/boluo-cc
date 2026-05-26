package nlhe6

import (
	"math/rand"
)

// MCCFR — External-Sampling Monte Carlo CFR for multi-player NLHE.
//
// Per iter, for each of NumPlayers traversers:
//   1. Sample 2N hole cards (uniform without replacement).
//   2. Walk betting tree: traverser expands all actions; other seats sample
//      from σ; chance nodes (street transitions, showdown fill) sample board.
//   3. Update regret/strategy only at infosets owned by the traverser.
//
// vs HUNL nlhe.MCCFR: same external-sampling principle but N seats per iter
// instead of 2. RM+ targeted flooring (engine/nlhe Phase 2d optimization) ported.
type MCCFR struct {
	cfg *GameConfig

	regret     map[uint64][]float64
	strategy   map[uint64][]float64
	sigmaBuf   map[uint64][]float64
	numActions map[uint64]int

	rng   *rand.Rand
	iters int

	useRMPlus bool
	linearAvg bool

	idFn func(*State) uint64

	walkVisited []uint64
}

func NewMCCFR(cfg *GameConfig, seed int64) *MCCFR {
	return &MCCFR{
		cfg:        cfg,
		regret:     make(map[uint64][]float64),
		strategy:   make(map[uint64][]float64),
		sigmaBuf:   make(map[uint64][]float64),
		numActions: make(map[uint64]int),
		rng:        rand.New(rand.NewSource(seed)),
		useRMPlus:  true,
		linearAvg:  true,
		idFn:       func(s *State) uint64 { return s.InfosetID() },
	}
}

// WithIDFn — override infoset key function (e.g. abstract bucket-based).
func (m *MCCFR) WithIDFn(fn func(*State) uint64) *MCCFR {
	m.idFn = fn
	return m
}

func (m *MCCFR) Iters() int       { return m.iters }
func (m *MCCFR) NumInfosets() int { return len(m.regret) }

func (m *MCCFR) ensure(id uint64, n int) (r, s, sigma []float64) {
	r, ok := m.regret[id]
	// Allocate if new OR if cached length doesn't match current legal-action
	// count (hash collision: two different states mapped to same id, with
	// different legal-action counts). Treat collision as new state — lose
	// accumulated regret but continue. With 27-bit hist hash at 6-max scale
	// (~10M infosets) collisions are non-negligible but bounded.
	if !ok || len(r) != n {
		r = make([]float64, n)
		m.regret[id] = r
		m.strategy[id] = make([]float64, n)
		m.sigmaBuf[id] = make([]float64, n)
		m.numActions[id] = n
	}
	s = m.strategy[id]
	sigma = m.sigmaBuf[id]
	return
}

// Iter — one MCCFR iteration: walk for each seat as traverser, then RM+ floor
// the seats just visited (targeted, not full-map sweep — Phase 2d perf trick).
func (m *MCCFR) Iter() {
	m.iters++
	n := m.cfg.NumPlayers
	for seat := 0; seat < n; seat++ {
		m.walkVisited = m.walkVisited[:0]
		m.runTraverser(Seat(seat))
		if m.useRMPlus {
			for _, id := range m.walkVisited {
				r := m.regret[id]
				for i := range r {
					if r[i] < 0 {
						r[i] = 0
					}
				}
			}
		}
	}
}

func (m *MCCFR) runTraverser(trav Seat) {
	s := NewStateWithButton(m.cfg, Seat(m.rng.Intn(m.cfg.NumPlayers)))
	m.dealHoles(s)
	m.walk(s, trav)
}

// dealHoles — uniformly sample 2*NumPlayers distinct cards.
func (m *MCCFR) dealHoles(s *State) {
	n := m.cfg.NumPlayers
	need := 2 * n
	var used [52]bool
	picked := make([]Card, 0, need)
	for i := 0; i < need; i++ {
		for {
			c := Card(m.rng.Intn(52))
			if !used[c] {
				picked = append(picked, c)
				used[c] = true
				break
			}
		}
	}
	for i := 0; i < n; i++ {
		s.SetHole(Seat(i), picked[2*i], picked[2*i+1])
	}
}

// chanceFill — sample n distinct board cards from remaining deck.
func (m *MCCFR) chanceFill(s *State, n int) Snapshot {
	snap := s.Snapshot()
	var used [52]bool
	for i := 0; i < m.cfg.NumPlayers; i++ {
		used[s.Hole[i][0]] = true
		used[s.Hole[i][1]] = true
	}
	for i := uint8(0); i < s.NumBoard; i++ {
		used[s.Board[i]] = true
	}
	for i := 0; i < n; i++ {
		for {
			c := Card(m.rng.Intn(52))
			if !used[c] {
				s.Board[s.NumBoard] = c
				s.NumBoard++
				used[c] = true
				break
			}
		}
	}
	return snap
}

// walk — MCCFR recursion. Returns expected utility for traverser.
func (m *MCCFR) walk(s *State, trav Seat) float64 {
	// Chance node: deal board cards.
	if n, needs := s.NeedsBoard(); needs {
		snap := m.chanceFill(s, n)
		v := m.walk(s, trav)
		s.Restore(snap)
		return v
	}
	if s.Terminal {
		return float64(s.Payoff(trav))
	}

	id := m.idFn(s)
	legal := s.LegalActions()
	nA := len(legal)
	regret, stratSum, sigma := m.ensure(id, nA)
	regretMatching(regret, sigma)

	if s.Cur == trav {
		var utilsArr [8]float64
		utils := utilsArr[:nA]
		var nodeUtil float64
		for i, a := range legal {
			snap := s.Snapshot()
			s.Apply(a)
			utils[i] = m.walk(s, trav)
			s.Restore(snap)
			nodeUtil += sigma[i] * utils[i]
		}
		for i := range legal {
			regret[i] += utils[i] - nodeUtil
		}
		w := 1.0
		if m.linearAvg {
			w = float64(m.iters)
		}
		for i := range legal {
			stratSum[i] += w * sigma[i]
		}
		m.walkVisited = append(m.walkVisited, id)
		return nodeUtil
	}

	// Non-traverser seat: sample one action from sigma.
	idx := m.sampleFromSigma(sigma)
	snap := s.Snapshot()
	s.Apply(legal[idx])
	v := m.walk(s, trav)
	s.Restore(snap)
	return v
}

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

func regretMatching(regret, out []float64) {
	var sum float64
	for i, r := range regret {
		if r > 0 {
			out[i] = r
			sum += r
		} else {
			out[i] = 0
		}
	}
	if sum > 0 {
		for i := range out {
			out[i] /= sum
		}
		return
	}
	u := 1.0 / float64(len(out))
	for i := range out {
		out[i] = u
	}
}

// AverageStrategy — normalized per-infoset action probabilities.
func (m *MCCFR) AverageStrategy() map[uint64][]float64 {
	out := make(map[uint64][]float64, len(m.strategy))
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

// NumActionsForID — cached action count.
func (m *MCCFR) NumActionsForID(id uint64) int {
	return m.numActions[id]
}
