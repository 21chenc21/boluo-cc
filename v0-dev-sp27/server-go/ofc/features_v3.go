package ofc

import (
	"math"
	"sort"
)

// features_v3.go — 147-d feature extractor.
// 2026-05-19 v2 加 Tier 1+2+3 (L/LR/N2 共 16 新 dim): 131 → 147.
//
// 设计:
//   - V2 删 C/F/H/I/K/L (65 dim) → 替成 V3 X/F/Y/Z/U/V/T/C/R5/Q/M/S/N (62 dim)
//   - 2026-05-19 补回 V2 L 组 (Tier 1, 6 dim) + locked-royalty/4-draw/pair-kicker (Tier 2, 8 dim) +
//     discard 真实信号 (Tier 3, N=2 dim 改真值 + N2 加 2 新 dim)
//   - 设计详见 features_v3_design.md.
//
// idx 布局:
//   A:    0-7    棋盘状态 (V2 保留)
//   B:    8-31   各行手牌等级 (V2 保留)
//   D:   32-39   鬼牌全局状态 (V2 保留)
//   E:   40-51   各行花色分布 (V2 保留)
//   G:   52-68   牌堆剩余感知 (V2 保留)
//   X:   69-89   各行成牌概率 (V3 新, 21 dim, 含 P(foul))
//   F:   90-93   Fantasy 粒度概率 QQ/KK/AA/trips (V3 新)
//   Y:   94-96   各行期望分 (V3 新)
//   Z:   97-101  高层 summary (V3 新, Z0 用 calibrated bonus)
//   U:  102-106  各行对子 rank (V3 新)
//   V:  107-111  对升 trips 条件概率 (V3 新)
//   T:  112-115  顶配 fantasy 锁信号 (V3 新)
//   C:  116-118  各行最大可达手型 (V3 新)
//   R5: 119-120  末轮强制信号 (V3 新)
//   Q:  121-124  路径承诺 (V3 新)
//   M:  125-127  各行 foul margin (V3 新)
//   S:  128      槽位平衡 (V3 新)
//   N:  129-130  弃牌主信号 — rank + premium-flag (Tier 3, R2-R5 only, R1 全 0)
//   L:  131-136  跨行 anti-pattern (Tier 1, 复用 V2 fillCrossRowSplits): pairs_split / flushgroup_split /
//                connectors_split / kicker_order / gap1_orphan / mid_minus_bot_fill_ratio
//   LR: 137-144  Tier 2: bot/mid locked-royalty tier + bot/mid 4-flush + bot 4-straight open/gutshot +
//                mid 4-straight combined + pair_kicker_rank_max
//   N2: 145-146  弃牌副信号 (Tier 3 EXTRA): break_bot_suit_commitment / break_connector
//   O:  147-149  成手行序 (2026-06-14 sp26 追加, partial+rank-aware): 中-顶 / 底-中 / 最紧 margin.
//                负 = "强成手压上层"倒置. 太子(147-d)不含此组, warm-start auto-pad 0.

// 2026-06-14 sp26: 147 → 150, 追加"成手行序" O 组 (dim147-149). 太子(147-d) warm-start 自动 pad 0.
// 2026-06-19 sp28: 150 → 154, 追加"前瞻支撑夹缝" W 组 (dim150-153). 旧 ckpt warm-start 自动 pad 0.
// 2026-06-21 sp28: 154 → 157, 追加"死牌/放牌效率" Z 组 (dim154-156, 每行死牌数). 追加法 warm-start pad 0.
// 2026-06-21 sp28: 157 → 160, 追加"draw 强度" Z2 组 (dim157-159, 每行顺/花potential). 追加法 pad 0.
// 2026-06-21 sp28: 160 → 161, 追加"顶trips范种子合法性" Z3 组 (dim160). 追加法 pad 0.
// 2026-06-25 sp29: 161 → 163, 追加"顺draw紧密度质量" B 组 (dim161-162, 中/底). 追加法 pad 0. (#122卡顺细化)
// 2026-06-29 sp32: 164 → 165, 追加"强成手放错行" W2 组 (dim164). 追加法 pad 0. (#23/#24 中KK>底QQ倒置)
const FeatureDimV3 = 165

// FeatureDimV3Base — 成手行序前的 147-d layout. 太子 ckpt 是此 dim; gen 用太子当 rollout 时建 147-d 特征.
const FeatureDimV3Base = 147

// Fantasy bonus calibration (与训练 label 对齐, 见 design doc).
// 2026-06-12: const → var, 可被 ckpt 里的 fanBonus* 字段覆盖 (见 SetFanBonusScale / loadWeightsFromBytes).
// ⚠️ 这 5 个值进 feature dim-0 (fillSummary), 训练和推理都用 → 必须 train↔serve 对齐.
// ckpt 无字段 (如太子) → 回退 default* → 行为与改前完全一致 (向后兼容).
const (
	defaultV3FanBonusQQ    float32 = 20.0
	defaultV3FanBonusKK    float32 = 40.0
	defaultV3FanBonusAA    float32 = 100.0 // 2026-05-20: 80 → 100 reward shaping (太子 scale)
	defaultV3FanBonusTrips float32 = 120.0 // 2026-05-20: 90 → 120
	defaultV3FoulCost      float32 = 6.0   // 用户设计: foul-cost 低 = 鼓励 fan-chase
)

var (
	V3FanBonusQQ    = defaultV3FanBonusQQ
	V3FanBonusKK    = defaultV3FanBonusKK
	V3FanBonusAA    = defaultV3FanBonusAA
	V3FanBonusTrips = defaultV3FanBonusTrips
	V3FoulCost      = defaultV3FoulCost
)

// SetFanBonusScale — 用 ckpt 里的值覆盖 fan-bonus scale. 每次加载全量设置:
// 非 nil → 用 ckpt 值; nil (ckpt 无此字段) → 回退 default (防上一个 ckpt 的 scale 残留).
func SetFanBonusScale(qq, kk, aa, trips, foulCost *float64) {
	set := func(dst *float32, v *float64, def float32) {
		if v != nil {
			*dst = float32(*v)
		} else {
			*dst = def
		}
	}
	set(&V3FanBonusQQ, qq, defaultV3FanBonusQQ)
	set(&V3FanBonusKK, kk, defaultV3FanBonusKK)
	set(&V3FanBonusAA, aa, defaultV3FanBonusAA)
	set(&V3FanBonusTrips, trips, defaultV3FanBonusTrips)
	set(&V3FoulCost, foulCost, defaultV3FoulCost)
}

// BuildFeaturesV3 — 主入口. 输入 post-placement state, 返回 131-d feature.
func BuildFeaturesV3(gs *GameState) []float32 {
	if zeroUsedCardsAblation { // 实验: usedCards 只留 board (去 deck 感知). 见 usedcards_ablation.go
		gs = gsBoardOnlyUsed(gs)
	}
	f := make([]float32, FeatureDimV3)

	// 预算共享 eval (跟 V2 一致)
	// 2026-05-22 critical fix: cap 只在 cap 行满时有效, 否则 partial highcard 假 cap 错砍 top KK/AA.
	// case 55 真凶: mid [5h] partial highcard 把 top [2c X Kh] KK pair 错认为 over-cap → KK 失活, NN feature 全错.
	botEval := evalRowSafe(gs.Bottom, 5, nil)
	var midCap *HandValue
	if len(gs.Bottom) == 5 {
		midCap = &botEval
	}
	midEval := evalRowSafe(gs.Middle, 5, midCap)
	var topCap *HandValue
	if len(gs.Middle) == 5 {
		topCap = &midEval
	}
	topEvalCapped := evalRowSafe(gs.Top, 3, topCap)

	// 预算 deck remaining (X/F/V/T/C 共用)
	rankRem, suitRem, jokerRem := computeDeckRemaining(gs)
	deckTotal := jokerRem
	for r := 0; r < 13; r++ {
		deckTotal += rankRem[r]
	}

	// V2 保留 (5 组, 69 dim)
	fillBoardState(f[0:8], gs)
	fillHandTiers(f[8:32], gs, topEvalCapped, midEval, botEval)
	fillJokerState(f[32:40], gs, topEvalCapped, midEval, botEval)
	fillSuitDist(f[40:52], gs)
	fillDeckAware(f[52:69], gs)

	// V3 新 (12 组)
	fillProbabilities(f[69:90], gs, rankRem, suitRem, jokerRem, deckTotal, topEvalCapped, midEval, botEval)
	fillFantasyGranular(f[90:94], gs, midEval, rankRem, jokerRem, deckTotal) // sp16: 传 midEval 做 cap
	fillExpectedRoyalty(f[94:97], gs, rankRem, suitRem, jokerRem, deckTotal, topEvalCapped, midEval, botEval)
	fillSummary(f[97:102], gs, topEvalCapped, midEval, botEval, f) // 用前面的 P 值
	fillPairRank(f[102:107], gs, topEvalCapped)
	fillPairToTrips(f[107:112], gs, rankRem, suitRem, jokerRem, deckTotal)
	fillTopFantasyLocks(f[112:116], gs, topEvalCapped, midEval, rankRem, jokerRem)
	fillMaxAchievable(f[116:119], gs, rankRem, suitRem, jokerRem)
	fillLastRound(f[119:121], gs)
	fillCommitment(f[121:125], gs)
	fillFoulMargin(f[125:128], gs, topEvalCapped, midEval, botEval)
	fillSlotBalance(f[128:129], gs)
	fillDiscard(f[129:131], gs) // 2026-05-19: 用 gs.LastDiscard, R1 / 无 discard 时 0

	// 2026-05-19 Tier 1+2+3 新增 (16 dim, idx 131-146)
	fillCrossRowSplits(f[131:137], gs) // L: V2 复用, 6 dim
	fillLockedAndDraws(f[137:145], gs) // LR: 8 dim
	fillDiscardExtra(f[145:147], gs)   // N2: 2 dim, 弃牌副信号

	// 2026-06-03: dim130 (N_disc "本轮弃了 Q/K/A" 二元标志) 固化清零 — 净负特征.
	// value head 训练时学到 "弃高牌≈好局面" 的伪相关, 在"弃 Q 留 J 做成中道花"类局面
	// 反噬: 错把"弃 Qd 留 Jd"的 value 抬 ~1.5 分, 盖过 Qd 留顶追范优势 (ypk-178127178-8 R4 / 实战 9).
	// 实测 mask dim130 → 实战 9 修正 (Qd 头) 且 std 63 零回退 (61/2w/0f).
	// (注: 只清 dim130; dim129 弃牌 rank 连续值在别处有用, 一起清会掉 1 个 std case.)
	// fanProb head 本就正确偏 Qd, 但 DefaultMultiHeadCfg.FanBoost=0 没接线, 救不了 value head —— 故从特征侧治本.
	f[130] = 0

	// 2026-06-14 sp26 追加: O 组 成手行序 (3 dim, idx 147-149). 治"强成手压上层"倒置.
	fillMadeRowOrder(f[147:150], gs)

	// 2026-06-19 sp28 追加: W 组 前瞻支撑夹缝 (4 dim, idx 150-153). 治"上行承诺要下行前瞻发育托住"gap.
	fillSupportHeadroom(f[150:154], gs, rankRem, suitRem, jokerRem)

	// 2026-06-21 sp28 追加: Z 组 死牌/放牌效率 (3 dim, idx 154-156). 治"低估顺/不浪费牌/不污染强行"主线.
	fillWastedCards(f[154:157], gs)

	// 2026-06-21 sp28 追加: Z2 组 draw 强度 (3 dim, idx 157-159). 显著 count-based 顺/花 potential 信号.
	fillDrawStrength(f[157:160], gs)

	// 2026-06-21 sp28 追加: Z3 组 顶trips范种子合法性 (1 dim, idx 160). 治48(合法trips范种子)+46(顶trips倒置).
	fillTopTripsSeed(f[160:161], gs, rankRem, suitRem, jokerRem)

	// 2026-06-25 sp29 追加: B 组 顺draw紧密度质量 (2 dim, idx 161-162: 中/底). 细化"是顺就满分"→按紧密度(1/tier). 治#122卡顺.
	fillStraightTightness(f[161:163], gs)

	// 2026-06-25 sp29 追加: C 组 进范可行性 (1 dim, idx 163). 复用规则层 FantasyLost/fantasyOnlyViaFoul
	//   (hard_rules.go, 与 prod sp25featfix 同源). F组只算"顶摸QQ+概率"行序盲 → 锁低成手/堵行序杀范时给假范信号.
	//   此维 = !(范死 || 假范) → NN 学会"F组范概率只在此=1时算数". 治#116(底2s6h锁66/22杀范被假范骗).
	fillFantasyReachable(f[163:164], gs)

	// 2026-06-29 W2 组: 强成手放错行 (dim164). 治 #23/#24 中KK>底QQ 倒置. 追加法 warm-start pad 0.
	fillStrongHandMisplaced(f[164:165], gs)

	return f
}

// fillFantasyReachable — 进范可行性 (C组, dim163). 1=范foul-free可达, 0=已失范或只能靠foul(假范).
//   复用 hard_rules.go 的 FantasyLost(逐行独立) + fantasyOnlyViaFoul(中→底链). 见 #116.
func fillFantasyReachable(f []float32, gs *GameState) {
	if FantasyLost(gs) || fantasyOnlyViaFoul(gs) {
		f[0] = 0
	} else {
		f[0] = 1
	}
}

// ============ Group X: 各行成牌概率 (21 dim, idx 69-89) ============
// 用 hypergeometric 闭式 + 简化估算.

func fillProbabilities(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal int,
	topEv, midEv, botEv HandValue) {

	topSlots := 3 - len(gs.Top)
	midSlots := 5 - len(gs.Middle)
	botSlots := 5 - len(gs.Bottom)
	cs := cardsSeenRemaining(gs)

	// Top (3 dim, idx 0-2 in group X)
	f[0] = pTopPairQKA(gs, rankRem, jokerRem, deckTotal, topSlots)
	f[1] = pTopTrips(gs, rankRem, jokerRem, deckTotal, topSlots)
	f[2] = pTopNoFoulVsMid(topEv, midEv)

	// Mid (8 dim, idx 3-10)
	f[3] = pRowAtLeast(gs.Middle, TypePair, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	f[4] = pRowAtLeast(gs.Middle, TypeTwoPair, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	f[5] = pRowAtLeast(gs.Middle, TypeThreeOfAKind, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	f[6] = pRowStraight(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs)
	f[7] = pRowFlush(gs.Middle, suitRem, jokerRem, deckTotal, midSlots, cs)
	f[8] = pRowAtLeast(gs.Middle, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	f[9] = pRowAtLeast(gs.Middle, TypePair, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs) // 累加 ≥ pair, 同 X3
	f[10] = pMidGTBot(gs, midEv, botEv, rankRem, suitRem, jokerRem, deckTotal, midSlots, botSlots)

	// Bot (9 dim, idx 11-19)
	f[11] = pRowAtLeast(gs.Bottom, TypePair, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[12] = pRowAtLeast(gs.Bottom, TypeTwoPair, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[13] = pRowAtLeast(gs.Bottom, TypeThreeOfAKind, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[14] = pRowStraight(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
	f[15] = pRowFlush(gs.Bottom, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[16] = pRowAtLeast(gs.Bottom, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[17] = pRowAtLeast(gs.Bottom, TypeFourOfAKind, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	f[18] = pRowGEPair10(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
	f[19] = pBotGEMid(gs, midEv, botEv, rankRem, suitRem, jokerRem, deckTotal, midSlots, botSlots)

	// Foul global (1 dim, idx 20)
	f[20] = pFoulFinal(gs, topEv, midEv, botEv, rankRem, suitRem, jokerRem, deckTotal)
}

// ============ Group F: Fantasy 粒度概率 (4 dim, idx 90-93) ============

// 2026-05-20 sp16.1: cap-aware 但只在 mid full pair 时锁 (mid partial 可升 trips/FH).
// case 50: mid full pair-K → AA top cap → P=0 ✓
// 反例: mid 4 张 pair-K partial → 可能升 trips → P(AA) 不该置 0
func fillFantasyGranular(f []float32, gs *GameState, midEv HandValue, rankRem [13]int, jokerRem, deckTotal int) {
	topSlots := 3 - len(gs.Top)
	// 只有 mid full + pair 才锁 (partial 可升级类型)
	midFull := len(gs.Middle) == 5
	midPairCap := -1
	if midFull && midEv.Type == TypePair {
		midPairCap = int((midEv.Value - 1000000) / 50625)
	}
	capRank := func(r uint8) bool {
		return midPairCap >= 0 && int(r) > midPairCap
	}
	if capRank(RankQ) {
		f[0] = 0
	} else {
		f[0] = pTopFinalPairExact(gs, RankQ, rankRem, jokerRem, deckTotal, topSlots)
	}
	if capRank(RankK) {
		f[1] = 0
	} else {
		f[1] = pTopFinalPairExact(gs, RankK, rankRem, jokerRem, deckTotal, topSlots)
	}
	if capRank(RankA) {
		f[2] = 0
	} else {
		f[2] = pTopFinalPairExact(gs, RankA, rankRem, jokerRem, deckTotal, topSlots)
	}
	// Trips: top trips type=3 > mid pair type=1 必 foul, 仅 mid full pair 时锁.
	if midPairCap >= 0 {
		f[3] = 0
	} else {
		f[3] = pTopTrips(gs, rankRem, jokerRem, deckTotal, topSlots)
	}
}

// ============ Group Y: 各行期望分 (3 dim, idx 94-96) ============
// E[royalty per row], 不含 fantasy bonus.

func fillExpectedRoyalty(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal int,
	topEv, midEv, botEv HandValue) {

	topSlots := 3 - len(gs.Top)
	midSlots := 5 - len(gs.Middle)
	botSlots := 5 - len(gs.Bottom)

	f[0] = eRoyaltyTop(gs, topEv, rankRem, jokerRem, deckTotal, topSlots) / 250.0 // sp16: cap-aware
	f[1] = eRoyaltyMid(gs, rankRem, suitRem, jokerRem, deckTotal, midSlots) / 30.0
	f[2] = eRoyaltyBot(gs, rankRem, suitRem, jokerRem, deckTotal, botSlots) / 60.0
}

// ============ Group Z: 高层 summary (5 dim, idx 97-101) ============
// 用前面的 X/F/Y 计算.

func fillSummary(f []float32, gs *GameState, topEv, midEv, botEv HandValue, all []float32) {
	// 从 f 数组中取出 X/F/Y 的值 (idx 在 all 全数组中)
	// X20 = all[89], F0-F3 = all[90-93], Y0-Y2 = all[94-96]
	y0 := all[94] * 250.0
	y1 := all[95] * 30.0
	y2 := all[96] * 60.0
	f0Bonus := all[90]*V3FanBonusQQ + all[91]*V3FanBonusKK + all[92]*V3FanBonusAA + all[93]*V3FanBonusTrips
	pFoul := all[89]

	// 2026-05-20: fantasy bonus 跟 royalty 都 conditional on 不 foul.
	// 旧版 finalScore = royalty + fan_bonus - foul_cost × pFoul 把 fan 当独立加, 严重 over-estimate
	// "high fan + high foul" 的 EV. 例 NN 选 mid-加牌 → P(AA)=0.7 P(foul)=0.5,
	// 旧 net = 80×0.7 - 6×0.5 = +53. 实际 (royalty+80×0.7) × 0.5 - 6 × 0.5 = +25.
	// 改: expected_score = (royalty + fan_bonus) × (1 - pFoul) + (-foul_cost) × pFoul
	nonFoulValue := y0 + y1 + y2 + f0Bonus
	finalScore := nonFoulValue*(1-pFoul) - V3FoulCost*pFoul
	f[0] = clampF(finalScore/300.0, -1, 1) // normalize, max ~300

	f[1] = float32(topEv.Type) / 3.0
	f[2] = float32(midEv.Type) / 9.0
	f[3] = float32(botEv.Type) / 9.0

	slotsTotal := (3 - len(gs.Top)) + (5 - len(gs.Middle)) + (5 - len(gs.Bottom))
	f[4] = float32(slotsTotal) / 13.0
}

// ============ Group U: 各行对子 rank (5 dim, idx 102-106) ============

// 2026-05-20 sp16: top 用 cap-aware topEv 提 pair rank (joker+A 被 mid pair-K cap 时反映 pair-2 真值)
// case 50 R5 教训: AI top=[X 2c As] vs mid KK → joker cap → top 实际 pair-2 (非 AA)
func fillPairRank(f []float32, gs *GameState, topEv HandValue) {
	pairToFeat := func(r int) float32 {
		if r < 0 {
			return -1.0
		}
		return float32(r) / 12.0
	}
	// top: 从 cap-aware topEv 提取 (joker 已被 cap 限制)
	topPair := -1
	if topEv.Type == TypePair {
		topPair = int((topEv.Value - 1000000) / 15)
	}
	// 2026-06-29 (#110): 顶有鬼且只配出 sub-QQ 低对(或无对) = 范种子, 不是低对. 鬼该当种子(价值在 f160/f163),
	//   别钉成低对(鬼配77=0.417)、更别把孤鬼种子当 -1 最差 → 中性 0. (真低对/QQ+鬼对/无鬼无对 走原逻辑.)
	topJokers := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			topJokers++
		}
	}
	if topJokers >= 1 && topPair < int(RankQ) {
		f[0] = 0
	} else {
		f[0] = pairToFeat(topPair)
	}
	f[1] = pairToFeat(maxPairRankRow(gs.Middle))
	f[2] = pairToFeat(maxPairRankRow(gs.Bottom))
	f[3] = pairToFeat(twoPairHighRank(gs.Middle))
	f[4] = pairToFeat(twoPairHighRank(gs.Bottom))
}

// ============ Group V: 对升 trips 条件概率 (5 dim, idx 107-111) ============

func fillPairToTrips(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal int) {
	cs := cardsSeenRemaining(gs)
	topSlots := 3 - len(gs.Top)
	midSlots := 5 - len(gs.Middle)
	botSlots := 5 - len(gs.Bottom)

	// foul cap (#124): 顶升trips须≤中max, 中须≤底max, 底无上限(999).
	botMax := maxAchievableCmpCapped(gs.Bottom, botSlots, rankRem, suitRem, jokerRem, 999)
	midMax := maxAchievableCmpCapped(gs.Middle, midSlots, rankRem, suitRem, jokerRem, botMax)
	f[0] = pPairToTrips(gs.Top, rankRem, jokerRem, deckTotal, topSlots, cs, midMax)
	f[1] = pPairToTrips(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs, botMax)
	f[2] = pPairToTrips(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs, 999)
	f[3] = p2PairToFH(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs)
	f[4] = p2PairToFH(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
}

// ============ Group T: 顶配 fantasy 锁信号 (4 dim, idx 112-115) ============

// 2026-05-20 sp15 fix: 用 cap-aware topEvalCapped 提取 pair rank, 不再 maxPairRankRow (无 cap)
// case 50 R5 教训: top=[X 2c As] + mid KK pair → joker cap 后 top 是 pair-2 不是 AA.
// 旧版 maxPairRankRow 算 joker+A=AA → T_topLock[0]+[1] 都 1.0 → NN 误信 fantasy 实=+20 over-est.
func fillTopFantasyLocks(f []float32, gs *GameState, topEv, midEv HandValue, rankRem [13]int, jokerRem int) {
	// 从 cap-aware topEv 提 pair rank (joker cap 后真值)
	topPairRank := -1
	if topEv.Type == TypePair {
		topPairRank = int((topEv.Value - 1000000) / 15)
	}
	// trips 仍用 raw (joker 凑 trips 罕见, cap chain 对空 mid 可能未返 Type=trips)
	hasTrips := hasRealTripsTop(gs.Top)

	if topPairRank >= int(RankQ) {
		f[0] = 1 // pair Q+ (cap-aware)
	}
	if topPairRank == int(RankA) {
		f[1] = 1 // AA (cap-aware, 不会因 joker+A 被 mid pair-K cap 时误 fire)
	}
	if hasTrips {
		f[2] = 1
	}

	// T3: max pair rank reachable (future), 考虑 mid cap
	midCap := -1 // 若 mid 是 pair, 把 cap rank 设为 mid pair rank
	if midEv.Type == TypePair {
		midCap = int((midEv.Value - 1000000) / 50625)
	} else if midEv.Type > TypePair {
		// mid 是 trips+ → top pair 永远不会 ≥ mid, cap 不限制 top pair rank (理论上 AA pair < trips)
		// 但 fantasy 是 pair-based, 这里只关心 pair lock 信号, 保持不限.
		midCap = -1
	}
	maxReach := topPairRank
	for r := 12; r >= 0; r-- {
		// 跳过会被 mid cap 阻挡的 pair rank
		if midCap >= 0 && r > midCap {
			continue
		}
		topHasR := 0
		for _, c := range gs.Top {
			if !c.IsJoker() && int(c.Rank()) == r {
				topHasR++
			}
		}
		if topHasR+rankRem[r]+jokerRem >= 2 && r > maxReach {
			maxReach = r
		}
	}
	if maxReach < 0 {
		f[3] = -1.0
	} else {
		f[3] = float32(maxReach) / 12.0
	}
}

// ============ Group C: 各行最大可达手型 (3 dim, idx 116-118) ============

func fillMaxAchievable(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) {
	topMax := maxAchievableHandType(gs.Top, 3-len(gs.Top), rankRem, suitRem, jokerRem)
	midMax := maxAchievableHandType(gs.Middle, 5-len(gs.Middle), rankRem, suitRem, jokerRem)
	botMax := maxAchievableHandType(gs.Bottom, 5-len(gs.Bottom), rankRem, suitRem, jokerRem)
	// Top max: HighCard / Pair / ThreeKind → 0/1/3 (skip 2pair which can't on 3 card top)
	// Normalize / 3
	f[0] = float32(topMax) / 3.0
	// Mid / Bot: HighCard...SF / 0-9
	f[1] = float32(midMax) / 9.0
	f[2] = float32(botMax) / 9.0
}

// ============ Group R5: 末轮强制信号 (2 dim, idx 119-120) ============

func fillLastRound(f []float32, gs *GameState) {
	if gs.Round == 5 {
		f[0] = 1
	}
	// forced count: 末轮还需必填几张 (R5 摆 2)
	slotsTotal := (3 - len(gs.Top)) + (5 - len(gs.Middle)) + (5 - len(gs.Bottom))
	f[1] = float32(slotsTotal) / 3.0 // R5 一般剩 3 总槽 (1 顶 + ?)
}

// ============ Group Q: 路径承诺 (4 dim, idx 121-124) ============

func fillCommitment(f []float32, gs *GameState) {
	// Q0: bot 主色张数 / 5
	maxBotSuit := 0
	suitCnt := make(map[uint8]int)
	for _, c := range gs.Bottom {
		if !c.IsJoker() {
			suitCnt[c.Suit()]++
		}
	}
	for _, n := range suitCnt {
		if n > maxBotSuit {
			maxBotSuit = n
		}
	}
	f[0] = float32(maxBotSuit) / 5.0

	// Q1: bot 连号最长子串 / 5
	f[1] = float32(consecutiveRunMax(gs.Bottom)) / 5.0

	// Q2: mid 当前最大 same-rank count / 5
	midMaxRank := 0
	midRankCnt := make(map[uint8]int)
	for _, c := range gs.Middle {
		if !c.IsJoker() {
			midRankCnt[c.Rank()]++
		}
	}
	for _, n := range midRankCnt {
		if n > midMaxRank {
			midMaxRank = n
		}
	}
	f[2] = float32(midMaxRank) / 5.0

	// Q3: top 已 Q+ 张数 / 3
	qPlus := 0
	for _, c := range gs.Top {
		if c.IsJoker() || int(c.Rank()) >= int(RankQ) {
			qPlus++
		}
	}
	f[3] = float32(qPlus) / 3.0
}

// ============ Group M: 各行 foul margin (3 dim, idx 125-127) ============
// 用 RAW eval (Evaluate3/5, 不带 cap) 计算 — 避免 cap chain 把 mid > bot 改成 Type=-2

// rowMadeScore — 行成手强度标量 (type*13 + 主rank), joker-aware + partial-aware.
// "成手行序"特征用: 让 value head 看见各行*当前成手*的强弱序, 抓"强成手压在上层"的倒置.
// partialEvalTP: 满行 → Evaluate5JokerCap (含 flush/straight); partial → made-floor (joker 补三条/对).
func rowMadeScore(row []Card) int {
	hv := partialEvalTP(row)
	var cnt [13]int
	for _, c := range row {
		if !c.IsJoker() {
			cnt[c.Rank()]++
		}
	}
	prim := -1
	for r := 12; r >= 0; r-- { // 最高真对/三条/四条 rank
		if cnt[r] >= 2 {
			prim = r
			break
		}
	}
	if prim < 0 { // 无对 → 最高真单张
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 1 {
				prim = r
				break
			}
		}
	}
	if prim < 0 {
		prim = 0 // 全鬼 / 空行
	}
	return int(hv.Type)*13 + prim
}

// fillFoulMargin — 原版 foul margin (满行 + type-only), dim125-127. 太子按此训, 不改 (零扰动).
func fillFoulMargin(f []float32, gs *GameState, topEv, midEv, botEv HandValue) {
	// 重算 raw eval (无 cap), 完整 row 才算
	topRaw := safeRawEvalTop(gs.Top)
	midRaw := safeRawEvalRow(gs.Middle, 5)
	botRaw := safeRawEvalRow(gs.Bottom, 5)

	dMidTop := float32(midRaw.Type-topRaw.Type) / 9.0
	dBotMid := float32(botRaw.Type-midRaw.Type) / 9.0
	f[0] = clampF(dMidTop, -1, 1)
	f[1] = clampF(dBotMid, -1, 1)
	minM := dMidTop
	if dBotMid < minM {
		minM = dBotMid
	}
	f[2] = clampF(minM, -1, 1)
}

// fillMadeRowOrder — 成手行序 (新 dim147-149, 追加法 warm-start pad=0 零扰动).
// 治 value head "强成手压上层"倒置 (大对爱放中道 / 89X三条不进底). partial+rank-aware,
// 补 fillFoulMargin 两 gap: ① 满行才算 ② type-only (中KK对 vs 底44对 都 Pair → margin 0).
// 归一化 /13 = 一个 type 级; 同type rank 差 (KK vs 44 = 9/13≈0.7) 也体现.
func fillMadeRowOrder(f []float32, gs *GameState) {
	topS := rowMadeScore(gs.Top)
	midS := rowMadeScore(gs.Middle)
	botS := rowMadeScore(gs.Bottom)
	f[0] = clampF(float32(midS-topS)/13.0, -1, 1) // 中-顶 行序 (正常 ≥0)
	f[1] = clampF(float32(botS-midS)/13.0, -1, 1) // 底-中 行序 (负 = 中>底倒置, 主 bias)
	minM := f[0]
	if f[1] < minM {
		minM = f[1]
	}
	f[2] = minM // 最紧行序 margin (负 = 某处倒置)
}

// safeRawEvalTop — 3-card top raw eval
func safeRawEvalTop(cards []Card) HandValue {
	if len(cards) == 3 {
		return Evaluate3(cards)
	}
	return partialEval(cards)
}

// safeRawEvalRow — 5-card row raw eval (无 cap)
func safeRawEvalRow(cards []Card, expectSize int) HandValue {
	if len(cards) == expectSize && expectSize == 5 {
		return Evaluate5(cards)
	}
	return partialEval(cards)
}

// ============ Group S: 槽位平衡 (1 dim, idx 128) ============

// 2026-05-20 sp15: S_slot 改 scale 0.3 全场 (R1-R5).
// 原 1.0 min/max 在 R2-R3 太强反 OFC 直觉 (case 34: 底 4-flush 被压);
// 直接 R<4 设 0 又过激 (实测 baseline 退 15 点, NN 学的 w≈12.7 信号丢光).
// 折中: 全场 scale 0.3, 信号方向保留 (NN 还能用), 强度减 70% (防止单 feature 主导决策).
func fillSlotBalance(f []float32, gs *GameState) {
	topR := 3 - len(gs.Top)
	midR := 5 - len(gs.Middle)
	botR := 5 - len(gs.Bottom)
	minR, maxR := topR, topR
	if midR < minR {
		minR = midR
	}
	if midR > maxR {
		maxR = midR
	}
	if botR < minR {
		minR = botR
	}
	if botR > maxR {
		maxR = botR
	}
	if maxR == 0 {
		f[0] = 0.3 // 完成 (scale 后)
	} else {
		f[0] = 0.3 * float32(minR) / float32(maxR)
	}
}

// ============ Group N: 弃牌主信号 (2 dim, idx 129-130) ============
// 2026-05-19 实现: 用 gs.LastDiscard / gs.HasLastDiscard.
// R1 (无弃牌) 或 caller 没设 → 全 0.
//
// N0: discarded_rank / 12 (0=2, 1=A, joker 算 RankA+1=14 → 1.0 clamp)
// N1: discard_premium_flag (Q+ 或 joker → 1, 表示烧高价值牌)

func fillDiscard(f []float32, gs *GameState) {
	if !gs.HasLastDiscard {
		return
	}
	d := gs.LastDiscard
	if d.IsJoker() {
		f[0] = 1.0
		f[1] = 1.0
		return
	}
	r := int(d.Rank())
	f[0] = float32(r) / 12.0
	if r >= int(RankQ) { // Q, K, A
		f[1] = 1.0
	}
}

// ============ Group LR: locked-royalty + 4-draw + pair-kicker (8 dim, idx 137-144) ============
// Tier 2 新增 (2026-05-19).
//
// LR0 (137): bot 锁定 royalty tier — Bottom 5 张已成 ≥ Straight (type≥4) → 1 ((type-3)/6 clamp 0-1)
// LR1 (138): mid 锁定 royalty tier — Middle 5 张已成 ≥ Trips (type≥3 给 royalty) → 1 ((type-2)/7 clamp)
// LR2 (139): bot 4-flush draw — bot 4 同色 1 空槽 → 1 else 0 (joker 算 wild)
// LR3 (140): bot 4-straight open-ended draw → 1 (8 outs)
// LR4 (141): bot 4-straight gutshot draw → 1 (4 outs)
// LR5 (142): mid 4-flush draw → 1
// LR6 (143): mid 4-straight any (open ∨ gutshot) → 1
// LR7 (144): max pair kicker rank (mid/bot, 不含 top) / 12 — 区分 pair 强弱

func fillLockedAndDraws(f []float32, gs *GameState) {
	// LR0/1: 锁定 tier
	if len(gs.Bottom) == 5 {
		ev := Evaluate5JokerCap(gs.Bottom, nil)
		t := int(ev.Type)
		if t >= int(TypeStraight) {
			f[0] = clampF(float32(t-int(TypeStraight)+1)/6.0, 0, 1) // straight→1/6, SF→6/6
		}
	}
	if len(gs.Middle) == 5 {
		bot := Evaluate5JokerCap(gs.Bottom, nil)
		ev := Evaluate5JokerCap(gs.Middle, &bot)
		t := int(ev.Type)
		if t >= int(TypeThreeOfAKind) {
			f[1] = clampF(float32(t-int(TypeThreeOfAKind)+1)/7.0, 0, 1) // trips→1/7, SF→7/7
		}
	}

	// LR2-4: bot draws
	if len(gs.Bottom) == 4 {
		if hasNFlushDraw(gs.Bottom, 4) {
			f[2] = 1
		}
		open, gut := classifyStraightDraw4(gs.Bottom)
		if open {
			f[3] = 1
		}
		if gut {
			f[4] = 1
		}
	}
	// LR5-6: mid draws
	if len(gs.Middle) == 4 {
		if hasNFlushDraw(gs.Middle, 4) {
			f[5] = 1
		}
		open, gut := classifyStraightDraw4(gs.Middle)
		if open || gut {
			f[6] = 1
		}
	}

	// LR7: max pair kicker rank (mid + bot, 取最高 kicker 的 rank)
	maxKicker := 0
	if k := maxPairKickerRank(gs.Middle); k > maxKicker {
		maxKicker = k
	}
	if k := maxPairKickerRank(gs.Bottom); k > maxKicker {
		maxKicker = k
	}
	f[7] = float32(maxKicker) / 12.0
}

// ============ Group N2: 弃牌副信号 (2 dim, idx 145-146) ============
// Tier 3 extra (2026-05-19).
//
// N2-0 (145): discard_breaks_bot_suit_commitment — 弃牌 suit 与 bot 主色 (≥3 张) 匹配 → 1
// N2-1 (146): discard_breaks_connector — 弃牌 rank N, 任一行有 N-1 或 N+1 → 1 (拆 connector)

func fillDiscardExtra(f []float32, gs *GameState) {
	if !gs.HasLastDiscard || gs.LastDiscard.IsJoker() {
		return // joker 不算 suit / connector 拆
	}
	d := gs.LastDiscard
	dSuit := d.Suit()
	dRank := int(d.Rank())

	// N2-0: bot 主色 ≥3 且弃牌同色
	suitCnt := [4]int{}
	for _, c := range gs.Bottom {
		if !c.IsJoker() {
			suitCnt[c.Suit()]++
		}
	}
	if suitCnt[dSuit] >= 3 {
		f[0] = 1
	}

	// N2-1: 任一行有 N-1 或 N+1
	checkRow := func(row []Card) bool {
		for _, c := range row {
			if c.IsJoker() {
				continue
			}
			r := int(c.Rank())
			if r == dRank-1 || r == dRank+1 {
				return true
			}
		}
		return false
	}
	if checkRow(gs.Top) || checkRow(gs.Middle) || checkRow(gs.Bottom) {
		f[1] = 1
	}
}

// hasNFlushDraw — row 是否有 ≥ n 张同色 (joker wild)
func hasNFlushDraw(row []Card, n int) bool {
	suitCnt := [4]int{}
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++
			continue
		}
		suitCnt[c.Suit()]++
	}
	for _, cnt := range suitCnt {
		if cnt+jokers >= n {
			return true
		}
	}
	return false
}

// classifyStraightDraw4 — 4 张是否构成 straight draw, open-ended 还是 gutshot.
// 简化: 找最长连续 rank 段 + 检查能不能 +1 张填顺子. open=两端可填, gutshot=只有内部空 (含 A-low).
func classifyStraightDraw4(row []Card) (open, gutshot bool) {
	if len(row) != 4 {
		return false, false
	}
	ranks := []int{}
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++
		} else {
			ranks = append(ranks, int(c.Rank()))
		}
	}
	if len(ranks) == 0 {
		return false, false
	}
	// sort + dedup
	rankSet := map[int]bool{}
	for _, r := range ranks {
		rankSet[r] = true
	}
	uniq := []int{}
	for r := range rankSet {
		uniq = append(uniq, r)
	}
	// sort uniq ascending
	for i := 0; i < len(uniq); i++ {
		for j := i + 1; j < len(uniq); j++ {
			if uniq[i] > uniq[j] {
				uniq[i], uniq[j] = uniq[j], uniq[i]
			}
		}
	}
	// 4 distinct ranks + 0 joker: 检 span
	if len(uniq) == 4 && jokers == 0 {
		span := uniq[3] - uniq[0]
		if span == 3 {
			// 4 张连续 — 两端可填 (low - 1 / high + 1, 不超 0/12)
			open = true
			return
		}
		if span == 4 {
			// 4 张, span 4 → 中间缺 1 张 (gutshot)
			gutshot = true
			return
		}
		// 含 A-low 特殊: A,2,3,4 (rank 12,0,1,2) 也是 4-straight gutshot to 5
		if uniq[0] == 0 && uniq[1] == 1 && uniq[2] == 2 && uniq[3] == 12 {
			gutshot = true
			return
		}
	}
	// 3 distinct + 1 joker: 用 joker 填任意空位 → open 或 gutshot
	if len(uniq) == 3 && jokers == 1 {
		span := uniq[2] - uniq[0]
		if span == 2 {
			// 3 连续 + joker 填两端 → open-ended
			open = true
			return
		}
		if span == 3 {
			// 3 距 span 3 缺 1 → joker 填中 → gutshot
			gutshot = true
			return
		}
		if span == 4 {
			// 3 距 span 4 缺 2 → joker 填 1, 还需 1 → 还是 gutshot
			gutshot = true
			return
		}
	}
	// 2 distinct + 2 joker: 一定能凑成 4-straight, 算 open
	if len(uniq) == 2 && jokers == 2 {
		open = true
		return
	}
	return false, false
}

// maxPairKickerRank — 一行内若有 pair, 返回 kicker (非 pair rank) 最大值, 否则 0.
// 中行 5 张: pair 占 2, kicker 3. 取 3 张 kicker 中最大.
// 不完整行返回 0.
func maxPairKickerRank(row []Card) int {
	if len(row) < 4 {
		return 0
	}
	rankCnt := [13]int{}
	for _, c := range row {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	// 找 pair rank (任意 ≥2)
	pairRank := -1
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 2 {
			pairRank = r
			break
		}
	}
	if pairRank < 0 {
		return 0
	}
	// kicker = max rank where rankCnt[r] >= 1 且 r != pairRank
	for r := 12; r >= 0; r-- {
		if r != pairRank && rankCnt[r] >= 1 {
			return r
		}
	}
	return 0
}

// ============================================================
// 概率计算辅助函数
// ============================================================

// hypergeoP — P(exactly k 成功 抽取 n from deckSize, deckSize 含 targetCount 个成功)
func hypergeoP(deckSize, targetCount, drawCount, k int) float32 {
	if drawCount < 0 || drawCount > deckSize || k > drawCount || k > targetCount {
		return 0
	}
	if deckSize == 0 {
		return 0
	}
	// P = C(target, k) * C(deckSize-target, drawCount-k) / C(deckSize, drawCount)
	num := logComb(targetCount, k) + logComb(deckSize-targetCount, drawCount-k)
	den := logComb(deckSize, drawCount)
	v := math.Exp(num - den)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return float32(v)
}

// cardsSeenRemaining — 整局剩余轮还能"看到"的发牌数 (每填2格发3张). 2026-06-25 选牌模型:
//   真实大菠萝不是被迫随机填 slots 个空位, 而是从这些发牌里"挑"目标牌放进行. 治"低估draw发育"总根.
func cardsSeenRemaining(gs *GameState) int {
	rem := 13 - (len(gs.Top) + len(gs.Middle) + len(gs.Bottom))
	if rem <= 0 {
		return 0
	}
	return rem * 3 / 2
}

// pDraw — P(行最终凑到目标): 从 cardsSeen 张发牌里挑 ≥k 张目标 (行只能放 slots, k>slots→0).
//   替代旧"被迫随机填 slots 个空位"的 hypergeoAtLeast(...,slots,k) (严重低估, 见 #117 花 5.7%→47.5%).
func pDraw(deckTotal, targetCount, cardsSeen, slots, k int) float32 {
	if k <= 0 {
		return 1
	}
	if k > slots { // 行放不下这么多, 不可能
		return 0
	}
	dc := cardsSeen
	if dc > deckTotal {
		dc = deckTotal
	}
	return hypergeoAtLeast(deckTotal, targetCount, dc, k)
}

// hypergeoAtLeast — P(≥ k 成功)
func hypergeoAtLeast(deckSize, targetCount, drawCount, k int) float32 {
	if k <= 0 {
		return 1
	}
	if k > drawCount {
		return 0
	}
	var sum float32
	for i := k; i <= drawCount; i++ {
		sum += hypergeoP(deckSize, targetCount, drawCount, i)
	}
	if sum > 1 {
		sum = 1
	}
	return sum
}

// logComb — log(C(n, k)) via log-gamma
func logComb(n, k int) float64 {
	if k < 0 || k > n || n < 0 {
		return math.Inf(-1)
	}
	if k == 0 || k == n {
		return 0
	}
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// ============================================================
// Per-row P() 计算
// ============================================================

// pTopPairQKA — P(top final pair Q/K/A 任一)
func pTopPairQKA(gs *GameState, rankRem [13]int, jokerRem, deckTotal, topSlots int) float32 {
	// 已经 pair Q+ ?
	if maxPairRankRow(gs.Top) >= int(RankQ) {
		return 1
	}
	if topSlots == 0 {
		return 0
	}
	// 用 inclusion-exclusion 对 Q/K/A 三选一
	// 简化: 各 rank 独立估算 + 取 max (不严格但快)
	maxP := float32(0)
	for _, r := range []int{int(RankQ), int(RankK), int(RankA)} {
		topHasR := countRankInRow(gs.Top, r)
		needed := 2 - topHasR - jokerRem // joker 能凑 1 张 (假设可用)
		if needed <= 0 {
			return 1
		}
		// P(从 deck 抽 topSlots 张, ≥ needed 张是 rank r)
		p := hypergeoAtLeast(deckTotal, rankRem[r], topSlots, needed)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// pTopFinalPairExact — P(top final 恰好是 pair_r, 不是 trips_r)
// jokerTopMadePairRank — 满顶含 joker 时 joker 最优完成的对子 rank (joker 配最高真单张).
// 已有真对(joker→三条) / 无 joker → 返回 -1 (非 exact joker-pair).
func jokerTopMadePairRank(top []Card) int {
	jokers := 0
	var cnt [13]int
	for _, c := range top {
		if c.IsJoker() {
			jokers++
		} else {
			cnt[c.Rank()]++
		}
	}
	if jokers == 0 {
		return -1
	}
	for _, n := range cnt {
		if n >= 2 {
			return -1 // 已真对, joker 升三条非 exact pair
		}
	}
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 1 {
			return r // joker 配最高真单张
		}
	}
	return -1
}

func pTopFinalPairExact(gs *GameState, r uint8, rankRem [13]int, jokerRem, deckTotal, topSlots int) float32 {
	// 简化: 假设 = P(top 至少 2 张 r) - P(top ≥ 3 张 r)
	topHasR := countRankInRow(gs.Top, int(r))
	// 2026-06-14 (uniform): 顶上鬼锁的对子 rank (鬼配最高真单张) 算进 topHasR — 鬼已成对锁范.
	// 修 F_fantasyGran 鬼锁 AA/KK 范误判: 满顶 [As Kc X]=AA (countRankInRow 不数鬼→原 0) +
	// partial 顶含鬼 [Ac X] (原 hypergeo 漏鬼→低估). 统一处理满顶/partial, 含升三条概率. ypk 89X.
	if jokerTopMadePairRank(gs.Top) == int(r) {
		topHasR++
	}
	deckR := rankRem[int(r)] + jokerRem // joker 可顶
	needFor2 := 2 - topHasR
	needFor3 := 3 - topHasR
	if needFor2 <= 0 {
		// 已 pair 或 trips
		if needFor3 <= 0 {
			return 0 // 已 trips, 不是 exact pair
		}
		// 已 pair, 看是否会升 trips
		pUp := hypergeoAtLeast(deckTotal, deckR, topSlots, needFor3)
		return 1 - pUp
	}
	if topSlots == 0 {
		return 0 // 满顶且未成对 → 非 exact pair
	}
	p2 := hypergeoAtLeast(deckTotal, deckR, topSlots, needFor2)
	p3 := hypergeoAtLeast(deckTotal, deckR, topSlots, needFor3)
	return p2 - p3
}

// pTopTrips — P(top final trips, any rank)
func pTopTrips(gs *GameState, rankRem [13]int, jokerRem, deckTotal, topSlots int) float32 {
	// 2026-07-01 (#110 bug): 旧版 topHasR=countRankInRow 不数顶上的鬼 → 孤鬼顶[🃏](2空)被当成
	//   "顶0张要摸3张成trips"=0 → exp 靠鬼成 trips 范的概率被清零(f93=0). 顶上的鬼可当任意 rank, 计入 need.
	topJokers := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			topJokers++
		}
	}
	if topSlots == 0 {
		// 含鬼也算: 顶满且(真三条 或 真对+鬼 或 双鬼)
		if hasRealTripsTop(gs.Top) || topJokers >= 2 {
			return 1
		}
		if topJokers == 1 {
			for r := 0; r < 13; r++ {
				if countRankInRow(gs.Top, r) >= 2 {
					return 1 // 真对 + 鬼 = trips
				}
			}
		}
		return 0
	}
	// 2026-07-01 (#110): union over ranks (顶可成多条 trips, 不是单 max) + 合法性.
	//   顶 r-trips ≤ 中现值 → 合法全算; 顶 r-trips > 中(且中已成三条+) → 要中再发育成葫芦+ 才合法, 打折.
	//   (例 AI 顶🃏7h 唯一 777>中666: 折扣; exp 顶🃏 可成 2/3/4/5/6 多条 ≤中666: union 全算 → exp>AI.)
	midCur := int(rowMadeScore(gs.Middle))
	capTrips := midCur/13 >= int(HtThreeKind)
	pNone := float32(1) // P(没成任何合法 trips 范)
	for r := 0; r < 13; r++ {
		have := countRankInRow(gs.Top, r) + topJokers
		need := 3 - have
		var pMake float32
		if need <= 0 {
			pMake = 1 // 顶已成 r-trips
		} else {
			pMake = hypergeoAtLeast(deckTotal, rankRem[r]+jokerRem, topSlots, need)
		}
		legalFactor := float32(1)
		if capTrips && int(HtThreeKind)*13+r > midCur {
			legalFactor = 0.3 // 顶 r-trips > 中已成三条 → 要中发育超过它(葫芦+), 折扣
		}
		pNone *= (1 - pMake*legalFactor)
	}
	return 1 - pNone
}

// pTopNoFoulVsMid — 估算 P(top ≤ mid final)
// 简化: 当前已 foul (top > mid) → 0; 否则 ≈ 1 - P(top 暴涨)
func pTopNoFoulVsMid(topEv, midEv HandValue) float32 {
	if len(topRowFromEv(topEv)) > 0 && topEv.Type > midEv.Type {
		return 0
	}
	return 0.9 // 简化默认值, 后续可细化
}

// pRowAtLeast — P(row final ≥ targetType). 用 hypergeometric 估算.
// 这是简化版: 用 row 已有 + max achievable type 估
func pRowAtLeast(row []Card, targetType int, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if len(row) == 0 && slots == 0 {
		return 0
	}
	// 已经 ≥ target ?
	currentType := rowCurrentType(row)
	if currentType >= targetType {
		return 1
	}
	if slots == 0 {
		return 0
	}
	// max achievable
	maxAch := int(maxAchievableHandType(row, slots, rankRem, suitRem, jokerRem))
	if maxAch < targetType {
		return 0
	}
	// 简化估算: pair/2pair/trips 用 rank-count hypergeometric
	switch targetType {
	case TypePair:
		// 需要 row 至少有一组 pair (existing rank + 1 more, or new rank ≥ 2)
		return pRowAtLeastPair(row, rankRem, jokerRem, deckTotal, slots, cardsSeen)
	case TypeTwoPair:
		return pRowAtLeastTwoPair(row, rankRem, jokerRem, deckTotal, slots, cardsSeen)
	case TypeThreeOfAKind:
		return pRowAtLeastTrips(row, rankRem, jokerRem, deckTotal, slots, cardsSeen)
	case TypeFullHouse:
		// FH = trips + pair. 复杂, 用粗估
		return pRowAtLeastTrips(row, rankRem, jokerRem, deckTotal, slots, cardsSeen) * 0.3
	case TypeFourOfAKind:
		return pRowAtLeastQuads(row, rankRem, jokerRem, deckTotal, slots, cardsSeen)
	}
	return 0
}

// pRowAtLeastPair — row 至少 pair 的概率
func pRowAtLeastPair(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if hasRealPair(row) {
		return 1
	}
	if len(row)+slots < 2 {
		return 0
	}
	// 用 inclusion-exclusion 估: P(凑 pair) ≈ 1 - P(完全无 pair)
	// 简化: 用 expected 同 rank 数
	// 各 rank 算 P(row 最终至少 2 张该 rank)
	maxP := float32(0)
	for r := 0; r < 13; r++ {
		rowHasR := countRankInRow(row, r)
		need := 2 - rowHasR
		if need <= 0 {
			return 1
		}
		deckR := rankRem[r] + jokerRem
		p := pDraw(deckTotal, deckR, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	// 简化: any-rank 联合用 1 - prod(1-p_r), 但这里用 maxP 作下界 (低估)
	// 改进: 用 1 - exp(-Σp_r) 近似 (因为各 rank 几乎独立)
	sumP := float32(0)
	for r := 0; r < 13; r++ {
		rowHasR := countRankInRow(row, r)
		need := 2 - rowHasR
		if need <= 0 {
			return 1
		}
		deckR := rankRem[r] + jokerRem
		sumP += pDraw(deckTotal, deckR, cardsSeen, slots, need)
	}
	if sumP > 5 {
		sumP = 5
	}
	approxP := 1 - float32(math.Exp(-float64(sumP)))
	if approxP > 1 {
		approxP = 1
	}
	return approxP
}

// pRowAtLeastTwoPair — row 至少两对
func pRowAtLeastTwoPair(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if has2PairOrBetter(row) {
		return 1
	}
	// 简化: 用 pRowAtLeastPair^2 (粗估两对独立)
	p1 := pRowAtLeastPair(row, rankRem, jokerRem, deckTotal, slots, cardsSeen)
	return p1 * p1 * 0.5
}

// pRowAtLeastTrips — row 至少 trips
func pRowAtLeastTrips(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if hasRealTrips(row) {
		return 1
	}
	maxP := float32(0)
	for r := 0; r < 13; r++ {
		rowHasR := countRankInRow(row, r)
		need := 3 - rowHasR
		if need <= 0 {
			return 1
		}
		deckR := rankRem[r] + jokerRem
		p := pDraw(deckTotal, deckR, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// pRowAtLeastQuads — row 至少 quads
func pRowAtLeastQuads(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	maxP := float32(0)
	for r := 0; r < 13; r++ {
		rowHasR := countRankInRow(row, r)
		need := 4 - rowHasR
		if need <= 0 {
			return 1
		}
		deckR := rankRem[r] + jokerRem
		p := pDraw(deckTotal, deckR, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// pRowStraight — row 最终顺子概率 (简化)
// pRowStraight — P(行成顺), deck-aware + 挑牌模型 (cardsSeen). 2026-06-25 重写:
//   旧版 (5/13)^need 拍脑袋, 不看牌/不挑牌/maxRun数错卡顺 → 系统低估顺draw.
//   新: 滑10个5连窗, 每窗缺的真rank由"行内鬼补最难缺口 + 抽剩下" → 每缺口 P(抽≥1该rank或鬼).
//   窗内积(填满该窗所有缺) → 窗间取并集 1-∏(1-pWin) (开口顺两窗 → 高于单窗). 跟 pRowFlush 一套挑牌模型.
func pRowStraight(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if isStraight(row) {
		return 1
	}
	if len(row)+slots < 5 {
		return 0
	}
	var present [13]bool
	jokersInRow := 0
	for _, c := range row {
		if c.IsJoker() {
			jokersInRow++
		} else {
			present[c.Rank()] = true
		}
	}
	windows := [10][5]int{
		{12, 0, 1, 2, 3}, {0, 1, 2, 3, 4}, {1, 2, 3, 4, 5}, {2, 3, 4, 5, 6}, {3, 4, 5, 6, 7},
		{4, 5, 6, 7, 8}, {5, 6, 7, 8, 9}, {6, 7, 8, 9, 10}, {7, 8, 9, 10, 11}, {8, 9, 10, 11, 12},
	}
	pNone := float32(1.0)
	any := false
	for _, w := range windows {
		var missing []int
		for _, r := range w {
			if !present[r] {
				missing = append(missing, r)
			}
		}
		need := len(missing) - jokersInRow // 行内鬼补最难(rankRem最少)的缺口
		if need <= 0 || need > slots {
			continue
		}
		// rankRem 升序: 前 jokersInRow 个(最难)给鬼, 剩 need 个要抽
		sort.Slice(missing, func(i, j int) bool { return rankRem[missing[i]] < rankRem[missing[j]] })
		toDraw := missing[jokersInRow:]
		pWin := float32(1.0)
		for _, r := range toDraw {
			outs := rankRem[r] + jokerRem // 抽到该rank或牌堆里的鬼都能填
			if outs <= 0 {
				pWin = 0
				break
			}
			pWin *= pDraw(deckTotal, outs, cardsSeen, slots, 1)
		}
		if pWin > 0 {
			pNone *= (1 - pWin)
			any = true
		}
	}
	if !any {
		return 0
	}
	return 1 - pNone
}

// pRowFlush — row 最终同花概率
func pRowFlush(row []Card, suitRem [4]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if isFlush(row) {
		return 1
	}
	if len(row)+slots < 5 {
		return 0
	}
	// 各 suit 算 P(row 最终 5 张同色)
	maxP := float32(0)
	for s := 0; s < 4; s++ {
		rowHasS := countSuitInRow(row, s)
		need := 5 - rowHasS
		if need <= 0 {
			return 1
		}
		deckS := suitRem[s] + jokerRem
		p := pDraw(deckTotal, deckS, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// pRowGEPair10 — row 最终 ≥ pair 10
func pRowGEPair10(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	// 已经 ≥ pair 10 ?
	if maxPairRankRow(row) >= int(RankT) {
		return 1
	}
	if len(row)+slots < 2 {
		return 0
	}
	maxP := float32(0)
	for r := int(RankT); r <= 12; r++ {
		rowHasR := countRankInRow(row, r)
		need := 2 - rowHasR
		if need <= 0 {
			return 1
		}
		deckR := rankRem[r] + jokerRem
		p := pDraw(deckTotal, deckR, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// pMidGTBot — 估算 P(mid > bot final). foul 主路径.
func pMidGTBot(gs *GameState, midEv, botEv HandValue, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal, midSlots, botSlots int) float32 {
	// 2026-05-20 sp16: 终局态 (mid+bot 都满) mid ≤ bot 已确定 no foul, 返 0 不再"估算"
	// case 50: pFoul=0.2 bug 让 exp1 advantage 缩水 (finalScore 被假罚 1.2)
	terminal := midSlots == 0 && botSlots == 0
	if terminal {
		if midEv.Type > botEv.Type {
			return 1 // 已 foul
		}
		if midEv.Type == botEv.Type && midEv.Value > botEv.Value {
			return 1 // 同 type, mid value 高 = foul
		}
		return 0 // 确定 no foul
	}
	// 2026-06-29 (#51 同根): 改真概率 P(底 ≥ 中) 替换粗桶乐观 maxAchievable. P(中>底)=1-P(底≥中).
	cs := cardsSeenRemaining(gs)
	if midEv.Type < TypePair {
		// 2026-06-29 (#67): 中高牌 ≠ 安全. "中小底大"常识 — 中的高牌将来配对可能倒置.
		//   P(中>底) ≈ P(中配出最高牌的对) × P(底 < 该对). AI把J放中赌来Q, 来J不来Q就 中JJ>底Q高 倒置.
		midHigh := -1
		midHighCnt := 0
		for _, c := range gs.Middle {
			if !c.IsJoker() {
				if r := int(c.Rank()); r > midHigh {
					midHigh, midHighCnt = r, 1
				} else if r == midHigh {
					midHighCnt++
				}
			}
		}
		if midHigh < 0 { // 中全鬼(罕见)
			return 0.1
		}
		var pMidPairsHigh float32
		if midHighCnt >= 2 {
			pMidPairsHigh = 1
		} else {
			pMidPairsHigh = pDraw(deckTotal, rankRem[midHigh], cs, midSlots, 1)
		}
		// 底是否摸到 ≥ "中最高牌对"(配 ≥midHigh 的对, 或两对+)
		pBotGEpair := pRowPairAtLeastRank(gs.Bottom, midHigh, rankRem, jokerRem, deckTotal, botSlots, cs)
		pBotTwoPair := pRowAtLeast(gs.Bottom, TypeTwoPair, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
		pBotGE0 := pBotGEpair + pBotTwoPair*(1-pBotGEpair)
		p := pMidPairsHigh * (1 - pBotGE0)
		if p < 0 {
			p = 0
		} else if p > 0.9 {
			p = 0.9
		}
		return p
	}
	var pBotGE float32 // P(底道现实摸到 ≥ 中)
	if midEv.Type == TypePair {
		midR := highestRealPairRank(gs.Middle)
		if midR < 0 {
			midR = 0
		}
		pBotPairGE := pRowPairAtLeastRank(gs.Bottom, midR, rankRem, jokerRem, deckTotal, botSlots, cs)
		pBotTwoPairPlus := pRowAtLeast(gs.Bottom, TypeTwoPair, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
		pBotGE = pBotPairGE + pBotTwoPairPlus*(1-pBotPairGE)
	} else if midEv.Type == TypeTwoPair {
		// 2026-07-01 (#24 bug2): 中是两对(高对 rank=midHi). 底要"配出 ≥midHi 的对(成更高两对)或三条+"才能超 —
		//   不是"随便成个两对"(QQ两对超不过中 KK两对). 旧 else 用 pRowAtLeast(底,TwoPair) rank-blind 低估 foul.
		midHi := highestRealPairRank(gs.Middle)
		if midHi < 0 {
			midHi = 0
		}
		pBotPairGE := pRowPairAtLeastRank(gs.Bottom, midHi, rankRem, jokerRem, deckTotal, botSlots, cs)
		pBotTrips := pRowAtLeast(gs.Bottom, TypeThreeOfAKind, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
		pBotGE = pBotPairGE + pBotTrips*(1-pBotPairGE)
	} else {
		pBotGE = pRowAtLeast(gs.Bottom, midEv.Type, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	}
	p := 1 - pBotGE
	if p < 0 {
		p = 0
	} else if p > 1 {
		p = 1
	}
	return p
}

// pRowPairAtLeastRank — P(row 配出一个 rank ≥ minRank 的对子). union over ranks≥minRank, 每 rank 用 pDraw.
//   已成对/鬼配=1; 单张+1摸; 无该rank=2摸(rare). (#51 同根: foul 概率复用 pDraw, 不拍桶.)
func pRowPairAtLeastRank(row []Card, minRank int, rankRem [13]int, jokerRem, deckTotal, slots, cs int) float32 {
	var rc [13]int
	hasJoker := false
	for _, c := range row {
		if c.IsJoker() {
			hasJoker = true
		} else {
			rc[c.Rank()]++
		}
	}
	pNone := float32(1)
	for r := minRank; r <= 12; r++ {
		var pPair float32
		switch {
		case rc[r] >= 2 || (rc[r] >= 1 && hasJoker):
			pPair = 1
		case rc[r] == 1:
			pPair = pDraw(deckTotal, rankRem[r], cs, slots, 1)
		default:
			pPair = pDraw(deckTotal, rankRem[r], cs, slots, 2)
		}
		pNone *= (1 - pPair)
	}
	return 1 - pNone
}

// pRowTripsAtLeastRank — P(row 配出一个 rank ≥ minRank 的三条). #42: 顶三条 foul 比较要 rank-aware.
//   行内(含joker)已成 ≥minRank 三条=1; 否则 union over ranks≥minRank, pDraw(outs含deck joker).
func pRowTripsAtLeastRank(row []Card, minRank int, rankRem [13]int, jokerRem, deckTotal, slots, cs int) float32 {
	var rc [13]int
	jrow := 0
	for _, c := range row {
		if c.IsJoker() {
			jrow++
		} else {
			rc[c.Rank()]++
		}
	}
	pNone := float32(1)
	for r := minRank; r <= 12; r++ {
		if rc[r]+jrow >= 3 {
			return 1
		}
		pNone *= (1 - pDraw(deckTotal, rankRem[r]+jokerRem, cs, slots, 3-rc[r]))
	}
	return 1 - pNone
}

// pBotGEMid — 等于 1 - pMidGTBot
func pBotGEMid(gs *GameState, midEv, botEv HandValue, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal, midSlots, botSlots int) float32 {
	return 1 - pMidGTBot(gs, midEv, botEv, rankRem, suitRem, jokerRem, deckTotal, midSlots, botSlots)
}

// drawSeedScore — #104 (2026-06-29): 保种子期权价值 (silver-label 注入, 复用现成概率).
//   heuristic rollout 不实现长尾"底QQ→葫芦托住中花/顺", label 低估"保种子别堵底"摆 → 按概率补.
//   = P(底成葫芦)  +  P(中flush/顺) × P(底能托住)(否则中花/顺反而倒置, 无价值).
func drawSeedScore(state *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) float32 {
	midSlots := 5 - len(state.Middle)
	botSlots := 5 - len(state.Bottom)
	cs := cardsSeenRemaining(state)
	deckTotal := jokerRem
	for _, v := range rankRem {
		deckTotal += v
	}
	// 底种子: 葫芦/花/顺 取最强 (底是最强行, 成强手无倒置风险, 无条件计). bp=底FH 单留作中draw的support.
	// 2026-06-29 (#117): 加底花/顺draw — 底2d6dKd(3方块)是花种子, 旧版只算葫芦漏掉.
	var bp, botSeed float32
	if botSlots > 0 {
		bp = pRowAtLeast(state.Bottom, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
		botSeed = bp
		if fl := pRowFlush(state.Bottom, suitRem, jokerRem, deckTotal, botSlots, cs); fl > botSeed {
			botSeed = fl
		}
		if st := pRowStraight(state.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs); st > botSeed {
			botSeed = st
		}
	}
	// 中 flush/顺 draw, 条件: 底能托(底≥FH 才不被中花/顺倒置) → 用 bp 当 support
	var midVal float32
	if midSlots > 0 {
		md := pRowFlush(state.Middle, suitRem, jokerRem, deckTotal, midSlots, cs)
		if st := pRowStraight(state.Middle, rankRem, jokerRem, deckTotal, midSlots, cs); st > md {
			md = st
		}
		midVal = md * bp
	}
	return botSeed + midVal
}

// pFoulFinal — P(top > mid ∨ mid > bot)
// 注: evalRowSafe 用 cap chain, 当 mid > bot 时 midEv.Type = -2 (overCap 标志). 用 raw Evaluate5 重算.
// pTopGTMid — P(顶 > 中道最终牌型) = 顶压中犯规概率 (中道未满时). joker-aware floor + maxAchievable
// 粗粒度桶 (同 pMidGTBot 风格). 治: 顶已成三条/高对, 中道当前追不上且要大跳才能托住 → 高 foul.
func pTopGTMid(gs *GameState, topEv, midEv HandValue, rankRem [13]int, suitRem [4]int, jokerRem, midSlots int) float32 {
	if len(gs.Top) == 0 || midSlots == 0 {
		return 0 // 顶空 / 中道已满(由外层精确判)
	}
	topT := topEv.Type
	if topT < TypePair {
		return 0 // 顶高牌, 顶>中 foul 风险低
	}
	// 当前顶 > 中? (先比 type, 同 type 比对子/三条 rank)
	cur := topT > midEv.Type
	if topT == midEv.Type {
		cur = highestRealPairRank(gs.Top) > highestRealPairRank(gs.Middle)
	}
	if !cur {
		return 0 // 中道现状已 ≥ 顶 → 无顶>中 foul
	}
	// 2026-06-29 (#51): 改用真概率(pDraw/pRowAtLeast)算 P(中道现实摸到 ≥ 顶), 不再用乐观 maxAchievable 拍桶.
	//   旧 bug: 中 3,Ac 因"3空位能凑轮子顺"被乐观算成 midMax>顶 → 全 flat 0.7 → foul 特征死. 概率机器(pRowAtLeast)早有, 没复用.
	cs := cardsSeenRemaining(gs)
	deckTotal := jokerRem
	for _, v := range rankRem {
		deckTotal += v
	}
	var pMidGE float32 // P(中道 ≥ 顶)
	if topT == TypePair {
		topR := int((topEv.Value - 1000000) / 15)
		// 顶 kicker (满顶才锁定; 未满=-1 待发育)
		topKicker := -1
		if len(gs.Top) == 3 {
			for _, c := range gs.Top {
				if !c.IsJoker() {
					if r := int(c.Rank()); r != topR && r > topKicker {
						topKicker = r
					}
				}
			}
		}
		var midRc [13]int
		midJoker := false
		for _, c := range gs.Middle {
			if c.IsJoker() {
				midJoker = true
			} else {
				midRc[c.Rank()]++
			}
		}
		// P(中配出 ≥R 的对) — union over ranks≥R, 每 rank 用 pDraw
		pNone := float32(1)
		for r := topR; r <= 12; r++ {
			var pPair float32
			switch {
			case midRc[r] >= 2 || (midRc[r] >= 1 && midJoker):
				pPair = 1 // 已成对/鬼配
			case midRc[r] == 1:
				pPair = pDraw(deckTotal, rankRem[r], cs, midSlots, 1)
			default:
				pPair = pDraw(deckTotal, rankRem[r], cs, midSlots, 2)
			}
			// kicker (#51): 配出"恰好 R"同对时需 kicker 超顶. 顶满低kicker → 中易超(不折); 顶高kicker → 难超(折);
			//   顶未满(kicker未定可能高) → 部分折. 治"4→头锁低kicker更安全".
			if r == topR {
				switch {
				case topKicker < 0:
					pPair *= 0.7 // 顶未满, kicker 待定
				case topKicker >= int(RankJ):
					pPair *= 0.4 // 顶锁高 kicker, 中同对难超
				}
			}
			pNone *= (1 - pPair)
		}
		pPairGE := 1 - pNone
		pTwoPairPlus := pRowAtLeast(gs.Middle, TypeTwoPair, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
		pMidGE = pPairGE + pTwoPairPlus*(1-pPairGE)
	} else if topT == TypeThreeOfAKind {
		// 2026-07-01 (#42 bug): 顶三条(rank=topR). 中要"三条≥topR 或 葫芦+"才托住 —
		//   不是"中随便成个三条"(中444托不住顶KKK). 旧 pRowAtLeast(中,Trips) rank-blind → pTopGTMid=0 漏报foul.
		topR := int((topEv.Value - 3000000) / 15)
		pMidFH := pRowAtLeast(gs.Middle, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
		pMidTripsGE := pRowTripsAtLeastRank(gs.Middle, topR, rankRem, jokerRem, deckTotal, midSlots, cs)
		pMidGE = pMidFH + pMidTripsGE*(1-pMidFH)
	} else {
		pMidGE = pRowAtLeast(gs.Middle, topT, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	}
	p := 1 - pMidGE
	if p < 0 {
		p = 0
	} else if p > 1 {
		p = 1
	}
	return p
}

func pFoulFinal(gs *GameState, topEv, midEv, botEv HandValue, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal int) float32 {
	midSlots := 5 - len(gs.Middle)
	botSlots := 5 - len(gs.Bottom)

	// Type=-2 是 overCap 标志, 直接 foul
	if midEv.Type == -2 || topEv.Type == -2 || botEv.Type == -2 {
		return 1
	}

	// 用 raw eval 严格比较 (避免 cap 混淆)
	if len(gs.Middle) == 5 && len(gs.Bottom) == 5 {
		midRaw := Evaluate5(gs.Middle)
		botRaw := Evaluate5(gs.Bottom)
		if midRaw.Value > botRaw.Value {
			return 1
		}
	}
	if len(gs.Top) == 3 && len(gs.Middle) == 5 {
		topRaw := Evaluate3(gs.Top)
		midRaw := Evaluate5(gs.Middle)
		if topRaw.Type > midRaw.Type {
			return 1
		}
		// 同 type 加比较 value (top eval value 跟 mid 不同尺度, 跳过细节比)
	}

	// 估算 P(将来 foul)
	pMidGT := pMidGTBot(gs, midEv, botEv, rankRem, suitRem, jokerRem, deckTotal, midSlots, botSlots)
	// 2026-06-15: 加 P(顶>中) 路径 — 中道未满时估"顶压中"犯规概率 (原只算 mid>bot, 漏判
	// KKK顶 + 中道封顶托不住 这类 ~70% foul, ypk-36634954-18). 组合 = P(任一行 foul).
	pTopGT := pTopGTMid(gs, topEv, midEv, rankRem, suitRem, jokerRem, midSlots)
	return pMidGT + pTopGT - pMidGT*pTopGT
}

// ============================================================
// Expected royalty per row
// ============================================================

// 2026-05-20 sp16: cap-aware. joker 在 top 时, 算 wild joker 凑 pair 后的 royalty.
// 用 topEv (=topEvalCapped) 提取真实 pair rank — 反映 mid cap 后的真值.
// case 49 R5: top [X Kh 8h] cap 后 = pair-8 = +3 royalty (旧版 countRankInRow 不算 joker → 漏)
// case 50 R5: top [X 2c As] cap 后 = pair-2 = 0 royalty (joker 不能当 A 因 mid KK 撞 foul)
func eRoyaltyTop(gs *GameState, topEv HandValue, rankRem [13]int, jokerRem, deckTotal, topSlots int) float32 {
	cs := cardsSeenRemaining(gs)
	r := float32(0)
	// 当前 top eval (cap-aware) 直接给定值
	if topEv.Type == TypeThreeOfAKind {
		// trips: 22+ royalty (per OFC top: 222=10, 333=11, ..., AAA=22)
		tripsRank := int((topEv.Value - 3000000) / 15)
		r = float32(10 + tripsRank) // 222=10, ..., AAA=22
	} else if topEv.Type == TypePair {
		// pair: 66=1, 77=2, ..., AA=9 (per OFC top pair royalty)
		pairRank := int((topEv.Value - 1000000) / 15)
		if pairRank >= int(Rank6) {
			r = float32(pairRank - 3) // 6(rank4)=1, A(rank12)=9
		}
	}
	if topSlots == 0 {
		return r
	}
	// future expected: 估计后续 round 升级到 trips / 高 pair 的概率
	// 简化: 对每个 rank 估算 future hits, 加权 royalty
	pTrips := pTopTrips(gs, rankRem, jokerRem, deckTotal, topSlots)
	r += pTrips * 5 // future trips 增量 (减小权重避免双倍计)
	for rk := 4; rk < 13; rk++ {
		royalty := float32(rk - 3)
		topHasR := countRankInRow(gs.Top, rk)
		deckR := rankRem[rk] + jokerRem
		need := 2 - topHasR
		if need <= 0 {
			continue // 已 pair (上面已加分)
		}
		p := pDraw(deckTotal, deckR, cs, topSlots, need)
		r += royalty * p * 0.15 // future pair 升级, discount
	}
	return r
}

func eRoyaltyMid(gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal, midSlots int) float32 {
	cs := cardsSeenRemaining(gs)
	pTrips := pRowAtLeastTrips(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs)
	pStraight := pRowStraight(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs)
	pFlush := pRowFlush(gs.Middle, suitRem, jokerRem, deckTotal, midSlots, cs)
	pFH := pRowAtLeast(gs.Middle, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, midSlots, cs)
	pQ := pRowAtLeastQuads(gs.Middle, rankRem, jokerRem, deckTotal, midSlots, cs)
	return pTrips*2 + pStraight*4 + pFlush*8 + pFH*12 + pQ*20
}

func eRoyaltyBot(gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem, deckTotal, botSlots int) float32 {
	cs := cardsSeenRemaining(gs)
	pTrips := pRowAtLeastTrips(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
	pStraight := pRowStraight(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
	pFlush := pRowFlush(gs.Bottom, suitRem, jokerRem, deckTotal, botSlots, cs)
	pFH := pRowAtLeast(gs.Bottom, TypeFullHouse, rankRem, suitRem, jokerRem, deckTotal, botSlots, cs)
	pQ := pRowAtLeastQuads(gs.Bottom, rankRem, jokerRem, deckTotal, botSlots, cs)
	return pTrips*0 + pStraight*2 + pFlush*4 + pFH*6 + pQ*10
}

// ============================================================
// V/U helpers
// ============================================================

// pPairToTrips — row 已 pair, P(升 trips)
func pPairToTrips(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen, ceil int) float32 {
	pr := maxPairRankRow(row)
	if pr < 0 {
		return 0
	}
	// 2026-06-28 (#124): 升 trips 会倒置(trips值 > 下行 max-achievable)就不算发育潜力 — 那是 foul 不是好牌.
	//   治"中22→222三条"被算高分但 222>底JJ99 必倒置. 跟 W组/topTripsSeed 同口径. 底道 ceil=999 无上限.
	if int(HtThreeKind)*13+pr > ceil {
		return 0
	}
	if hasRealTrips(row) || hasRealTripsTop(row) {
		return 1
	}
	rowHasR := countRankInRow(row, pr)
	deckR := rankRem[pr] + jokerRem
	need := 3 - rowHasR
	if need <= 0 {
		return 1
	}
	if slots == 0 {
		return 0
	}
	return pDraw(deckTotal, deckR, cardsSeen, slots, need)
}

// p2PairToFH — row 已 2pair, P(升 FH)
func p2PairToFH(row []Card, rankRem [13]int, jokerRem, deckTotal, slots, cardsSeen int) float32 {
	if !has2PairOrBetter(row) {
		return 0
	}
	// 找两对 rank, 任一升 trips → FH
	pairRanks := getAllPairRanks(row)
	if len(pairRanks) < 2 {
		return 0
	}
	maxP := float32(0)
	for _, r := range pairRanks {
		rowHasR := countRankInRow(row, r)
		deckR := rankRem[r] + jokerRem
		need := 3 - rowHasR
		if need <= 0 {
			return 1
		}
		p := pDraw(deckTotal, deckR, cardsSeen, slots, need)
		if p > maxP {
			maxP = p
		}
	}
	return maxP
}

// ============================================================
// Row inspection helpers
// ============================================================

// maxPairRankRow — row 中最大 pair 的 rank (含 joker wild). -1 if no pair.
func maxPairRankRow(row []Card) int {
	if len(row) < 2 {
		return -1
	}
	var rankCnt [13]int
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	maxR := -1
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 2 || (rankCnt[r] >= 1 && jokers >= 1) {
			maxR = r
			break
		}
	}
	return maxR
}

// twoPairHighRank — row 两对中较大对的 rank. -1 if no 2pair.
func twoPairHighRank(row []Card) int {
	pairs := getAllPairRanks(row)
	if len(pairs) == 0 {
		return -1 // 真无对
	}
	if len(pairs) >= 2 {
		maxR := -1
		for _, r := range pairs {
			if r > maxR {
				maxR = r
			}
		}
		return maxR
	}
	// 2026-06-14: 单一成对 rank — 若是三条+(cnt>=3)即三条/金刚, 返回该 rank (强成手, 别给 -1
	// junk 哨兵, 否则三条/金刚在此维跟"无对"同信号 → value head 低估, ypk-12124490-13 89X).
	// 单纯一对(cnt==2)仍 -1 (非两对).
	r := pairs[0]
	cnt := 0
	for _, c := range row {
		if !c.IsJoker() && int(c.Rank()) == r {
			cnt++
		}
	}
	if cnt >= 3 {
		return r
	}
	return -1
}

// getAllPairRanks — row 中所有 pair 的 rank 列表
func getAllPairRanks(row []Card) []int {
	var rankCnt [13]int
	for _, c := range row {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	out := make([]int, 0, 4)
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 2 {
			out = append(out, r)
		}
	}
	return out
}

// has2PairOrBetter — row 是否 ≥ 2pair
func has2PairOrBetter(row []Card) bool {
	pairs := getAllPairRanks(row)
	return len(pairs) >= 2
}

// hasRealTripsTop — top 3 张是否 real trips (不含 joker wild 凑的, 但允许 joker)
func hasRealTripsTop(row []Card) bool {
	if len(row) != 3 {
		return false
	}
	var rankCnt [13]int
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	for r := 0; r < 13; r++ {
		if rankCnt[r]+jokers >= 3 {
			return true
		}
	}
	return false
}

// countRankInRow — row 中 rank r 的张数 (不含 joker)
func countRankInRow(row []Card, r int) int {
	n := 0
	for _, c := range row {
		if !c.IsJoker() && int(c.Rank()) == r {
			n++
		}
	}
	return n
}

// countSuitInRow — row 中 suit s 的张数
func countSuitInRow(row []Card, s int) int {
	n := 0
	for _, c := range row {
		if !c.IsJoker() && int(c.Suit()) == s {
			n++
		}
	}
	return n
}

// rowCurrentType — row 当前 HandType (考虑 joker, 简化)
func rowCurrentType(row []Card) int {
	if len(row) == 0 {
		return TypeHighCard
	}
	if len(row) == 5 {
		v := Evaluate5JokerCap(row, nil)
		return v.Type
	}
	if len(row) == 3 {
		v := Evaluate3JokerCap(row, nil)
		return v.Type
	}
	v := partialEval(row)
	return v.Type
}

// isStraight — 行 5 张是否顺子
func isStraight(row []Card) bool {
	if len(row) != 5 {
		return false
	}
	v := Evaluate5JokerCap(row, nil)
	return v.Type == TypeStraight || v.Type == TypeStraightFlush
}

// isFlush — 行 5 张是否同花
func isFlush(row []Card) bool {
	if len(row) != 5 {
		return false
	}
	v := Evaluate5JokerCap(row, nil)
	return v.Type == TypeFlush || v.Type == TypeStraightFlush
}

// topRowFromEv — 占位 (没用上, 防止 lint)
func topRowFromEv(ev HandValue) []Card {
	return nil
}
