package ofc

import "testing"

// 2026-07-04 sp39 FE 组 (dim168): 范EV专用维 = f0Bonus×(1-pFoul)/140.
// 治 #110/#120: 范EV数值差被 f97 的 /300 归一淹没(+9分真实差→0.029), 干不过 f9 顶成对 one-hot.
func TestFanEVDim(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(top, mid, bot []string, round int) []float32 {
		g := &GameState{NumJokers: 2, Round: round}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		return BuildFeaturesV3(g)
	}

	// #110 结构: 鬼独守顶(2空位, 可摸QKA配对进范) vs 鬼配7h锁77(1空位).
	// exp 范EV 应显著 > AI (f90-92 概率高且 slots 多).
	exp := build([]string{"Xj0"}, []string{"6h", "6d", "4c", "6c", "7h"}, []string{"9d", "8h", "8c", "8d", "8s"}, 5)
	ai := build([]string{"Xj0", "7h"}, []string{"6h", "6d", "4c", "6c"}, []string{"9d", "8h", "8c", "8d", "8s"}, 5)
	if exp[168] <= ai[168] {
		t.Errorf("#110 鬼独守顶 范EV 应 > 鬼配77锁死: exp=%.3f ai=%.3f", exp[168], ai[168])
	}
	// 一致性: dim168 应 = 手工重算 f0Bonus×(1-pFoul)/140 (与 f89-93 同源, 防漂移)
	for name, f := range map[string][]float32{"exp": exp, "ai": ai} {
		want := clampF((f[90]*V3FanBonusQQ+f[91]*V3FanBonusKK+f[92]*V3FanBonusAA+f[93]*V3FanBonusTrips)*(1-f[89])/140.0, 0, 1)
		if f[168] != want {
			t.Errorf("%s dim168 与 f89-93 重算不一致: got %.4f want %.4f", name, f[168], want)
		}
	}

	// 顶已锁死低对(无鬼, 3张满, 无范可能) → 范EV = 0
	dead := build([]string{"7h", "7d", "2c"}, []string{"6h", "6d", "4c", "6c", "5s"}, []string{"9d", "8h", "8c", "8d", "8s"}, 5)
	if dead[168] != 0 {
		t.Errorf("顶77x锁死无范, dim168 应=0, got %.3f", dead[168])
	}

	// 顶QQ已成(真牌) → f90=1 → 范EV ≥ QQ bonus/140 (只要不foul)
	qq := build([]string{"Qs", "Qh"}, []string{"Kh", "Kd", "3h", "4s", "5h"}, []string{"Ah", "Ad", "As", "8s", "9s"}, 5)
	if qq[168] <= 0 {
		t.Errorf("顶QQ真对已成(底AAA托住), 范EV 应 > 0, got %.3f", qq[168])
	}

	// BuildFeatures(gs,168) 截断分支: 必须 = 169-d 的前 168 (漏加会静默掉 V2 兜底 → bench 135失败, 2026-07-04 实翻过车)
	g := &GameState{NumJokers: 2, Round: 5}
	g.Top, g.Middle, g.Bottom = mk("Xj0"), mk("6h", "6d", "4c", "6c", "7h"), mk("9d", "8h", "8c", "8d", "8s")
	g.UsedCards = map[string]bool{}
	for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
		for _, c := range row {
			g.UsedCards[c.ID()] = true
		}
	}
	f168 := BuildFeatures(g, 168)
	full := BuildFeaturesV3(g)
	if len(f168) != 168 {
		t.Fatalf("BuildFeatures(gs,168) len 应=168, got %d", len(f168))
	}
	for i := range f168 {
		if f168[i] != full[i] {
			t.Fatalf("BuildFeatures(gs,168)[%d]=%.4f ≠ BuildFeaturesV3 前168维 %.4f (截断分支坏了/掉V2兜底)", i, f168[i], full[i])
		}
	}
}
