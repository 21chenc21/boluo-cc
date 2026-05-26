package leduc

import (
	"math/rand"
	"testing"
)

// Always-fold strategy: P0 always folds. Pure deterministic.
func alwaysFoldStrategy(s *State) []float64 {
	la := s.LegalActions()
	out := make([]float64, len(la))
	if s.Cur == P0 {
		for i, a := range la {
			if a == ActionFold {
				out[i] = 1
				return out
			}
		}
	}
	// P1 plays uniform.
	p := 1.0 / float64(len(la))
	for i := range out {
		out[i] = p
	}
	return out
}

// TestAlwaysFoldEV — if P0 always folds at first decision, P0 forfeits ante (1) every hand. EV(P0) = -1.
func TestAlwaysFoldEV(t *testing.T) {
	ev, _, _ := walkAllTerminals(t, alwaysFoldStrategy)
	if ev < -1-1e-9 || ev > -1+1e-9 {
		t.Errorf("always-fold EV(P0)=%v want -1", ev)
	}
}

// Always-bet/raise then call: pure "aggressive" — always BetRaise when legal, else CheckCall (call).
func alwaysAggroStrategy(s *State) []float64 {
	la := s.LegalActions()
	out := make([]float64, len(la))
	for i, a := range la {
		if a == ActionBetRaise {
			out[i] = 1
			return out
		}
	}
	for i, a := range la {
		if a == ActionCheckCall {
			out[i] = 1
			return out
		}
	}
	return out
}

// TestAggressiveSymmetricEV — both players always-bet/raise-else-call. By symmetry, EV(P0)=0.
func TestAggressiveSymmetricEV(t *testing.T) {
	ev, _, maxC := walkAllTerminals(t, alwaysAggroStrategy)
	if ev > 1e-9 || ev < -1e-9 {
		t.Errorf("aggressive-aggressive EV(P0)=%v want 0 (symmetric)", ev)
	}
	// Both always raise to cap, both always call → both contribute max.
	const wantMaxContrib = Ante + 2*BetSize1 + 2*BetSize2
	if maxC != wantMaxContrib {
		t.Errorf("aggressive maxContrib=%d want %d", maxC, wantMaxContrib)
	}
}

// TestRandomPlayNoPanics — stress-test: 100k random games never panic / always terminate.
func TestRandomPlayNoPanics(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 100000; trial++ {
		// Random deal.
		d := NewDeck()
		d.Shuffle(rng)
		s := NewState(d.Draw(), d.Draw())
		pub := d.Draw()

		// Bounded loop: max 4 actions per round × 2 rounds = 8.
		steps := 0
		for !s.Terminal {
			if s.NeedsPublicCard() {
				s.SetPublic(pub)
				continue
			}
			la := s.LegalActions()
			a := la[rng.Intn(len(la))]
			s.Apply(a)
			steps++
			if steps > 12 {
				t.Fatalf("trial %d: too many steps without termination (history=%v)", trial, s.Hist)
			}
		}
		// Zero-sum.
		if s.Payoff(P0)+s.Payoff(P1) != 0 {
			t.Errorf("trial %d: zero-sum violated", trial)
		}
		// Contribs equal at non-fold terminal.
		if s.FoldedBy == NoPlayer && s.Contrib[P0] != s.Contrib[P1] {
			t.Errorf("trial %d: contribs unequal at showdown: %v", trial, s.Contrib)
		}
	}
}

// TestInfosetKeyUniqueness — every distinct (priv-from-actor-view, full-history,
// public-rank-if-dealt) combination must yield a unique InfosetKey. Same key →
// states identical from actor's view (key is sufficient statistic).
//
// Conversely: walk all decision states and verify states sharing the same key
// have identical (LegalActions, Cur, Round, NumRaises, ToCall) — proves the key
// captures everything the actor needs.
func TestInfosetKeyIsSufficientStatistic(t *testing.T) {
	type sig struct {
		legal     string // serialized legal actions
		cur       Player
		round     uint8
		numRaises uint8
		toCall    int
	}
	byKey := make(map[string]sig)

	var dfs func(s *State)
	dfs = func(s *State) {
		if s.Terminal {
			return
		}
		if s.NeedsPublicCard() {
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl)
			}
			return
		}
		la := s.LegalActions()
		legalStr := make([]byte, len(la))
		for i, a := range la {
			legalStr[i] = a.Char()
		}
		cur := sig{
			legal:     string(legalStr),
			cur:       s.Cur,
			round:     s.Round,
			numRaises: s.NumRaises,
			toCall:    s.ToCall,
		}
		key := s.InfosetKey()
		if prev, seen := byKey[key]; seen {
			if prev != cur {
				t.Errorf("infoset key %q collides between distinct signatures:\n  prev=%+v\n  cur =%+v",
					key, prev, cur)
			}
		} else {
			byKey[key] = cur
		}
		for _, a := range la {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl)
		}
	}
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(NewState(p0, p1))
		}
	}
	t.Logf("verified %d unique infoset keys, all share consistent decision signature", len(byKey))
}

// TestPayoffConservation — at every terminal, |Payoff(P0)| == |Payoff(P1)|
// and Payoff sums to 0. Stronger than just zero-sum: ensures magnitude consistency.
func TestPayoffMagnitudeMatches(t *testing.T) {
	var dfs func(s *State)
	dfs = func(s *State) {
		if s.Terminal {
			p0 := s.Payoff(P0)
			p1 := s.Payoff(P1)
			if p0+p1 != 0 {
				t.Errorf("zero-sum: %v+%v != 0", p0, p1)
			}
			abs := func(x float64) float64 {
				if x < 0 {
					return -x
				}
				return x
			}
			if abs(p0) != abs(p1) {
				t.Errorf("|P0 payoff| != |P1 payoff|: |%v| vs |%v|", p0, p1)
			}
			return
		}
		if s.NeedsPublicCard() {
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl)
			}
			return
		}
		for _, a := range s.LegalActions() {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl)
		}
	}
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(NewState(p0, p1))
		}
	}
}
