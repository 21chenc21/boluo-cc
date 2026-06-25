package ofc
import "testing"
// 2026-06-14 RnLoneSubQOnTopPenalty — 起手扔空顶 1 张 [中max, Q) 牌(零范路径)+底成对 → -2 (实战28)
func TestLoneSubQTop_Fire(t *testing.T) {
	pre := st([]string{}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"}) // 中max6, 底TT
	post := st([]string{"Jd"}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"}) // J ≥6 <Q
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 2 {
		t.Fatalf("Jd(≥中max6)扔空顶+弱中+底TT 应罚2, got %v", got)
	}
}
func TestLoneSubQTop_Skip_BelowMidMax(t *testing.T) {
	pre := st([]string{}, []string{"9c", "8s", "2h"}, []string{"Qh", "Td", "Th"}) // 中max9
	post := st([]string{"7d"}, []string{"9c", "8s", "2h"}, []string{"Qh", "Td", "Th"}) // 7 < 中max9
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("7d<中max9 中能匹配不foul 应不罚, got %v", got)
	}
}
func TestLoneSubQTop_Skip_MidSupportsTrips(t *testing.T) {
	pre := st([]string{}, []string{"Tc", "Ts", "Th"}, []string{"Qh", "9d", "9s"}) // 中TTT能托TTT
	post := st([]string{"Td"}, []string{"Tc", "Ts", "Th"}, []string{"Qh", "9d", "9s"})
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("中TTT能托顶T三条范 应不罚, got %v", got)
	}
}
func TestLoneSubQTop_Skip_BotNoPair(t *testing.T) {
	pre := st([]string{}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "8s"}) // 底无对
	post := st([]string{"Jd"}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "8s"})
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("底无对 应不罚, got %v", got)
	}
}
func TestLoneSubQTop_Skip_QueenOK(t *testing.T) {
	pre := st([]string{}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"})
	post := st([]string{"Qs"}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"}) // Q有QQ范苗头
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("Q上顶(QQ范苗头) 应不罚, got %v", got)
	}
}
