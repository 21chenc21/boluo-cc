package ofc

import "testing"

// RnMidTwoPairBotDrawBonus: 中两对+底花draw还活 → +2; 填死/中没两对 → 0.
func TestMidTwoPairBotDraw(t *testing.T) {
	mk := func(ss ...string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	st := func(mid, bot []string) *GameState {
		g := NewGameState(2)
		g.Round = 3
		for _, c := range mk("Ac", "X") { g.PlaceCard(c, RowTop) }
		for _, c := range mk(mid...) { g.PlaceCard(c, RowMiddle) }
		for _, c := range mk(bot...) { g.PlaceCard(c, RowBottom) }
		return g
	}
	if v := RnMidTwoPairBotDrawBonus(st([]string{"7h", "7d", "6h", "6c"}, []string{"Tc", "5c"})); v != 2 {
		t.Fatalf("中7766两对+底Tc5c(2梅3空可成花) 应+2, 得%v", v)
	}
	if v := RnMidTwoPairBotDrawBonus(st([]string{"7h", "7d", "6h", "6c"}, []string{"Tc", "5c", "4h"})); v != 0 {
		t.Fatalf("底Tc5c4h(2梅2空最多4花,填死) 应0, 得%v", v)
	}
	if v := RnMidTwoPairBotDrawBonus(st([]string{"7h", "7d", "5c"}, []string{"Tc", "5c"})); v != 0 {
		t.Fatalf("中只单对77 应0, 得%v", v)
	}
}

// R1BottomDrawBonus: 底4连顺(4-5-6-7)→+5; 3连张→+2; 无→0.
func TestR1BotDraw4Straight(t *testing.T) {
	mk := func(ss ...string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	// 全放底: dealt顺序对应placement
	d4 := mk("5c", "7s", "4s", "6h", "Kd")                                       // 4567在底 + Kd
	p4 := Placement{RowBottom, RowBottom, RowBottom, RowBottom, RowTop}          // 5c7s4s6h→底=4567, Kd→顶
	if v := R1BottomDrawBonus(p4, d4); v != 5 {
		t.Fatalf("底4连顺4567 应+5, 得%v", v)
	}
	d3 := mk("5c", "7s", "4s", "Kd", "Qd")                                       // 457在底(3连张窗内)
	p3 := Placement{RowBottom, RowBottom, RowBottom, RowTop, RowTop}             // 5c7s4s→底=4-5-7
	if v := R1BottomDrawBonus(p3, d3); v != 2 {
		t.Fatalf("底3连张457 应+2, 得%v", v)
	}
}
