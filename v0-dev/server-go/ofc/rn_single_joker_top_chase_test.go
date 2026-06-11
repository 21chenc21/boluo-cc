package ofc

import "testing"

// 2026-06-05: RnSingleJokerTopChaseABonus — 孤鬼(或鬼+sub-Q)在顶 + 1A上顶追AA范(废A放底) → +8.
// ypk-32571722-17 R3.

func st(top, mid, bot []string) *GameState {
	g := NewGameState(2)
	for _, s := range top {
		g.Top = append(g.Top, mustParse(s))
	}
	for _, s := range mid {
		g.Middle = append(g.Middle, mustParse(s))
	}
	for _, s := range bot {
		g.Bottom = append(g.Bottom, mustParse(s))
	}
	return g
}

func TestRnTopChase_Fire_LoneJoker(t *testing.T) {
	pre := st([]string{"X"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts", "Ac"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 8 {
		t.Fatalf("孤鬼+A上顶+废A放底 应 +8, got %v", got)
	}
}

func TestRnTopChase_Fire_JokerSubQ(t *testing.T) {
	// 鬼+J (配对 JJ < QQ, 不能直接进范) → 加 A 升 AA → fire
	pre := st([]string{"X", "Jd"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "Jd", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts", "Ac"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 8 {
		t.Fatalf("鬼+J(<QQ)+A上顶 应 +8, got %v", got)
	}
}

func TestRnTopChase_Skip_JokerQ(t *testing.T) {
	// 鬼+Q (已可进 QQ范) → skip
	pre := st([]string{"X", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "Qc", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts", "Ac"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 0 {
		t.Fatalf("鬼+Q(已锁QQ) 应 skip(0), got %v", got)
	}
}

func TestRnTopChase_Skip_TwoJokers(t *testing.T) {
	pre := st([]string{"X", "X"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "X", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts", "Ac"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 0 {
		t.Fatalf("双鬼(AA锁) 应 skip(0), got %v", got)
	}
}

func TestRnTopChase_Skip_MidFullNotTwoPair(t *testing.T) {
	// case 50: mid 满 KK (one pair < two-pair) → top AA 托不住 → skip
	pre := st([]string{"X", "2c"}, []string{"Kh", "Kd", "3h", "4s", "5h"}, []string{"9d", "Th", "Jc", "Qd"})
	post := st([]string{"X", "2c", "As"}, []string{"Kh", "Kd", "3h", "4s", "5h"}, []string{"9d", "Th", "Jc", "Qd", "8s"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 0 {
		t.Fatalf("mid满KK(<两对) top AA托不住 应 skip(0), got %v", got)
	}
}

func TestRnTopChase_Skip_AInMid(t *testing.T) {
	// 另一张 A 进中道 (没放底) → 不奖 (强制废A放底)
	pre := st([]string{"X"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "As"}, []string{"9s", "2h", "Ah"}, []string{"Th", "Tc", "Kh", "Ts"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 0 {
		t.Fatalf("另一张A进中道 应不奖(0), got %v", got)
	}
}

func TestRnLoneAMid_Fire(t *testing.T) {
	// 鬼+Q在顶 + 本轮孤 Ac 进中 (中道只1张A) → 罚 +8
	pre := st([]string{"X", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"})
	post := st([]string{"X", "Qc", "As"}, []string{"9s", "2h", "Ac"}, []string{"Th", "Tc", "Ts"})
	if got := RnLoneAceMidJokerTopPenalty(post, pre); got != 8 {
		t.Fatalf("鬼顶+孤A进中 应罚 8, got %v", got)
	}
}

func TestRnLoneAMid_Skip_AAPairMid(t *testing.T) {
	// 双 A 进中成 AA对 (中道2张A) → 不罚 (强中道)
	pre := st([]string{"X", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"})
	post := st([]string{"X", "Qc"}, []string{"9s", "2h", "As", "Ac"}, []string{"Th", "Tc", "Ts"})
	if got := RnLoneAceMidJokerTopPenalty(post, pre); got != 0 {
		t.Fatalf("双A进中成对 应不罚(0), got %v", got)
	}
}

func TestRnLoneAMid_Skip_NoJokerTop(t *testing.T) {
	// 非鬼顶 → 不归这条
	pre := st([]string{"Kc", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"})
	post := st([]string{"Kc", "Qc", "As"}, []string{"9s", "2h", "Ac"}, []string{"Th", "Tc", "Ts"})
	if got := RnLoneAceMidJokerTopPenalty(post, pre); got != 0 {
		t.Fatalf("非鬼顶 应不罚(0), got %v", got)
	}
}

func TestRnLoneAMid_Skip_AToBot(t *testing.T) {
	// 废A放底 (中道没加A) → 不罚
	pre := st([]string{"X", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"})
	post := st([]string{"X", "Qc", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts", "Ac"})
	if got := RnLoneAceMidJokerTopPenalty(post, pre); got != 0 {
		t.Fatalf("废A放底 应不罚(0), got %v", got)
	}
}

func TestRnTopChase_Skip_AAATop(t *testing.T) {
	// 2 张 A 上顶 = AAA 陷阱 → 不奖
	pre := st([]string{"X"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	post := st([]string{"X", "As", "Ah"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Kh", "Ts"})
	if got := RnSingleJokerTopChaseABonus(post, pre); got != 0 {
		t.Fatalf("AAA上顶 应不奖(0), got %v", got)
	}
}

// 2026-06-11 RnTopTripsFantasyBonus — top foul-safe 三条 > top AA对 (ypk-102367562-12 R4)
func TestRnTopTrips_Fire_333(t *testing.T) {
	// top=[X X 3c] mid=888三条 → 333三条 (cap 下), foul-safe → +5
	g := st([]string{"X", "X", "3c"}, []string{"8c", "8d", "7h", "8h"}, []string{"Td", "Jd", "Tc", "Th"})
	if got := RnTopTripsFantasyBonus(g); got != 5 {
		t.Fatalf("top 333三条 应 +5, got %v", got)
	}
}

func TestRnTopTrips_Skip_AAPairCapped(t *testing.T) {
	// top=[X X Ts] mid=888 → 被 cap 成 AA对 (TTT会犯规), 非三条 → 0
	g := st([]string{"X", "X", "Ts"}, []string{"8c", "8d", "7h", "8h"}, []string{"Td", "Jd", "Tc", "Th"})
	if got := RnTopTripsFantasyBonus(g); got != 0 {
		t.Fatalf("top 被cap成AA对 应 0, got %v", got)
	}
}

func TestRnTopTrips_Skip_Incomplete(t *testing.T) {
	// top 未满 (2张) → 0
	g := st([]string{"X", "X"}, []string{"8c", "8d", "7h", "8h"}, []string{"Td", "Jd", "Tc", "Th"})
	if got := RnTopTripsFantasyBonus(g); got != 0 {
		t.Fatalf("top 未满 应 0, got %v", got)
	}
}

// 2026-06-11 边界: midMadeFloor 必须认得 mid 满时的 花/顺 (> 三条) + foul guard
func TestRnTopTrips_MidFlushStraight(t *testing.T) {
	cases := []struct {
		name     string
		top, mid []string
		want     float32
	}{
		{"mid满花+top333三条", []string{"X", "X", "3c"}, []string{"2h", "5h", "8h", "Jh", "Kh"}, 5},
		{"mid满顺+top333三条", []string{"X", "X", "3c"}, []string{"5h", "6c", "7d", "8h", "9s"}, 5},
		{"mid两对+top333应犯规→0", []string{"X", "X", "3c"}, []string{"5h", "5c", "6h", "6c", "Kd"}, 0},
		{"mid满888+topKKK犯规→0", []string{"X", "X", "Kc"}, []string{"8c", "8d", "8h", "2s", "3d"}, 0},
		{"mid满888+top333foulsafe→5", []string{"X", "X", "3c"}, []string{"8c", "8d", "8h", "2s", "3d"}, 5},
		{"mid部分高张+top444保守→0", []string{"X", "X", "4c"}, []string{"2h", "5c", "8d", "Jh"}, 0},
	}
	for _, c := range cases {
		g := st(c.top, c.mid, nil)
		if got := RnTopTripsFantasyBonus(g); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
