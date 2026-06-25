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

// 顺支撑(中3-4-6→顺,2.5 tier above 顶AA) 应明显 > 两对支撑(中3-6-K→KK/两对,1 tier).
// 旧 /13 两者都 clamp ~1.0 (Δ=0.077); 新 /60 应 Δ≥0.25.
func TestSupportScale_StraightVsPair(t *testing.T) {
	used := []string{"Qd", "Td", "8d", "Kd", "4d"}
	pairSup := scSt([]string{"Xj0", "As", "Qc"}, []string{"3c", "6h", "Kh"}, used, used)     // K进中, 无顺面
	straightSup := scSt([]string{"Xj0", "As", "Kh"}, []string{"3c", "6h", "4s"}, used, used) // 4s进中, 3-4-6顺draw
	fp := BuildFeaturesV3(pairSup)
	fs := BuildFeaturesV3(straightSup)
	d := fs[150] - fp[150]
	if d < 0.25 {
		t.Fatalf("顺支撑应明显>两对支撑, f[150] Δ=%.3f 应≥0.25 (旧/13被clamp成0.077)", d)
	}
	// 范围检查
	for _, f := range [][]float32{fp, fs} {
		for i := 150; i < 154; i++ {
			if f[i] < -1.01 || f[i] > 1.01 {
				t.Fatalf("f[%d]=%.3f 超 [-1,1]", i, f[i])
			}
		}
	}
}

// 负向: 满中道高牌(<顶AA)托不住顶 → f[150] < 0 (必犯规信号).
func TestSupportScale_CantSupportNegative(t *testing.T) {
	// 顶AA, 中满5张=K高无对(<AA), 底花
	g := scSt([]string{"Ac", "Ah"}, []string{"Kd", "9s", "7h", "4c", "2s"}, []string{"Qd", "Td", "8d", "Kc", "4d"}, nil)
	f := BuildFeaturesV3(g)
	if f[150] >= 0 {
		t.Fatalf("中满K高(<顶AA)托不住, f[150]=%.3f 应<0", f[150])
	}
}
