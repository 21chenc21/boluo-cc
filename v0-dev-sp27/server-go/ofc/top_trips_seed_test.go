package ofc

import "testing"

func st3(top, mid, bot []string) *GameState {
	g := NewGameState(2)
	for _, s := range top { g.Top = append(g.Top, mustParse(s)) }
	for _, s := range mid { g.Middle = append(g.Middle, mustParse(s)) }
	for _, s := range bot { g.Bottom = append(g.Bottom, mustParse(s)) }
	for _, c := range append(append(g.Top, g.Middle...), g.Bottom...) {
		g.UsedCards[c.ID()] = true
	}
	return g
}

// #110: 孤鬼顶 + 中666 → 能种 ≤666 合法trips → +1; 顶[🃏 7h]只能种777>666 → 非合法 → 不是+1
func TestTopTripsSeed_LoneJoker(t *testing.T) {
	rr := func(gs *GameState) float32 {
		rankRem, suitRem, jokerRem := computeDeckRemaining(gs)
		return topTripsSeedScore(gs, rankRem, suitRem, jokerRem)
	}
	exp := st3([]string{"X"}, []string{"6h", "6d", "4c", "6c"}, []string{"9d", "8h", "8c", "8d", "8s"})
	ai := st3([]string{"X", "7h"}, []string{"6h", "6d", "4c", "6c"}, []string{"9d", "8h", "8c", "8d", "8s"})
	ge, ga := rr(exp), rr(ai)
	t.Logf("期望 孤鬼顶=%.0f (应+1合法trips种子), AI 顶[🃏7h]=%.0f", ge, ga)
	if ge != 1 {
		t.Fatalf("孤鬼顶+中666 应 +1 (能种222..666合法范), 得 %.0f", ge)
	}
	if ge == ga {
		t.Fatalf("期望(%.0f)和AI(%.0f) 不该相同 — 特征要能区分留鬼 vs 7h占顶", ge, ga)
	}
}
