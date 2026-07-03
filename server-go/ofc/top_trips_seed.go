package ofc

// 「顶 trips 范种子合法性」特征 Z3 组 (sp28, 2026-06-21, dim160).
//
// 背景 (用户失败case 48): 顶[joker+3] 能种 333-trips = 范(任何顶trips都进范). 但前提是 333 ≤ 中道
// (否则倒置 foul). 48: 中是 666-trips, 顶该种 333(≤666 合法范) 不是 777(>666 必倒置 foul).
// value-head 既低估"合法trips范种子", 又看不出"顶trips > 中 = 未来倒置". W组只看"中≥顶当前",
// 抓不住"顶**未来trips** > 中". 也复用治 46 (顶222trips 中托不住).
//
// 信号: 顶能成trips(真对/鬼配对+发育) → trips score=3*13+R.
//   trips ≤ 中道当前成手 → +1 (稳的合法范种子, 该留)
//   trips > 中道乐观max → -1 (中托不住, 必倒置 foul)
//   之间 → 0 (不确定, 不误判发育中道)

func topTripsSeedScore(gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) float32 {
	var rc [13]int
	tj := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			tj++
		} else {
			rc[c.Rank()]++
		}
	}
	topSlots := 3 - len(gs.Top)
	midCur := int(rowMadeScore(gs.Middle))
	botMax := maxAchievableCmpCapped(gs.Bottom, 5-len(gs.Bottom), rankRem, suitRem, jokerRem, 999)
	midMax := maxAchievableCmpCapped(gs.Middle, 5-len(gs.Middle), rankRem, suitRem, jokerRem, botMax)
	// 2026-06-25 (#110): 修"孤鬼/空顶不识 trips 种子"bug — 原版要 rc[r]+tj>=2(至少1真牌+鬼),
	//   孤鬼[🃏](have=1)被判无种子. 实际 🃏+2空位 能摸2张同 rank 成 trips. 改: 按可补 topSlots 张算可达,
	//   且看"能不能成 ≤中 的合法范 trips"(玩家会选低 trips 进合法范), 不是只看最高 trips.
	legalSeed := false // 能成 ≤midCur 的合法范 trips (该留鬼种)
	anyTrips := false
	lowestTrips := 1 << 30
	for r := 0; r <= 12; r++ {
		need := 3 - rc[r] - tj // 还需摸几张真 r (鬼已当 r)
		if need < 0 {
			need = 0
		}
		if need <= topSlots && rankRem[r] >= need {
			anyTrips = true
			ts := int(HtThreeKind)*13 + r
			if ts < lowestTrips {
				lowestTrips = ts
			}
			if ts <= midCur {
				legalSeed = true // 顶能种 ≤中 的合法范 trips
			}
		}
	}
	if !anyTrips {
		return 0 // 顶无 trips 范种子
	}
	if legalSeed {
		return 1 // 稳的合法范种子, 该留 (#110/#48 孤鬼种 ≤中 低trips 走这里, 先返回, 碰不到下面)
	}
	// 2026-06-28 (#51): 顶若已是 QQ+ 对范(真高对/鬼配现有Q+/双鬼 = 0摸即成), 是安全的对子范路,
	//   不套"被迫trips必倒置"的 -1 (玩家会留对子范, 不会强凑foul trips). 治 🃏+Ad=AA 被误判foul险.
	//   区别 #110 孤鬼: 无真高牌, 成对也要摸, 跟trips种子一样待发育, 不触发(且已被上面 legalSeed +1 接走).
	topHighPairNow := tj >= 2
	for r := int(RankQ); r <= 12 && !topHighPairNow; r++ {
		if rc[r] >= 2 || (tj >= 1 && rc[r] >= 1) {
			topHighPairNow = true
		}
	}
	if topHighPairNow {
		return 0 // 已有安全对子范路, 不报trips foul
	}
	if lowestTrips > midMax {
		return -1 // 最低 trips 都 > 中乐观max → 必倒置 foul
	}
	return 0
}

func fillTopTripsSeed(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) {
	f[0] = topTripsSeedScore(gs, rankRem, suitRem, jokerRem)
}

// topFanProb — P(顶最终进范) = union(顶QQ/KK/AA exact 对, 合法 trips). [0,1].
// 2026-07-04 sp40 (用户): label 侧 seedBonus 概率加权用 — 平价 ±8 让 2% 烂trips种子和 40% 鬼配QQ种子
// 同价, 冲稀桶内 label (f160=1 组均值仅 9.14). f160 特征本体保持离散合法性 flag, 不动.
func topFanProb(gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) float32 {
	deckTotal := jokerRem
	for _, v := range rankRem {
		deckTotal += v
	}
	topSlots := 3 - len(gs.Top)
	pQ := pTopFinalPairExact(gs, RankQ, rankRem, jokerRem, deckTotal, topSlots)
	pK := pTopFinalPairExact(gs, RankK, rankRem, jokerRem, deckTotal, topSlots)
	pA := pTopFinalPairExact(gs, RankA, rankRem, jokerRem, deckTotal, topSlots)
	pT := pTopTrips(gs, rankRem, suitRem, jokerRem, deckTotal, topSlots)
	return clampF(1-(1-pQ)*(1-pK)*(1-pA)*(1-pT), 0, 1)
}
