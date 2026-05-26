package ofc

// features_v2.go — 128-d feature extractor.
// 替代 90-d BuildFeatures, 显式编码 joker wild / fantasy floor / deck-aware 等关键 strategic signal.
// 详细设计见 features_v2_design.md.
//
// idx 布局 (134 = 8+24+22+8+12+12+17+5+7+13+6):
//   A:  0-7    Board state
//   B:  8-31   Hand tier per row (top:6 + mid:9 + bot:9)
//   C:  32-53  Top fantasy progress
//   D:  54-61  Joker 全局状态
//   E:  62-73  Suit 分布 per row
//   F:  74-85  Straight draw 检测
//   G:  86-102 Deck awareness
//   H:  103-107 Foul 风险
//   I:  108-114 Pair preservation mid/bot
//   K:  115-127 Joker 完成 hand-type
//   L:  128-133 Cross-row anti-pattern (splits + kicker + gap1-orphan + slot-imbalance)

const FeatureDimV2 = 134

// BuildFeaturesV2 — 主入口. 输入 post-placement state, 返回 128-d feature.
func BuildFeaturesV2(gs *GameState) []float32 {
	f := make([]float32, FeatureDimV2)

	// 预算共享数据
	botEval := evalRowSafe(gs.Bottom, 5, nil)
	midEval := evalRowSafe(gs.Middle, 5, &botEval)
	// 重算 top 用 cap (mid 的 eval 作 cap)
	topEvalCapped := evalRowSafe(gs.Top, 3, &midEval)

	fillBoardState(f[0:8], gs)
	fillHandTiers(f[8:32], gs, topEvalCapped, midEval, botEval)
	fillTopFantasy(f[32:54], gs, topEvalCapped)
	fillJokerState(f[54:62], gs, topEvalCapped, midEval, botEval)
	fillSuitDist(f[62:74], gs)
	fillStraightDraw(f[74:86], gs)
	fillDeckAware(f[86:103], gs)
	fillFoulRisk(f[103:108], gs, topEvalCapped, midEval, botEval)
	fillPairSignals(f[108:115], gs, midEval, botEval)
	fillJokerCompletes(f[115:128], gs)
	fillCrossRowSplits(f[128:134], gs)

	return f
}

// ============ helpers ============

// evalRowSafe — 安全 eval (含 joker), 不完整行返回 high-card placeholder.
// row size 3 → Evaluate3JokerCap; size 5 → Evaluate5JokerCap.
func evalRowSafe(cards []Card, expectSize int, cap *HandValue) HandValue {
	if len(cards) == 0 {
		return HandValue{Type: TypeHighCard, Value: 0}
	}
	if len(cards) == expectSize {
		if expectSize == 3 {
			return Evaluate3JokerCap(cards, cap)
		}
		return Evaluate5JokerCap(cards, cap)
	}
	// 不完整: 凑伪 high card eval (用 rank 排序)
	return partialEval(cards)
}

// partialEval — 不完整行的简易 eval. 当前实现: 检 pair/trips, 否则 high card.
// 不处理 straight/flush (不完整时无意义).
func partialEval(cards []Card) HandValue {
	if len(cards) == 0 {
		return HandValue{Type: TypeHighCard, Value: 0}
	}
	var rankCnt [13]int
	jokerCnt := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCnt++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	maxCnt := 0
	maxRank := 0
	for r, n := range rankCnt {
		if n > maxCnt {
			maxCnt = n
			maxRank = r
		}
	}
	// joker 可助配 pair/trips
	effMax := maxCnt + jokerCnt
	if effMax >= 3 {
		return HandValue{Type: TypeThreeOfAKind, Value: int64(3000000 + maxRank*15)}
	}
	if effMax >= 2 {
		// 找 kicker
		kicker := 0
		for r := 12; r >= 0; r-- {
			if rankCnt[r] >= 1 && r != maxRank {
				kicker = r
				break
			}
		}
		return HandValue{Type: TypePair, Value: int64(1000000 + maxRank*15 + kicker)}
	}
	// high card
	var ranks []int
	for _, c := range cards {
		if !c.IsJoker() {
			ranks = append(ranks, int(c.Rank()))
		}
	}
	// 简单编码: 取 top 1 rank
	top := 0
	for _, r := range ranks {
		if r > top {
			top = r
		}
	}
	return HandValue{Type: TypeHighCard, Value: int64(top)}
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// ============ Group A: Board state ============

func fillBoardState(f []float32, gs *GameState) {
	topN := len(gs.Top)
	midN := len(gs.Middle)
	botN := len(gs.Bottom)
	f[0] = float32(topN) / 3
	f[1] = float32(midN) / 5
	f[2] = float32(botN) / 5
	f[3] = float32(3-topN) / 3
	f[4] = float32(5-midN) / 5
	f[5] = float32(5-botN) / 5
	f[6] = float32(gs.Round) / 5
	if topN == 3 && midN == 5 && botN == 5 {
		f[7] = 1
	}
}

// ============ Group B: Hand tier per row (24) ============

// fillHandTiers — top:6 + mid:9 + bot:9 = 24 维 one-hot
// Top tiers: HighCard / Pair<Q / Pair_Q / Pair_K / Pair_A / Trips
func fillHandTiers(f []float32, gs *GameState, topEv, midEv, botEv HandValue) {
	// Top (idx 0-5 in f[0:6])
	switch topEv.Type {
	case TypeHighCard:
		f[0] = 1
	case TypePair:
		pairRank := int((topEv.Value % 1000000) / 15)
		switch {
		case pairRank < RankQ:
			f[1] = 1
		case pairRank == RankQ:
			f[2] = 1
		case pairRank == RankK:
			f[3] = 1
		case pairRank == RankA:
			f[4] = 1
		}
	case TypeThreeOfAKind:
		f[5] = 1
	}
	// Mid (idx 6-14)
	fillMidBotTier(f[6:15], midEv)
	// Bot (idx 15-23)
	fillMidBotTier(f[15:24], botEv)
}

func fillMidBotTier(f []float32, ev HandValue) {
	// HighCard=0 Pair=1 TwoPair=2 Trips=3 Straight=4 Flush=5 FullHouse=6 Quads=7 SF=8
	switch ev.Type {
	case TypeHighCard:
		f[0] = 1
	case TypePair:
		f[1] = 1
	case TypeTwoPair:
		f[2] = 1
	case TypeThreeOfAKind:
		f[3] = 1
	case TypeStraight:
		f[4] = 1
	case TypeFlush:
		f[5] = 1
	case TypeFullHouse:
		f[6] = 1
	case TypeFourOfAKind:
		f[7] = 1
	case TypeStraightFlush, TypeRoyalFlush:
		f[8] = 1
	}
}

// ============ Group C: Top fantasy progress (22) ============

func fillTopFantasy(f []float32, gs *GameState, topEv HandValue) {
	// rankCnt 用于 pair_rank one-hot + wild_pair / floor logic
	var rankCnt [13]int
	jokerOnTop := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			jokerOnTop++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	maxRealCnt := 0
	maxRealRank := -1
	for r := 12; r >= 0; r-- {
		if rankCnt[r] > maxRealCnt {
			maxRealCnt = rankCnt[r]
			maxRealRank = r
		}
	}

	// idx 0-12: top_pair_rank_onehot[13] — only if real pair
	if maxRealCnt >= 2 && maxRealRank >= 0 {
		f[maxRealRank] = 1
	}
	// idx 13: top_has_real_pair
	if maxRealCnt >= 2 {
		f[13] = 1
	}
	// idx 14: top_has_wild_pair (joker + ≥1 rank)
	if jokerOnTop >= 1 && maxRealCnt >= 1 {
		f[14] = 1
	}
	// idx 15: top_has_real_trips
	if maxRealCnt >= 3 {
		f[15] = 1
	}
	// idx 16-20: top_fantasy_floor_tier_onehot[5] = [none, QQ, KK, AA, trips]
	// floor: joker 在 non-foul 前提下能 play 的最低 fantasy tier
	floorTier := fantasyFloorTier(rankCnt, jokerOnTop, maxRealCnt, maxRealRank)
	f[16+floorTier] = 1
	// idx 21: top_can_upgrade_to_AA
	if canUpgradeToAA(gs, jokerOnTop, floorTier) {
		f[21] = 1
	}
}

// fantasyFloorTier — 返回 0=none, 1=QQ, 2=KK, 3=AA, 4=trips
//
// Top with X+Q: joker plays as Q → QQ pair → tier 1
// Top with X+K: tier 2
// Top with X+A: tier 3
// Top with QQ (real): tier 1
// Top with X+X (2 jokers): tier 4 (any rank works)
// Top with AAA: tier 4
func fantasyFloorTier(rankCnt [13]int, jokerCnt, maxRealCnt, maxRealRank int) int {
	// trips: real or with joker assist
	if maxRealCnt >= 3 {
		return 4
	}
	if maxRealCnt >= 2 && jokerCnt >= 1 {
		// 2 real + 1 joker → trips (wild paired with existing pair = trips)
		return 4
	}
	if jokerCnt >= 2 && maxRealCnt >= 1 {
		// 1 real + 2 jokers → trips (both jokers paired with real rank)
		return 4
	}
	if jokerCnt >= 2 && maxRealCnt == 0 {
		// 2 jokers alone (top can hold 2 more cards): joker pairs are wild but no rank to pair
		// floor depends on future. Strictly: trips potential if any rank lands. Floor = pair_unknown.
		// 保守: 当作 pair (level depend on future). 返回 1 (QQ) as min guarantee since joker plays as Q+ to lock fantasy
		// 实际上 2 jokers 顶最终至少 trips. 但 floor 严格说不是 trips (depends on 3rd card).
		// 简化: 返回 4 (trips) — 2 jokers + 任一 ≥Q rank 都成 trips, 几乎必然达成
		return 4
	}

	// real pair: floor = that rank's fantasy tier
	if maxRealCnt >= 2 {
		switch maxRealRank {
		case RankQ:
			return 1
		case RankK:
			return 2
		case RankA:
			return 3
		default:
			return 0 // pair < Q
		}
	}
	// wild pair (1 joker + 1 real): floor = the real rank's tier
	if jokerCnt >= 1 && maxRealCnt >= 1 {
		// floor = joker 至少 play 成 maxRealRank, 形成 pair_maxRealRank
		switch maxRealRank {
		case RankQ:
			return 1
		case RankK:
			return 2
		case RankA:
			return 3
		default:
			return 0
		}
	}
	return 0
}

func canUpgradeToAA(gs *GameState, jokerOnTop int, floorTier int) bool {
	// 已经 AA / trips 不需 upgrade
	if floorTier >= 3 {
		return false
	}
	// 顶有 joker + 剩 slot
	if jokerOnTop == 0 {
		return false
	}
	if len(gs.Top) >= 3 {
		return false
	}
	// deck 还有 A 才能 upgrade
	for s := uint8(0); s < 4; s++ {
		c := MakeCard(RankA, s)
		if !gs.UsedCards[c.ID()] {
			return true
		}
	}
	return false
}

// ============ Group D: Joker state (8) ============

func fillJokerState(f []float32, gs *GameState, topEv, midEv, botEv HandValue) {
	jt := jokerCount(gs.Top)
	jm := jokerCount(gs.Middle)
	jb := jokerCount(gs.Bottom)
	total := jt + jm + jb
	inDeck := jokersInDeck(gs)

	f[0] = float32(jt) / 4
	f[1] = float32(jm) / 4
	f[2] = float32(jb) / 4
	f[3] = float32(total) / 4
	f[4] = float32(inDeck) / 4

	// joker_eff_rank: 用 HandValue 反推 joker 当前 plays 成的 rank.
	// 简化版: 取 eval 出的 max kicker rank (实际 cap-chain 较复杂)
	f[5] = jokerEffRank(gs.Top, topEv) / 12
	f[6] = jokerEffRank(gs.Middle, midEv) / 12
	f[7] = jokerEffRank(gs.Bottom, botEv) / 12
}

func jokerCount(cards []Card) int {
	n := 0
	for _, c := range cards {
		if c.IsJoker() {
			n++
		}
	}
	return n
}

func jokersInDeck(gs *GameState) int {
	used := 0
	for jid := uint8(0); jid < 4; jid++ {
		c := MakeJokerWithJID(jid)
		if gs.UsedCards[c.ID()] {
			used++
		}
	}
	// total possible = gs.NumJokers (但 gs 可能没该 field)
	// 简化: deck 总 jokers = ScoreHand 时假设 0/2/4. 这里返回 4 - used (上界)
	return 4 - used
}

// jokerEffRank — joker 在该行扮演的 rank.
// 简化: 若该行 type=Pair, joker 通常扮演 pair rank. type=Trips 同.
// type=HighCard 时, joker 扮演 max non-foul = 取 eval value 的 top kicker.
// 不完美但够 feature 用.
func jokerEffRank(cards []Card, ev HandValue) float32 {
	if jokerCount(cards) == 0 {
		return 0
	}
	switch ev.Type {
	case TypePair, TypeThreeOfAKind:
		pairRank := int((ev.Value % 1000000) / 15)
		return float32(pairRank)
	case TypeHighCard:
		// 取 top rank from kickers
		return float32(ev.Value % 15) // crude
	}
	return float32(RankA) // 高 hand 时 joker 通常 = A
}

// ============ Group E: Suit distribution (12) ============

func fillSuitDist(f []float32, gs *GameState) {
	// top: idx 0-3 (♠♥♦♣), normalize / 3
	suitCnt(gs.Top, f[0:4])
	for i := 0; i < 4; i++ {
		f[i] /= 3
	}
	// mid: idx 4-7, / 5
	suitCnt(gs.Middle, f[4:8])
	for i := 4; i < 8; i++ {
		f[i] /= 5
	}
	// bot: idx 8-11, / 5
	suitCnt(gs.Bottom, f[8:12])
	for i := 8; i < 12; i++ {
		f[i] /= 5
	}
}

func suitCnt(cards []Card, out []float32) {
	for i := range out {
		out[i] = 0
	}
	for _, c := range cards {
		if !c.IsJoker() {
			out[c.Suit()]++
		}
	}
}

// ============ Group F: Straight draw (12) ============

func fillStraightDraw(f []float32, gs *GameState) {
	f[0] = float32(consecutiveRunMax(gs.Top)) / 3
	f[1] = float32(consecutiveRunMax(gs.Middle)) / 5
	f[2] = float32(consecutiveRunMax(gs.Bottom)) / 5

	f[3] = boolF(hasFourCardOE(gs.Middle))
	f[4] = boolF(hasFourCardOE(gs.Bottom))

	f[5] = float32(straightOuts(gs.Middle, gs)) / 8
	f[6] = float32(straightOuts(gs.Bottom, gs)) / 8

	f[7] = float32(highCount(gs.Top)) / 3
	f[8] = float32(highCount(gs.Middle)) / 5
	f[9] = float32(highCount(gs.Bottom)) / 5

	f[10] = boolF(has3ConsecHigh(gs.Middle))
	f[11] = boolF(has3ConsecHigh(gs.Bottom))
}

// consecutiveRunMax — 该行最长连续 rank 子串 (非 joker), 含 A-low straight 不考虑此处.
func consecutiveRunMax(cards []Card) int {
	var has [13]bool
	for _, c := range cards {
		if !c.IsJoker() {
			has[c.Rank()] = true
		}
	}
	maxRun := 0
	cur := 0
	for r := 0; r < 13; r++ {
		if has[r] {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	return maxRun
}

// hasFourCardOE — 4 张连续 (open-ended), i.e. 最长 run >= 4.
func hasFourCardOE(cards []Card) bool {
	return consecutiveRunMax(cards) >= 4
}

// straightOuts — 完成 5-card straight 还需的 outs 中, deck 剩多少.
// 简化: 找 best 5-card straight window, 计算需要哪些 ranks, 看 deck 剩余.
// 完整算法复杂 (各种 gap 和 A-low), 这里给保守估算.
func straightOuts(cards []Card, gs *GameState) int {
	var has [13]bool
	for _, c := range cards {
		if !c.IsJoker() {
			has[c.Rank()] = true
		}
	}
	bestOuts := 0
	// 枚举所有 5-rank windows (含 A-low: 2-3-4-5-A, idx -1 to 3)
	for start := -1; start <= 8; start++ {
		// 窗口 ranks: start ~ start+4
		needRanks := []int{}
		for off := 0; off < 5; off++ {
			r := start + off
			if r == -1 {
				r = 12
			} // A-low
			if r < 0 || r >= 13 {
				continue
			}
			if !has[r] {
				needRanks = append(needRanks, r)
			}
		}
		// 此 window 需 needRanks ≤ 2 张才有意义 (差 1-2 张可补)
		if len(needRanks) > 2 {
			continue
		}
		// outs 总数 = needRanks 在 deck 剩余
		outs := 0
		for _, r := range needRanks {
			outs += rankRemainingInDeck(r, gs)
		}
		if outs > bestOuts {
			bestOuts = outs
		}
	}
	if bestOuts > 8 {
		bestOuts = 8
	}
	return bestOuts
}

func rankRemainingInDeck(rank int, gs *GameState) int {
	cnt := 0
	for s := uint8(0); s < 4; s++ {
		c := MakeCard(uint8(rank), s)
		if !gs.UsedCards[c.ID()] {
			cnt++
		}
	}
	return cnt
}

func highCount(cards []Card) int {
	n := 0
	for _, c := range cards {
		if !c.IsJoker() && c.Rank() >= RankT {
			n++
		}
	}
	return n
}

func has3ConsecHigh(cards []Card) bool {
	var has [13]bool
	for _, c := range cards {
		if !c.IsJoker() && c.Rank() >= RankT {
			has[c.Rank()] = true
		}
	}
	cur := 0
	for r := RankT; r <= RankA; r++ {
		if has[r] {
			cur++
			if cur >= 3 {
				return true
			}
		} else {
			cur = 0
		}
	}
	return false
}

// ============ Group G: Deck awareness (17) ============

func fillDeckAware(f []float32, gs *GameState) {
	// rank_remaining[13] (2→A) idx 0-12
	for r := 0; r < 13; r++ {
		f[r] = float32(rankRemainingInDeck(r, gs)) / 4
	}
	// suit_remaining[4] idx 13-16
	for s := 0; s < 4; s++ {
		cnt := 0
		for r := uint8(0); r < 13; r++ {
			c := MakeCard(r, uint8(s))
			if !gs.UsedCards[c.ID()] {
				cnt++
			}
		}
		f[13+s] = float32(cnt) / 13
	}
}

// ============ Group H: Foul risk (5) ============

func fillFoulRisk(f []float32, gs *GameState, topEv, midEv, botEv HandValue) {
	// foul_currently_inevitable: 当前 top > mid (rare 但 binary)
	if len(gs.Top) > 0 && len(gs.Middle) > 0 && topEv.Type > midEv.Type {
		f[0] = 1
	} else if len(gs.Top) > 0 && len(gs.Middle) > 0 && topEv.Type == midEv.Type && topEv.Value > midEv.Value {
		f[0] = 1
	}

	// strength_normalized: tier-based (0-9) / 9
	f[1] = float32(topEv.Type) / 9
	f[2] = float32(midEv.Type) / 9
	f[3] = float32(botEv.Type) / 9

	// min_margin: min(mid-top, bot-mid) tier diff / 9
	dMidTop := float32(midEv.Type-topEv.Type) / 9
	dBotMid := float32(botEv.Type-midEv.Type) / 9
	minMargin := dMidTop
	if dBotMid < minMargin {
		minMargin = dBotMid
	}
	f[4] = clampF(minMargin, -1, 1)
}

// ============ Group I: Pair preservation mid/bot (7) ============

func fillPairSignals(f []float32, gs *GameState, midEv, botEv HandValue) {
	// mid_max_pair_rank, bot_max_pair_rank
	f[0] = float32(maxPairRank(gs.Middle)) / 12
	f[1] = float32(maxPairRank(gs.Bottom)) / 12

	// has_real_pair / trips
	f[2] = boolF(hasRealPair(gs.Middle))
	f[3] = boolF(hasRealPair(gs.Bottom))
	f[4] = boolF(hasRealTrips(gs.Middle))
	f[5] = boolF(hasRealTrips(gs.Bottom))

	// bot_has_flush_potential: 3+ 同色
	f[6] = boolF(maxSuitCount(gs.Bottom) >= 3)
}

func maxPairRank(cards []Card) int {
	var rankCnt [13]int
	for _, c := range cards {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 2 {
			return r
		}
	}
	return 0
}

func hasRealPair(cards []Card) bool {
	var rankCnt [13]int
	for _, c := range cards {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	for _, n := range rankCnt {
		if n >= 2 {
			return true
		}
	}
	return false
}

func hasRealTrips(cards []Card) bool {
	var rankCnt [13]int
	for _, c := range cards {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	for _, n := range rankCnt {
		if n >= 3 {
			return true
		}
	}
	return false
}

func maxSuitCount(cards []Card) int {
	var suitCnt [4]int
	for _, c := range cards {
		if !c.IsJoker() {
			suitCnt[c.Suit()]++
		}
	}
	max := 0
	for _, n := range suitCnt {
		if n > max {
			max = n
		}
	}
	return max
}

// ============ Group K: Joker completes hand-type (13) ============

func fillJokerCompletes(f []float32, gs *GameState) {
	// idx 0: top_has_wild_trips
	{
		jt := jokerCount(gs.Top)
		maxR := maxRankCountReal(gs.Top)
		if maxR+jt >= 3 {
			f[0] = 1
		}
	}
	// idx 1-6: mid (pair, trips, quad, straight, flush, FH)
	fillRowCompletes(f[1:7], gs.Middle)
	// idx 7-12: bot
	fillRowCompletes(f[7:13], gs.Bottom)
}

func fillRowCompletes(f []float32, cards []Card) {
	j := jokerCount(cards)
	maxRank := maxRankCountReal(cards)
	consec := consecutiveRunMax(cards)
	maxSuit := maxSuitCount(cards)

	// pair
	if maxRank+j >= 2 {
		f[0] = 1
	}
	// trips
	if maxRank+j >= 3 {
		f[1] = 1
	}
	// quad (金刚)
	if maxRank+j >= 4 {
		f[2] = 1
	}
	// straight: consecutive_run + jokers >= 5
	if consec+j >= 5 {
		f[3] = 1
	}
	// flush: same-suit + jokers >= 5
	if maxSuit+j >= 5 {
		f[4] = 1
	}
	// full house: ∃ r1 != r2 with count[r1]+j1 >= 3 AND count[r2]+j2 >= 2, j1+j2 <= j
	if hasFullHouseWithJokers(cards, j) {
		f[5] = 1
	}
}

func maxRankCountReal(cards []Card) int {
	var rankCnt [13]int
	for _, c := range cards {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	max := 0
	for _, n := range rankCnt {
		if n > max {
			max = n
		}
	}
	return max
}

// ============ Group L: Cross-row anti-pattern signals (6) ============

// fillCrossRowSplits — splits + kicker + gap1-orphan + slot-imbalance
func fillCrossRowSplits(f []float32, gs *GameState) {
	// idx 0: pairs_split_count
	pairsSplit := countRankSplits(gs)
	f[0] = clampF(float32(pairsSplit)/4, 0, 1)

	// idx 1: flushgroup_split_count
	flushSplit := countFlushGroupSplits(gs)
	f[1] = clampF(float32(flushSplit)/4, 0, 1)

	// idx 2: connectors_split_count
	connSplit := countConnectorSplits(gs)
	f[2] = clampF(float32(connSplit)/6, 0, 1)

	// idx 3: bot_min_minus_mid_max_norm
	f[3] = botMidKickerOrder(gs)

	// idx 4: gap1_orphan_count
	//   (rank N 跟 N+2 同行 X) ∧ (rank N+1 在 ≠X) → "24 同 + 3 别" 反常
	gap1Orphan := countGap1Orphans(gs)
	f[4] = clampF(float32(gap1Orphan)/4, 0, 1)

	// idx 5: mid_minus_bot_fill_ratio (signed)
	//   midR - botR. 正值 = mid 比 bot 多 (anomaly), 负值 = bot 多 (正常 OFC)
	//   不含 top (top 满 = fantasy 锁定, 正常)
	//   2-3-0: +0.6 (严重 anomaly), 2-2-1: +0.2 (轻微), 2-1-2: -0.2 (正常)
	f[5] = midMinusBotFillRatio(gs)
}

// midMinusBotFillRatio — mid 填充比减 bot 填充比, signed
// 正值 = mid 重于 bot (反常, bot 该最满). 负值 = bot 重 (正常).
func midMinusBotFillRatio(gs *GameState) float32 {
	midR := float32(len(gs.Middle)) / 5
	botR := float32(len(gs.Bottom)) / 5
	return midR - botR
}

// countGap1Orphans — 检测 (N, N+2) 同行但 (N+1) 在别行的反常摆法
// 例: mid=[2c, 4h], bot=[3d] → (0, 2) 同 mid, 1 在 bot → +1
func countGap1Orphans(gs *GameState) int {
	count := 0
	for r := 0; r < 11; r++ {
		nInTop := rankInRow(gs.Top, r)
		nInMid := rankInRow(gs.Middle, r)
		nInBot := rankInRow(gs.Bottom, r)
		n2InTop := rankInRow(gs.Top, r+2)
		n2InMid := rankInRow(gs.Middle, r+2)
		n2InBot := rankInRow(gs.Bottom, r+2)

		// 确定 (r, r+2) 是否同行
		sharedRow := -1
		if nInTop && n2InTop {
			sharedRow = 0
		}
		if nInMid && n2InMid {
			sharedRow = 1
		}
		if nInBot && n2InBot {
			sharedRow = 2
		}
		if sharedRow == -1 {
			continue
		}

		// r+1 必须存在 (否则没"被孤立"概念)
		n1InTop := rankInRow(gs.Top, r+1)
		n1InMid := rankInRow(gs.Middle, r+1)
		n1InBot := rankInRow(gs.Bottom, r+1)
		n1Exists := n1InTop || n1InMid || n1InBot
		if !n1Exists {
			continue
		}

		// r+1 在 sharedRow → 没 orphan (实际是 3-consecutive); 在别行 → orphan
		n1InShared := false
		switch sharedRow {
		case 0:
			n1InShared = n1InTop
		case 1:
			n1InShared = n1InMid
		case 2:
			n1InShared = n1InBot
		}
		if !n1InShared {
			count++
		}
	}
	return count
}

func botMidKickerOrder(gs *GameState) float32 {
	botMin, botHas := minNonJokerRank(gs.Bottom)
	midMax, midHas := maxNonJokerRank(gs.Middle)
	if !botHas || !midHas {
		return 0
	}
	return float32(botMin-midMax) / 12
}

func minNonJokerRank(cards []Card) (int, bool) {
	minR := 13
	has := false
	for _, c := range cards {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		if r < minR {
			minR = r
		}
		has = true
	}
	if !has {
		return 0, false
	}
	return minR, true
}

func maxNonJokerRank(cards []Card) (int, bool) {
	maxR := -1
	has := false
	for _, c := range cards {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		if r > maxR {
			maxR = r
		}
		has = true
	}
	if !has {
		return 0, false
	}
	return maxR, true
}

// countRankSplits — 同 rank (≥2 张, 非 joker) 出现在 ≥2 行 的 rank 数
func countRankSplits(gs *GameState) int {
	count := 0
	for r := 0; r < 13; r++ {
		rowsWithRank := 0
		if rankInRow(gs.Top, r) {
			rowsWithRank++
		}
		if rankInRow(gs.Middle, r) {
			rowsWithRank++
		}
		if rankInRow(gs.Bottom, r) {
			rowsWithRank++
		}
		if rowsWithRank >= 2 {
			count++
		}
	}
	return count
}

func rankInRow(cards []Card, r int) bool {
	for _, c := range cards {
		if !c.IsJoker() && int(c.Rank()) == r {
			return true
		}
	}
	return false
}

// countFlushGroupSplits — 同 suit 总数 ≥3 张但散到 ≥2 行 的 suit 数
func countFlushGroupSplits(gs *GameState) int {
	count := 0
	for s := uint8(0); s < 4; s++ {
		total, rowsWith := suitDistribution(gs, s)
		if total >= 3 && rowsWith >= 2 {
			count++
		}
	}
	return count
}

func suitDistribution(gs *GameState, suit uint8) (total, rowsWith int) {
	tn := countSuit(gs.Top, suit)
	mn := countSuit(gs.Middle, suit)
	bn := countSuit(gs.Bottom, suit)
	total = tn + mn + bn
	if tn > 0 {
		rowsWith++
	}
	if mn > 0 {
		rowsWith++
	}
	if bn > 0 {
		rowsWith++
	}
	return
}

func countSuit(cards []Card, suit uint8) int {
	n := 0
	for _, c := range cards {
		if !c.IsJoker() && c.Suit() == suit {
			n++
		}
	}
	return n
}

// countConnectorSplits — (rank N) 与 (rank N+1) 在不同行且无共享行的对数.
// "共享行" = 某行同时含 rank N AND rank N+1, 此情况不算 split.
func countConnectorSplits(gs *GameState) int {
	count := 0
	for r := 0; r < 12; r++ {
		nInTop := rankInRow(gs.Top, r)
		nInMid := rankInRow(gs.Middle, r)
		nInBot := rankInRow(gs.Bottom, r)
		n1InTop := rankInRow(gs.Top, r+1)
		n1InMid := rankInRow(gs.Middle, r+1)
		n1InBot := rankInRow(gs.Bottom, r+1)

		// 任一行同时含 (N, N+1) → 共享, 不 split
		if (nInTop && n1InTop) || (nInMid && n1InMid) || (nInBot && n1InBot) {
			continue
		}
		// 两 rank 都存在但无共享行 → split
		hasN := nInTop || nInMid || nInBot
		hasN1 := n1InTop || n1InMid || n1InBot
		if hasN && hasN1 {
			count++
		}
	}
	return count
}

// hasFullHouseWithJokers — 检测能否凑 FH (trips + pair) 用现 cards + jokers
func hasFullHouseWithJokers(cards []Card, jokers int) bool {
	var rankCnt [13]int
	for _, c := range cards {
		if !c.IsJoker() {
			rankCnt[c.Rank()]++
		}
	}
	// 枚举哪个 rank 做 trips, 哪个做 pair
	for tripRank := 0; tripRank < 13; tripRank++ {
		needForTrips := 3 - rankCnt[tripRank]
		if needForTrips < 0 {
			needForTrips = 0
		}
		if needForTrips > jokers {
			continue
		}
		for pairRank := 0; pairRank < 13; pairRank++ {
			if pairRank == tripRank {
				continue
			}
			needForPair := 2 - rankCnt[pairRank]
			if needForPair < 0 {
				needForPair = 0
			}
			if needForTrips+needForPair <= jokers {
				return true
			}
		}
	}
	return false
}
