package ofc

import "testing"

// 2026-06-21 W组 scaling fix: /13→/60 让"支撑强度"(顺支撑 vs 两对支撑)有 magnitude 区分.
// 治"低估顺/支撑发育"主线 (用户失败case注释 64/76/98).

func scMk(ss ...string) []Card {
	var r []Card
	for _, s := range ss {
		c, _ := ParseCard(s)
		r = append(r, c)
	}
	return r
}
func scSt(top, mid, bot, used []string) *GameState {
	g := NewGameState(2)
	g.Round = 4
	for _, c := range scMk(top...) {
		g.PlaceCard(c, RowTop)
	}
	for _, c := range scMk(mid...) {
		g.PlaceCard(c, RowMiddle)
	}
	for _, c := range scMk(bot...) {
		g.PlaceCard(c, RowBottom)
	}
	for _, u := range used {
		g.UsedCards[u] = true
	}
	return g
}

// 2026-07-03 sp37: 旧 TestSupportScale_StraightVsPair 删除 —— 它断言"gutshot顺draw=强支撑>>对(Δ≥0.25)",
//   而 sp37 把 midMaxCapped 改概率加权后, gutshot顺(要摸2张特定牌)刻意不再当强支撑(顺≈对, 都弱).
//   新概率行为由 sp37_fixes_test.go 覆盖: ProbableMaxTier_NoPhantomQuad / F153_NoPhantomQuadHeadroom /
//   F153_MadeTwoPairLowHeadroom. 范围/负向支撑由 TestSupportScale_CantSupportNegative 保留.
// ⚠️ 64/76/98(中路发育顺/A托顶AA)原靠 W组乐观支撑 → 现改概率后弱化, sp37 bench 需盯这三个是否回退.

// 负向: 满中道高牌(<顶AA)托不住顶 → f[150] < 0 (必犯规信号).
func TestSupportScale_CantSupportNegative(t *testing.T) {
	// 顶AA, 中满5张=K高无对(<AA), 底花
	g := scSt([]string{"Ac", "Ah"}, []string{"Kd", "9s", "7h", "4c", "2s"}, []string{"Qd", "Td", "8d", "Kc", "4d"}, nil)
	f := BuildFeaturesV3(g)
	if f[150] >= 0 {
		t.Fatalf("中满K高(<顶AA)托不住, f[150]=%.3f 应<0", f[150])
	}
}
