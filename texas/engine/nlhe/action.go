package nlhe

import "fmt"

// ActionKind — coarse action category. Specific bet size encoded in Action.SizeIdx.
type ActionKind uint8

const (
	ActionFold      ActionKind = 0 // give up the pot, opponent wins
	ActionCheckCall ActionKind = 1 // check (no bet to call) OR call (match bet)
	ActionBet       ActionKind = 2 // open bet OR raise; size from BetSizes[SizeIdx]
	ActionAllIn     ActionKind = 3 // commit remaining stack
	NumActionKinds             = 4
)

// Action — one decision at one infoset.
//
//	{Kind: ActionFold | ActionCheckCall | ActionAllIn} ignore SizeIdx (set to 0).
//	{Kind: ActionBet} uses SizeIdx ∈ [0, len(BetSizes)).
type Action struct {
	Kind    ActionKind
	SizeIdx uint8 // index into BetSizes; only valid for ActionBet
}

func (a Action) String() string {
	switch a.Kind {
	case ActionFold:
		return "f"
	case ActionCheckCall:
		return "c"
	case ActionBet:
		return fmt.Sprintf("b%d", a.SizeIdx)
	case ActionAllIn:
		return "a"
	}
	return "?"
}

// DefaultBetSizes — pot fractions for bet sizing (Pluribus-lite starting point).
// 0.5 pot, 1 pot, 2 pot. Plus implicit AllIn covered by ActionAllIn.
var DefaultBetSizes = []float64{0.5, 1.0, 2.0}

// GameConfig — immutable per-game settings. Engines that solve different stack
// sizes / bet-size discretizations build with different configs.
type GameConfig struct {
	SmallBlind   int       // SB chip value
	BigBlind     int       // BB chip value
	StartStack   int       // starting stack per player (in chips)
	BetSizes     []float64 // bet sizes as fraction of pot
	PushFoldOnly bool      // if true: preflop only, ActionAllIn + ActionFold + ActionCheckCall(call). No flop/turn/river.
}

// DefaultConfig — 100 BB heads-up NLHE with 3 bet sizes.
func DefaultConfig() *GameConfig {
	return &GameConfig{
		SmallBlind:   1,
		BigBlind:     2,
		StartStack:   200, // 100 BB
		BetSizes:     append([]float64(nil), DefaultBetSizes...),
		PushFoldOnly: false,
	}
}

// PushFoldConfig — minimal subgame: SB shoves all-in or folds; BB calls or folds.
// Single decision per player, single street, deterministic showdown if both stay in.
// Useful for engine smoke testing: HUNL push/fold has known analytical Nash near
// 10-20 BB stacks (Nash push/call ranges in equilibrium tables).
func PushFoldConfig(stackBBs int) *GameConfig {
	return &GameConfig{
		SmallBlind:   1,
		BigBlind:     2,
		StartStack:   2 * stackBBs, // 2 chips per BB
		BetSizes:     nil,          // no Bet actions in push/fold mode
		PushFoldOnly: true,
	}
}
