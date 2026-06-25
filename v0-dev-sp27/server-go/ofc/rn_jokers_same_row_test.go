package ofc

import "testing"

// TestRnJokersSameRow_Mid2X — mid 已有 1 X, action 把第 2 X 也进 mid → 罚 5
func TestRnJokersSameRow_Mid2X(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("Ac")}
	gs.Middle = []Card{mustParse("X")}
	gs.Bottom = mustCards("7d", "9d", "8d")
	dealt := mustCards("Kc", "X", "7s")
	// action: top=[Kc], mid=[X], discard 7s
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokersSameRowPenalty(action, post); got != 10 {
		t.Errorf("mid has 2 jokers: got %v, want 10", got)
	}
}

// TestRnJokersSameRow_TopXSpread — X 进 top (mid 1 X 不变) → 0
func TestRnJokersSameRow_TopXSpread(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("Ac")}
	gs.Middle = []Card{mustParse("X")}
	gs.Bottom = mustCards("7d", "9d", "8d")
	dealt := mustCards("Kc", "X", "7s")
	// action: top=[X], mid=[7s], discard Kc
	action := makeRNAction(0, []Card{dealt[1], dealt[2]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokersSameRowPenalty(action, post); got != 0 {
		t.Errorf("X to top, mid still 1 X: got %v, want 0", got)
	}
}

// TestRnJokersSameRow_TopBothX — X+Kc 都进 top (fantasy lock), mid 1 X → 0
func TestRnJokersSameRow_TopBothX(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("Ac")}
	gs.Middle = []Card{mustParse("X")}
	gs.Bottom = mustCards("7d", "9d", "8d")
	dealt := mustCards("Kc", "X", "7s")
	// action: top=[Kc, X], discard 7s
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowTop})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokersSameRowPenalty(action, post); got != 0 {
		t.Errorf("Kc+X both to top (top has 1 X but mid/bot unaffected): got %v, want 0", got)
	}
}

// TestRnJokersSameRow_Bot2X — bot 含 1 X, action 把第 2 X 进 bot → 罚 5
func TestRnJokersSameRow_Bot2X(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("Ac")}
	gs.Bottom = []Card{mustParse("X"), mustParse("7d"), mustParse("9d")}
	dealt := mustCards("Kc", "X", "8h")
	// action: top=[Kc], bot=[X], discard 8h
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokersSameRowPenalty(action, post); got != 10 {
		t.Errorf("bot has 2 jokers: got %v, want 10", got)
	}
}

// TestRnJokersSameRow_NoJoker — action 没动 X (X 留在原行) → 0
func TestRnJokersSameRow_NoJoker(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 3
	gs.Top = []Card{mustParse("Ac")}
	gs.Middle = []Card{mustParse("X"), mustParse("2c")}
	gs.Bottom = mustCards("7d", "9d", "8d")
	dealt := mustCards("Kc", "5h", "8s")
	// action: top=[Kc], mid=[5h], discard 8s
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokersSameRowPenalty(action, post); got != 0 {
		t.Errorf("no joker placed by action: got %v, want 0", got)
	}
}
