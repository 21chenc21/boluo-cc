package ofc

import "testing"

// #51/#67 同根 (2026-06-29): pMidGTBot 改真概率 P(底≥中)(pDraw/pRowAtLeast), 不再粗桶乐观maxAchievable.
//   含"中高牌未来配对倒置"(中小底大常识): 中的高牌将来配对可能 > 底.
func TestPMidGTBot_Prob(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			r = append(r, mustParse(s))
		}
		return r
	}
	bld := func(top, mid, bot []Card, d string) *GameState {
		g := NewGameState(2)
		g.Top, g.Middle, g.Bottom = top, mid, bot
		for _, c := range append(append(g.Top, g.Middle...), g.Bottom...) {
			g.UsedCards[c.ID()] = true
		}
		g.SetDiscard(mustParse(d))
		return g
	}
	v := func(g *GameState) float32 {
		rr, sr, jr := computeDeckRemaining(g)
		dt := jr
		for _, r := range rr {
			dt += r
		}
		return pMidGTBot(g, evalRowSafe(g.Middle, 5, nil), evalRowSafe(g.Bottom, 5, nil), rr, sr, jr, dt, 5-len(g.Middle), 5-len(g.Bottom))
	}

	// 1) 已成对倒置: 中KK > 底QQ6 → foul 抬高 (旧粗桶同type只给0.2)
	inv := v(bld(mk("2c", "3c"), mk("Ks", "Kh"), mk("Qs", "Qd", "6c"), "9d"))
	ok := v(bld(mk("2c", "3c"), mk("7s", "7h"), mk("As", "Ks"), "9d"))
	if inv <= ok {
		t.Fatalf("倒置(%.3f)该 > 正常(%.3f)", inv, ok)
	}
	if inv < 0.25 {
		t.Fatalf("倒置 foul 该抬高(>0.25), 得 %.3f (旧粗桶只0.2)", inv)
	}

	// 2) #67 中小底大: 高牌放中(危险) vs 低牌放中(安全)
	aiHi := v(bld(mk("Kh"), mk("3s", "2c", "Jc"), mk("7c", "Qd", "8c"), "5s"))  // J→中
	expLo := v(bld(mk("Kh"), mk("3s", "2c", "5s"), mk("7c", "Qd", "Jc"), "8c")) // 5→中
	if aiHi <= expLo {
		t.Fatalf("#67 高牌放中(%.3f)该 > 中小底大(%.3f)", aiHi, expLo)
	}

	// 3) 真安全: 中低高牌 < 底高对 → ~0
	safe := v(bld(mk("2c", "3c"), mk("4s", "5h"), mk("Kh", "Kd"), "9d"))
	if safe > 0.1 {
		t.Fatalf("中低牌<底KK 该极低, 得 %.3f", safe)
	}
}
