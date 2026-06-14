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

func TestBotMakeTwoPair_Skip_LowKickerPairUnderHighPair(t *testing.T) {
	// 底已KK(高对) + 塞低对33 → KK33: 33进中不倒置(33<KK), 该奖让位给"33→中成对" → 不奖.
	pre := st([]string{}, []string{"5d", "4d"}, []string{"Kh", "Kd", "8h"})
	post := st([]string{}, []string{"5d", "4d"}, []string{"Kh", "Kd", "8h", "3d", "3h"}) // KK33
	if got := RnBotMakeTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("底KK + 低对33凑KK33 不该奖(无倒置可防), got %v", got)
	}
}
