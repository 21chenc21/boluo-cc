package ofc
import "testing"
// 2026-06-13 RnMidMakeTwoPairBonus — 中凑两对 + 底>中 → +8 (实战22/J版 KK/JJ→中)
func TestMidMakeTwoPair_Fire(t *testing.T) {
	// 中 77 + 发 KK → KK77 两对, 底 KKK三条 > 中 → +8
	pre := st([]string{"X"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "X"})
	post := st([]string{"X"}, []string{"2d", "7s", "7d", "Ks", "Kh"}, []string{"Kc", "Kd", "X"})
	if got := RnMidMakeTwoPairBonus(post, pre); got != 8 {
		t.Fatalf("中凑KK77两对+底KKK>中 应+8, got %v", got)
	}
}
func TestMidMakeTwoPair_Skip_BotNotGreater(t *testing.T) {
	// 底 < 中两对 (底高张) → 不奖 (防 case9/倒置)
	pre := st([]string{"X"}, []string{"2s", "7d"}, []string{"4c", "6h"})
	post := st([]string{"X"}, []string{"2s", "7d", "7h", "2c"}, []string{"4c", "6h"}) // 中2277两对, 底高张
	if got := RnMidMakeTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("底<中两对 应不奖, got %v", got)
	}
}
func TestMidMakeTwoPair_Skip_MidAlreadyTwoPair(t *testing.T) {
	pre := st([]string{"X"}, []string{"7s", "7d", "8c", "8d"}, []string{"Kc", "Kd", "X"}) // 中已7788两对
	post := st([]string{"X"}, []string{"7s", "7d", "8c", "8d", "2c"}, []string{"Kc", "Kd", "X"})
	if got := RnMidMakeTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("中已两对 应不奖, got %v", got)
	}
}
