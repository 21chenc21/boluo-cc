package ofc
import "testing"

// 2026-06-18 局30 R3: 顶AA + 底999, 低牌2h该进底凑葫芦, 拍2h上顶填死 → 罚4 (2026-06-20: 2.5→4, 鬼jokerRem修干净后实战93需更强罚)
func TestDeadLowKickerFanTop_Fire(t *testing.T) {
	mid := []string{"6s", "8s", "4c"}
	pre := st([]string{"Ad", "X"}, mid, []string{"9c", "9s", "9h"})
	post := st([]string{"Ad", "X", "2h"}, mid, []string{"9c", "9s", "9h"}) // 2h上顶, 底999
	if got := RnDeadLowKickerOnFanTopPenalty(post, pre); got != 4 {
		t.Fatalf("低死2h填AA顶+底999 应罚4, got %v", got)
	}
}

// 实战72 反例: 顶AA + 底[Tc]单张(非trips), 低牌该上顶让位5c留花draw → 不罚
func TestDeadLowKickerFanTop_SkipBotNotTrips(t *testing.T) {
	mid := []string{"7h", "7d", "6h", "6c"}
	pre := st([]string{"Ac", "X"}, mid, []string{"Tc"})
	post := st([]string{"Ac", "X", "4h"}, mid, []string{"Tc", "5c"}) // 4h上顶, 底高牌
	if got := RnDeadLowKickerOnFanTopPenalty(post, pre); got != 0 {
		t.Fatalf("底非trips 低牌上顶是对的(实战72) 应0, got %v", got)
	}
}

// 守护: 低对顶(<QQ)要留弱, 不罚
func TestDeadLowKickerFanTop_SkipLowPairTop(t *testing.T) {
	pre := st([]string{"6h", "6d"}, []string{"As"}, []string{"9c", "9s", "9h"})
	post := st([]string{"6h", "6d", "2h"}, []string{"As"}, []string{"9c", "9s", "9h"})
	if got := RnDeadLowKickerOnFanTopPenalty(post, pre); got != 0 {
		t.Fatalf("低对66顶填kicker(留弱) 应0, got %v", got)
	}
}

// 守护: 高kicker(K)可能安全dump, 不罚
func TestDeadLowKickerFanTop_SkipHighKicker(t *testing.T) {
	pre := st([]string{"Ad", "X"}, []string{"6s"}, []string{"9c", "9s", "9h"})
	post := st([]string{"Ad", "X", "Kh"}, []string{"6s"}, []string{"9c", "9s", "9h"})
	if got := RnDeadLowKickerOnFanTopPenalty(post, pre); got != 0 {
		t.Fatalf("高kicker K上顶(安全dump) 应0, got %v", got)
	}
}
