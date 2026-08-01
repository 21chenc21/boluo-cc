package ofc

import "testing"

// SB组(dim169) 次高牌搁浅 单元测试: 131正误线区分 + 边界豁免
func TestMidStrandedBig(t *testing.T) {
	parse := func(ss ...string) []Card {
		var r []Card
		for _, x := range ss {
			c, _ := ParseCard(x)
			r = append(r, c)
		}
		return r
	}
	mk := func(mid, bot []Card) *GameState {
		return &GameState{NumJokers: 2, Middle: mid, Bottom: bot}
	}
	cases := []struct {
		name    string
		gs      *GameState
		wantPos bool // true=信号>0(有搁浅), false=0
	}{
		{"131错线 K中Qc底", mk(parse("Kh", "2h", "2c"), parse("Qc")), true},
		{"131正线 22中 KQ底", mk(parse("2h", "2c"), parse("Kh", "Qc")), false},
		{"131 Q中Kh底", mk(parse("2h", "Qc", "2c"), parse("Kh")), false}, // Qc(10)<=Kh(11)底, 不搁浅
		{"9中Qc底(9<Q)", mk(parse("9h", "2h", "2c"), parse("Qc")), false}, // 9<Q, 不搁浅(9留中不亏顶... 但exp要沉底, 边界)
		{"成顺豁免 9TJ中", mk(parse("9h", "Th", "Jc"), parse("5c")), false}, // 中道成顺潜力, 豁免
		{"纯对无搁浅 22中", mk(parse("2h", "2c"), parse("8c")), false},
		{"底满不算 K中", mk(parse("Kh", "2h"), parse("2c", "3c", "4c", "5c", "6c")), false},
	}
	for _, c := range cases {
		got := midStrandedBig(c.gs)
		pos := got > 0
		status := "✓"
		if pos != c.wantPos {
			status = "✗ FAIL"
			t.Errorf("%s: 信号=%.3f, 期望pos=%v", c.name, got, c.wantPos)
		}
		t.Logf("%s %-22s 信号=%.3f", status, c.name, got)
	}
}
