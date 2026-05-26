package leduc

import (
	"fmt"
	"strings"
)

// Leduc Hold'em rules (matching OpenSpiel leduc_poker):
//   - 2 players, ante 1 chip each.
//   - Round 1: each gets 1 private card, betting (bet=2, max 2 raises).
//   - Round 2: 1 public card revealed, betting (bet=4, max 2 raises).
//   - Showdown: pair-of-board beats high private; else higher private wins; tie if same rank.
//   - Actions: Fold (0), CheckCall (1), BetRaise (2). BetRaise illegal once NumRaises == MaxRaises.
//
// Game value at Nash equilibrium for P0 ≈ -0.0856 (P0 = first-to-act both rounds, slight disadvantage).
const (
	Ante      = 1
	BetSize1  = 2 // round-1 bet/raise size
	BetSize2  = 4 // round-2 bet/raise size
	MaxRaises = 2 // bets+raises per round
)

type Player uint8

const (
	P0       Player = 0
	P1       Player = 1
	NoPlayer Player = 255
)

func (p Player) Other() Player { return 1 - p }

type Action uint8

const (
	ActionFold      Action = 0
	ActionCheckCall Action = 1
	ActionBetRaise  Action = 2
	NumActions             = 3
)

func (a Action) Char() byte {
	switch a {
	case ActionFold:
		return 'f'
	case ActionCheckCall:
		return 'c'
	case ActionBetRaise:
		return 'r'
	}
	return '?'
}

// State — full Leduc game state. Caller drives chance nodes (private deal at start,
// public deal between rounds) by setting Priv at construction and calling SetPublic
// once NeedsPublicCard() returns true.
type State struct {
	Priv   [NumPlayers]Card
	Pub    Card
	HasPub bool

	Round uint8 // 0 (preflop) or 1 (turn)

	NumRaises uint8 // bets+raises in current round (0..MaxRaises)
	ToCall    int   // chips current actor must match (0 means open)
	Cur       Player

	Contrib [NumPlayers]int // total chips contributed this hand (incl ante)

	Hist [2][]Action // per-round betting history

	Terminal bool
	FoldedBy Player // NoPlayer if not folded
}

// NewState — start of a new hand with private cards dealt. Public card is dealt later via SetPublic.
func NewState(priv0, priv1 Card) *State {
	return &State{
		Priv:     [NumPlayers]Card{priv0, priv1},
		Pub:      NoCard,
		Round:    0,
		Cur:      P0,
		Contrib:  [NumPlayers]int{Ante, Ante},
		FoldedBy: NoPlayer,
	}
}

func (s *State) BetSize() int {
	if s.Round == 0 {
		return BetSize1
	}
	return BetSize2
}

func (s *State) IsLegal(a Action) bool {
	if s.Terminal {
		return false
	}
	switch a {
	case ActionFold, ActionCheckCall:
		return true
	case ActionBetRaise:
		return s.NumRaises < MaxRaises
	}
	return false
}

func (s *State) LegalActions() []Action {
	if s.Terminal {
		return nil
	}
	if s.NumRaises < MaxRaises {
		return []Action{ActionFold, ActionCheckCall, ActionBetRaise}
	}
	return []Action{ActionFold, ActionCheckCall}
}

// NeedsPublicCard — true between round-1 end and the first round-2 action.
func (s *State) NeedsPublicCard() bool { return s.Round == 1 && !s.HasPub && !s.Terminal }

// SetPublic — deal the public card. Must be called once NeedsPublicCard returns true.
func (s *State) SetPublic(c Card) {
	if !s.NeedsPublicCard() {
		panic("SetPublic called outside round-2 transition")
	}
	if c == s.Priv[0] || c == s.Priv[1] {
		panic("public card collides with a private card")
	}
	s.Pub = c
	s.HasPub = true
}

// Apply mutates state with action a (must be legal). Returns self for chaining.
// Caller is responsible for calling SetPublic when NeedsPublicCard is true.
func (s *State) Apply(a Action) *State {
	if s.Terminal {
		panic("Apply on terminal state")
	}
	if s.NeedsPublicCard() {
		panic("Apply called before SetPublic at round-2 transition")
	}
	if !s.IsLegal(a) {
		panic(fmt.Sprintf("illegal action %d at infoset %s", a, s.InfosetKey()))
	}

	actor := s.Cur
	s.Hist[s.Round] = append(s.Hist[s.Round], a)

	switch a {
	case ActionFold:
		s.FoldedBy = actor
		s.Terminal = true
		return s

	case ActionBetRaise:
		bet := s.BetSize()
		s.Contrib[actor] += s.ToCall + bet
		s.ToCall = bet
		s.NumRaises++
		s.Cur = actor.Other()
		return s

	case ActionCheckCall:
		roundEnds := false
		if s.ToCall > 0 {
			s.Contrib[actor] += s.ToCall
			s.ToCall = 0
			roundEnds = true
		} else {
			// Check. Round ends if both have checked.
			// ToCall==0 throughout means no bets this round, so prior actions are all checks.
			roundEnds = len(s.Hist[s.Round]) >= 2
		}
		if !roundEnds {
			s.Cur = actor.Other()
			return s
		}
		if s.Round == 0 {
			// Transition to round 2. Public card dealt by caller via SetPublic.
			s.Round = 1
			s.NumRaises = 0
			s.ToCall = 0
			s.Cur = P0
			return s
		}
		s.Terminal = true
		return s
	}

	panic("unreachable")
}

// Payoff returns the chip utility for player p at terminal state.
// Convention: winner gets +opponent_contrib, loser gets -own_contrib. Contribs equal
// in Leduc (limit) so payoffs are symmetric ±Contrib.
func (s *State) Payoff(p Player) float64 {
	if !s.Terminal {
		panic("Payoff on non-terminal state")
	}
	opp := p.Other()
	if s.FoldedBy != NoPlayer {
		if s.FoldedBy == p {
			return -float64(s.Contrib[p])
		}
		return +float64(s.Contrib[opp])
	}
	// Showdown — public card must be dealt.
	if !s.HasPub {
		panic("showdown without public card")
	}
	rankP := s.handStrength(p)
	rankO := s.handStrength(opp)
	switch {
	case rankP > rankO:
		return +float64(s.Contrib[opp])
	case rankP < rankO:
		return -float64(s.Contrib[p])
	default:
		return 0
	}
}

// handStrength: pair-of-board > high private. Pair = 100 + rank; high = rank.
func (s *State) handStrength(p Player) int {
	priv := s.Priv[p]
	if priv.Rank() == s.Pub.Rank() {
		return 100 + int(priv.Rank())
	}
	return int(priv.Rank())
}

// Clone — deep copy (history slices independent).
// Use Snapshot/Restore instead in performance-sensitive recursion (no alloc).
func (s *State) Clone() *State {
	out := *s
	out.Hist[0] = append([]Action(nil), s.Hist[0]...)
	out.Hist[1] = append([]Action(nil), s.Hist[1]...)
	return &out
}

// Snapshot — captures all mutable fields for O(1) restore.
// Pattern for zero-alloc recursive walks:
//
//	snap := s.Snapshot()
//	s.Apply(a)         // or s.SetPublic(c)
//	v := recurse(s)
//	s.Restore(snap)
type Snapshot struct {
	Round, NumRaises uint8
	ToCall           int
	Cur              Player
	Contrib          [NumPlayers]int
	HistLen          [2]int
	Terminal         bool
	FoldedBy         Player
	Pub              Card
	HasPub           bool
}

func (s *State) Snapshot() Snapshot {
	return Snapshot{
		Round:     s.Round,
		NumRaises: s.NumRaises,
		ToCall:    s.ToCall,
		Cur:       s.Cur,
		Contrib:   s.Contrib,
		HistLen:   [2]int{len(s.Hist[0]), len(s.Hist[1])},
		Terminal:  s.Terminal,
		FoldedBy:  s.FoldedBy,
		Pub:       s.Pub,
		HasPub:    s.HasPub,
	}
}

// Restore — undo all mutations since snap was taken. O(1) (history is truncated via slice reslice).
func (s *State) Restore(snap Snapshot) {
	s.Round = snap.Round
	s.NumRaises = snap.NumRaises
	s.ToCall = snap.ToCall
	s.Cur = snap.Cur
	s.Contrib = snap.Contrib
	s.Hist[0] = s.Hist[0][:snap.HistLen[0]]
	s.Hist[1] = s.Hist[1][:snap.HistLen[1]]
	s.Terminal = snap.Terminal
	s.FoldedBy = snap.FoldedBy
	s.Pub = snap.Pub
	s.HasPub = snap.HasPub
}

// InfosetID — packed uint64 identifier (bits 0-26 used). Faster than InfosetKey() string
// for map keys in CFR/BR. Same information content as InfosetKey, suit-collapsed.
//
// Layout:
//
//	bits  0-1   priv rank (0=J, 1=Q, 2=K)
//	bits  2-4   pub rank (0=J, 1=Q, 2=K, 7=undealt)
//	bits  5-7   r1 hist length (0..4)
//	bits  8-15  r1 hist (4 actions × 2 bits)
//	bits 16-18  r2 hist length (0..4)
//	bits 19-26  r2 hist (4 actions × 2 bits)
func (s *State) InfosetID() uint64 {
	var id uint64
	id |= uint64(s.Priv[s.Cur].Rank())
	if s.HasPub {
		id |= uint64(s.Pub.Rank()) << 2
	} else {
		id |= uint64(7) << 2
	}
	id |= uint64(len(s.Hist[0])) << 5
	for i, a := range s.Hist[0] {
		id |= uint64(a) << (8 + uint(i)*2)
	}
	id |= uint64(len(s.Hist[1])) << 16
	for i, a := range s.Hist[1] {
		id |= uint64(a) << (19 + uint(i)*2)
	}
	return id
}

// InfosetKey — canonical key from the current actor's view (private rank + public rank if dealt + history).
// Suits are collapsed (no information value in Leduc). Format:
//
//	"<privRank>/<pubRank|?>/<round0_hist>/<round1_hist>"  e.g. "Q/J/cr/c"
//
// Used as map key for regret/strategy tables.
func (s *State) InfosetKey() string {
	var sb strings.Builder
	sb.WriteByte(rankSym[s.Priv[s.Cur].Rank()])
	sb.WriteByte('/')
	if s.HasPub {
		sb.WriteByte(rankSym[s.Pub.Rank()])
	} else {
		sb.WriteByte('?')
	}
	sb.WriteByte('/')
	for _, a := range s.Hist[0] {
		sb.WriteByte(a.Char())
	}
	sb.WriteByte('/')
	for _, a := range s.Hist[1] {
		sb.WriteByte(a.Char())
	}
	return sb.String()
}
