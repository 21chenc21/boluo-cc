package ofc

import "testing"

func mkrow(ss ...string) []Card {
	var r []Card
	for _, s := range ss {
		r = append(r, mustParse(s))
	}
	return r
}

// rowStraightTightness = 1/tier. 连张1.0, 卡顺逐档压低.
func TestRowStraightTightness(t *testing.T) {
	cases := []struct {
		name string
		row  []string
		want float32
	}{
		{"456连张", []string{"4s", "5d", "6h"}, 1.0},          // tier1
		{"468卡顺2缺口", []string{"4s", "6d", "8h"}, 1.0 / 3}, // tier3
		{"467卡顺1缺口", []string{"4s", "6d", "7h"}, 0.5},     // tier2
		{"46两张", []string{"4s", "6d"}, 0.5},                 // tier2
		{"48两张", []string{"4s", "8d"}, 0.25},                // tier4
		{"4578", []string{"4s", "5d", "7h", "8c"}, 0.5},      // tier2
		{"无顺(4 T)", []string{"4s", "Td"}, 0.0},             // span>4
		{"鬼填缺口 46+X→连", []string{"4s", "6d", "X"}, 1.0}, // 鬼填→tier1
	}
	for _, c := range cases {
		got := rowStraightTightness(mkrow(c.row...))
		if got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("%s: 期望 %.3f, 得 %.3f", c.name, c.want, got)
		}
	}
}

// 边缘单边顺 (用户 2026-06-25): A234 / JQKA 只一头能接 = 卡顺 2档. 中间4连张开口 = 1档.
func TestRowStraightTightness_OneSidedEdge(t *testing.T) {
	cases := []struct {
		name string
		row  []string
		want float32
	}{
		{"A234单边", []string{"Ah", "2s", "3d", "4c"}, 0.5},   // 只接5 → tier2
		{"JQKA单边", []string{"Jh", "Qs", "Kd", "Ac"}, 0.5},   // 只接T → tier2
		{"2345开口", []string{"2h", "3s", "4d", "5c"}, 1.0},   // 接A或6 → tier1
		{"TJQK开口", []string{"Th", "Js", "Qd", "Kc"}, 1.0},   // 接9或A → tier1
		{"5678开口", []string{"5h", "6s", "7d", "8c"}, 1.0},   // tier1
	}
	for _, c := range cases {
		if got := rowStraightTightness(mkrow(c.row...)); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("%s: 期望 %.3f, 得 %.3f", c.name, c.want, got)
		}
	}
}
