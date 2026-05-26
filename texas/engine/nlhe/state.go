package nlhe

import (
	"fmt"
)

// Player identifiers. P0 = SB (button), P1 = BB.
//
// HUNL convention:
//   - Preflop: SB acts first (out of position)
//   - Postflop: BB acts first (out of position; SB on button)
type Player uint8

const (
	P0       Player = 0 // SB / button
	P1       Player = 1
	NoPlayer Player = 255
)

func (p Player) Other() Player { return 1 - p }

// Street ordinal.
type Street uint8

const (
	StreetPreflop Street = 0
	StreetFlop    Street = 1
	StreetTurn    Street = 2
	StreetRiver   Street = 3
)

// State — HUNL game state.
//
// Driven externally for chance nodes (hole/board dealing) similar to engine/leduc.
// All chip values in raw chips (cfg.BigBlind=2 by default; everything else scales).
type State struct {
	Cfg *GameConfig // immutable per-state

	// Cards — caller deals via SetHole / SetBoard between rounds.
	Hole         [NumPlayers][2]Card // hole cards (set before preflop)
	HoleSet      [NumPlayers]bool
	Board        [5]Card // board cards: 0..NumBoard-1 are dealt
	NumBoard     uint8   // 0=preflop, 3=flop, 4=turn, 5=river

	// Pot / stacks.
	Stacks  [NumPlayers]int // remaining chips per player
	Wagered [NumPlayers]int // chips put in this hand (incl blinds)

	// Street state.
	Street          Street
	BetThisStreet   [NumPlayers]int // chips put in THIS street
	LastBetAmount   int             // amount of last bet/raise this street (incl. call portion); 0 = no bet
	LastRaiseSize   int             // size of last raise increment (for min-raise rule)
	NumActionsThisStreet uint8       // count of actions this street

	// Turn / history.
	Cur     Player
	Hist    [4][]Action // history per street
	AllIn   [NumPlayers]bool

	// Terminal.
	Terminal bool
	FoldedBy Player // NoPlayer if not folded
}

// NewState — start of a new hand. Hole cards dealt via SetHole; board dealt via SetBoard.
func NewState(cfg *GameConfig) *State {
	s := &State{
		Cfg:           cfg,
		NumBoard:      0,
		Street:        StreetPreflop,
		LastBetAmount: 0,
		FoldedBy:      NoPlayer,
	}
	s.Stacks[P0] = cfg.StartStack - cfg.SmallBlind
	s.Stacks[P1] = cfg.StartStack - cfg.BigBlind
	s.Wagered[P0] = cfg.SmallBlind
	s.Wagered[P1] = cfg.BigBlind
	s.BetThisStreet[P0] = cfg.SmallBlind
	s.BetThisStreet[P1] = cfg.BigBlind
	s.LastBetAmount = cfg.BigBlind
	s.LastRaiseSize = cfg.BigBlind
	// Preflop: SB acts first.
	s.Cur = P0
	for c := range s.Board {
		s.Board[c] = NoCard
	}
	for p := range s.Hole {
		s.Hole[p] = [2]Card{NoCard, NoCard}
	}
	return s
}

// SetHole — deal player p's hole cards. Must be called before first action.
func (s *State) SetHole(p Player, c1, c2 Card) {
	if s.HoleSet[p] {
		panic(fmt.Sprintf("SetHole: P%d already dealt", p))
	}
	if !c1.IsValid() || !c2.IsValid() || c1 == c2 {
		panic(fmt.Sprintf("SetHole: invalid cards %v %v", c1, c2))
	}
	s.Hole[p] = [2]Card{c1, c2}
	s.HoleSet[p] = true
}

// SetBoard — deal `numNew` board cards (3 for flop, 1 for turn/river).
// Caller must SetBoard with right count at the right transition.
func (s *State) SetBoard(cards ...Card) {
	for _, c := range cards {
		if !c.IsValid() {
			panic(fmt.Sprintf("SetBoard: invalid card %v", c))
		}
		if int(s.NumBoard) >= len(s.Board) {
			panic("SetBoard: board already full")
		}
		s.Board[s.NumBoard] = c
		s.NumBoard++
	}
}

// Pot — total chips in pot (sum of Wagered across players).
func (s *State) Pot() int { return s.Wagered[P0] + s.Wagered[P1] }

// ToCall — amount current player needs to add to match the leading bet.
func (s *State) ToCall() int {
	opp := s.Cur.Other()
	return s.BetThisStreet[opp] - s.BetThisStreet[s.Cur]
}

// minBet — minimum legal opening bet this street (= BB).
func (s *State) minBet() int { return s.Cfg.BigBlind }

// minRaiseTo — total amount to raise to (== opponent's BetThisStreet + LastRaiseSize).
func (s *State) minRaiseTo() int {
	opp := s.Cur.Other()
	return s.BetThisStreet[opp] + s.LastRaiseSize
}

// LegalActions — enumerate currently-legal actions. Order is canonical:
//
//	Fold, CheckCall, [Bet/Raise sizes that are individually legal], AllIn
//
// Push/fold mode: highly restricted action set (no limping):
//   - Without opp all-in: {Fold, AllIn}
//   - Facing opp all-in: {Fold, CheckCall (=call shove)}
//
// Normal mode: Fold, CheckCall, Bet sizes (per config.BetSizes), AllIn.
func (s *State) LegalActions() []Action {
	if s.Terminal {
		return nil
	}
	if s.Cfg.PushFoldOnly {
		return s.legalPushFold()
	}
	return s.legalNormal()
}

// legalPushFold — restricted push/fold action set (shove-or-fold style).
func (s *State) legalPushFold() []Action {
	out := []Action{{Kind: ActionFold}}
	oppAllIn := s.AllIn[s.Cur.Other()]
	if oppAllIn {
		// Facing shove: call or fold. CheckCall means "call the all-in".
		out = append(out, Action{Kind: ActionCheckCall})
	} else if s.Stacks[s.Cur] > 0 {
		// No bet yet from opp: only legal aggressive action is shove (no limp).
		out = append(out, Action{Kind: ActionAllIn})
	}
	return out
}

// legalNormal — full HUNL action set.
func (s *State) legalNormal() []Action {
	out := []Action{{Kind: ActionFold}, {Kind: ActionCheckCall}}
	toCall := s.ToCall()
	stackAfterCall := s.Stacks[s.Cur] - toCall

	// Bet/Raise: only if stack permits raising AFTER calling.
	if stackAfterCall > 0 {
		minRaise := s.minRaiseTo()
		pot := s.Pot() + toCall
		for i, frac := range s.Cfg.BetSizes {
			raiseTo := s.BetThisStreet[s.Cur] + toCall + int(float64(pot)*frac)
			if raiseTo < minRaise {
				continue
			}
			if raiseTo >= s.BetThisStreet[s.Cur]+s.Stacks[s.Cur] {
				continue
			}
			out = append(out, Action{Kind: ActionBet, SizeIdx: uint8(i)})
		}
	}
	// AllIn: legal whenever Cur has any chips (even if can't full-call).
	if s.Stacks[s.Cur] > 0 {
		out = append(out, Action{Kind: ActionAllIn})
	}
	return out
}

// IsLegal — check whether action a is in LegalActions.
func (s *State) IsLegal(a Action) bool {
	for _, b := range s.LegalActions() {
		if a == b {
			return true
		}
	}
	return false
}

// Apply — mutate state with a (must be legal).
func (s *State) Apply(a Action) *State {
	if s.Terminal {
		panic("Apply on terminal state")
	}
	if !s.IsLegal(a) {
		panic(fmt.Sprintf("illegal action %v at %s", a, s.summary()))
	}

	actor := s.Cur
	s.Hist[s.Street] = append(s.Hist[s.Street], a)
	s.NumActionsThisStreet++

	switch a.Kind {
	case ActionFold:
		s.FoldedBy = actor
		s.Terminal = true
		return s

	case ActionCheckCall:
		toCall := s.ToCall()
		if toCall > 0 {
			// Clamp pay to available stack — short-stack call → auto-allin.
			pay := toCall
			if pay > s.Stacks[actor] {
				pay = s.Stacks[actor]
			}
			s.Stacks[actor] -= pay
			s.BetThisStreet[actor] += pay
			s.Wagered[actor] += pay
			if s.Stacks[actor] == 0 {
				s.AllIn[actor] = true
			}
		}
		return s.completeStreetOrAdvanceTurn()

	case ActionBet:
		frac := s.Cfg.BetSizes[a.SizeIdx]
		toCall := s.ToCall()
		pot := s.Pot() + toCall
		raiseTo := s.BetThisStreet[actor] + toCall + int(float64(pot)*frac)
		// Cap at all-in (shouldn't happen — filtered by LegalActions, but defensive).
		maxStreet := s.BetThisStreet[actor] + s.Stacks[actor]
		if raiseTo > maxStreet {
			raiseTo = maxStreet
		}
		amount := raiseTo - s.BetThisStreet[actor]
		s.Stacks[actor] -= amount
		s.BetThisStreet[actor] = raiseTo
		s.Wagered[actor] += amount
		newRaiseSize := raiseTo - s.BetThisStreet[actor.Other()]
		if newRaiseSize > s.LastRaiseSize {
			s.LastRaiseSize = newRaiseSize
		}
		s.LastBetAmount = raiseTo
		s.Cur = actor.Other()
		return s

	case ActionAllIn:
		amount := s.Stacks[actor]
		s.BetThisStreet[actor] += amount
		s.Wagered[actor] += amount
		s.Stacks[actor] = 0
		s.AllIn[actor] = true
		oppBet := s.BetThisStreet[actor.Other()]
		if s.BetThisStreet[actor] > oppBet {
			// All-in raise: opp must respond.
			newRaiseSize := s.BetThisStreet[actor] - oppBet
			if newRaiseSize > s.LastRaiseSize {
				s.LastRaiseSize = newRaiseSize
			}
			s.LastBetAmount = s.BetThisStreet[actor]
			s.Cur = actor.Other()
			return s
		}
		// All-in for ≤ opp's bet: short-stack call or exact match. Either way no
		// reopen. completeStreetOrAdvanceTurn handles under-call refund.
		return s.completeStreetOrAdvanceTurn()
	}
	panic("unreachable")
}

// completeStreetOrAdvanceTurn — called after CheckCall or all-in-call. Decides
// whether to (a) refund + terminate (under-call), (b) advance street/showdown
// (matched bets after both acted), or (c) pass turn to opp.
func (s *State) completeStreetOrAdvanceTurn() *State {
	// Under-call refund: if any all-in player's BetThisStreet < opp's, refund
	// the excess and force terminal showdown (no action reopen per poker rules).
	for _, p := range [2]Player{P0, P1} {
		if s.AllIn[p] && s.BetThisStreet[p] < s.BetThisStreet[p.Other()] {
			s.refundExcess(p)
			s.Terminal = true
			return s
		}
	}

	// Both all-in (or one all-in with bets now matched) → no more action possible.
	if s.AllIn[P0] && s.AllIn[P1] {
		return s.advanceToShowdownOrNextStreet()
	}

	betsMatched := s.BetThisStreet[P0] == s.BetThisStreet[P1]

	// Matched + at least one player all-in → no further action possible (opp
	// can't bet alone). Go to showdown.
	if betsMatched && (s.AllIn[P0] || s.AllIn[P1]) && s.NumActionsThisStreet >= 1 {
		return s.advanceToShowdownOrNextStreet()
	}

	if betsMatched && s.NumActionsThisStreet >= 2 {
		return s.advanceToShowdownOrNextStreet()
	}
	// Preflop SB-just-called (BB still has option to raise).
	if betsMatched && s.Street == StreetPreflop && s.NumActionsThisStreet == 1 {
		s.Cur = P1
		return s
	}
	// Pass turn.
	s.Cur = s.Cur.Other()
	return s
}

// refundExcess — when actor goes all-in for less than opp's outstanding bet,
// refund opp the difference. Used for under-call termination.
//
// After refund, opp's Stacks > 0 (they got chips back), so they cannot be
// considered all-in anymore. Clear the flag.
func (s *State) refundExcess(actor Player) {
	opp := actor.Other()
	excess := s.BetThisStreet[opp] - s.BetThisStreet[actor]
	if excess <= 0 {
		return
	}
	s.BetThisStreet[opp] -= excess
	s.Wagered[opp] -= excess
	s.Stacks[opp] += excess
	if s.Stacks[opp] > 0 {
		s.AllIn[opp] = false
	}
}

// advanceToShowdownOrNextStreet — transition out of current street.
//   - River or either player all-in → terminal at showdown (caller fills board).
//   - Else: advance street, reset BetThisStreet, Cur = P1 (postflop BB first).
func (s *State) advanceToShowdownOrNextStreet() *State {
	if s.Street == StreetRiver || s.AllIn[P0] || s.AllIn[P1] {
		s.Terminal = true
		return s
	}
	s.Street++
	s.BetThisStreet = [NumPlayers]int{0, 0}
	s.LastBetAmount = 0
	s.LastRaiseSize = s.Cfg.BigBlind
	s.NumActionsThisStreet = 0
	s.Cur = P1
	return s
}

// NeedsBoard — true if next step is "deal board cards" (rather than action).
//
// Mid-game (not Terminal): standard per-street dealing
//
//	preflop done → 3 (flop)
//	flop done    → 1 (turn)
//	turn done    → 1 (river)
//
// Terminal:
//   - fold terminal: no board (FoldedBy set, hand resolved without showdown)
//   - showdown terminal (under-call, both all-in, river check-check): fill to 5
func (s *State) NeedsBoard() (n int, needs bool) {
	if s.Terminal {
		if s.FoldedBy != NoPlayer {
			return 0, false
		}
		rem := 5 - int(s.NumBoard)
		return rem, rem > 0
	}
	switch s.Street {
	case StreetFlop:
		if s.NumBoard == 0 {
			return 3, true
		}
	case StreetTurn:
		if s.NumBoard == 3 {
			return 1, true
		}
	case StreetRiver:
		if s.NumBoard == 4 {
			return 1, true
		}
	}
	return 0, false
}

// Payoff — chip utility for player p at terminal state.
// Convention: returns net chip change relative to starting stack (positive = won).
func (s *State) Payoff(p Player) int {
	if !s.Terminal {
		panic("Payoff on non-terminal")
	}
	pot := s.Pot()
	opp := p.Other()

	if s.FoldedBy != NoPlayer {
		if s.FoldedBy == p {
			return -s.Wagered[p]
		}
		return s.Wagered[opp]
	}

	// Showdown. Both players' wagered amounts are returned to the winner (or split).
	if s.NumBoard < 5 {
		panic(fmt.Sprintf("Payoff: showdown with NumBoard=%d (need 5)", s.NumBoard))
	}
	_ = pot
	pR := s.handRank(p)
	oR := s.handRank(opp)
	switch {
	case pR > oR:
		return s.Wagered[opp] // winner gains opp's wager (own wager is returned)
	case pR < oR:
		return -s.Wagered[p]
	default:
		return 0 // tie — pot split; net 0 since wagers equal at showdown
	}
}

// handRank — player p's 7-card hand rank (hole + board).
func (s *State) handRank(p Player) HandRank {
	var c [7]Card
	c[0] = s.Hole[p][0]
	c[1] = s.Hole[p][1]
	c[2] = s.Board[0]
	c[3] = s.Board[1]
	c[4] = s.Board[2]
	c[5] = s.Board[3]
	c[6] = s.Board[4]
	return Evaluate7(c)
}

// summary — short debug string.
func (s *State) summary() string {
	return fmt.Sprintf("street=%d cur=P%d pot=%d toCall=%d hist=%v terminal=%v",
		s.Street, s.Cur, s.Pot(), s.ToCall(), s.Hist, s.Terminal)
}

// Snapshot — captures all mutable fields for O(1) restore. Pattern matches
// engine/leduc: snap, mutate, restore. Hole cards are immutable per-game so
// not snapshotted; Board mutations are tracked via NumBoard (entries past
// NumBoard are "garbage" but never read).
type Snapshot struct {
	Stacks               [NumPlayers]int
	Wagered              [NumPlayers]int
	Street               Street
	BetThisStreet        [NumPlayers]int
	LastBetAmount        int
	LastRaiseSize        int
	NumActionsThisStreet uint8
	Cur                  Player
	HistLen              [4]int
	AllIn                [NumPlayers]bool
	Terminal             bool
	FoldedBy             Player
	NumBoard             uint8
}

func (s *State) Snapshot() Snapshot {
	return Snapshot{
		Stacks:               s.Stacks,
		Wagered:              s.Wagered,
		Street:               s.Street,
		BetThisStreet:        s.BetThisStreet,
		LastBetAmount:        s.LastBetAmount,
		LastRaiseSize:        s.LastRaiseSize,
		NumActionsThisStreet: s.NumActionsThisStreet,
		Cur:                  s.Cur,
		HistLen:              [4]int{len(s.Hist[0]), len(s.Hist[1]), len(s.Hist[2]), len(s.Hist[3])},
		AllIn:                s.AllIn,
		Terminal:             s.Terminal,
		FoldedBy:             s.FoldedBy,
		NumBoard:             s.NumBoard,
	}
}

func (s *State) Restore(snap Snapshot) {
	s.Stacks = snap.Stacks
	s.Wagered = snap.Wagered
	s.Street = snap.Street
	s.BetThisStreet = snap.BetThisStreet
	s.LastBetAmount = snap.LastBetAmount
	s.LastRaiseSize = snap.LastRaiseSize
	s.NumActionsThisStreet = snap.NumActionsThisStreet
	s.Cur = snap.Cur
	s.Hist[0] = s.Hist[0][:snap.HistLen[0]]
	s.Hist[1] = s.Hist[1][:snap.HistLen[1]]
	s.Hist[2] = s.Hist[2][:snap.HistLen[2]]
	s.Hist[3] = s.Hist[3][:snap.HistLen[3]]
	s.AllIn = snap.AllIn
	s.Terminal = snap.Terminal
	s.FoldedBy = snap.FoldedBy
	s.NumBoard = snap.NumBoard
}
