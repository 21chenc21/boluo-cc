package nlhe6

import (
	"fmt"

	"github.com/boluo/texas/engine/nlhe"
)

// HistEntry — single action with actor seat tracked. Needed for 6-max
// because irregular fold patterns prevent inferring actor from slot-index
// parity (HU could use parity since strict alternation).
type HistEntry struct {
	Seat   Seat
	Action Action
}

// HistList — typed slice for clarity in State.Hist.
type HistList []HistEntry

// State — multi-player NLHE state machine.
//
// Active player is `Cur`. After Apply, state is either terminal or `Cur` is
// the next seat to act (skipping folded/all-in). Multi-street + side-pot
// aware. Hole cards are immutable per game (set via SetHole). Board is dealt
// externally (caller sees NeedsBoard → fills Board → advances).
//
// Round-close rule (the key generalization vs HUNL):
//   round closes when ALL non-folded non-all-in seats have HasActed==true
//   AND their BetThisStreet matches LastBetAmount.
// Preflop edge: BB starts HasActed=false (their blind doesn't count as acting).
// On a bet/raise by seat S, every OTHER non-folded non-all-in seat resets
// HasActed=false (they need to respond).
type State struct {
	Cfg *GameConfig

	// Per-seat state. Only indices [0, Cfg.NumPlayers) are meaningful.
	Hole          [MaxPlayers][2]Card
	Stacks        [MaxPlayers]int
	Wagered       [MaxPlayers]int // total chips put in this hand (for side pot)
	BetThisStreet [MaxPlayers]int
	HasActed      [MaxPlayers]bool
	Folded        [MaxPlayers]bool
	AllIn         [MaxPlayers]bool

	// Common state.
	Board    [5]Card
	NumBoard uint8
	Street   Street
	Button   Seat
	Cur      Seat
	Hist     [4]HistList // per-street action sequence (with actor seat tracked)

	// Round-state scalars.
	LastBetAmount int // max BetThisStreet observed this street; 0 if checked around
	LastRaiseSize int // for min-raise; init = BigBlind

	// Terminal.
	Terminal   bool
	FoldWinner Seat // NoSeat if not fold-win
}

// NewState — start of hand. Button = 0 by default; caller can override.
// Hole cards must be SetHole'd before Apply().
func NewState(cfg *GameConfig) *State {
	s := &State{
		Cfg:           cfg,
		Street:        StreetPreflop,
		Button:        0,
		LastRaiseSize: cfg.BigBlind,
		FoldWinner:    NoSeat,
	}
	for i := 0; i < cfg.NumPlayers; i++ {
		s.Stacks[i] = cfg.StartStack
	}
	s.postBlinds()
	s.Cur = FirstToActPreflop(s.Button, cfg.NumPlayers)
	return s
}

// NewStateWithButton — same as NewState but sets button position.
func NewStateWithButton(cfg *GameConfig, button Seat) *State {
	s := &State{
		Cfg:           cfg,
		Street:        StreetPreflop,
		Button:        button,
		LastRaiseSize: cfg.BigBlind,
		FoldWinner:    NoSeat,
	}
	for i := 0; i < cfg.NumPlayers; i++ {
		s.Stacks[i] = cfg.StartStack
	}
	s.postBlinds()
	s.Cur = FirstToActPreflop(button, cfg.NumPlayers)
	return s
}

// postBlinds — SB and BB pay forced bets. Caller responsible for ensuring
// stacks can cover blinds (NewState assumes StartStack > BB).
func (s *State) postBlinds() {
	n := s.Cfg.NumPlayers
	sbSeat := Seat((int(s.Button) + 1) % n)
	bbSeat := Seat((int(s.Button) + 2) % n)
	// HU convention: button = SB, other = BB.
	if n == 2 {
		sbSeat = s.Button
		bbSeat = Seat((int(s.Button) + 1) % n)
	}
	sbPay := s.Cfg.SmallBlind
	if sbPay > s.Stacks[sbSeat] {
		sbPay = s.Stacks[sbSeat]
	}
	s.Stacks[sbSeat] -= sbPay
	s.BetThisStreet[sbSeat] = sbPay
	s.Wagered[sbSeat] = sbPay
	if s.Stacks[sbSeat] == 0 {
		s.AllIn[sbSeat] = true
	}

	bbPay := s.Cfg.BigBlind
	if bbPay > s.Stacks[bbSeat] {
		bbPay = s.Stacks[bbSeat]
	}
	s.Stacks[bbSeat] -= bbPay
	s.BetThisStreet[bbSeat] = bbPay
	s.Wagered[bbSeat] = bbPay
	if s.Stacks[bbSeat] == 0 {
		s.AllIn[bbSeat] = true
	}
	s.LastBetAmount = bbPay
	// BB hasn't "acted" — they have option. HasActed[bbSeat] stays false.
}

// SetHole — set seat's two hole cards.
func (s *State) SetHole(seat Seat, c1, c2 Card) {
	s.Hole[seat][0] = c1
	s.Hole[seat][1] = c2
}

// Pot — total chips currently committed across all seats.
func (s *State) Pot() int {
	var sum int
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		sum += s.Wagered[i]
	}
	return sum
}

// NumActive — count of non-folded seats.
func (s *State) NumActive() int {
	var c int
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		if !s.Folded[i] {
			c++
		}
	}
	return c
}

// nextActiveSeat — next seat clockwise from start that is non-folded and
// non-all-in. Returns NoSeat if none.
func (s *State) nextActiveSeat(start Seat) Seat {
	n := s.Cfg.NumPlayers
	seat := start
	for i := 0; i < n; i++ {
		seat = NextSeat(seat, n)
		if !s.Folded[seat] && !s.AllIn[seat] {
			return seat
		}
	}
	return NoSeat
}

// LegalActions — actions available to Cur. Empty if state is terminal or Cur
// has no decision (folded/all-in/chance node).
func (s *State) LegalActions() []Action {
	if s.Terminal {
		return nil
	}
	if s.Folded[s.Cur] || s.AllIn[s.Cur] {
		return nil
	}
	out := make([]Action, 0, 2+len(s.Cfg.BetSizes)+1)
	toCall := s.LastBetAmount - s.BetThisStreet[s.Cur]
	if toCall < 0 {
		toCall = 0
	}
	stack := s.Stacks[s.Cur]
	// Fold only meaningful if there's a bet to fold to.
	if toCall > 0 {
		out = append(out, Action{Kind: ActionFold})
	}
	// CheckCall always legal (auto-allin if stack < toCall is allowed as
	// implicit short-call; engine handles).
	out = append(out, Action{Kind: ActionCheckCall})
	// Bet options: must be at least min-raise size, and stack must allow.
	// Min-raise: total BetThisStreet must increase by at least LastRaiseSize.
	minRaiseTo := s.LastBetAmount + s.LastRaiseSize
	pot := s.Pot()
	for i, frac := range s.Cfg.BetSizes {
		raiseTo := s.BetThisStreet[s.Cur] + toCall + int(float64(pot)*frac)
		if raiseTo < minRaiseTo {
			raiseTo = minRaiseTo
		}
		// Maximum bet from this seat = current bet + remaining stack.
		maxRaise := s.BetThisStreet[s.Cur] + stack
		if raiseTo >= maxRaise {
			// Bet of this size would exceed stack → AllIn instead, skip this size.
			continue
		}
		out = append(out, Action{Kind: ActionBet, SizeIdx: uint8(i)})
	}
	// AllIn legal if stack > 0.
	if stack > 0 {
		out = append(out, Action{Kind: ActionAllIn})
	}
	return out
}

// Apply — apply action by Cur. Advances Cur or terminates.
func (s *State) Apply(a Action) *State {
	if s.Terminal {
		panic("Apply on terminal state")
	}
	if s.Folded[s.Cur] || s.AllIn[s.Cur] {
		panic(fmt.Sprintf("Apply: Cur=%d is folded=%v allin=%v", s.Cur, s.Folded[s.Cur], s.AllIn[s.Cur]))
	}
	actor := s.Cur
	s.Hist[s.Street] = append(s.Hist[s.Street], HistEntry{Seat: actor, Action: a})
	s.HasActed[actor] = true

	switch a.Kind {
	case ActionFold:
		s.Folded[actor] = true
		// FoldWin: only 1 active left.
		if s.NumActive() == 1 {
			// Find the sole non-folded seat.
			for i := 0; i < s.Cfg.NumPlayers; i++ {
				if !s.Folded[i] {
					s.FoldWinner = Seat(i)
					break
				}
			}
			s.Terminal = true
			return s
		}

	case ActionCheckCall:
		toCall := s.LastBetAmount - s.BetThisStreet[actor]
		if toCall < 0 {
			toCall = 0
		}
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

	case ActionBet:
		// Compute raiseTo (total BetThisStreet target).
		toCall := s.LastBetAmount - s.BetThisStreet[actor]
		if toCall < 0 {
			toCall = 0
		}
		pot := s.Pot()
		frac := s.Cfg.BetSizes[a.SizeIdx]
		raiseTo := s.BetThisStreet[actor] + toCall + int(float64(pot)*frac)
		minRaiseTo := s.LastBetAmount + s.LastRaiseSize
		if raiseTo < minRaiseTo {
			raiseTo = minRaiseTo
		}
		maxRaise := s.BetThisStreet[actor] + s.Stacks[actor]
		if raiseTo > maxRaise {
			raiseTo = maxRaise
		}
		amount := raiseTo - s.BetThisStreet[actor]
		s.Stacks[actor] -= amount
		newRaiseSize := raiseTo - s.LastBetAmount
		s.BetThisStreet[actor] = raiseTo
		s.Wagered[actor] += amount
		s.LastBetAmount = raiseTo
		if newRaiseSize >= s.LastRaiseSize {
			s.LastRaiseSize = newRaiseSize
		}
		if s.Stacks[actor] == 0 {
			s.AllIn[actor] = true
		}
		// Reset HasActed for OTHER non-folded non-all-in seats — they need to respond.
		s.resetOthersHasActed(actor)

	case ActionAllIn:
		stack := s.Stacks[actor]
		oldBet := s.BetThisStreet[actor]
		newBet := oldBet + stack
		s.Stacks[actor] = 0
		s.BetThisStreet[actor] = newBet
		s.Wagered[actor] += stack
		s.AllIn[actor] = true
		// Does this all-in raise the current bet?
		if newBet > s.LastBetAmount {
			newRaiseSize := newBet - s.LastBetAmount
			s.LastBetAmount = newBet
			if newRaiseSize >= s.LastRaiseSize {
				s.LastRaiseSize = newRaiseSize
			}
			s.resetOthersHasActed(actor)
		}
		// else: under-raise (all-in < LastBetAmount). Bet stays; no reset.
		// HasActed[actor] stays true. The all-in seat is done.
	}

	// Check round closure.
	if s.roundClosed() {
		s.advanceStreetOrShowdown()
		return s
	}
	// Else advance Cur to next active.
	nxt := s.nextActiveSeat(actor)
	if nxt == NoSeat {
		// All remaining are all-in → showdown straight to terminal.
		s.advanceStreetOrShowdown()
		return s
	}
	s.Cur = nxt
	return s
}

// resetOthersHasActed — after a raise, every non-folded non-all-in seat other
// than `raiser` needs to respond → reset their HasActed.
func (s *State) resetOthersHasActed(raiser Seat) {
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		if Seat(i) == raiser {
			continue
		}
		if s.Folded[i] || s.AllIn[i] {
			continue
		}
		s.HasActed[i] = false
	}
}

// roundClosed — round closes when:
//   - only 1 non-folded seat → already handled in Fold (FoldWin terminal)
//   - every non-folded non-all-in seat has HasActed AND BetThisStreet matches LastBetAmount
//   - all remaining non-folded are all-in (no further action possible)
func (s *State) roundClosed() bool {
	anyToAct := false
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		if s.Folded[i] {
			continue
		}
		if s.AllIn[i] {
			continue
		}
		if !s.HasActed[i] {
			return false
		}
		if s.BetThisStreet[i] != s.LastBetAmount {
			return false
		}
		anyToAct = true
	}
	// If all remaining are folded or all-in, we're done. (anyToAct=false means
	// no one can act further → round closes trivially.)
	_ = anyToAct
	return true
}

// advanceStreetOrShowdown — round just closed. Either advance street or go to
// showdown.
func (s *State) advanceStreetOrShowdown() {
	// If river or all remaining active are all-in / only 1 active → showdown.
	allInOrOne := true
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		if s.Folded[i] {
			continue
		}
		if !s.AllIn[i] {
			allInOrOne = false
			break
		}
	}
	if allInOrOne || s.Street == StreetRiver {
		s.Terminal = true
		return
	}
	// Advance street.
	s.Street++
	for i := 0; i < s.Cfg.NumPlayers; i++ {
		s.BetThisStreet[i] = 0
		s.HasActed[i] = false
	}
	s.LastBetAmount = 0
	s.LastRaiseSize = s.Cfg.BigBlind
	// First to act postflop is BTN+1 (= SB seat), skipping folded/all-in.
	firstSeat := FirstToActPostflop(s.Button, s.Cfg.NumPlayers)
	// Find first non-folded non-all-in starting at firstSeat.
	n := s.Cfg.NumPlayers
	for i := 0; i < n; i++ {
		probe := Seat((int(firstSeat) + i) % n)
		if !s.Folded[probe] && !s.AllIn[probe] {
			s.Cur = probe
			return
		}
	}
	// No actable seat → showdown.
	s.Terminal = true
}

// NeedsBoard — number of board cards to deal before further action, and whether
// dealing is needed. Caller (MCCFR walk) handles chance dealing.
func (s *State) NeedsBoard() (n int, needs bool) {
	if s.Terminal {
		if s.FoldWinner != NoSeat {
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

// handRank — seat's 7-card hand rank (hole + 5 board).
func (s *State) handRank(seat Seat) nlhe.HandRank {
	var c [7]Card
	c[0] = s.Hole[seat][0]
	c[1] = s.Hole[seat][1]
	for i := uint8(0); i < 5; i++ {
		c[2+i] = s.Board[i]
	}
	return nlhe.Evaluate7(c)
}
