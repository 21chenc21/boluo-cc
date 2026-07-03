package ofc

import (
	"math"
	"testing"
)

// ============================================================
// sp37 修复批单元测试 (2026-07-03): Fix0 pTopFinalPairExact 顶鬼 / Fix1 f153 不数deck鬼 / Fix3 row-trips-rank
// ============================================================

// Fix0 — pTopFinalPairExact: 顶[X 2c] 1空, 摸1张Q→鬼当Q→QQ. 旧版鬼贪心配2c → f90(QQ)=0 (#46 exp范被清零).
func TestSp37_TopJokerPairsFutureHigh(t *testing.T) {
	// ON: 顶留空的鬼能配未来摸的高牌 → P(top QQ+) > 0
	gs := makeStateV3(t, []string{"X", "2c"}, []string{"3d", "4d", "5d"}, []string{"7s", "8h", "9c"})
	f := BuildFeaturesV3(gs)
	if f[90] <= 0 { // f90 = pTopFinalPairExact(QQ)
		t.Errorf("#46: 顶[X 2c] 摸Q→鬼配成QQ, f90(QQ) 应>0, got %.3f", f[90])
	}
	if f[92] <= 0 { // f92 = pTopFinalPairExact(AA): 摸A→鬼配成AA
		t.Errorf("#46: 顶[X 2c] 摸A→AA, f92(AA) 应>0, got %.3f", f[92])
	}
	// 保留旧行为: 顶[X Ad] = 鬼锁 AA → f92 高 (不因改动丢锁)
	gs2 := makeStateV3(t, []string{"X", "Ad"}, []string{"3d", "4d", "5d"}, []string{"7s", "8h", "9c"})
	f2 := BuildFeaturesV3(gs2)
	if f2[92] < 0.5 {
		t.Errorf("顶[X Ad] 鬼锁AA, f92 应≥0.5, got %.3f", f2[92])
	}
}

// Fix1 — probableMaxTier(真实牌堆): 中666 金刚要摸最后1张6(~低概率) → 不该冲到葫芦/金刚, 停~三条.
func TestSp37_ProbableMaxTier_NoPhantomQuad(t *testing.T) {
	gs := makeStateV3(t, []string{"3s"}, []string{"6h", "6d", "6c"}, []string{"8s", "8h", "8d", "8c", "9d"})
	rr, sr, jr := computeDeckRemaining(gs)
	dt := jr
	for _, r := range rr {
		dt += r
	}
	pv := probableMaxTier(gs.Middle, 5-len(gs.Middle), rr, sr, jr, dt, cardsSeenRemaining(gs))
	if pv >= float32(int(HtFullHouse))/8.0 { // ≥葫芦(0.75) = 把 improbable 高tier 当真了
		t.Errorf("#110: 666金刚/葫芦空中楼阁, probableMaxTier 不该≥0.75, got %.3f", pv)
	}
	if pv < float32(int(HtThreeKind))/8.0-0.01 { // 楼板=当前三条(0.375)
		t.Errorf("probableMaxTier 应≥当前三条0.375(楼板), got %.3f", pv)
	}
}

// Fix1 wiring — #110: 中666(空位, 金刚靠最后1张6) + 底金刚8s. f153 不该虚高.
func TestSp37_F153_NoPhantomQuadHeadroom(t *testing.T) {
	gs := makeStateV3(t, []string{"Kh"}, []string{"6h", "6d", "6c"}, []string{"8s", "8h", "8d", "8c", "9d"})
	f := BuildFeaturesV3(gs)
	if f[153] > 0.2 { // 中已是trips, 金刚是空中楼阁 → 发育headroom≈0
		t.Errorf("#110: 中666金刚空中楼阁, f153 不该虚高(应≤0.2), got %.3f", f[153])
	}
}

// Fix1 — 用户例子: 中已成两对, 再往上升(葫芦/金刚)要摸特定牌概率低 → f153 应低 (AI 才肯把废牌放中, 不囤中路发育).
func TestSp37_F153_MadeTwoPairLowHeadroom(t *testing.T) {
	// 中 4477 两对(4空? 满4张1空), 底成顺(强天花板不挡), 顶留. 两对→葫芦 要摸4/7成三条(概率不高).
	gs := makeStateV3(t, []string{"5s"}, []string{"4s", "4c", "7d", "7s"}, []string{"9c", "Ts", "Jd", "Qh", "8h"})
	f := BuildFeaturesV3(gs)
	if f[153] > 0.35 { // 两对已成手, 升葫芦概率有限 → headroom 应偏低(别虚高诱导AI囤中)
		t.Errorf("中4477两对升葫芦概率有限, f153 不该虚高(应≤0.35), got %.3f", f[153])
	}
}

// 回归 — BuildFeatures(165) 必须走 V3 截断分支, 不能掉进 V2 兜底 (2026-07-03 review 抓到:
//   dim bump 165→168 后漏加 165 截断 → sp36/sp33 的 165-d ckpt 会静默拿 V2 特征 pad, gen rollout policy 全废).
func TestSp37_BuildFeatures165_TruncatesV3(t *testing.T) {
	gs := makeStateV3(t, []string{"Ac", "Ad"}, []string{"5h", "5c", "9d"}, []string{"Kc", "Kd", "7s"})
	full := BuildFeaturesV3(gs)
	trunc := BuildFeatures(gs, 165)
	if len(trunc) != 165 {
		t.Fatalf("BuildFeatures(165) 长度应165, got %d", len(trunc))
	}
	for i := range trunc {
		if trunc[i] != full[i] {
			t.Fatalf("BuildFeatures(165)[%d]=%v ≠ BuildFeaturesV3[%d]=%v — 掉进 V2 兜底了?", i, trunc[i], i, full[i])
		}
	}
}

// Fix3 — tripsRankRow: 区分 555 vs 333 (#90). joker-aware.
func TestSp37_TripsRankRow(t *testing.T) {
	// 5 5 3 4 + 鬼 → 555 (rank 5 = idx3)
	if got := tripsRankRow(roMk("5s", "X", "3c", "4s", "5c")); got != 3 {
		t.Errorf("{5,5,3,4,X} 应 555 (idx3), got %d", got)
	}
	// 3 3 4 5 + 鬼 → 333 (5只1张凑不成, rank 3 = idx1)
	if got := tripsRankRow(roMk("3c", "3s", "4s", "5s", "X")); got != 1 {
		t.Errorf("{3,3,4,5,X} 应 333 (idx1), got %d", got)
	}
	// 无三条 → -1
	if got := tripsRankRow(roMk("2c", "5c", "9d")); got != -1 {
		t.Errorf("{2,5,9} 无三条应 -1, got %d", got)
	}
}

// Fix3 — fillTripsRank 接线: 中555 的 f166 > 中333, 且底trips rank raw(f167).
func TestSp37_FillTripsRank_Wired(t *testing.T) {
	// 中 555 (底强不 foul, 中 trips rank gate≈不打折) — 用 R? 直接摆好
	g555 := makeStateV3(t, nil, []string{"5s", "5c", "5d", "3c", "4s"}, []string{"9c", "9d", "9h", "Kc", "Qc"})
	g333 := makeStateV3(t, nil, []string{"3s", "3c", "3d", "5c", "4s"}, []string{"9c", "9d", "9h", "Kc", "Qc"})
	f5 := BuildFeaturesV3(g555)
	f3 := BuildFeaturesV3(g333)
	if !(f5[166] > f3[166]) { // f166 = 中 trips rank
		t.Errorf("中555 的 f166 应 > 中333, got 555=%.3f 333=%.3f", f5[166], f3[166])
	}
	if math.Abs(float64(f5[167]-float32(7)/12.0)) > 0.01 { // f167 = 底 trips rank, 底999=idx7
		t.Errorf("底999 三条 f167 应=7/12, got %.3f", f5[167])
	}
}
