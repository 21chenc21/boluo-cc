package nlhe

import (
	"math/rand"
)

// MCCFR — External-Sampling Monte Carlo CFR specialized for HUNL.
//
// Per iter, for each traverser:
//  1. Sample 4 distinct cards from deck → P0 hole, P1 hole.
//  2. Walk betting tree:
//     - traverser expands all actions, opp samples one (external sampling)
//     - chance nodes (street transitions, showdown fill) sample board cards
//  3. Update regret/strategy only at traverser-owned infosets visited.
//
// Multi-street: NeedsBoard() triggers chance sampling mid-walk (flop 3 / turn 1 /
// river 1 cards). All-in showdown also goes through the same path (fills to 5).
//
// Compared with engine/leduc MCCFR:
//   - Chance is much bigger (52 cards vs 6), so sampling absolutely required
//   - Strategy keyed by uint64 InfosetID hash (FNV-64a) instead of dense small int
type MCCFR struct {
	cfg *GameConfig

	regret   map[uint64][]float64
	strategy map[uint64][]float64
	sigmaBuf map[uint64][]float64
	// numActions[id] = cached action count for the infoset (LegalActions stable per id)
	numActions map[uint64]int

	rng   *rand.Rand
	iters int

	useRMPlus bool
	linearAvg bool

	// idFn — InfosetID function (default = State.InfosetID).
	// Override via WithIDFn for abstraction-aware infoset keying.
	idFn func(*State) uint64

	// walkVisited — IDs of traverser-owned infosets touched during the current
	// walk. Reused across walks to avoid alloc. Drives RM+ targeted flooring
	// (avoids the old O(total infosets) full-map sweep that dominated per-iter
	// cost as the table grew).
	walkVisited []uint64
}

// NewMCCFR — defaults to RM+ + linear averaging (Pluribus-style).
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

// WithIDFn — override the infoset key function. Used to swap in abstraction
// (e.g. preflop bucket ID instead of lossless FNV hash).
// Must be set BEFORE any Iter() call.
func (m *MCCFR) WithIDFn(fn func(*State) uint64) *MCCFR {
	m.idFn = fn
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
		m.numActions[id] = n
	}
	s = m.strategy[id]
	sigma = m.sigmaBuf[id]
	return
}

// Iter — one MCCFR iteration: traverse for both players, then RM+ floor.
//
// RM+ flooring only sweeps infosets visited by THIS walk (tracked via
// walkVisited), not the full regret map. Crucial as the infoset table grows:
// full-sweep cost is O(total infosets), targeted is O(walk size) ≈ O(depth).
func (m *MCCFR) Iter() {
	m.iters++
	for _, trav := range [...]Player{P0, P1} {
		m.walkVisited = m.walkVisited[:0]
		m.runTraverser(trav)
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

func (m *MCCFR) runTraverser(trav Player) {
	s := NewState(m.cfg)
	m.dealHoles(s)
	m.walk(s, trav)
}

// dealHoles — uniformly sample 4 distinct cards: P0 hole + P1 hole.
func (m *MCCFR) dealHoles(s *State) {
	// Fisher-Yates on a small prefix (5 elements ≥ 4 needed).
	// Faster than full deck shuffle.
	var picked [4]Card
	var used [DeckSize]bool
	for i := 0; i < 4; i++ {
		for {
			c := Card(m.rng.Intn(DeckSize))
			if !used[c] {
				picked[i] = c
				used[c] = true
				break
			}
		}
	}
	s.SetHole(P0, picked[0], picked[1])
	s.SetHole(P1, picked[2], picked[3])
}

// chanceFill — sample n distinct board cards from remaining deck.
// Returns Snapshot taken BEFORE the fill so caller can Restore.
//
// Handles BOTH mid-game street transitions (3/1/1 cards) and all-in showdown
// fill-to-5 — generalized from the old sampleBoardFill.
func (m *MCCFR) chanceFill(s *State, n int) Snapshot {
	snap := s.Snapshot()
	var used [DeckSize]bool
	used[s.Hole[P0][0]] = true
	used[s.Hole[P0][1]] = true
	used[s.Hole[P1][0]] = true
	used[s.Hole[P1][1]] = true
	for i := uint8(0); i < s.NumBoard; i++ {
		used[s.Board[i]] = true
	}
	for i := 0; i < n; i++ {
		for {
			c := Card(m.rng.Intn(DeckSize))
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
func (m *MCCFR) walk(s *State, trav Player) float64 {
	// Chance node: deal needed board cards before continuing.
	// Covers preflop→flop (3), flop→turn (1), turn→river (1), and all-in
	// showdown fill-to-5.
	if n, needs := s.NeedsBoard(); needs {
		snap := m.chanceFill(s, n)
		v := m.walk(s, trav)
		s.Restore(snap)
		return v
	}

	if s.Terminal {
		// Fold terminal or already-filled showdown: just compute payoff.
		return float64(s.Payoff(trav))
	}

	id := m.idFn(s)
	legal := s.LegalActions()
	n := len(legal)
	regret, stratSum, sigma := m.ensure(id, n)
	regretMatching(regret, sigma)

	if s.Cur == trav {
		var utilsArr [8]float64
		utils := utilsArr[:n]
		var nodeUtil float64
		for i, a := range legal {
			snap := s.Snapshot()
			s.Apply(a)
			utils[i] = m.walk(s, trav)
			s.Restore(snap)
			nodeUtil += sigma[i] * utils[i]
		}
		// Regret update — no reach factor (sampling provides it).
		for i := range legal {
			regret[i] += utils[i] - nodeUtil
		}
		// Strategy sum (linear averaging if enabled).
		w := 1.0
		if m.linearAvg {
			w = float64(m.iters)
		}
		for i := range legal {
			stratSum[i] += w * sigma[i]
		}
		// Track for RM+ targeted flooring after walk.
		m.walkVisited = append(m.walkVisited, id)
		return nodeUtil
	}

	// Opponent — sample one action proportional to sigma.
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

// regretMatching — same RM as cfr/regret_matching.go. Inlined to avoid cross-package
// dependency for this engine-coupled solver.
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

// LegalActionsForID — look up the legal action set for a given infoset ID
// (by replaying the engine path that produced it would be expensive; we cache
// the action COUNT only, used for sigma sizing in downstream tools).
func (m *MCCFR) NumActionsForID(id uint64) int {
	return m.numActions[id]
}
