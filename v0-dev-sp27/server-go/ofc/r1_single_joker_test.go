package ofc

import "testing"

// 用户 2026-06-03: 单鬼无 A 时 joker 放顶 +5 (ypk-178127178-8 R1 [8h X 7c Qc 3c]).

func TestR1SingleJokerNoA_OnTop_Fires(t *testing.T) {
	// dealt [8h X 7c Qc 3c], joker(idx1) 放顶 → +5
	cards := parseHand("8h", "X", "7c", "Qc", "3c")
	p := Placement{RowMiddle, RowTop, RowBottom, RowBottom, RowBottom}
	if got := R1SingleJokerNoAOnTopBonus(p, cards); got != 5 {
		t.Fatalf("single joker no-A on top: got %v, want 5", got)
	}
}

func TestR1SingleJokerNoA_NotOnTop_Zero(t *testing.T) {
	// joker(idx1) 埋中道 → 0 (没放顶)
	cards := parseHand("8h", "X", "7c", "Qc", "3c")
	p := Placement{RowMiddle, RowMiddle, RowBottom, RowBottom, RowBottom}
	if got := R1SingleJokerNoAOnTopBonus(p, cards); got != 0 {
		t.Fatalf("joker not on top: got %v, want 0", got)
	}
}

func TestR1SingleJokerNoA_HasA_Zero(t *testing.T) {
	// dealt 含 A → 不归这条 (走 R1JokerWithAOnTopBonus)
	cards := parseHand("Ah", "X", "7c", "Qc", "3c")
	p := Placement{RowMiddle, RowTop, RowBottom, RowBottom, RowBottom}
	if got := R1SingleJokerNoAOnTopBonus(p, cards); got != 0 {
		t.Fatalf("dealt has A: got %v, want 0", got)
	}
}

func TestR1SingleJokerNoA_TwoJokers_Zero(t *testing.T) {
	// 2 张 joker → 不归这条 (jokers != 1)
	cards := parseHand("X", "X", "7c", "Qc", "3c")
	p := Placement{RowTop, RowMiddle, RowBottom, RowBottom, RowBottom}
	if got := R1SingleJokerNoAOnTopBonus(p, cards); got != 0 {
		t.Fatalf("two jokers: got %v, want 0", got)
	}
}
