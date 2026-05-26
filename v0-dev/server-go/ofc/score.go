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
