package ofc

import "testing"

// C组 进范可行性特征 (dim163) — #116: 底2s6h锁66/22杀范=0, 中3h底6h发育=1.
func TestFantasyReachableFeature(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			r = append(r, mustParse(s))
		}
		return r
	}
	build := func(rnd int, top, mid, bot []Card) *GameState {
		g := NewGameState(2)
		g.Round = rnd
		g.Top = top
		g.Middle = mid
		g.Bottom = bot
		for _, c := range append(append(append([]Card{}, top...), mid...), bot...) {
			g.UsedCards[c.ID()] = true
		}
		return g
	}
	kill := build(3, mk("Qc"), mk("Js", "4c", "Jd"), mk("2d", "6d", "Kd", "2s", "6h")) // 底2s6h锁死
	dev := build(3, mk("Qc"), mk("Js", "4c", "Jd", "3h"), mk("2d", "6d", "Kd", "6h"))   // 中3h底6h发育

	fK := BuildFeaturesV3(kill)
	fD := BuildFeaturesV3(dev)
	if len(fK) != FeatureDimV3 || FeatureDimV3 != 165 {
		t.Fatalf("FeatureDimV3 应=165, len=%d dim=%d", len(fK), FeatureDimV3)
	}
	if fK[163] != 0 {
		t.Errorf("底2s6h锁死 fantasyReachable 应=0, 得 %.1f", fK[163])
	}
	if fD[163] != 1 {
		t.Errorf("中3h底6h发育 fantasyReachable 应=1, 得 %.1f", fD[163])
	}
}

// 向后兼容: 163-d (旧sp29) / 161-d / 150-d / 147-d 截断仍工作.
func TestFantasyReachableBackcompat(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			r = append(r, mustParse(s))
		}
		return r
	}
	g := NewGameState(2)
	g.Round = 3
	g.Top = mk("Qc")
	g.Middle = mk("Js", "4c", "Jd")
	g.Bottom = mk("2d", "6d", "Kd")
	for _, c := range append(append(append([]Card{}, g.Top...), g.Middle...), g.Bottom...) {
		g.UsedCards[c.ID()] = true
	}
	for _, d := range []int{164, 163, 161, 150, 147} {
		if got := len(BuildFeatures(g, d)); got != d {
			t.Errorf("inDim=%d 应返回 %d 维, 得 %d", d, d, got)
		}
	}
}
