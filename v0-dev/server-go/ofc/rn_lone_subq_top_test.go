package ofc
import "testing"
// 2026-06-14 RnLoneSubQOnTopPenalty — T/J 起手扔空顶(零范路径)+底成对 → -3 (实战28)
func TestLoneSubQTop_Fire(t *testing.T) {
	pre := st([]string{}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"}) // 弱中max6, 底TT
	post := st([]string{"Jd"}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"})
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 3 {
		t.Fatalf("Jd扔空顶+弱中+底TT 应罚3, got %v", got)
	}
}
func TestLoneSubQTop_Skip_MidSupportsTrips(t *testing.T) {
	pre := st([]string{}, []string{"Kc", "Ks", "Kh"}, []string{"Qh", "Td", "Th"}) // 中KKK能托JJJ
	post := st([]string{"Jd"}, []string{"Kc", "Ks", "Kh"}, []string{"Qh", "Td", "Th"})
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("中能托JJJ 顶J有三条范苗头 应不罚, got %v", got)
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
	post := st([]string{"Qs"}, []string{"6h", "2h", "4c"}, []string{"Qh", "Td", "Th"}) // Q≥范苗头
	if got := RnLoneSubQOnTopPenalty(post, pre); got != 0 {
		t.Fatalf("Q上顶(有QQ范苗头) 应不罚, got %v", got)
	}
}
