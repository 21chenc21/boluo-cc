package ofc
import "testing"
func TestBotMakeTwoPair_Fire(t *testing.T) {
	pre := st([]string{"Ac","As"}, []string{}, []string{"Qh","Qc","6h"})
	post := st([]string{"Ac","As"}, []string{}, []string{"Qh","Qc","6h","Ks","Kh"}) // KKQQ两对
	if got := RnBotMakeTwoPairBonus(post, pre); got != 8 { t.Fatalf("QQ→KKQQ两对 应+8, got %v", got) }
}
func TestBotMakeTwoPair_Skip_FullHouse(t *testing.T) {
	pre := st([]string{"X","Qc"}, []string{"9s","2h"}, []string{"Th","Tc","Ts"}) // 底TTT
	post := st([]string{"X","Qc"}, []string{"9s","2h"}, []string{"Th","Tc","Ts","As","Ah"}) // TTTAA葫芦
	if got := RnBotMakeTwoPairBonus(post, pre); got != 0 { t.Fatalf("底TTT(已>两对) 应不奖, got %v", got) }
}
func TestBotMakeTwoPair_Skip_PreAlreadyTwoPair(t *testing.T) {
	pre := st([]string{"Ac","As"}, []string{}, []string{"Qh","Qc","Ks","Kh"}) // 底已KKQQ两对(4张)
	post := st([]string{"Ac","As"}, []string{}, []string{"Qh","Qc","Ks","Kh","6h"})
	if got := RnBotMakeTwoPairBonus(post, pre); got != 0 { t.Fatalf("底已两对 应不奖, got %v", got) }
}
