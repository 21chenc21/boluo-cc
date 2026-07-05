package ofc

import "testing"

// 2026-07-06 sp46: f89 鬼降级盲区修复 (#46 顶[🃏 2c 2s] 真foul=0 但 f89=0.796).
// foul 链威胁口径 = 含鬼行的"可降级最弱合法形态", 不是鬼打满.

func mkCards(t *testing.T, ss ...string) []Card {
	t.Helper()
	var r []Card
	for _, s := range ss {
		c, ok := ParseCard(s)
		if !ok {
			t.Fatalf("bad card %q", s)
		}
		r = append(r, c)
	}
	return r
}

func buildGS(t *testing.T, round int, top, mid, bot []string, discard string) *GameState {
	t.Helper()
	g := &GameState{NumJokers: 2, Round: round}
	g.Top, g.Middle, g.Bottom = mkCards(t, top...), mkCards(t, mid...), mkCards(t, bot...)
	g.UsedCards = map[string]bool{}
	for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
		for _, c := range row {
			g.UsedCards[c.ID()] = true
		}
	}
	if discard != "" {
		c, _ := ParseCard(discard)
		g.UsedCards[c.ID()] = true
		g.SetDiscard(c)
	}
	return g
}

// jokerFloorRow 单元: 底线形态正确
func TestJokerFloorRow(t *testing.T) {
	cases := []struct {
		name    string
		row     []string
		size    int
		wantTyp int
	}{
		{"顶 鬼+对2 → 22对", []string{"Xj0", "2c", "2s"}, 3, TypePair},
		{"顶 鬼+A5 → A高", []string{"Xj0", "Ah", "5c"}, 3, TypeHighCard},
		{"顶 双鬼+A → A高", []string{"Xj0", "Xj1", "Ah"}, 3, TypeHighCard},
		{"中 鬼+对9 → 9对(非999)", []string{"Xj0", "9s", "9h", "4d", "Kc"}, 5, TypePair},
		{"中 鬼+2345 → 高牌(不补顺)", []string{"Xj0", "2c", "3d", "4s", "5h"}, 5, TypeHighCard},
		{"中 鬼+4红桃 → 不补花", []string{"Xj0", "2h", "7h", "9h", "Kh"}, 5, TypeHighCard},
		{"中 鬼+葫芦坯 → 葫芦降不掉", []string{"Xj0", "9s", "9h", "9d", "Kc"}, 5, TypeThreeOfAKind},
	}
	for _, tc := range cases {
		row := mkCards(t, tc.row...)
		floor := jokerFloorRow(row, tc.size)
		var ev HandValue
		if tc.size == 3 {
			ev = Evaluate3(floor)
		} else {
			ev = Evaluate5(floor)
		}
		if ev.Type != tc.wantTyp {
			t.Errorf("%s: floor type=%d want %d (row=%v)", tc.name, ev.Type, tc.wantTyp, floor)
		}
	}
	// 无鬼行原样返回
	row := mkCards(t, "Kc", "Ks", "Kh")
	if got := jokerFloorRow(row, 3); &got[0] != &row[0] {
		t.Errorf("无鬼行应原样返回")
	}
}

// #46 实战: 顶[🃏 2c 2s] 鬼可降级 → f89 必须 ≈0 (旧 bug: 0.796)
func TestF89Case46JokerDegrade(t *testing.T) {
	gs := buildGS(t, 4,
		[]string{"Xj0", "2c", "2s"},
		[]string{"4s", "8c", "Th", "8s"},
		[]string{"7h", "7d", "7c", "Qc"}, "6c")
	f := BuildFeaturesV3(gs)
	if f[89] > 0.15 {
		t.Errorf("46A f89=%.3f, 鬼降级后应 <0.15 (真实 foul=0.0%%, 1000-rollout 实测)", f[89])
	}
}

// #42 回归: 顶 KKK 无鬼, 真 91.6%% foul → f89 必须保持高
func TestF89Case42StillHigh(t *testing.T) {
	gs := buildGS(t, 4,
		[]string{"Kc", "Ks", "Kh"},
		[]string{"3d", "4s", "4c", "Xj0"},
		[]string{"Ts", "Tc", "9h", "Th"}, "Qs")
	f := BuildFeaturesV3(gs)
	if f[89] < 0.8 {
		t.Errorf("42A f89=%.3f, 真foul=91.6%% 应保持 >0.8", f[89])
	}
}

// 满board必foul判定: 中含鬼降级后不再误报必foul
func TestPFoulFinalMidJokerNotCertain(t *testing.T) {
	// 中 [🃏 9s 9h 4d Kc]: 鬼打满=999 > 底 88 两对? 降级=99 < 底 → 非必 foul
	gs := buildGS(t, 5,
		[]string{"3c", "4h", "5s"},
		[]string{"Xj0", "9s", "9h", "4d", "Kc"},
		[]string{"Ts", "Td", "6c", "6h", "2s"}, "")
	f := BuildFeaturesV3(gs)
	if f[89] > 0.5 {
		t.Errorf("中鬼降级(99<底TT66)非必foul, f89=%.3f 应 <0.5", f[89])
	}
}
