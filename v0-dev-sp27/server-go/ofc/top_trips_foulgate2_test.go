package ofc

import "testing"

// 2026-07-05 sp43 (#6 AAA冒顶复发): pTopTrips 满顶分支 + fillTripsRank 顶列 补 topBeatsFullMid gate.
// [As 🃏 Ad] 顶 vs 满中(最大444) → 假trips范(foul线), f70/f165 不得再报满分.
func TestTopTripsFullTopFoulGate(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(top, mid, bot []string) *GameState {
		g := &GameState{NumJokers: 2, Round: 5}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		return g
	}
	// #6 AI 线: 顶[As 🃏 Ad] AAA > 满中(444 max) → f70=0, f165=-1
	bad := build([]string{"As", "Xj0", "Ad"}, []string{"4c", "Kc", "4d", "7d", "Xj1"}, []string{"9s", "Jd", "Jh", "2h"})
	fb := BuildFeaturesV3(bad)
	if fb[70] != 0 {
		t.Errorf("满顶AAA压死满中: f70(pTopTrips) 应=0, got %.3f", fb[70])
	}
	if fb[165] != -1 {
		t.Errorf("满顶AAA压死满中: f165(T3顶rank) 应=-1, got %.3f", fb[165])
	}
	// 合法对照: 顶222(真) vs 满中444 → 中托得住, f70 应保持 1, f165 应为 rank 0/12=0
	ok := build([]string{"2s", "2d", "2c"}, []string{"4c", "4d", "4h", "Kc", "7d"}, []string{"9s", "Jd", "Jh", "9h", "Js"})
	fo := BuildFeaturesV3(ok)
	if fo[70] != 1 {
		t.Errorf("顶222 vs 满中444 合法: f70 应=1, got %.3f", fo[70])
	}
	if fo[165] != 0 {
		t.Errorf("顶222 合法 trips rank 2: f165 应=0.0(rank0/12), got %.3f", fo[165])
	}
	// #46 变体: 顶222 vs 中对8(4张未满) → f70 = P(中长成>222) 应显著小 (≤0.35), 不是 1
	half := build([]string{"Xj0", "2c", "2s"}, []string{"4s", "8c", "Th", "8s"}, []string{"7h", "7d", "7c", "Qc"})
	fh := BuildFeaturesV3(half)
	if fh[70] > 0.35 {
		t.Errorf("顶222 vs 未满中对8: f70 应≤0.35(真概率), got %.3f", fh[70])
	}
	if fh[70] <= 0 {
		t.Errorf("中还有翻身可能, f70 不应为0, got %.3f", fh[70])
	}
}
