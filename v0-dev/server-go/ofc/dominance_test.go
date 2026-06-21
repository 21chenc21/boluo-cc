package ofc

import "testing"

// 2026-06-14 候选支配过滤 (同顶中底大者赢): bottomDomScore 排序 + 高牌draw=tier0 守护.
func TestBottomDomScore_OrderAndDrawGuard(t *testing.T) {
	trips := bottomDomScore(parseHand("8c", "9d", "8h", "8s"))   // 888 三条
	twoPair := bottomDomScore(parseHand("8c", "9d", "8h", "9h")) // 88-99 两对
	pair := bottomDomScore(parseHand("8c", "9d", "8h", "2c"))    // 88 一对
	draw := bottomDomScore(parseHand("5h", "6h", "7h", "8h"))    // 5678同花 顺+花draw (高牌)
	if !(trips > twoPair && twoPair > pair) {
		t.Fatalf("强弱序应 三条>两对>一对, got %d/%d/%d", trips, twoPair, pair)
	}
	if draw >= 100 {
		t.Fatalf("高牌draw 应 tier0(<100) 免被支配删, got %d", draw)
	}
	if pair < 100 {
		t.Fatalf("一对 应 >=100(tier1), got %d", pair)
	}
}

// 2026-06-18 中道支配过滤 (mirror 底道, 局75 R5): 同顶+同底下中道更强合法者支配弱者.
// 鬼555 (真5对+鬼) 应严格 > 鬼333, 同三条按高 rank 支配.
func TestMidDomScore_TripsRankOrder(t *testing.T) {
	mid555 := bottomDomScore(parseHand("5s", "X", "3c", "4s", "5c")) // 鬼555
	mid333 := bottomDomScore(parseHand("3c", "3s", "4s", "5s", "X")) // 鬼333
	if !(mid555 > mid333) {
		t.Fatalf("555 应严格 > 333 (同三条高rank支配), got %d/%d", mid555, mid333)
	}
}

// 安全锁: 更强中道若 > 底道则自身 foul, IsFoulJoker 拦住 → 不当支配者 (底=444 时中555反而犯规, 不能删合法的中333).
func TestMidDom_StrongerMidFoulsGuard(t *testing.T) {
	top := parseHand("Ad", "Ah", "9d")              // AA9
	bot := parseHand("4c", "4d", "4h", "2c", "7d")  // 底 444 三条
	mid555 := parseHand("5s", "5c", "5d", "3c", "4s") // 中 555 > 底444 → foul
	mid333 := parseHand("3s", "3c", "3d", "4s", "6c") // 中 333 < 底444 → 合法
	if !IsFoulJoker(top, mid555, bot) {
		t.Fatalf("中555 > 底444 应 foul (更强中道不得当支配者)")
	}
	if IsFoulJoker(top, mid333, bot) {
		t.Fatalf("中333 < 底444 应合法 (不该被foul的555误删)")
	}
}

// 2026-06-18 支配软罚 (用户改 加分 不强过滤): kicker-aware dom 分 + sibling-relative 罚, 封顶.
func TestDomScoreK_KickerAware(t *testing.T) {
	kkq := domScoreK(parseHand("Kh", "Ks", "Qs")) // KK + Q kicker
	kk3 := domScoreK(parseHand("Kh", "Ks", "3c")) // KK + 3 kicker
	if !(kkq > kk3) {
		t.Fatalf("KKQ 应 > KK3 (kicker-aware), got %.2f/%.2f", kkq, kk3)
	}
	qq := domScoreK(parseHand("Qs", "Tc", "Jh", "Qh")) // QQ
	jj := domScoreK(parseHand("Qs", "Tc", "Jh", "Js")) // JJ
	if !(qq > jj) {
		t.Fatalf("QQ 应 > JJ, got %.2f/%.2f", qq, jj)
	}
}

func TestDomSoftPen_Capped(t *testing.T) {
	if p := domSoftPen(0.4); p < 0.7 || p > 0.9 { // 2*0.4=0.8
		t.Fatalf("gap0.4 应~0.8, got %.2f", p)
	}
	if p := domSoftPen(5.0); p != float32(domSoftCap) { // 2*5=10 封顶
		t.Fatalf("大gap应封顶%.1f, got %.2f", float32(domSoftCap), p)
	}
}

func TestRowHasJoker(t *testing.T) {
	if !rowHasJoker(parseHand("Qs", "X", "9d")) {
		t.Fatalf("含鬼底应 true (手4类发育底, partial board 豁免软罚)")
	}
	if rowHasJoker(parseHand("Kh", "Ks", "3c")) {
		t.Fatalf("无鬼底应 false (局16 可软罚)")
	}
}
