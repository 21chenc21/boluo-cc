package nlhe6

import "fmt"

// ActionKind — same enum as engine/nlhe (Fold, CheckCall, Bet, AllIn).
type ActionKind uint8

const (
	ActionFold ActionKind = iota
	ActionCheckCall
	ActionBet
	ActionAllIn
)

// Action — single decision. SizeIdx only meaningful for ActionBet (index into
// GameConfig.BetSizes).
type Action struct {
	Kind    ActionKind
	SizeIdx uint8 // for ActionBet
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

// DefaultBetSizes — same 3-bet abstraction as HUNL.
var DefaultBetSizes = []float64{0.5, 1.0, 2.0}

// GameConfig — table settings. NumPlayers in [2, MaxPlayers].
type GameConfig struct {
	NumPlayers int       // 2-6
	SmallBlind int
	BigBlind   int
	StartStack int       // per-seat starting chips
	BetSizes   []float64 // fractions of pot
}

// DefaultConfig6 — 6-max NLHE, 100 BB, default bet sizes.
func DefaultConfig6() *GameConfig {
	return &GameConfig{
		NumPlayers: 6,
		SmallBlind: 1,
		BigBlind:   2,
		StartStack: 200, // 100 BB
		BetSizes:   append([]float64(nil), DefaultBetSizes...),
	}
}

// DefaultConfigN — n-handed NLHE, 100 BB, default bet sizes.
func DefaultConfigN(n int) *GameConfig {
	return &GameConfig{
		NumPlayers: n,
		SmallBlind: 1,
		BigBlind:   2,
		StartStack: 200,
		BetSizes:   append([]float64(nil), DefaultBetSizes...),
	}
}
