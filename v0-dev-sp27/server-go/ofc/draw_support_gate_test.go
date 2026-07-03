package ofc

import "testing"

// 2026-07-03 (#104/#124): draw-support-gate — 中 tier(两对+) draw × P(底≥该tier).
//   底托不住 → 中该tier会foul → 压到~0; 底能托 → 放行.
func TestDrawSupportGate(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	midF := func(mid, bot []string) float32 { // 返回 f76 中花(gated)
		g := &GameState{NumJokers: 2, Round: 2}
		g.Top, g.Middle, g.Bottom = mk(), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		return BuildFeaturesV3(g)[76] // 中花 (fillProbabilities f[7]=abs f76)
	}
	// 中 3♠花draw: 底弱(托不住) → 中花压到~0; 底强(独立能≥花) → 放行
	weak := midF([]string{"3s", "5s", "7s"}, []string{"2c", "2d", "9h"})       // 底2-2-9 高牌/小对, 到不了花
	strong := midF([]string{"3s", "5s", "7s"}, []string{"Ah", "Kh", "Qh", "Jh"}) // 底4红桃, 独立能成花
	if weak > 0.05 {
		t.Errorf("底托不住中花, gated f76 应~0(≤0.05), got %.3f", weak)
	}
	if strong <= weak {
		t.Errorf("底能托住(4红桃) 中花应放行 > 底弱, got 强=%.3f 弱=%.3f", strong, weak)
	}
	// 单对不 gate: 中已成对, f72(中对)=1.0 不受底影响
	g := &GameState{NumJokers: 2, Round: 2}
	g.Top, g.Middle, g.Bottom = mk(), mk("Kh", "Kd"), mk("2c", "3d", "4h")
	g.UsedCards = map[string]bool{}
	for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
		for _, c := range row {
			g.UsedCards[c.ID()] = true
		}
	}
	if f := BuildFeaturesV3(g); f[72] != 1 {
		t.Errorf("中已成对 单对不gate, f72 应=1.0, got %.3f", f[72])
	}
}
