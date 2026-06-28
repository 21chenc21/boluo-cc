package ofc

import "testing"

// v2 表: 顶22-55=0.2/0.4/0.6/0.8, 中对0.5/两对1, 底两对0.5/三条1.
func TestMadeHandRewardLabel(t *testing.T) {
	hc := HandValue{Type: TypeHighCard}
	pair := func(rankIdx int) HandValue { return HandValue{Type: TypePair, Value: 1000000 + int64(rankIdx)*15} }
	tp := HandValue{Type: TypeTwoPair}
	trips := HandValue{Type: TypeThreeOfAKind}
	cases := []struct {
		name             string
		top, mid, bot    HandValue
		want             float32
	}{
		{"顶22", pair(0), hc, hc, 0.2},
		{"顶55", pair(3), hc, hc, 0.8},
		{"顶66(royalty)", pair(4), hc, hc, 0.0},
		{"中一对", hc, pair(5), hc, 0.5},
		{"中两对(#66)", hc, tp, hc, 1.0},
		{"中三条(royalty)", hc, trips, hc, 0.0},
		{"底两对", hc, hc, tp, 0.5},
		{"底三条", hc, hc, trips, 1.0},
		{"底一对(不给)", hc, hc, pair(5), 0.0},
		{"顶44+中两对+底三条", pair(2), tp, trips, 0.6 + 1.0 + 1.0},
	}
	for _, c := range cases {
		if got := MadeHandRewardLabel(c.top, c.mid, c.bot); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("%s: 期望 %.2f, 得 %.2f", c.name, c.want, got)
		}
	}
}

// #66 验: 真实 eval 取对子rank 对不对 + 正解多奖
func TestMHR_Case66(t *testing.T) {
	hc := HandValue{Type: TypeHighCard}
	exp := MadeHandRewardLabel(hc, Evaluate5(mkrow("4s", "7d", "Qh", "4c", "7s")), hc) // 中4477两对
	ai := MadeHandRewardLabel(hc, Evaluate5(mkrow("4s", "7d", "Qh", "4c", "6h")), hc)  // 中对4
	t.Logf("期望中两对=%.2f, AI中一对=%.2f, 差=%.2f", exp, ai, exp-ai)
	if exp <= ai {
		t.Fatal("期望(两对)应比AI(一对)奖更多")
	}
	// 顶33K: 33是低对(pairRank1<4), 2026-06-28起不给kicker(防低对档翻55+kicker>66), 只33档=0.4.
	if got := MadeHandRewardLabel(Evaluate3(mkrow("3s", "3d", "Kh")), hc, hc); got < 0.399 || got > 0.401 {
		t.Errorf("顶33K 应0.4 (低对无kicker), 得%.2f", got)
	}
}
