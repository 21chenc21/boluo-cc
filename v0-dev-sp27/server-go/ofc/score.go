package ofc

// === Royalty 计算 (从已评估 HandValue 推导) ===

// TopRoyaltyFromEval: 头道 royalty (从 Evaluate3 结果)
//   666=1, 777=2, ..., AAA=22 (trips: rank+10)
//   66=1, 77=2, ..., AA=9 (pair: rank-3, 仅 ≥66)
func TopRoyaltyFromEval(ev HandValue) int {
	if ev.Type < 0 {
		return 0
	}
	if ev.Type == TypeThreeOfAKind {
		// value = 3e6 + tripRank*15
		tripRank := int((ev.Value - 3000000) / 15)
		return tripRank + 10
	}
	if ev.Type == TypePair {
		pairRank := int((ev.Value - 1000000) / 15)
		if pairRank >= 4 {
			return pairRank - 3
		}
	}
	return 0
}

// MiddleRoyaltyFromEval
func MiddleRoyaltyFromEval(ev HandValue) int {
	switch ev.Type {
	case TypeThreeOfAKind:
		return 2
	case TypeStraight:
		return 4
	case TypeFlush:
		return 8
	case TypeFullHouse:
		return 12
	case TypeFourOfAKind:
		return 20
	case TypeStraightFlush:
		return 30
	case TypeRoyalFlush:
		return 50
	}
	return 0
}

// BottomRoyaltyFromEval
func BottomRoyaltyFromEval(ev HandValue) int {
	switch ev.Type {
	case TypeStraight:
		return 2
	case TypeFlush:
		return 4
	case TypeFullHouse:
		return 6
	case TypeFourOfAKind:
		return 10
	case TypeStraightFlush:
		return 15
	case TypeRoyalFlush:
		return 25
	}
	return 0
}

// IsFantasyLandFromEval — 进范判定 (头道 trips OR pair ≥ QQ)
func IsFantasyLandFromEval(ev HandValue) bool {
	if ev.Type < 0 {
		return false
	}
	if ev.Type == TypeThreeOfAKind {
		return true
	}
	if ev.Type == TypePair {
		pairRank := int((ev.Value - 1000000) / 15)
		return pairRank >= 10 // QQ=10, KK=11, AA=12
	}
	return false
}

// FantasyBonusTier — 从 cap-chain'd top eval 判定 fantasy bonus tier.
// 返回 (bonus 倍数, 是否触发 fantasy).
//   te 必须是 Evaluate3JokerCap(top, &mid_eval) 出的结果 (joker 已被 cap 降到合法 rank).
//   trips → tripsBonus
//   pair AA → aaBonus
//   pair KK → kkBonus
//   pair QQ → qqBonus
//   其它 → 0, false
//
// 跟 game.js checkFantasyTrigger 修复后逻辑一致 (2026-05-18 该 bug 修复):
// 旧版手算 jokerCnt + realMax 找 effMax/pairR 不走 cap-chain → joker 在 cap 限制下被
// "升级"到非法 rank, fantasy bonus 多算. 改成走 te.Type/te.Value 后 cap-aware 正确.
func FantasyBonusTier(te HandValue, qqBonus, kkBonus, aaBonus, tripsBonus float32) (float32, bool) {
	if te.Type < 0 {
		return 0, false
	}
	if te.Type == TypeThreeOfAKind {
		return tripsBonus, true
	}
	if te.Type == TypePair {
		pairRank := int((te.Value - 1000000) / 15)
		switch {
		case pairRank >= int(RankA):
			return aaBonus, true
		case pairRank == int(RankK):
			return kkBonus, true
		case pairRank == int(RankQ):
			return qqBonus, true
		}
	}
	return 0, false
}

// FantasyBonusFromBoard — 完整 13 张 board 计算 fantasy bonus (cap-chain aware).
// 内部走 cap-chain: bot (no cap) → mid (cap=bot) → top (cap=mid).
// foul 时返回 (0, false). 非 fantasy 也返回 (0, false).
//
// 调用例: 替代手算 jokerCnt + effMax 的旧 classifyFanBonus, 避免 cap-down 时多算 bonus.
func FantasyBonusFromBoard(top, mid, bot []Card, qqBonus, kkBonus, aaBonus, tripsBonus float32) (float32, bool) {
	if len(top) != 3 || len(mid) != 5 || len(bot) != 5 {
		return 0, false
	}
	be := Evaluate5JokerCap(bot, nil)
	me := Evaluate5JokerCap(mid, &be)
	te := Evaluate3JokerCap(top, &me)
	if be.Type < 0 || me.Type < 0 || te.Type < 0 {
		return 0, false // foul
	}
	if HandExceeds5(me, be) || TopExceedsMid(te, me) {
		return 0, false // foul
	}
	return FantasyBonusTier(te, qqBonus, kkBonus, aaBonus, tripsBonus)
}

// === ScoreResult ===
type ScoreResult struct {
	Foul        bool
	Score       int      // 总 royalty (foul 时 = -20)
	Royalties   int      // foul=0
	TopRoyalty  int
	MidRoyalty  int
	BotRoyalty  int
	Fantasy     bool
	TopEval     HandValue
	MidEval     HandValue
	BotEval     HandValue
}

// MadeHandRewardLabel — silver-label 专用: 给"有 equity 但 royalty=0"的低端成手补分,
// 治 value-head "不珍惜成手"(#66 中两对没分被散成垃圾顶). 2026-06-25 用户定表 (v2).
//   顶: 低对 22/33/44/55 → 0.2/0.4/0.6/0.8 (66起royalty, 不给)
//   中: 一对0.5 / 两对1.0   (三条起royalty)
//   底: 两对0.5 / 三条1.0   (顺起royalty; 底一对不给)
//   ⚠️ 只进训练 label, 不是真实对局分(ScoreResult.Score/Royalties 不含此项). non-foul 才加.
var (
	MHRTopPairStep float32 = 0.2  // 顶低对(rank idx 0-3 = 22-55): (idx+1)*step
	MHRMidPair     float32 = 0.5
	MHRMidTwoPair  float32 = 1.0
	MHRBotTwoPair  float32 = 0.5
	MHRBotTrips    float32 = 1.0
	// 2026-06-28 用户: 0.01 太小被rollout噪声淹没(#64/#90/#124 NN忽略). 各牌型加到"档gap内"的安全上限.
	MHRRankStep      float32 = 0.03 // 中/底 单对rank(#92) + 两对高对rank(#124). 满 12*0.03=0.36 < 0.5(两对→三条 gap). 不翻牌型.
	MHRKickerStep    float32 = 0.08 // 顶对 kicker(AAK>AAQ #64), 仅 66+(royalty对, 档gap=1.0). 满 0.96 < 1.0. 22-55 不给(MHRTopPairStep 0.2 档太窄会翻).
	// 三条 rank (555>333 #90). 2026-06-28 用户: 中三条 royalty=2 到顺=4 有 0-2 headroom, 加大到学得动.
	MHRMidTripsStep float32 = 0.15 // 中三条: 满 12*0.15=1.8, royalty2+1.8=3.8 < 4(→顺). 555-333=2*0.15=0.30 (≈4x 旧0.12, 过#64阈值0.08).
	MHRBotTripsStep float32 = 0.07 // 底三条: base1.0, 满 0.84, 1.0+0.84=1.84 < 2(→底顺). 比中小(底 headroom 只 1).
)

// pairRank5 — 5张手牌(中/底) TypePair 的对子 rank. Value=1e6+pair*15^4+k1*15^3+...
//   → pairRank = (Value-1e6)/50625. (顶3张是 /15, 编码不同.)
func pairRank5(ev HandValue) int {
	if ev.Type != TypePair {
		return 0
	}
	return int((ev.Value - 1000000) / 50625)
}

// twoPairRankReward — TwoPair 按 高对rank*step + 次对rank*(step/15) 破平 (22/TT>22/88 #124).
//   Value=2e6+hp*50625+lp*3375+kicker*225 (含joker版同编码). 满格 12*0.01+12*0.01/15≈0.128 < 0.5 → 不翻牌型.
func twoPairRankReward(ev HandValue) float32 {
	if ev.Type != TypeTwoPair {
		return 0
	}
	base := ev.Value - 2000000
	hp := clampRank(int(base / 50625))
	lp := clampRank(int((base % 50625) / 3375))
	return float32(hp)*MHRRankStep + float32(lp)*(MHRRankStep/15)
}

// tripsRank5 — 5张手牌(中/底) TypeThreeOfAKind 的三条 rank. Value=3e6+trip*50625+...
func tripsRank5(ev HandValue) int {
	if ev.Type != TypeThreeOfAKind {
		return 0
	}
	return clampRank(int((ev.Value - 3000000) / 50625))
}

// clampRank — 防御: rank 提取出界(如合成/空 HandValue) 时夹到 [0,12], 避免奖励失控.
func clampRank(r int) int {
	if r < 0 {
		return 0
	}
	if r > 12 {
		return 12
	}
	return r
}

func MadeHandRewardLabel(topEval, midEval, botEval HandValue) float32 {
	var r float32
	// 顶: 低对(<66 无royalty) 按 rank 递增. 取对子rank 同 TopRoyaltyFromEval.
	if topEval.Type == TypePair {
		pairRank := int((topEval.Value - 1000000) / 15)
		if pairRank >= 0 && pairRank <= 3 { // 卡牌 2-5 (rank idx 4=6 起 royalty)
			r += float32(pairRank+1) * MHRTopPairStep
		}
		// kicker 微奖: 顶对第3张 kicker rank (AAK>AAQ #64). 2026-06-28 仅 66+(pairRank≥4, royalty对,
		//   档gap=1.0, kicker满0.96<1.0 不翻). 22-55 不给(MHRTopPairStep 0.2 档太窄会翻 55+kicker>66).
		if pairRank >= 4 {
			kicker := int(topEval.Value-1000000) % 15
			r += float32(kicker) * MHRKickerStep
		}
	}
	switch midEval.Type {
	case TypePair:
		// 中单对 base 0.5 + rank 破平 (中KK>中QQ). 0.5+满0.36=0.86 < 1.0 两对.
		r += MHRMidPair + float32(pairRank5(midEval))*MHRRankStep
	case TypeTwoPair:
		// 中两对 base 1.0 + 高对/次对 rank 破平 (22/TT>22/88 #124). 1.0+满0.38 < 2(royalty三条).
		r += MHRMidTwoPair + twoPairRankReward(midEval)
	case TypeThreeOfAKind:
		// 中三条 rank (555>333 #90). 无base(royalty2接管), rank用0-1.8填headroom. 2+满1.8 < 4(顺).
		r += float32(tripsRank5(midEval)) * MHRMidTripsStep
	}
	switch botEval.Type {
	case TypePair:
		// 底单对 base 0 (你表"底一对不给"), 只按 rank 破平 (底QQ>底JJ #92). 满0.12 < 0.5 两对.
		r += float32(pairRank5(botEval)) * MHRRankStep
	case TypeTwoPair:
		// 底两对 base 0.5 + 高对/次对 rank 破平. 0.5+满0.13=0.63 < 1.0 三条.
		r += MHRBotTwoPair + twoPairRankReward(botEval)
	case TypeThreeOfAKind:
		// 底三条 base 1.0 + rank 破平. 1.0+满0.84=1.84 < 2(底顺 royalty).
		r += MHRBotTrips + float32(tripsRank5(botEval))*MHRBotTripsStep
	}
	return r
}

// ScoreHand — 完整 13 张 board 评分.
// foul → -20, 否则 = top+mid+bot royalty 总和.
// 进范 (fantasy=true) 仅作 flag, 不直接加分 (与 JS 一致).
//
// joker 用 cap-chain: bot (no cap) → mid (cap=bot) → top (cap=mid)
// cap chain 让鬼牌降级避免 auto-foul (与 JS evaluateBoardJoker 一致)
func ScoreHand(top, middle, bottom []Card) ScoreResult {
	if len(top) != 3 || len(middle) != 5 || len(bottom) != 5 {
		return ScoreResult{Foul: true, Score: -100, Royalties: 0}
	}
	hasJ := HasJoker(top, middle, bottom)
	var foul bool
	var te, me, be HandValue
	if hasJ {
		be = Evaluate5JokerCap(bottom, nil)
		me = Evaluate5JokerCap(middle, &be)
		te = Evaluate3JokerCap(top, &me)
		// foul 判定: 任一行 overCap (Type=-2) 或 比较失败
		if be.Type < 0 || me.Type < 0 || te.Type < 0 {
			foul = true
		} else {
			foul = HandExceeds5(me, be) || TopExceedsMid(te, me)
		}
	} else {
		te = Evaluate3(top)
		me = Evaluate5(middle)
		be = Evaluate5(bottom)
		// 0-joker 用 IsFoul 深比较 (kicker 级)
		foul = IsFoul(top, middle, bottom)
	}
	if foul {
		return ScoreResult{Foul: true, Score: -20, Royalties: 0,
			TopEval: te, MidEval: me, BotEval: be}
	}
	tR := TopRoyaltyFromEval(te)
	mR := MiddleRoyaltyFromEval(me)
	bR := BottomRoyaltyFromEval(be)
	total := tR + mR + bR
	return ScoreResult{
		Foul: false, Score: total, Royalties: total,
		TopRoyalty: tR, MidRoyalty: mR, BotRoyalty: bR,
		Fantasy: IsFantasyLandFromEval(te),
		TopEval: te, MidEval: me, BotEval: be,
	}
}
