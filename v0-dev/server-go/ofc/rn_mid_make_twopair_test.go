package ofc
import "testing"
// 2026-06-14 恢复: RnMidMakeTwoPairBonus — 中凑两对 + 底>中 → +8 (ypk-459082-16 R5)
func TestMidMakeTwoPair_Fire(t *testing.T) {
	pre := st([]string{"6c", "7s"}, []string{"2d", "Jc", "3s", "2c"}, []string{"7c", "8s", "6h", "9h", "5d"}) // 中22, 底顺
	post := st([]string{"6c", "7s"}, []string{"2d", "Jc", "3s", "2c", "Jh"}, []string{"7c", "8s", "6h", "9h", "5d"}) // 中JJ22两对
	if got := RnMidMakeTwoPairBonus(post, pre); got != 8 {
		t.Fatalf("中凑JJ22两对+底顺>中 应+8, got %v", got)
	}
}
func TestMidMakeTwoPair_Skip_BotNotGreater(t *testing.T) {
	pre := st([]string{"X"}, []string{"2s", "7d"}, []string{"4c", "6h"})
	post := st([]string{"X"}, []string{"2s", "7d", "7h", "2c"}, []string{"4c", "6h"}) // 中2277, 底高张<中
	if got := RnMidMakeTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("底<中两对 应不奖, got %v", got)
	}
}
