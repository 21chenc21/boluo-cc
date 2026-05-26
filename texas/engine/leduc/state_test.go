package leduc

import (
	"testing"
)

// Helper: build state and play a sequence; deal public card when prompted.
// Aborts test if any panic occurs or sequence ends early/late.
func play(t *testing.T, priv0, priv1, pub Card, seq ...Action) *State {
	t.Helper()
	s := NewState(priv0, priv1)
	for i, a := range seq {
		if s.Terminal {
			t.Fatalf("seq idx %d (action %d): state already terminal", i, a)
		}
		if s.NeedsPublicCard() {
			s.SetPublic(pub)
		}
		s.Apply(a)
	}
	// Leave state at a decision point: if round just transitioned to 2 and public is unset, deal it.
	if s.NeedsPublicCard() {
		s.SetPublic(pub)
	}
	return s
}

// ──────────────────────── card / deck ────────────────────────

func TestCardRankSuit(t *testing.T) {
	for r := uint8(0); r < NumRanks; r++ {
		for su := uint8(0); su < NumSuits; su++ {
			c := MakeCard(r, su)
			if c.Rank() != r || c.Suit() != su {
				t.Errorf("MakeCard(%d,%d)=%v rank/suit=%d,%d", r, su, c, c.Rank(), c.Suit())
			}
		}
	}
}

func TestDeckSize(t *testing.T) {
	d := NewDeck()
	if d.Remaining() != DeckSize {
		t.Errorf("deck size %d, want %d", d.Remaining(), DeckSize)
	}
	seen := map[Card]bool{}
	for d.Remaining() > 0 {
		c := d.Draw()
		if seen[c] {
			t.Errorf("duplicate card %v", c)
		}
		seen[c] = true
	}
	if len(seen) != DeckSize {
		t.Errorf("drew %d unique cards, want %d", len(seen), DeckSize)
	}
}

// ──────────────────────── action legality ────────────────────────

func TestLegalActionsAtStart(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	la := s.LegalActions()
	if len(la) != 3 {
		t.Errorf("opening legal actions = %v, want 3", la)
	}
}

func TestLegalActionsAtRaiseCap(t *testing.T) {
	// P0 bet, P1 raise → NumRaises=2 (cap) → P0 should only have Fold, CheckCall.
	s := play(t, MakeCard(0, 0), MakeCard(1, 0), MakeCard(2, 0),
		ActionBetRaise, ActionBetRaise)
	la := s.LegalActions()
	if len(la) != 2 || la[0] != ActionFold || la[1] != ActionCheckCall {
		t.Errorf("at cap legal=%v, want {Fold, CheckCall}", la)
	}
	if s.IsLegal(ActionBetRaise) {
		t.Errorf("BetRaise illegal at NumRaises=cap")
	}
}

// ──────────────────────── round transition ────────────────────────

func TestCheckCheckEndsRound1(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionCheckCall) // P0 check
	if s.Round != 0 || s.Terminal {
		t.Errorf("after P0 check: round=%d terminal=%v want round=0 terminal=false", s.Round, s.Terminal)
	}
	if s.Cur != P1 {
		t.Errorf("after P0 check: cur=%d want P1", s.Cur)
	}
	s.Apply(ActionCheckCall) // P1 check → round ends
	if s.Round != 1 {
		t.Errorf("after check-check: round=%d want 1", s.Round)
	}
	if !s.NeedsPublicCard() {
		t.Errorf("expected NeedsPublicCard after round 1 ends")
	}
	if s.Cur != P0 {
		t.Errorf("round 2 first actor = %d, want P0", s.Cur)
	}
}

func TestBetCallEndsRound1(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0)) // P0=J, P1=Q
	s.Apply(ActionBetRaise)                       // P0 bet (round 1 bet=2)
	if s.ToCall != BetSize1 || s.NumRaises != 1 {
		t.Errorf("after P0 bet: ToCall=%d NumRaises=%d want %d/1", s.ToCall, s.NumRaises, BetSize1)
	}
	if s.Contrib[P0] != Ante+BetSize1 {
		t.Errorf("P0 contrib=%d want %d", s.Contrib[P0], Ante+BetSize1)
	}
	s.Apply(ActionCheckCall) // P1 call
	if s.Round != 1 {
		t.Errorf("after bet-call: round=%d want 1", s.Round)
	}
	if s.Contrib[P0] != s.Contrib[P1] {
		t.Errorf("contribs unequal: P0=%d P1=%d", s.Contrib[P0], s.Contrib[P1])
	}
}

// ──────────────────────── payoff: fold ────────────────────────

func TestFoldPreflopP1WinsAnte(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionBetRaise) // P0 bet → P0 invested 3 (ante+2), P1 still 1.
	s.Apply(ActionFold)     // P1 fold
	if !s.Terminal {
		t.Fatal("not terminal after fold")
	}
	// P1 forfeits ante (1). P0 wins +1, P1 loses -1.
	// Note: P0 invested 3 but P1 only invested 1, so pot if P1 folds = P0_contrib_returned + P1_ante = win +P1_contrib.
	if got := s.Payoff(P0); got != +float64(s.Contrib[P1]) {
		t.Errorf("Payoff(P0) on P1 fold = %v, want +%d (=Contrib[P1])", got, s.Contrib[P1])
	}
	if got := s.Payoff(P1); got != -float64(s.Contrib[P1]) {
		t.Errorf("Payoff(P1) on own fold = %v, want -%d", got, s.Contrib[P1])
	}
	if s.Payoff(P0)+s.Payoff(P1) != 0 {
		t.Errorf("zero-sum violated: %v + %v != 0", s.Payoff(P0), s.Payoff(P1))
	}
}

func TestFoldImmediateLosesAnte(t *testing.T) {
	// P0 checks, P1 bets, P0 folds → P0 loses ante (1), P1 wins ante.
	// Note: by convention fold-when-free is legal but dominated; we still test "P0 bets, P1 folds" above.
	s := NewState(MakeCard(0, 0), MakeCard(2, 0))
	s.Apply(ActionCheckCall) // P0 check
	s.Apply(ActionBetRaise)  // P1 bet
	s.Apply(ActionFold)      // P0 fold
	if !s.Terminal {
		t.Fatal("not terminal")
	}
	if s.Payoff(P0) != -1 || s.Payoff(P1) != +1 {
		t.Errorf("P0 fold after P1 bet: payoffs P0=%v P1=%v want -1/+1",
			s.Payoff(P0), s.Payoff(P1))
	}
}

// ──────────────────────── payoff: showdown ────────────────────────

func TestShowdownHighCardWins(t *testing.T) {
	// P0=K, P1=J, public=Q (no pair) → P0 wins (K > J).
	s := play(t, MakeCard(2, 0), MakeCard(0, 0), MakeCard(1, 0),
		ActionCheckCall, ActionCheckCall, // round 1 check-check
		ActionCheckCall, ActionCheckCall) // round 2 check-check
	if !s.Terminal {
		t.Fatal("not terminal")
	}
	if s.Payoff(P0) <= 0 {
		t.Errorf("P0=K vs P1=J pub=Q → P0 payoff %v, want positive", s.Payoff(P0))
	}
	if s.Payoff(P0)+s.Payoff(P1) != 0 {
		t.Errorf("zero-sum violated")
	}
	if s.Payoff(P0) != +1 {
		// Both anted 1, no bets, P0 wins +1.
		t.Errorf("P0 showdown win amount = %v, want +1", s.Payoff(P0))
	}
}

func TestShowdownPairBeatsHighCard(t *testing.T) {
	// P0=J, P1=K, public=J → P0 has pair of J, beats P1's K-high.
	s := play(t, MakeCard(0, 0), MakeCard(2, 0), MakeCard(0, 1),
		ActionCheckCall, ActionCheckCall,
		ActionCheckCall, ActionCheckCall)
	if s.Payoff(P0) <= 0 {
		t.Errorf("P0 paired J vs K-high → payoff %v want positive", s.Payoff(P0))
	}
}

func TestShowdownTieSameRank(t *testing.T) {
	// P0=J♠, P1=J♥, public=K → tie (both J-high, no pair).
	s := play(t, MakeCard(0, 0), MakeCard(0, 1), MakeCard(2, 0),
		ActionCheckCall, ActionCheckCall,
		ActionCheckCall, ActionCheckCall)
	if s.Payoff(P0) != 0 {
		t.Errorf("tie expected, P0 payoff = %v", s.Payoff(P0))
	}
}

// ──────────────────────── invariants ────────────────────────

func TestContribsAlwaysEqual(t *testing.T) {
	// Limit Hold'em invariant: at any non-fold terminal state, both players contributed equally.
	sequences := [][]Action{
		{ActionCheckCall, ActionCheckCall, ActionCheckCall, ActionCheckCall},
		{ActionBetRaise, ActionCheckCall, ActionCheckCall, ActionCheckCall},
		{ActionBetRaise, ActionBetRaise, ActionCheckCall, ActionBetRaise, ActionCheckCall},
		{ActionCheckCall, ActionBetRaise, ActionCheckCall, ActionCheckCall, ActionCheckCall},
	}
	for i, seq := range sequences {
		s := play(t, MakeCard(0, 0), MakeCard(1, 0), MakeCard(2, 0), seq...)
		if !s.Terminal {
			t.Errorf("seq %d not terminal: %v", i, seq)
			continue
		}
		if s.FoldedBy == NoPlayer && s.Contrib[P0] != s.Contrib[P1] {
			t.Errorf("seq %d contribs unequal: P0=%d P1=%d", i, s.Contrib[P0], s.Contrib[P1])
		}
	}
}

// ──────────────────────── infoset key ────────────────────────

func TestInfosetKeySuitInvariance(t *testing.T) {
	// Same rank, different suit → same infoset key from current player's view.
	for su := uint8(0); su < NumSuits; su++ {
		s := NewState(MakeCard(1, su), MakeCard(0, 0)) // P0 has Q of various suits
		key := s.InfosetKey()
		want := "Q/?//"
		if key != want {
			t.Errorf("suit %d → key=%q want %q", su, key, want)
		}
	}
}

func TestInfosetKeyEncoding(t *testing.T) {
	s := play(t, MakeCard(2, 0), MakeCard(1, 0), MakeCard(0, 0),
		ActionBetRaise, ActionBetRaise, ActionCheckCall, // round 1: r r c
	)
	// After call, we're at round-2 start, P0 to act. Pub dealt.
	if s.NeedsPublicCard() {
		t.Fatal("should have public dealt by play()")
	}
	if s.Round != 1 || s.Cur != P0 {
		t.Fatalf("round=%d cur=%d want 1/P0", s.Round, s.Cur)
	}
	got := s.InfosetKey()
	want := "K/J/rrc/"
	if got != want {
		t.Errorf("key=%q want %q", got, want)
	}
}

func TestInfosetKeyOpponentPrivateNotLeaked(t *testing.T) {
	s1 := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s2 := NewState(MakeCard(0, 0), MakeCard(2, 0))
	if s1.InfosetKey() != s2.InfosetKey() {
		t.Errorf("opponent private leaked: %q vs %q", s1.InfosetKey(), s2.InfosetKey())
	}
}

// ──────────────────────── clone independence ────────────────────────

func TestCloneIndependent(t *testing.T) {
	s := NewState(MakeCard(0, 0), MakeCard(1, 0))
	s.Apply(ActionBetRaise)
	c := s.Clone()
	c.Apply(ActionFold)
	if s.Terminal {
		t.Error("original terminal after clone mutated")
	}
	if !c.Terminal {
		t.Error("clone not terminal after fold")
	}
}

// ──────────────────────── enumeration sanity ────────────────────────

// Walk all (priv0, priv1, pub) combinations + all action sequences, ensure
// every terminal state has well-defined payoffs and zero-sum holds.
func TestEnumerationZeroSum(t *testing.T) {
	var dfs func(s *State, pub Card)
	dfs = func(s *State, pub Card) {
		if s.Terminal {
			p0 := s.Payoff(P0)
			p1 := s.Payoff(P1)
			if p0+p1 != 0 {
				t.Errorf("zero-sum violated: priv=%v,%v pub=%v hist=%v p0=%v p1=%v",
					s.Priv[0], s.Priv[1], s.Pub, s.Hist, p0, p1)
			}
			return
		}
		if s.NeedsPublicCard() {
			// Try every legal public card (different from both privates).
			for c := Card(0); c < DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl, c)
			}
			return
		}
		for _, a := range s.LegalActions() {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl, pub)
		}
	}
	for p0 := Card(0); p0 < DeckSize; p0++ {
		for p1 := Card(0); p1 < DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			s := NewState(p0, p1)
			dfs(s, NoCard)
		}
	}
}
