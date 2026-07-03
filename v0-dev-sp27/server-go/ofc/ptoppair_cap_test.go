package ofc

import "testing"

// 2026-07-03 (#50): pTopPairQKA 中满对时 cap 顶Q+对 (>中对=foul假范).
func TestPTopPairQKA_MidCap(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	st := func(top, mid, bot []string) *GameState {
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
	// #50: 顶🃏2c As=AA, 中满KK → AA>KK foul → f69=0
	if f := BuildFeaturesV3(st([]string{"Xj0", "2c", "As"}, []string{"Kh", "Kd", "3h", "4s", "5h"}, []string{"9d", "Th", "Jc", "Qd", "8s"})); f[69] != 0 {
		t.Errorf("#50 顶AA>中KK满 假范, f69 应=0, got %.2f", f[69])
	}
	// 不误伤: 顶🃏2c As=AA, 中满 22对(小) → AA≤? AA>22 → 还是foul → 0. 用中满AAA? 中不可能AA.
	// 正向: 顶🃏Qc(QQ), 中满KK → QQ≤KK 合法 → f69=1
	if f := BuildFeaturesV3(st([]string{"Xj0", "Qc", "2s"}, []string{"Kh", "Kd", "3h", "4s", "5h"}, []string{"9d", "Th", "Jc", "Qd", "8s"})); f[69] != 1 {
		t.Errorf("顶QQ≤中KK 合法范, f69 应=1, got %.2f", f[69])
	}
	// 中未满不 cap: 顶🃏2c As=AA, 中3张 → f69=1 (中能发育托)
	if f := BuildFeaturesV3(st([]string{"Xj0", "2c", "As"}, []string{"Kh", "Kd", "3h"}, []string{"9d", "Th", "Jc"})); f[69] != 1 {
		t.Errorf("中未满不cap, f69 应=1, got %.2f", f[69])
	}
}
