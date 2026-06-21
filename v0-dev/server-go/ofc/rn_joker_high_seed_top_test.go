package ofc

import "testing"

// 2026-06-17 RnJokerHighSeedOnTopBonus — 局70: 本轮鬼+K/Q→顶 = KK/QQ范种子 → +4
func TestJokerHighSeedTop_Fire_K(t *testing.T) {
	post := st([]string{"Kh", "X"}, []string{"6s", "5d", "4h", "4s"}, []string{"9c"})
	a := &RoundNAction{Kept: []Card{mustCard("X"), mustCard("4s")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerHighSeedOnTopBonus(a, post); got != 4 {
		t.Fatalf("鬼+K→顶 KK种子 应+4, got %v", got)
	}
}
func TestJokerHighSeedTop_Fire_Q(t *testing.T) {
	post := st([]string{"Qh", "X"}, []string{"6s", "5d"}, []string{"9c"})
	a := &RoundNAction{Kept: []Card{mustCard("X"), mustCard("6s")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerHighSeedOnTopBonus(a, post); got != 4 {
		t.Fatalf("鬼+Q→顶 QQ种子 应+4, got %v", got)
	}
}
func TestJokerHighSeedTop_Skip_NoJokerThisRound(t *testing.T) {
	post := st([]string{"X", "Kh"}, []string{"6s", "5d"}, []string{"9c"}) // 鬼上轮已在顶, 本轮只加K
	a := &RoundNAction{Kept: []Card{mustCard("Kh"), mustCard("6s")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerHighSeedOnTopBonus(a, post); got != 0 {
		t.Fatalf("本轮没鬼上顶 应0, got %v", got)
	}
}
func TestJokerHighSeedTop_Skip_LowCard(t *testing.T) {
	post := st([]string{"7h", "X"}, []string{"6s", "5d"}, []string{"9c"}) // 鬼+7 (<Q) 非种子
	a := &RoundNAction{Kept: []Card{mustCard("X"), mustCard("6s")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerHighSeedOnTopBonus(a, post); got != 0 {
		t.Fatalf("鬼+低牌(非≥Q) 应0, got %v", got)
	}
}
func TestJokerHighSeedTop_Skip_A(t *testing.T) {
	post := st([]string{"Ah", "X"}, []string{"6s", "5d"}, []string{"9c"}) // 鬼+A → 走JokerAOnTop
	a := &RoundNAction{Kept: []Card{mustCard("X"), mustCard("6s")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerHighSeedOnTopBonus(a, post); got != 0 {
		t.Fatalf("鬼+A走JokerAOnTop 这条应0, got %v", got)
	}
}
