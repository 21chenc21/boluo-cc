package ofc

import "testing"

// 2026-06-13 RnTopTripsOvercommitPenalty — pre QQ+对升三条但中道托不住 → 罚 12 (ypk-70123850-2 R4)
func TestRnTopOvercommit_Fire_KKK_over222(t *testing.T) {
	pre := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	post := st([]string{"Ks", "Kh", "Kd"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	if got := RnTopTripsOvercommitPenalty(post, pre); got != 12 {
		t.Fatalf("KKK over mid222 应罚 12, got %v", got)
	}
}

func TestRnTopOvercommit_Skip_KeepKK(t *testing.T) {
	// 保 KK 不升 (post-top 还 2 张) → 不罚
	pre := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	post := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s", "Kd"}, []string{"Ts", "8h", "Js", "9c"})
	if got := RnTopTripsOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("保KK不升 应 0, got %v", got)
	}
}

func TestRnTopOvercommit_Skip_PairKicker(t *testing.T) {
	// post-top = KK + 6 kicker (3张但还是对子, 没升三条) → 不罚
	pre := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	post := st([]string{"Ks", "Kh", "6c"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	if got := RnTopTripsOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("对子加kicker(非三条) 应 0, got %v", got)
	}
}

func TestRnTopOvercommit_Skip_MidSupports(t *testing.T) {
	// mid 已是葫芦(托得住 KKK) → free upgrade, 不罚
	pre := st([]string{"Ks", "Kh"}, []string{"Ad", "Ac", "As", "Qd", "Qc"}, []string{"Ts", "8h", "Js"})
	post := st([]string{"Ks", "Kh", "Kd"}, []string{"Ad", "Ac", "As", "Qd", "Qc"}, []string{"Ts", "8h", "Js"})
	if got := RnTopTripsOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("mid葫芦托得住 应 0 (free re-fan), got %v", got)
	}
}

func TestRnTopOvercommit_Skip_LowPairPre(t *testing.T) {
	// pre-top 是 99 (< QQ, 没锁范) 升 999 → 不归这条 (可能是合理 re-fan)
	pre := st([]string{"9s", "9h"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	post := st([]string{"9s", "9h", "9d"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js", "9c"})
	if got := RnTopTripsOvercommitPenalty(post, pre); got != 0 {
		t.Fatalf("低对(非范锁)升三条 应 0, got %v", got)
	}
}
