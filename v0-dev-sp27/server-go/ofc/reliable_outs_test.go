package ofc

import "testing"

// 2026-06-28 (用户"这里很关键要写好"): support 口径 maxAchievableCmpCapped 的"靠谱 outs"判定.
// 核心: 抽某 rank 升对/三条/四条/两对, 有效 outs = 真牌 rankRem + 可摸鬼 jokerRem (鬼万能补),
//   ≥ need+1 才算可达 (不赌某 rank 最后一张的"空中楼阁"). 鬼是关键: +1鬼=+1out / +2鬼=+2out.

func roBase() ([13]int, [4]int) {
	var rr [13]int
	for i := range rr {
		rr[i] = 4
	}
	var sr [4]int
	for i := range sr {
		sr[i] = 13
	}
	return rr, sr
}
func roMk(ss ...string) []Card {
	var r []Card
	for _, s := range ss {
		r = append(r, mustParse(s))
	}
	return r
}

// 单张升对 / 对升三条: 真牌不足 2 时鬼补, 剩1张真牌无鬼 = 空中楼阁不可达
func TestReliableOuts_SinglePairTrips(t *testing.T) {
	HtP := int(HtPair)
	row := roMk("Qh", "Qd", "5c") // QQ + 单5, 1 slot; Q 摸光挡 QQQ 干扰
	rr, sr := roBase()
	rr[10] = 0
	rr[3] = 1 // Q=idx10 摸光, 5=idx3 剩1
	if g := maxAchievableCmpCapped(row, 1, rr, sr, 0, 999); g/13 != HtP {
		t.Fatalf("5剩1真无鬼: 配不上, 应只QQ对(tier%d), got tier%d", HtP, g/13)
	}
	rr, sr = roBase()
	rr[10] = 0
	rr[3] = 2
	if g := maxAchievableCmpCapped(row, 1, rr, sr, 0, 999); g/13 != int(HtTwoPair) {
		t.Fatalf("5剩2真: 可配, 应两对, got tier%d", g/13)
	}
	rr, sr = roBase()
	rr[10] = 0
	rr[3] = 1
	if g := maxAchievableCmpCapped(row, 1, rr, sr, 1, 999); g/13 != int(HtTwoPair) {
		t.Fatalf("5剩1真+1鬼=2outs: 鬼救场, 应两对, got tier%d", g/13)
	}
	// 三条 QQ→QQQ: 剩1张Q无鬼=不可达(留QQ对) / 剩1Q+1鬼=可达
	row2 := roMk("Qh", "Qd", "2c")
	rr, sr = roBase()
	rr[10] = 1
	rr[0] = 0
	if g := maxAchievableCmpCapped(row2, 1, rr, sr, 0, 999); g/13 != HtP {
		t.Fatalf("QQ+剩1Q无鬼: 不该QQQ, got tier%d", g/13)
	}
	rr, sr = roBase()
	rr[10] = 1
	rr[0] = 0
	if g := maxAchievableCmpCapped(row2, 1, rr, sr, 1, 999); g/13 != int(HtThreeKind) {
		t.Fatalf("QQ+剩1Q+1鬼: 应QQQ三条, got tier%d", g/13)
	}
}

// 两对共享鬼池: 一个鬼不能同时救两个"最后一张"对 (按补到2outs消耗鬼池)
func TestReliableOuts_TwoPairJokerPool(t *testing.T) {
	row := roMk("Ah", "Kc") // 2 单张, 2 slots, 都设剩1真牌(last-card)
	rr, sr := roBase()
	rr[12] = 1
	rr[11] = 1
	if g := maxAchievableCmpCapped(row, 2, rr, sr, 1, 999); g/13 != int(HtPair) {
		t.Fatalf("2个last-card对+1鬼: 1鬼只救1对, 应pair非两对, got tier%d", g/13)
	}
	rr, sr = roBase()
	rr[12] = 1
	rr[11] = 1
	if g := maxAchievableCmpCapped(row, 2, rr, sr, 2, 999); g/13 != int(HtTwoPair) {
		t.Fatalf("2个last-card对+2鬼: 应两对, got tier%d", g/13)
	}
	rr, sr = roBase()
	rr[12] = 2
	rr[11] = 2
	if g := maxAchievableCmpCapped(row, 2, rr, sr, 0, 999); g/13 != int(HtTwoPair) {
		t.Fatalf("2真牌各剩2: 无鬼也两对, got tier%d", g/13)
	}
}
