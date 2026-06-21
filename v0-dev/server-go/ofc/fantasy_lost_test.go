package ofc

import "testing"

func flState(top, mid, bot []string) *GameState {
	mk := func(ss []string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	st := NewGameState(2)
	st.Round = 4
	for _, c := range mk(top) { st.PlaceCard(c, RowTop); st.UsedCards[c.ID()] = true }
	for _, c := range mk(mid) { st.PlaceCard(c, RowMiddle); st.UsedCards[c.ID()] = true }
	for _, c := range mk(bot) { st.PlaceCard(c, RowBottom); st.UsedCards[c.ID()] = true }
	return st
}

// hand20: 中66/22两对, 底[8h Kh 3h 9s]只能成1对(9s破红心花) → 底托不住中 → 范死.
func TestFantasyLost_Hand20DeadBottom(t *testing.T) {
	st := flState([]string{"X", "4s"}, []string{"6c", "2s", "3c", "2d", "6s"}, []string{"8h", "Kh", "3h", "9s"})
	if !FantasyLost(st) {
		t.Fatal("底[8h Kh 3h 9s]只能成1对<中两对, 范该死 (FantasyLost应true)")
	}
}

// hand20 state: 底[8h Kh 3h]+2空位能成红心花 > 中两对 → 范还活.
func TestFantasyLost_Hand20StateAlive(t *testing.T) {
	st := flState([]string{"X"}, []string{"6c", "2s", "3c", "2d", "6s"}, []string{"8h", "Kh", "3h"})
	if FantasyLost(st) {
		t.Fatal("底能成红心花>中两对, 顶能QQ, 范该活 (FantasyLost应false)")
	}
}

// QQ2/QX3/QX4: 顶QQ + 中Q鬼=QQ + 底Q鬼=QQ, 阶梯kicker合法范 → 不该判死.
func TestFantasyLost_QQChain(t *testing.T) {
	st := flState([]string{"Qc", "Qd", "2s"}, []string{"Qh", "X", "3s"}, []string{"Qs", "X", "4s"})
	if FantasyLost(st) {
		t.Fatal("QQ2/QX3/QX4 全QQ阶梯, 合法范, 不该判死 (FantasyLost应false)")
	}
}

// 顶死: 顶[Js 9s 2c] 满 max=JJ<QQ → 范死.
func TestFantasyLost_TopCantQQ(t *testing.T) {
	st := flState([]string{"Js", "9s", "2c"}, []string{"Kc", "Kd"}, []string{"As", "Ah"})
	if !FantasyLost(st) {
		t.Fatal("顶满JJ<QQ, 范该死 (FantasyLost应true)")
	}
}

// 用户例1: 顶[Q] 中[A678] 底[A398] — 顶可QQ/中可AA/底可AA, 阶梯可托 → 可追范.
func TestFantasyLost_Ex1Possible(t *testing.T) {
	st := flState([]string{"Qs"}, []string{"Ac", "6d", "7h", "8s"}, []string{"Ad", "3c", "9h", "8d"})
	if FantasyLost(st) {
		t.Fatal("例1 顶可QQ+中底可AA, 应可追范 (FantasyLost=false)")
	}
}

// 用户例2: 顶[AKA]满 中[A578A]满 — 顶AA-K > 中AA-8 (kicker+3vs5), 已犯规 → 范死.
func TestFantasyLost_Ex2KickerFoul(t *testing.T) {
	st := flState([]string{"Ah", "Kh", "Ad"}, []string{"Ac", "5d", "7h", "8s", "As"}, []string{"3s", "9d", "8c"})
	if !FantasyLost(st) {
		t.Fatal("例2 顶AA-K>中AA-8 已kicker犯规, 范该死 (FantasyLost=true)")
	}
}

func r2drState(top, mid, bot []string) *GameState {
	mk := func(ss []string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	st := NewGameState(2); st.Round = 2
	for _, c := range mk(top) { st.PlaceCard(c, RowTop) }
	for _, c := range mk(mid) { st.PlaceCard(c, RowMiddle) }
	for _, c := range mk(bot) { st.PlaceCard(c, RowBottom) }
	return st
}
func TestR2BotPairMidDraw_Flush(t *testing.T) { // 底QQ(3)+中3黑桃 → +3
	st := r2drState([]string{"4c"}, []string{"3s", "5s", "7s"}, []string{"9h", "Qd", "Qh"})
	if R2BotPairMidDrawBonus(st, st) != 3 { t.Fatalf("底QQ3张+中3黑桃flush应+3, got %v", R2BotPairMidDrawBonus(st, st)) }
}
func TestR2BotPairMidDraw_Straight(t *testing.T) { // 底KK(3)+中5-6-7 → +3
	st := r2drState([]string{"2c"}, []string{"5h", "6d", "7c"}, []string{"Kh", "Ks", "2d"})
	if R2BotPairMidDrawBonus(st, st) != 3 { t.Fatalf("底KK3张+中567连应+3, got %v", R2BotPairMidDrawBonus(st, st)) }
}
func TestR2BotPairMidDraw_BotNot3(t *testing.T) { // 底4张 → 0
	st := r2drState([]string{}, []string{"3s", "5s", "7s"}, []string{"9h", "Qd", "Qh", "4c"})
	if R2BotPairMidDrawBonus(st, st) != 0 { t.Fatalf("底4张应0, got %v", R2BotPairMidDrawBonus(st, st)) }
}

func TestRedundantHighLockedAA_StraightExempt(t *testing.T) {
	mk := func(ss ...string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	mkst := func(top, bot []string) *GameState {
		st := NewGameState(2); st.Round = 4
		for _, c := range mk(top...) { st.PlaceCard(c, RowTop) }
		for _, c := range mk(bot...) { st.PlaceCard(c, RowBottom) }
		return st
	}
	pre := mkst([]string{"As", "Ac"}, []string{"X", "Qd", "Kh", "Js"})
	// post: Ad→底 成 broadway 顺 → 豁免, 0
	post := mkst([]string{"As", "Ac"}, []string{"X", "Qd", "Kh", "Js", "Ad"})
	if v := RnRedundantHighOnLockedAAPenalty(post, pre); v != 0 {
		t.Fatalf("Ad成broadway顺该豁免(0), got %v", v)
	}
	// 真 redundant: 底是对子(9h9c), 加Ad不成顺 → 罚5
	pre2 := mkst([]string{"As", "Ac"}, []string{"9h", "9c", "5d", "2s"})
	post2 := mkst([]string{"As", "Ac"}, []string{"9h", "9c", "5d", "2s", "Ad"})
	if v := RnRedundantHighOnLockedAAPenalty(post2, pre2); v != 5 {
		t.Fatalf("A丢对子底(非顺)该罚5, got %v", v)
	}
}

func TestRedundantHighLockedAA_TripsExempt(t *testing.T) {
	mk := func(ss ...string) []Card { var r []Card; for _, s := range ss { c, _ := ParseCard(s); r = append(r, c) }; return r }
	mkst := func(top, bot []string) *GameState {
		st := NewGameState(2); st.Round = 4
		for _, c := range mk(top...) { st.PlaceCard(c, RowTop) }
		for _, c := range mk(bot...) { st.PlaceCard(c, RowBottom) }
		return st
	}
	// 底 X Kh Ks Qd + Ad: 鬼配K成KKK三条(或鬼=T成顺更高) → 豁免 (>=三条)
	pre := mkst([]string{"As", "Ac"}, []string{"X", "Kh", "Ks", "Qd"})
	post := mkst([]string{"As", "Ac"}, []string{"X", "Kh", "Ks", "Qd", "Ad"})
	if v := RnRedundantHighOnLockedAAPenalty(post, pre); v != 0 {
		t.Fatalf("底成三条+该豁免(0), got %v", v)
	}
}

// A6 R4 split post: 顶[Ad 2s]唯一范对AA(28) > 中[9h 8s 7s Qh]最大QQ(26) → 范只能foul.
func TestFantasyOnlyViaFoul_A6Split(t *testing.T) {
	st := flState([]string{"Ad", "2s"}, []string{"9h", "8s", "7s", "Qh"}, []string{"Qd", "6d", "5d", "Jd", "Kd"})
	if !fantasyOnlyViaFoul(st) {
		t.Fatal("顶AA>中QQ, 范只能foul, 应 true")
	}
}

// foul-free: 顶[Qc]+2空能成QQ, 中[9h 8s 7s]+2空能成顺(>QQ) → QQ≤中max → 不是只能foul.
func TestFantasyOnlyViaFoul_FoulFree(t *testing.T) {
	st := flState([]string{"Qc"}, []string{"9h", "8s", "7s"}, []string{"2c", "3d", "4h"})
	if fantasyOnlyViaFoul(st) {
		t.Fatal("顶QQ≤中顺, foul-free范在, 应 false")
	}
}

// G94 R3 8h→中 post: 顶要QQ → 中88须升两对 → 底[9h Kh Qh 7s]剩1空只成一对(27)<两对(32) → 假范.
func TestFantasyOnlyViaFoul_G94MidBotChain(t *testing.T) {
	st := flState([]string{"4c"}, []string{"8c", "Js", "2s", "8h"}, []string{"9h", "Kh", "Qh", "7s"})
	if !fantasyOnlyViaFoul(st) {
		t.Fatal("底只成对撑不住中两对, 范只能foul, 应 true")
	}
}

// 对照: 同样中88, 但底[Th Jh Ks Ac]能凑broadway顺撑住中的顺/两对 → 有foul-free范 → false.
func TestFantasyOnlyViaFoul_BotCanSupport(t *testing.T) {
	st := flState([]string{"4c"}, []string{"8c", "Js", "2s", "8h"}, []string{"Th", "Jh", "Ks", "Ac"})
	if fantasyOnlyViaFoul(st) {
		t.Fatal("底能成broadway顺撑住, foul-free范在, 应 false")
	}
}
