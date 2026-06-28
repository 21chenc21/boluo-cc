package ofc

import "testing"

func trMk(ss ...string) []Card {
	var r []Card
	for _, s := range ss {
		r = append(r, mustParse(s))
	}
	return r
}

// #90 (2026-06-28 用户): 中三条 rank 用 0-2 headroom (royalty2→顺4), 加大到学得动.
func TestMHR_MidTripsRank0to2(t *testing.T) {
	none := HandValue{Type: -1}
	m3 := MadeHandRewardLabel(none, Evaluate5(trMk("3s", "3d", "3c", "7h", "2c")), none)
	m5 := MadeHandRewardLabel(none, Evaluate5(trMk("5s", "5d", "5c", "7h", "2c")), none)
	mA := MadeHandRewardLabel(none, Evaluate5(trMk("As", "Ad", "Ac", "7h", "2c")), none)
	if !(m5 > m3) {
		t.Fatalf("中555(%.3f) > 中333(%.3f)", m5, m3)
	}
	if d := m5 - m3; d < 0.25 { // 0-2 档后 555-333 ≈ 0.30 (旧 0.12 太小)
		t.Fatalf("中 555-333 差应 ≥0.25 (用 0-2 headroom), 得 %.3f", d)
	}
	if mA >= 2.0 { // 中AAA: royalty2 + MHR<2 → 总<4(顺), 不翻档
		t.Fatalf("中AAA MHR(%.3f) 必<2 (royalty2+MHR<4顺)", mA)
	}
	// 底三条 base1.0, headroom 只 1 → 满 <2
	if bA := MadeHandRewardLabel(none, none, Evaluate5(trMk("As", "Ad", "Ac", "7h", "2c"))); bA >= 2.0 {
		t.Fatalf("底AAA MHR(%.3f) 必<2 (底顺royalty)", bA)
	}
}

// #124 (2026-06-28 用户): pPairToTrips foul cap — 升trips会倒置(>下行max)就不算潜力.
func TestPairToTrips_FoulCap(t *testing.T) {
	g := NewGameState(2)
	g.Top = trMk("X", "Ah")
	g.Middle = trMk("2s", "X", "2h") // 22+鬼 → 222三条
	g.Bottom = trMk("8s", "Jh", "9h", "9s", "Js") // JJ99两对(满)
	for _, c := range append(append(g.Top, g.Middle...), g.Bottom...) {
		g.UsedCards[c.ID()] = true
	}
	f := BuildFeaturesV3(g)
	if f[108] > 0.01 { // 中 pair→trips: 222 > 底JJ99两对 必倒置 → cap 到 0
		t.Fatalf("中22→222 该被底JJ99 cap到0(foul), 得 %.3f", f[108])
	}
	// 反例: 中22→222 但底是三条KKK(>222) → 222≤KKK 合法, 不该cap
	g2 := NewGameState(2)
	g2.Top = trMk("X", "Ah")
	g2.Middle = trMk("2s", "2h", "5c")             // 22 pair, 1 slot 内可升222
	g2.Bottom = trMk("Ks", "Kh", "Kd", "7c", "8c") // KKK三条 > 222
	for _, c := range append(append(g2.Top, g2.Middle...), g2.Bottom...) {
		g2.UsedCards[c.ID()] = true
	}
	if f2 := BuildFeaturesV3(g2); f2[108] <= 0.0 {
		t.Fatalf("中22→222 ≤ 底KKK三条 合法, 不该cap, 得 %.3f", f2[108])
	}
}
