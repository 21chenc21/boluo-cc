package ofc

// 「前瞻支撑夹缝」特征 (sp28, 2026-06-19).
// 治 feature gap: 旧特征只描述"当前各行成什么手"+type-only foul-margin, 不描述
//   "未摆满的行还能发育成什么、能不能托住上行的承诺(夹进上承诺与下天花板之间)".
// 验证缺特征的 case: 46(222顶要中发育888夹底三条)/68(QQ顶要中发育2266)/66(中4477要底发育超)/76(中道留A发育值).
//
// 可比手值口径 = rowMadeScore = int(HandType)*13 + 主rank(0-12). joker-aware, 跨行 ordering 可比.

// maxAchievableCmpCapped — row + slots 张能发育出的最高可比手值, 且 ≤ ceil.
//   关键: 枚举该行能达到的 (tier,rank), 只保留 ≤ ceil 的取最大. 对/两对/三条/金刚 rank-precise;
//   顺/花/葫芦/同花顺 用 maxAchievableHandType 的 tier (rank=0 保守, 反正跨tier已压制).
//   乐观上界 (jokers 在各 tier 复用) —— 特征用足够.
func maxAchievableCmpCapped(row []Card, slots int, rankRem [13]int, suitRem [4]int, jokerRem, ceil int) int {
	var rc [13]int
	j := 0
	for _, c := range row {
		if c.IsJoker() {
			j++
		} else {
			rc[c.Rank()]++
		}
	}
	best := -1
	add := func(tier HandTypeEnum, rank int) {
		v := int(tier)*13 + rank
		if v <= ceil && v > best {
			best = v
		}
	}
	// 当前成手总能保持 (加牌只升不降)
	if cur := rowMadeScore(row); cur <= ceil && cur > best {
		best = cur
	}
	// reliableDraw — 抽 need 张 rank r "靠得住"判定 (2026-06-28 用户): 有效 outs = 真牌 rankRem[r] +
	//   可摸鬼 jokerRem (鬼万能补任意 rank, 故 +1鬼=+1out / +2鬼=+2out). 要求 ≥ need+1 (留1张余量,
	//   不赌某 rank 最后一张的"空中楼阁"). need<=0 (已成手) 恒真. 治"只看牌面发不出来还当可达". (鬼计入是关键.)
	reliableDraw := func(r, need int) bool {
		if need <= 0 {
			return true
		}
		return need <= slots && rankRem[r]+jokerRem >= need+1
	}
	// 对 / 三条 / 金刚 (rank-precise)
	for r := 12; r >= 0; r-- {
		have := rc[r] + j // jokers 当 r (乐观)
		// pair: 单张+1draw(靠谱outs) 或 鬼配/已成对 (合理)
		if have >= 2 || (rc[r] >= 1 && reliableDraw(r, 2-have)) {
			add(HtPair, r)
		}
		// trips/quads: 必须**已有对**(真对或鬼配) 才算可发育 — 不从单张硬凑三条(太投机, 否则 222顶+底777
		//   会被"中道单4硬凑444夹进缝"误判成可托). 这是关键: 看本行现有强度能不能托.
		if have >= 2 {
			if reliableDraw(r, 3-have) {
				add(HtThreeKind, r)
			}
			// 四条: 现实口径(2026-06-28 #24) — 需已有三条(have>=3, 即 ≤1摸, 且 outs 靠谱). 单对凑四条要摸2张
			//   特定牌, 概率太低不当稳托 (旧版 QQ6 误判可成 QQQQ → dim151 假+1.0 压掉倒置警告).
			if have >= 3 && reliableDraw(r, 4-have) {
				add(HtFourKind, r)
			}
		}
	}
	// 两对: 已成对(rc>=2)免费; 单张升对每个花 1 预算(slots+jokers). 受预算约束才能凑够 2 个对.
	//   (修: 旧版没管预算, 底[2389]+1slot 被误判成两对.)
	budget := slots + j   // 升对预算: 槽数 + 行内鬼
	jokerPool := jokerRem // 牌堆鬼共享池 — 救 last-card single→pair 按"补到2有效outs"消耗, 防一个鬼救多对
	var pr []int          // 预算内能成的对 rank, r 从高到低
	for r := 12; r >= 0; r-- {
		if rc[r] >= 2 {
			pr = append(pr, r) // 已成对, 免费
			continue
		}
		if rc[r] == 1 && budget > 0 {
			// single→pair 需 1 draw 靠谱(有效 outs ≥ 2): 真牌 rankRem 不足 2 时鬼补差额, 鬼是共享池用掉减少.
			needJokers := 2 - rankRem[r]
			if needJokers < 0 {
				needJokers = 0
			}
			if needJokers <= jokerPool {
				pr = append(pr, r)
				budget--
				jokerPool -= needJokers
			}
		}
	}
	if len(pr) >= 2 {
		add(HtTwoPair, pr[0]) // 最高对 rank
	}
	// 顺子 (rank-aware): 枚举 5-连窗, 取可达最高 high-card. 治"中9高顺 vs 底7高顺"同tier比rank.
	if sh := bestAchievableStraightHigh(rc, j, slots, rankRem); sh >= 0 {
		add(HtStraight, sh)
	}
	// 花 (rank-aware): 某门可凑满5张 → high-card = 该门可达最高 rank.
	if fh := bestAchievableFlushHigh(row, slots, suitRem, rankRem, j); fh >= 0 {
		add(HtFlush, fh)
	}
	// 葫芦/金刚/同花顺: tier-only (rank 估高). 现实口径(2026-06-28 #24) — 仅本行当前已 ≥两对时算高tier
	//   (两对→葫芦 ≤1摸, 三条→葫芦/四条 ≤1摸). 单对凑葫芦要2摸(三条+对)概率太低, 旧版 QQ6 误判可成
	//   QQQ66/QQQQ → dim151 假+1.0 稳托, 压掉 O-行序倒置警告 → NN 选了必 foul 摆法.
	if int(rowMadeScore(row))/13 >= int(HtTwoPair) {
		if t := maxAchievableHandType(row, slots, rankRem, suitRem, jokerRem); t >= HtFullHouse {
			add(t, 12)
		}
	}
	return best
}

// bestAchievableStraightHigh — 该行 slots+jokers 能凑出的最高顺子 high-card rank(index). 无 → -1.
//   枚举 high-card 从 A(12,即TJQKA) 到 6(4,即23456) + 轮子 A2345(high=5,index3).
func bestAchievableStraightHigh(rc [13]int, jokers, slots int, rankRem [13]int) int {
	check := func(ranks []int, highIdx int) int {
		nEmpty, nNonDrawable := 0, 0
		for _, r := range ranks {
			if rc[r] == 0 {
				nEmpty++
				if rankRem[r] == 0 {
					nNonDrawable++
				}
			}
		}
		// 空位用 draw(需rankRem) 或 joker(wild) 填: 总空 ≤ slots+jokers 且 非可draw的 ≤ jokers
		if nEmpty <= slots+jokers && nNonDrawable <= jokers {
			return highIdx
		}
		return -1
	}
	for hi := 12; hi >= 4; hi-- { // hi=highcard index; 窗 [hi-4..hi]
		if v := check([]int{hi - 4, hi - 3, hi - 2, hi - 1, hi}, hi); v >= 0 {
			return v
		}
	}
	// 轮子 A2345 (A=12 当低)
	if v := check([]int{12, 0, 1, 2, 3}, 3); v >= 0 {
		return v
	}
	return -1
}

// bestAchievableFlushHigh — 该行能凑满5张同花的最高 high-card rank. 无 → -1.
func bestAchievableFlushHigh(row []Card, slots int, suitRem [4]int, rankRem [13]int, jokers int) int {
	var suitCnt [4]int
	var maxRankInSuit [4]int
	for i := range maxRankInSuit {
		maxRankInSuit[i] = -1
	}
	for _, c := range row {
		if c.IsJoker() {
			continue
		}
		s := c.Suit()
		suitCnt[s]++
		if int(c.Rank()) > maxRankInSuit[s] {
			maxRankInSuit[s] = int(c.Rank())
		}
	}
	best := -1
	for s := 0; s < 4; s++ {
		need := 5 - suitCnt[s]
		if need <= 0 || (need <= slots+jokers && suitRem[s]+jokers >= need) {
			// high-card: 现有该门最高, 能draw则可更高 (取该门 live 最高 rank 估)
			hi := maxRankInSuit[s]
			if slots > 0 { // 还能 draw, 估该门可达最高 (12=A 若 live, 否则现有最高)
				for r := 12; r > hi; r-- {
					if rankRem[r] > 0 { // 近似: 该 rank 至少有牌剩 (不细分花色)
						hi = r
						break
					}
				}
			}
			if hi > best {
				best = hi
			}
		}
	}
	return best
}

// fillSupportHeadroom — 4 dim 前瞻支撑夹缝 (idx 150-153).
//   dim0 midSupportsTop: 中道能发育到的(≤底天花板)最高手值 − 顶承诺. <0 = 顶承诺无解必犯规, >0 = 有夹缝可赌.
//   dim1 botSupportsMid: 底道能发育到的最高手值 − 中道承诺. <0 = 中承诺底托不住.
//   dim2 topCommitTier:  顶道当前承诺 tier/8 (越高越该看 dim0).
//   dim3 midDevelopHeadroom: 中道(≤底天花板)还能发育多少 = midMaxCapped − 中道当前. 治"留关键张(A)发育值".
// 信号强 (±0.5 量级), 盖过旧 ±0.22 foul-margin —— "给够权重".
// 2026-06-21 SCALING fix: 旧版 /13 = "1 tier 即 clamp 1.0" → 把"两对支撑(1 tier)"和
//   "顺支撑(2.5 tier)"都压成 ~1.0, 支撑**强度**信号丢失 (64/76/98 实测 Δ 仅 0.077).
//   rowMadeScore = HandType*13+rank, 差值跨 0~4+ tier. 改 /60 (~4.6 tier=1.0) 保符号+给magnitude
//   → 64/76/98 Δ 0.077→0.30 (4x). 治"低估顺/支撑发育"主线 (用户 2026-06-21 失败case注释).
const supportScale = 60.0

func fillSupportHeadroom(f []float32, gs *GameState, rankRem [13]int, suitRem [4]int, jokerRem int) {
	topV := rowMadeScore(gs.Top)
	midV := rowMadeScore(gs.Middle)
	botV := rowMadeScore(gs.Bottom)
	midSlots := 5 - len(gs.Middle)
	botSlots := 5 - len(gs.Bottom)
	// 中道天花板 = 底道当前成手 (保守; 底前瞻发育由 dim1 单独管).
	// midMaxCapped 概率加权 (2026-07-03 sp37, 跟 MaxAchiev f117/118 的 probableMaxTier 同口径):
	//   "还能发育多高"按真实概率折 — improbable 高tier(#110 金刚靠摸最后1张6~2.5%)被压回, 不当空中楼阁满值.
	//   顺/三条/葫芦/金刚不假设摸鬼来烧, 同花/对子仍数鬼. probableMaxTier∈[0,1] → 还原 cmp-score(×8tier×13),
	//   cap 底天花板, floor 中当前成手. (旧乐观 maxAchievableCmpCapped 只要理论摸得到就当满值 → f153 虚高, #110.)
	cs := cardsSeenRemaining(gs)
	deckTotal := jokerRem
	for _, r := range rankRem {
		deckTotal += r
	}
	midMaxCapped := int(probableMaxTier(gs.Middle, midSlots, rankRem, suitRem, jokerRem, deckTotal, cs)*8*13 + 0.5)
	if midMaxCapped > botV {
		midMaxCapped = botV
	}
	if midMaxCapped < midV {
		midMaxCapped = midV
	}
	botMax := maxAchievableCmpCapped(gs.Bottom, botSlots, rankRem, suitRem, jokerRem, 999)
	f[0] = clampF(float32(midMaxCapped-topV)/supportScale, -1, 1)
	f[1] = clampF(float32(botMax-midV)/supportScale, -1, 1)
	f[2] = float32(partialEvalTP(gs.Top).Type) / 8.0
	f[3] = clampF(float32(midMaxCapped-midV)/supportScale, 0, 1)
}

// fillStrongHandMisplaced — W2 组 (2026-06-29 #23/#24, dim164): "强成手放错行" binary 信号.
//   底已成对+ 且 中道当前成手 > 底道当前成手 → 中压底(强牌该在底不在中) → -1.
//   治 "KK塞中(中KK>底QQ) 而非 KK→底凑KKQQ". 现存特征全线性 margin: KK vs QQ 才差1档 rank → 压成~0 抓不住;
//   W组又比"底max vs 中当前"漏了中会发育成KKK. 用 binary 当前成手比较 — gate"底成对+"排发育draw(高牌底),
//   用当前成手不用max(避空/未满中道的顺/花潜力误伤 exp).
func fillStrongHandMisplaced(f []float32, gs *GameState) {
	midMade := int(rowMadeScore(gs.Middle))
	botMade := int(rowMadeScore(gs.Bottom))
	if botMade >= int(HtPair)*13 && midMade > botMade {
		f[0] = -1
	}
}
