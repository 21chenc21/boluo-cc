package ofc

import "testing"

// 2026-07-03 (#6): 顶trips范 foul-gate 收窄版.
//   ON: 顶AAA + 中444满(撑不住) → f114(trips锁)=0, f93(pTopTrips)=0 (假范).
//   OFF(不误伤): 顶222 + 中空/未满(能发育托) → f114=1 保留合法早局trips.
func TestTopTripsFoulGate(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	st := func(top, mid, bot []string) *GameState {
		g := &GameState{NumJokers: 2, Round: 4}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		return g
	}

	// #6: 顶AAA(As Ad 鬼) 中444满 → 撑不住 → f114/f93 应=0
	g6 := st([]string{"As", "Ad", "Xj0"}, []string{"4c", "Kc", "4d", "7d", "Xj1"}, []string{"9s", "Jd", "Jh"})
	f6 := BuildFeaturesV3(g6)
	if f6[114] != 0 {
		t.Errorf("#6 顶AAA>中444满 假范, f114 应=0, got %.2f", f6[114])
	}
	if f6[93] != 0 {
		t.Errorf("#6 顶AAA>中444满 假范, f93(pTopTrips) 应=0, got %.2f", f6[93])
	}

	// 早局合法: 顶222 中空(2槽发育) → 不 gate, f114=1
	gEarly := st([]string{"2c", "2d", "2h"}, []string{"5h", "6h"}, []string{"9s", "9d"})
	fE := BuildFeaturesV3(gEarly)
	// 2026-07-05 sp43 概率化: 中56两张+3空位要最终>222, f114=真概率 (顺/花/FH/trips>2 组合), 应显著>0 但≠1
	// sp43b 链条版(中超×底跟)是乘法, 数值更小但更诚实
	if fE[114] < 0.05 || fE[114] > 1 {
		t.Errorf("顶222+中未满(底也有空): f114 链条概率应∈[0.05,1], got %.2f", fE[114])
	}

	// 合法: 顶222 中满且中是顺(>222trips 托得住) → 不 gate
	gOk := st([]string{"2c", "2d", "2h"}, []string{"5h", "6h", "7s", "8d", "9c"}, []string{"Ts", "Jd", "Qh", "Kc", "Ah"})
	fO := BuildFeaturesV3(gOk)
	if fO[114] != 1 {
		t.Errorf("顶222+中顺(托得住), f114 应=1, got %.2f", fO[114])
	}
}
