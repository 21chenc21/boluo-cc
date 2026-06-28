package ofc

import "testing"

// kicker 微奖 (#64): 同对内高 kicker 微胜, 但绝不翻牌型/pairRank/royalty.
func mkTopPair(pairR, kickerR uint8) HandValue {
	return HandValue{Type: TypePair, Value: int64(TypePair)*1000000 + int64(pairR)*15 + int64(kickerR)}
}

func TestMHR_KickerBreaksTie(t *testing.T) {
	none := HandValue{Type: -1}
	aak := MadeHandRewardLabel(mkTopPair(RankA, RankK), none, none) // AAK
	aaq := MadeHandRewardLabel(mkTopPair(RankA, RankQ), none, none) // AAQ
	aaj := MadeHandRewardLabel(mkTopPair(RankA, RankJ), none, none) // AAJ
	if !(aak > aaq && aaq > aaj) {
		t.Fatalf("kicker 应破平 AAK>AAQ>AAJ, got %.4f %.4f %.4f", aak, aaq, aaj)
	}
	// 2026-06-28: kicker 步长 bump 到 0.08 (66+对). 一档差=0.08, 满12档=0.96.
	//   注: MHR不含royalty(rollout单独加). 全label里 AA royalty9 vs KK royalty8 gap=1.0 > kicker满0.96 → 不翻.
	if d := aak - aaq; d < 0.07 || d > 0.09 {
		t.Fatalf("kicker 一档差应 ~0.08, got %.4f", d)
	}
}

// kicker 满格也不能翻 pairRank: 55+A kicker < 66 (66有royalty=1, label另加; 这里只看 MHR 内部不越 0.2 档).
func TestMHR_KickerNeverFlipsPairRank(t *testing.T) {
	none := HandValue{Type: -1}
	// 55 (idx3) 满 kicker A vs 66... 66 不进 MHRTopPairStep(>=66交royalty), 但 5档 step=0.8.
	p55A := MadeHandRewardLabel(mkTopPair(3, RankA), none, none) // 0.8 + 0.12
	p44A := MadeHandRewardLabel(mkTopPair(2, RankA), none, none) // 0.6 + 0.12
	// 44 满 kicker (0.72) 必须 < 55 最低 kicker (0.8 + 0)
	p55low := MadeHandRewardLabel(mkTopPair(3, 0), none, none) // 0.8 + 0
	if !(p44A < p55low) {
		t.Fatalf("kicker 不该翻 pairRank: 44+Akicker=%.4f 应 < 55+lowkicker=%.4f", p44A, p55low)
	}
	if !(p55A > p44A) {
		t.Fatalf("55>44 sanity, got %.4f %.4f", p55A, p44A)
	}
}

// 高对 (AA/KK royalty 行) kicker 也加, 但不翻 pair: AAQ(royalty9) 内部 mhr 仍 > KKA mhr 不成立(KK pair更高kicker), 靠 royalty 区分.
// 这里只验 AA 内 kicker 单调 + KK 内 kicker 单调, 不跨对比 (跨对由 royalty 管).
func TestMHR_HighPairKickerMonotone(t *testing.T) {
	none := HandValue{Type: -1}
	kka := MadeHandRewardLabel(mkTopPair(RankK, RankA), none, none)
	kkq := MadeHandRewardLabel(mkTopPair(RankK, RankQ), none, none)
	if !(kka > kkq) {
		t.Fatalf("KKA>KKQ kicker, got %.4f %.4f", kka, kkq)
	}
}

// #92: 中/底 单对按 rank 破平 (QQ>JJ), 且不翻牌型.
func TestMHR_MidBotPairRank(t *testing.T) {
	none := HandValue{Type: -1}
	// 底 QQ vs JJ (5张: 对+3kicker). 底QQ > 底JJ.
	botQQ := MadeHandRewardLabel(none, none, Evaluate5(mkrow("Qs", "Qh", "Tc", "9d", "7s")))
	botJJ := MadeHandRewardLabel(none, none, Evaluate5(mkrow("Js", "Jh", "Tc", "9d", "7s")))
	if !(botQQ > botJJ) {
		t.Fatalf("底QQ>底JJ, got %.4f %.4f", botQQ, botJJ)
	}
	// 底单对(满rank A) 必 < 底两对 0.5 (不翻牌型)
	botAA := MadeHandRewardLabel(none, none, Evaluate5(mkrow("As", "Ah", "Tc", "9d", "7s")))
	botTwoP := MadeHandRewardLabel(none, none, Evaluate5(mkrow("4s", "4h", "7c", "7d", "9s")))
	if !(botAA < botTwoP) {
		t.Fatalf("底单对(%.3f) 必 < 底两对(%.3f)", botAA, botTwoP)
	}
	// 中 KK vs QQ: 0.5 base + rank. 中KK>中QQ, 且 < 中两对 1.0.
	midKK := MadeHandRewardLabel(none, Evaluate5(mkrow("Ks", "Kh", "Tc", "9d", "7s")), none)
	midQQ := MadeHandRewardLabel(none, Evaluate5(mkrow("Qs", "Qh", "Tc", "9d", "7s")), none)
	midTwoP := MadeHandRewardLabel(none, Evaluate5(mkrow("4s", "4h", "7c", "7d", "9s")), none)
	if !(midKK > midQQ && midKK < midTwoP) {
		t.Fatalf("中KK(%.3f)>中QQ(%.3f) 且 <中两对(%.3f)", midKK, midQQ, midTwoP)
	}
}

// #124: 两对按高对rank破平 (22/TT>22/88), 含 joker 两对, 且不翻牌型.
func TestMHR_TwoPairRank(t *testing.T) {
	none := HandValue{Type: -1}
	// 真牌两对: 中 22/TT vs 22/88 (高对 T>8)
	midTT := MadeHandRewardLabel(none, Evaluate5(mkrow("2s", "2h", "Ts", "Td", "5c")), none)
	mid88 := MadeHandRewardLabel(none, Evaluate5(mkrow("2s", "2h", "8s", "8d", "5c")), none)
	if !(midTT > mid88) {
		t.Fatalf("中22/TT(%.3f) > 中22/88(%.3f)", midTT, mid88)
	}
	// joker 两对 (#124 真场景): 底JJ/99 cap → 鬼不能成222trips, 只能 22/TT (鬼配T)
	capJJ99 := Evaluate5(mkrow("Jh", "Js", "9h", "9s", "8s"))
	jTT := Evaluate5JokerCap(mkrow("2s", "2h", "Xj0", "Ts", "5c"), &capJJ99)
	if jTT.Type != TypeTwoPair {
		t.Fatalf("鬼配(底JJ99 cap)应成两对, 得 type=%d", jTT.Type)
	}
	rj := MadeHandRewardLabel(none, jTT, none)
	if !(rj > mid88) {
		t.Fatalf("鬼22/TT(%.3f) > 真22/88(%.3f)", rj, mid88)
	}
	// 不翻牌型: 中两对满rank < 中三条? 三条不在MHR(走royalty), 验 两对(满) < 底三条MHR=1.0 不直接可比;
	// 验 中两对(满0.13) 仍 > 中单对(0.5+满0.12=0.62)? 两对base1.0 > 0.62 ✓; 且 底两对(满0.63) < 底三条(1.0)
	botTwoPMax := MadeHandRewardLabel(none, none, Evaluate5(mkrow("As", "Ah", "Ks", "Kd", "5c"))) // 底AA/KK两对(高对A满)
	botTrips := MadeHandRewardLabel(none, none, Evaluate5(mkrow("2s", "2h", "2d", "7c", "5s")))    // 底222三条
	if !(botTwoPMax < botTrips) {
		t.Fatalf("底两对满rank(%.3f) 必 < 底三条(%.3f)", botTwoPMax, botTrips)
	}
}
