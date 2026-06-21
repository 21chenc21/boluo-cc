package ofc

import "testing"

// RnTopPairOvercommitPenalty — R2-R4 顶做成真 QQ/KK 对(非AA)+ deck 还有 A/鬼可升 + 中底托不住 → 罚6
func TestTopPairOvercommit_Fire_RealKK(t *testing.T) {
	pre := st([]string{"Kc"}, []string{"3d", "4d"}, []string{"5h", "6h"})
	pre.Round = 2
	post := st([]string{"Kc", "Kh"}, []string{"3d", "4d"}, []string{"5h", "6h"}) // 真 KK (2张真K)
	post.Round = 2
	if got := RnTopPairOvercommitPenalty(post, pre); got != 6 {
		t.Fatalf("真KK锁顶+中底托不住 应罚6, got %v", got)
	}
}

// 2026-06-17 局70: 鬼+K = 灵活范种子 (鬼可配未来A成AA), 非锁死KK → 不罚
func TestTopPairOvercommit_Skip_JokerSeed(t *testing.T) {
	pre := st([]string{"Kc"}, []string{"3d", "4d"}, []string{"5h", "6h"})
	pre.Round = 2
	post := st([]string{"Kc", "X"}, []string{"3d", "4d"}, []string{"5h", "6h"}) // 鬼+K, 真K仅1张
	post.Round = 2
	if got := RnTopPairOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("鬼+K灵活种子 应0(不罚), got %v", got)
	}
}

// AA 无可升, 不归这条
func TestTopPairOvercommit_Skip_AA(t *testing.T) {
	pre := st([]string{"Ac"}, []string{"3d", "4d"}, []string{"5h", "6h"})
	pre.Round = 2
	post := st([]string{"Ac", "Ah"}, []string{"3d", "4d"}, []string{"5h", "6h"})
	post.Round = 2
	if got := RnTopPairOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("AA无可升 应0, got %v", got)
	}
}
