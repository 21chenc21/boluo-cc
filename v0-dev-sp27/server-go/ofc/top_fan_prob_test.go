package ofc

import "testing"

// 2026-07-04 sp40: seedBonus 概率加权 — topFanProb 必须把 premium 种子和烂种子分开.
func TestTopFanProb(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(top, mid, bot []string) (*GameState, [13]int, [4]int, int) {
		g := &GameState{NumJokers: 2, Round: 4}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		rankRem, suitRem, jokerRem := computeDeckRemaining(g)
		return g, rankRem, suitRem, jokerRem
	}
	// premium: #110 exp 孤鬼守顶(2空位, QKA全活, 鬼配任一即QQ+范) → P 应显著 (≥0.3)
	gP, rrP, srP, jP := build([]string{"Xj0"}, []string{"6h", "6d", "4c", "6c", "7h"}, []string{"9d", "8h", "8c", "8d", "8s"})
	pPrem := topFanProb(gP, rrP, srP, jP)
	// junk: 无鬼顶[2c]+2空位, QQ/KK/AA 要摸2张, trips 2 也要摸2张 → P 低
	gJ, rrJ, srJ, jJ := build([]string{"2c"}, []string{"6h", "6d", "4c", "6c", "7h"}, []string{"9d", "8h", "8c", "8d", "8s"})
	pJunk := topFanProb(gJ, rrJ, srJ, jJ)
	if pPrem < 0.3 {
		t.Errorf("孤鬼守顶 premium 种子 P 应≥0.3, got %.3f", pPrem)
	}
	if pJunk >= pPrem {
		t.Errorf("无鬼低牌顶 P 应 < 孤鬼: junk=%.3f prem=%.3f", pJunk, pPrem)
	}
	// label bonus 效果: premium ×2×8 应 ≈ 旧+8 量级 (5~16 区间), junk 显著低
	bPrem, bJunk := pPrem*2*8, pJunk*2*8
	if bPrem < 5 {
		t.Errorf("premium 种子 bonus 应保持旧+8量级(≥5), got %.2f", bPrem)
	}
	if bJunk >= bPrem*0.6 {
		t.Errorf("junk 种子 bonus 应显著低于 premium: junk=%.2f prem=%.2f", bJunk, bPrem)
	}
}
