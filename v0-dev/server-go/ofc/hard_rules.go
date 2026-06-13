package ofc

// hard_rules.go — 打地鼠用 candidate filter (R1 + R2-R5).
// 在 candidate 枚举之后, rollout-Q 排序之前应用. 不修 rollout 内部.
// 任何 rule 把候选清空则跳过该 rule (保留前一步结果) — 即"宽容"应用.

// HardRuleVerbose — 可选 log (用于 debug 看哪个 rule 触发)
var HardRuleVerbose = false

// HardRulesDisabled — 若 true, 跳过所有 candidate filter (env DISABLE_HARD_RULES=1)
var HardRulesDisabled = false

// PolicyBoost — 把 head 3 (policy logit) 加权到 prerank score 中.
// 0 = 不用 policy (默认), 30 = 强 bias. 通过 env POLICY_BOOST 设置.
var PolicyBoost float32 = 0

// MctsDisabled — 若 true, ExpertPlace5/3 跳过 MCTS rollout, 直接取 prerank top-1.
// 用 env DISABLE_MCTS=1 设. 用于纯 MLP value-head 推理 (排除 rollout 干扰).
var MctsDisabled = false

// MctsSimsMult — MCTS 各 stage sims 全局倍率 (额外乘到 R1Mult 之上).
// 默认 1.0; env MCTS_SIMS_MULT=10 → 各 stage sims × 10 (更精, 更慢).
var MctsSimsMult float32 = 1.0

// MctsPrerankW — Stage 1 ranking 中 prerank (value head) 跟 rollout_mean 的权重.
// stage1_score = MctsPrerankW * prerank + (1 - MctsPrerankW) * rollout_mean
//   0 = 纯 rollout (默认, 老行为)
//   1 = 纯 prerank (跳过 stage 1 rollout, 直接 value-head 排 → 喂 stage 2)
//   0.5 = blend
// env MCTS_PRERANK_W 设. 用于诊断 rollout policy bias vs value head signal.
var MctsPrerankW float32 = 0

// MctsStage{1,2,3}Min — 2026-05-23 加, 控制 stage candidate 下限.
// default 0 = 用 expert_place.go 内 hardcoded 默认 (5/3/2)
// 测 top-N MCTS 效果: 设 s1=N s2=N s3=N
var MctsStage1Min int = 0
var MctsStage2Min int = 0
var MctsStage3Min int = 0

// MctsTopKSample — 2026-05-23 加. MctsDisabled/PureMLP 路径下 R1 从 NN prerank top-K 随机选 1.
// 0=top-1 deterministic, 2=top-2 随机, 5=top-5 随机.
var MctsTopKSample int = 0

// MctsTopKSampleRN — R2-R5 top-K sample. 默认 0 = top-1 deterministic (保 endgame quality).
// 测全 round sample 效果用: 设跟 MctsTopKSample 一样.
var MctsTopKSampleRN int = 0

// MctsDebugTrace — 若 true, ExpertPlace5/3 打印每 stage 的 top 候选 (prerank / stage1 / stage2 / stage3)
// 仅供单 case 调试用 (case-mcts-trace tool 启用). 输出到 stdout.
var MctsDebugTrace = false

// === Student NN 蒸馏部署 ===
// 训完 student ckpt 后部署用:
//   DISABLE_MCTS=1 POLICY_BOOST=30 SEED=42 ./ofc-go ...
// 效果: ExpertPlace5/3 跳过 rollout (DISABLE_MCTS), 用 value+policy 排序 (POLICY_BOOST 让 policy 主导).
// 没新加 flag, 复用现有 2 个 env.

// detectDealtPairs — 返回 rank → count (含 ≥2 的 ranks). joker 不算.
func detectDealtPairs(cards []Card) map[uint8]int {
	out := make(map[uint8]int)
	cnt := make(map[uint8]int)
	for _, c := range cards {
		if c.IsJoker() {
			continue
		}
		cnt[c.Rank()]++
	}
	for r, v := range cnt {
		if v >= 2 {
			out[r] = v
		}
	}
	return out
}

func dealtHasJoker(cards []Card) bool {
	for _, c := range cards {
		if c.IsJoker() {
			return true
		}
	}
	return false
}

func dealtHasA(cards []Card) bool {
	for _, c := range cards {
		if !c.IsJoker() && c.Rank() == RankA {
			return true
		}
	}
	return false
}

// detectFlushGroup — 返回 dealt 中 ≥3 张同 suit 的 indices (joker + A 排除).
// A 单独可上顶, 不强制跟 flush 在底.
func detectFlushGroup(cards []Card) []int {
	bySuit := make(map[uint8][]int)
	for i, c := range cards {
		if c.IsJoker() || c.Rank() == RankA {
			continue
		}
		bySuit[c.Suit()] = append(bySuit[c.Suit()], i)
	}
	out := []int{}
	for _, idxs := range bySuit {
		if len(idxs) >= 3 {
			out = append(out, idxs...)
		}
	}
	return out
}

func dealtJokerCount(cards []Card) int {
	n := 0
	for _, c := range cards {
		if c.IsJoker() {
			n++
		}
	}
	return n
}

// noAvailableAces — state.UsedCards 已含全部 4 个 A
func noAvailableAces(state *GameState) bool {
	for r := uint8(0); r < 4; r++ {
		c := MakeCard(RankA, r)
		if !state.UsedCards[c.ID()] {
			return false
		}
	}
	return true
}

// ============ R1 rules (Placement) ============

// r1RuleNoSplitDealtPair — dealt 同 rank ≥2 张必须同行
func r1RuleNoSplitDealtPair(p Placement, cards []Card) bool {
	pairs := detectDealtPairs(cards)
	if len(pairs) == 0 {
		return true
	}
	for rank, cnt := range pairs {
		// 2026-06-05: trips+ (≥3 同 rank) 允许拆 — 一对锁范上顶, 多余的拆去 mid/bot.
		// 否则三条 A 被强制同行 → AAA 上顶 foul trap (ypk-63963466-4). 只对 exactly-2 pair 强制不拆.
		if cnt >= 3 {
			continue
		}
		var firstRow Row
		first := true
		for i, c := range cards {
			if c.IsJoker() || c.Rank() != rank {
				continue
			}
			if first {
				firstRow = p[i]
				first = false
			} else if p[i] != firstRow {
				return false
			}
		}
	}
	return true
}

// r1RuleJokerWithA_OnTop — dealt 有 X + 单 A (无 AA pair) → X+A 必须都在 top
// 若 dealt 已有 AA pair, 此规则不应用 (AA 自身已锁 fantasy, 不需 X 配)
func r1RuleJokerWithA_OnTop(p Placement, cards []Card) bool {
	if !dealtHasJoker(cards) || !dealtHasA(cards) {
		return true
	}
	// AA pair 已经在 dealt, 不需 joker 配 A
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; ok && cnt >= 2 {
		return true
	}
	jokerOnTop := false
	aOnTop := false
	for i, c := range cards {
		if p[i] != RowTop {
			continue
		}
		if c.IsJoker() {
			jokerOnTop = true
		} else if c.Rank() == RankA {
			aOnTop = true
		}
	}
	return jokerOnTop && aOnTop
}

// r1RuleFlushGroup_OnBot — dealt 有 ≥3 同 suit → 全部上底
// 例外 (2026-05-13): dealt 还含 TT+ 大对子 → 跳过 (case 18 fix:
//   TT 已锁 royalty, ♦ 不必强压底, 让 6d 中保 mid draw)
func r1RuleFlushGroup_OnBot(p Placement, cards []Card) bool {
	grp := detectFlushGroup(cards)
	if len(grp) == 0 {
		return true
	}
	pairs := detectDealtPairs(cards)
	for rank := range pairs {
		if rank >= RankT { // TT+ pair → 已锁 royalty, flush 不强制全底
			return true
		}
	}
	for _, i := range grp {
		if p[i] != RowBottom {
			return false
		}
	}
	return true
}

// r1RuleDealtBigPair_Top — dealt 有 AA pair → 必须 上顶 (锁 fantasy)
// 不处理 KK (要看 deck 还有没 A, 较复杂)
func r1RuleDealtBigPair_Top(p Placement, cards []Card) bool {
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; ok && cnt >= 2 {
		// 2026-06-05: 要求 ≥2 张 A 上顶 (一对锁 fantasy), 不是"所有 A 上顶".
		// 三条 A 时旧逻辑强制全上顶 → AAA top foul trap (ypk-63963466-4).
		// 改成只要一对上顶, 多余的 A 可放 mid/bot (NN 决定, TE 偏好拆牌).
		acesOnTop := 0
		for i, c := range cards {
			if c.IsJoker() || c.Rank() != RankA {
				continue
			}
			if p[i] == RowTop {
				acesOnTop++
			}
		}
		if acesOnTop < 2 {
			return false
		}
	}
	return true
}

// r1RuleJokerNotOnTopWithAA — dealt 有 AA pair (real) → joker 不能上顶 (AA 已锁 fantasy, 不需 X 加 AAA wild)
func r1RuleJokerNotOnTopWithAA(p Placement, cards []Card) bool {
	pairs := detectDealtPairs(cards)
	cnt, ok := pairs[RankA]
	if !(ok && cnt >= 2) {
		return true
	}
	for i, c := range cards {
		if c.IsJoker() && p[i] == RowTop {
			return false
		}
	}
	return true
}

// r1RuleKK_OnBot_WithAA — R1 dealt 含 AA pair + KK pair → KK 必上底 (AA 顶, KK 底, 防 KK 中堵 foul)
// Pattern 修复: case 6 (X+KK+AA → AA top + KK bot + X mid/bot)
func r1RuleKK_OnBot_WithAA(p Placement, cards []Card) bool {
	pairs := detectDealtPairs(cards)
	cntA, okA := pairs[RankA]
	cntK, okK := pairs[RankK]
	if !(okA && cntA >= 2 && okK && cntK >= 2) {
		return true
	}
	for i, c := range cards {
		if !c.IsJoker() && c.Rank() == RankK && p[i] != RowBottom {
			return false
		}
	}
	return true
}

// r1RuleJokerOnBot_WithAA — R1 dealt 含 X + AA pair → joker 必上底 (AA 已锁 fantasy, joker 撑底)
// 例外: dealt 还含 KK/QQ trips/4-suit 等更强底候选 → 仍可 (此版本简化, 不区分)
// Pattern 修复: case 11 (X+AA+low → AA top + X bot)
func r1RuleJokerOnBot_WithAA(p Placement, cards []Card) bool {
	pairs := detectDealtPairs(cards)
	cnt, ok := pairs[RankA]
	if !(ok && cnt >= 2) {
		return true
	}
	for i, c := range cards {
		if c.IsJoker() && p[i] != RowBottom {
			return false
		}
	}
	return true
}

// r1RuleJokerOnTop_General — R1 dealt 含 joker, 无 AA pair → 至少 1 joker 上顶 (fantasy anchor)
// 例外: dealt 含 AA pair → r1RuleJokerNotOnTopWithAA 反向处理
// Pattern 修复: case 2 (X+Q+low) / case 56 (X+K+low) / case 11 (X+AA - 已被 AA 例外排除)
// 注: case 8 (2 jokers) — 任一 joker 上顶即满足
func r1RuleJokerOnTop_General(p Placement, cards []Card) bool {
	if !dealtHasJoker(cards) {
		return true
	}
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; ok && cnt >= 2 {
		return true // AA pair: 不强制 joker 上顶
	}
	// 至少 1 joker 在 top
	for i, c := range cards {
		if c.IsJoker() && p[i] == RowTop {
			return true
		}
	}
	return false
}

// ============ Fantasy Feasibility (deck-aware) ============

// HandTypeEnum — OFC hand 类型枚举 (低到高)
type HandTypeEnum int

const (
	HtHigh HandTypeEnum = iota
	HtPair
	HtTwoPair
	HtThreeKind
	HtStraight
	HtFlush
	HtFullHouse
	HtFourKind
	HtStraightFlush
)

// computeDeckRemaining — 给定 state, 算 deck 剩余各 rank/suit/joker 数
// 2026-05-22 fix: jokerRem 从 state.NumJokers 起算 (本局总鬼数), 不再写死 2.
// 之前写死 2 跟 NumJokers 脱钩 → 0/4 鬼局 feature 大错估. usedCards + state 现摆牌都扣.
func computeDeckRemaining(state *GameState) (rankRem [13]int, suitRem [4]int, jokerRem int) {
	jokerRem = state.NumJokers
	for r := 0; r < 13; r++ {
		rankRem[r] = 4
	}
	for s := 0; s < 4; s++ {
		suitRem[s] = 13
	}
	seen := make(map[string]bool)
	for cid := range state.UsedCards {
		seen[cid] = true
	}
	for _, c := range state.Top {
		seen[c.ID()] = true
	}
	for _, c := range state.Middle {
		seen[c.ID()] = true
	}
	for _, c := range state.Bottom {
		seen[c.ID()] = true
	}
	for cid := range seen {
		c, ok := ParseCard(cid)
		if !ok {
			continue
		}
		if c.IsJoker() {
			jokerRem--
		} else {
			rankRem[c.Rank()]--
			suitRem[c.Suit()]--
		}
	}
	if jokerRem < 0 {
		jokerRem = 0
	}
	return
}

// maxAchievableHandType — 估行能达到的最高 hand type, 给 deck 剩余 + slots
// 简化: 检查高到低, 返回首个可达
func maxAchievableHandType(rowCards []Card, slots int, rankRem [13]int, suitRem [4]int, jokerRem int) HandTypeEnum {
	var rowRankCnt [13]int
	var rowSuitCnt [4]int
	rowJokers := 0
	for _, c := range rowCards {
		if c.IsJoker() {
			rowJokers++
		} else {
			rowRankCnt[c.Rank()]++
			rowSuitCnt[c.Suit()]++
		}
	}
	rowSize := len(rowCards) + slots

	// 4-kind: 任 rank 凑 4 (row + joker wild + deck draws)
	for r := 0; r < 13; r++ {
		have := rowRankCnt[r] + rowJokers
		need := 4 - have
		if need <= 0 {
			return HtFourKind
		}
		if need <= slots && (rankRem[r]+jokerRem) >= need {
			return HtFourKind
		}
	}

	// Full house: trips of r3 + pair of r2 (r2 != r3)
	for r3 := 0; r3 < 13; r3++ {
		for r2 := 0; r2 < 13; r2++ {
			if r2 == r3 {
				continue
			}
			have3 := rowRankCnt[r3]
			have2 := rowRankCnt[r2]
			need3 := 3 - have3
			need2 := 2 - have2
			if need3 < 0 {
				need3 = 0
			}
			if need2 < 0 {
				need2 = 0
			}
			totalNeed := need3 + need2
			// 可用 cards (rank r3/r2 + jokers)
			totalAvail := rankRem[r3] + rankRem[r2] + rowJokers + jokerRem
			if totalNeed <= slots+rowJokers && totalAvail >= totalNeed {
				return HtFullHouse
			}
		}
	}

	// Flush: 5 同色 (joker wild)
	if rowSize >= 5 {
		for s := 0; s < 4; s++ {
			have := rowSuitCnt[s] + rowJokers
			need := 5 - have
			if need <= 0 {
				return HtFlush
			}
			if need <= slots && (suitRem[s]+jokerRem) >= need {
				return HtFlush
			}
		}
	}

	// Straight: 5-rank window, 缺口 ≤ slots + jokers (deck 有缺口卡)
	if rowSize >= 5 {
		for start := 0; start <= 8; start++ {
			ranksInWindow := 0
			deckInWindow := 0
			for r := start; r <= start+4; r++ {
				if rowRankCnt[r] > 0 {
					ranksInWindow++
				} else {
					deckInWindow += rankRem[r]
				}
			}
			need := 5 - ranksInWindow
			if need <= slots && (deckInWindow+rowJokers+jokerRem) >= need {
				return HtStraight
			}
		}
	}

	// Trips: 任 rank 凑 3
	for r := 0; r < 13; r++ {
		have := rowRankCnt[r] + rowJokers
		need := 3 - have
		if need <= 0 {
			return HtThreeKind
		}
		if need <= slots && (rankRem[r]+jokerRem) >= need {
			return HtThreeKind
		}
	}

	// 2-pair: 任 2 个 ranks 凑 pair
	pairCount := 0
	for r := 0; r < 13; r++ {
		have := rowRankCnt[r] + rowJokers
		need := 2 - have
		if need <= 0 {
			pairCount++
		} else if need <= slots && (rankRem[r]+jokerRem) >= need {
			pairCount++
		}
	}
	if pairCount >= 2 {
		return HtTwoPair
	}
	if pairCount >= 1 {
		return HtPair
	}
	return HtHigh
}

// canTopReachPairQPlus — top 能凑 pair Q+ 或 trips (fantasy 触发)
func canTopReachPairQPlus(state *GameState) bool {
	rankRem, _, jokerRem := computeDeckRemaining(state)
	var topRankCnt [13]int
	topJokers := 0
	for _, c := range state.Top {
		if c.IsJoker() {
			topJokers++
		} else {
			topRankCnt[c.Rank()]++
		}
	}
	topSlots := 3 - len(state.Top)
	// pair Q+
	for r := int(RankQ); r <= int(RankA); r++ {
		have := topRankCnt[r] + topJokers
		need := 2 - have
		if need <= 0 {
			return true
		}
		if need <= topSlots && (rankRem[r]+jokerRem) >= need {
			return true
		}
	}
	// trips (any rank)
	for r := 0; r < 13; r++ {
		have := topRankCnt[r] + topJokers
		need := 3 - have
		if need <= 0 {
			return true
		}
		if need <= topSlots && (rankRem[r]+jokerRem) >= need {
			return true
		}
	}
	return false
}

// FantasyLost — state 是否已经失去 fantasy 机会
// 检查:
//   - top 不能凑 pair Q+ / trips
//   - mid_max ≤ 2-pair (无法 > 2-pair → 用户要求)
//   - bot_max < mid_max (foul 必然)
func FantasyLost(state *GameState) bool {
	if !canTopReachPairQPlus(state) {
		return true
	}
	rankRem, suitRem, jokerRem := computeDeckRemaining(state)
	midSlots := 5 - len(state.Middle)
	botSlots := 5 - len(state.Bottom)
	midMax := maxAchievableHandType(state.Middle, midSlots, rankRem, suitRem, jokerRem)
	if midMax <= HtTwoPair {
		return true
	}
	botMax := maxAchievableHandType(state.Bottom, botSlots, rankRem, suitRem, jokerRem)
	if botMax < midMax {
		return true
	}
	return false
}

// rnRuleFantasyPossible — RN 应用候选后, 若 fantasy lost AND 当前 state 还没 lost → reject
func rnRuleFantasyPossible(a *RoundNAction, cards []Card, state *GameState) bool {
	// 只在 R2-R5 应用
	if state.Round < 2 || state.Round > 5 {
		return true
	}
	// 当前 state 已 lost, 不再过滤 (反正没救)
	if FantasyLost(state) {
		return true
	}
	// 模拟 post-state
	post := state.Clone()
	post.UsedCards[cards[a.DiscardIdx].ID()] = true
	for k, c := range a.Kept {
		post.PlaceCard(c, a.Placement[k])
	}
	// post 应仍 fantasy possible
	return !FantasyLost(post)
}

// canFantasyTopFinal — top 3 张是否可能 fantasy (pair≥Q / trips / joker+高牌)
func canFantasyTopFinal(topCards []Card) bool {
	if len(topCards) < 3 {
		return true // 未满, 未来可补
	}
	hasJoker := false
	var rankCnt [13]int
	hasHigh := false
	for _, c := range topCards {
		if c.IsJoker() {
			hasJoker = true
		} else {
			rankCnt[c.Rank()]++
			if int(c.Rank()) >= int(RankQ) {
				hasHigh = true
			}
		}
	}
	// trips?
	for _, n := range rankCnt {
		if n >= 3 {
			return true
		}
	}
	// pair ≥Q?
	for r := int(RankQ); r <= int(RankA); r++ {
		if rankCnt[r] >= 2 {
			return true
		}
	}
	// joker + 高牌 → 立刻 pair ≥Q via wild
	if hasJoker && hasHigh {
		return true
	}
	return false
}

// r1RuleTopMustAllowFantasy — R1 摆完 top 3 张但不能 fantasy → reject
func r1RuleTopMustAllowFantasy(p Placement, cards []Card) bool {
	var topCards []Card
	for i, c := range cards {
		if p[i] == RowTop {
			topCards = append(topCards, c)
		}
	}
	return canFantasyTopFinal(topCards)
}

// rnRuleTopMustAllowFantasy — RN action 摆完 top 3 张但不能 fantasy → reject
func rnRuleTopMustAllowFantasy(a *RoundNAction, cards []Card, state *GameState) bool {
	// 2026-05-20 sp15: 只在 R2-R3 触发. R4-R5 已临近终局, 是否能进范基本确定,
	// 强制保留 fantasy 路径会误杀"放弃 fantasy 走 mid flush draw 避 foul" 类合理策略 (case 44/50).
	if state.Round >= 4 {
		return true
	}
	topCards := append([]Card(nil), state.Top...)
	for k, c := range a.Kept {
		if a.Placement[k] == RowTop {
			topCards = append(topCards, c)
		}
	}
	return canFantasyTopFinal(topCards)
}

// R1Split2SuitPenalty — Pattern 2: dealt 有 2+ 同色卡, 但摆到不同行 (排除 top) → -5/对
// 例: dealt 有 Td + Jd (♦♦), AI 摆 中[Jd] 底[Td] → 拆 ♦ flush 苗 → penalty
// 例外: top 不算 (top 顶多 3 张, 不凑 flush)
// 例外: dealt 含 ≥3 同色由 r1RuleFlushGroup_OnBot 处理 (强制全上底)
func R1Split2SuitPenalty(p Placement, cards []Card) float32 {
	// 统计每 suit 出现位置
	suitRows := make(map[uint8][]Row)
	for i, c := range cards {
		if c.IsJoker() {
			continue
		}
		suitRows[c.Suit()] = append(suitRows[c.Suit()], p[i])
	}
	var penalty float32
	for _, rows := range suitRows {
		if len(rows) < 2 {
			continue
		}
		// 在 mid+bot 行中数它们的分布
		midCount, botCount := 0, 0
		for _, r := range rows {
			if r == RowMiddle {
				midCount++
			} else if r == RowBottom {
				botCount++
			}
		}
		if midCount >= 1 && botCount >= 1 {
			// 同色拆 mid + bot
			pairs := minInt(midCount, botCount)
			penalty += float32(pairs) * 5
		}
	}
	return penalty
}

// R1TopPairKickerEVPenalty — Pattern 6: top 已有 ≥1 张 K+ rank, dealt 有 A,
// 但 candidate 把 Q/K 上顶而不是 A → -8 (AA fan_bonus 80 vs QQ 20 vs KK 40)
// 仅 R1, R2-R5 由 rnRule 系列处理 top 完整性
func R1TopPairKickerEVPenalty(p Placement, cards []Card) float32 {
	// 检查 dealt 是否有 A
	hasA := false
	hasA_pos := -1
	for i, c := range cards {
		if !c.IsJoker() && c.Rank() == RankA {
			hasA = true
			hasA_pos = i
		}
	}
	if !hasA {
		return 0
	}
	// A 上顶 → 0 penalty
	if p[hasA_pos] == RowTop {
		return 0
	}
	// A 不在顶, 检查 top 有没有 Q/K (有 → 浪费 AA 机会)
	for i, c := range cards {
		if p[i] != RowTop || c.IsJoker() {
			continue
		}
		r := c.Rank()
		if r == RankQ || r == RankK {
			return 8 // top 已锁 Q/K, A 还没上 → 浪费 fan_bonus EV
		}
	}
	return 0
}

// R4FoulImminentPenalty — Pattern 1: R4 候选 apply 后, 若 mid/bot 已满 + top 待 1 张,
// 且 mid 是 high-card + top 已有比 mid 高的 rank → R5 给任何牌都 foul (top > mid 必然)
// 返回 +20 penalty (足够大让候选基本不被选)
// 注意: 此函数操作 POST-apply state (已经 apply 候选).
func R4FoulImminentPenalty(state *GameState) float32 {
	midSlots := 5 - len(state.Middle)
	botSlots := 5 - len(state.Bottom)
	topSlots := 3 - len(state.Top)
	if midSlots > 0 || botSlots > 0 || topSlots != 1 {
		return 0
	}
	if len(state.Middle) != 5 || len(state.Bottom) != 5 {
		return 0
	}
	midEval := Evaluate5(state.Middle)
	botEval := Evaluate5(state.Bottom)
	if midEval.Type > botEval.Type {
		return 20 // 已 foul
	}
	if midEval.Type != TypeHighCard {
		return 0 // mid 已经至少 pair, 跟 top 比一般不会爆
	}
	// mid 是 high-card, 看 top 最大 rank
	topMaxRank := -1
	for _, c := range state.Top {
		if c.IsJoker() {
			topMaxRank = 12 // joker 当 A
		} else if int(c.Rank()) > topMaxRank {
			topMaxRank = int(c.Rank())
		}
	}
	midMaxRank := -1
	for _, c := range state.Middle {
		if c.IsJoker() {
			midMaxRank = 12
		} else if int(c.Rank()) > midMaxRank {
			midMaxRank = int(c.Rank())
		}
	}
	if topMaxRank > midMaxRank {
		// top 已有比 mid 最高 rank 大的卡, R5 给任何牌:
		// - 若 R5 卡 < topMaxRank → top 仍 high-card with topMaxRank > mid high-card → foul
		// - 若 R5 卡 == top 某 rank → top 成 pair > mid high-card → foul
		// - 若 R5 卡 > topMaxRank → top high-card 升级, 还是 > mid → foul
		// → 无论 R5 怎样必 foul
		return 20
	}
	return 0
}

// R1TopKWhenJokerAFishPenalty — R1 dealt 含 joker, joker 上顶配 K → -10
// 理由: joker+K 锁死 KK, 浪费 joker 钓 A 升 AA 进范的机会
// 修正 2026-05-17: 只在 dealt 真有 joker 时 fire (不再用 deck-aware), 否则误伤普通 K-top 决策
// (case 17/22 类: TT底 + K单独上顶, 不该 fire)
func R1TopKWhenJokerAFishPenalty(p Placement, cards []Card, state *GameState) float32 {
	// 必须 dealt 含 joker
	dealtHasJoker := false
	for _, c := range cards {
		if c.IsJoker() {
			dealtHasJoker = true
			break
		}
	}
	if !dealtHasJoker {
		return 0
	}
	// 牌堆仍要有 A 可钓
	rankRem, _, _ := computeDeckRemaining(state)
	for _, c := range cards {
		if !c.IsJoker() {
			rankRem[c.Rank()]--
		}
	}
	if rankRem[RankA] < 1 {
		return 0
	}
	// joker 上顶 + 同 placement 还有 K 上顶 → 锁 KK
	jokerOnTop := false
	kOnTop := false
	for i, c := range cards {
		if p[i] != RowTop {
			continue
		}
		if c.IsJoker() {
			jokerOnTop = true
		} else if c.Rank() == RankK {
			kOnTop = true
		}
	}
	if jokerOnTop && kOnTop {
		return 10
	}
	return 0
}

// R1TopNonAKXPenalty — R1 top 含非 A/K/joker 卡 → 每张 -5 (2026-05-17 加重 2→5)
// 例外: 该 rank 在 usedCards 已 ≥3 张 (deck-aware, 余 ≤1) — 此时凑 trips fantasy 可行
// joker 不算 (wild)
// Pattern 4 修复: case 14/17 类 "硬塞头道" — NN value 没把 9/3 上顶的代价学透, 加重 penalty 直接 prune
func R1TopNonAKXPenalty(p Placement, cards []Card, state *GameState) float32 {
	var usedByRank [13]int
	for cid := range state.UsedCards {
		c, ok := ParseCard(cid)
		if !ok || c.IsJoker() {
			continue
		}
		usedByRank[c.Rank()]++
	}
	var penalty float32
	for i, c := range cards {
		if p[i] != RowTop || c.IsJoker() {
			continue
		}
		r := c.Rank()
		if r == RankA || r == RankK {
			continue
		}
		if usedByRank[r] >= 3 {
			continue
		}
		penalty += 5
	}
	return penalty
}

// R1IncoherentRowPenalty — R1 mid/bot 行 ≥3 张, 但既无 pair/trips, 又非纯色, 也非 ≥4-straight 潜力 → -2
// 即 "毫无成型潜力" 的杂烩行
func R1IncoherentRowPenalty(p Placement, cards []Card) float32 {
	rowCards := make(map[Row][]Card)
	for i, c := range cards {
		rowCards[p[i]] = append(rowCards[p[i]], c)
	}
	var penalty float32
	for row, cs := range rowCards {
		if row == RowTop || len(cs) < 3 {
			continue
		}
		// 检查 pair / trips / pure-suit / 4-straight 任一
		var rankCnt [13]int
		var suitCnt [4]int
		jokers := 0
		var ranks []int
		for _, c := range cs {
			if c.IsJoker() {
				jokers++
				continue
			}
			rankCnt[c.Rank()]++
			suitCnt[c.Suit()]++
			ranks = append(ranks, int(c.Rank()))
		}
		// pair / trips?
		hasPair := false
		for _, n := range rankCnt {
			if n+jokers >= 2 {
				hasPair = true
				break
			}
		}
		if hasPair {
			continue
		}
		// pure suit?
		placedSuits := 0
		for _, n := range suitCnt {
			if n > 0 {
				placedSuits++
			}
		}
		if placedSuits <= 1 {
			continue
		}
		// 3+ consecutive (joker wild fill, span ≤ 3) or ≥4 in 5-window?
		// sort ranks
		for i := 0; i < len(ranks); i++ {
			for j := i + 1; j < len(ranks); j++ {
				if ranks[i] > ranks[j] {
					ranks[i], ranks[j] = ranks[j], ranks[i]
				}
			}
		}
		hasStraight := false
		if len(ranks) > 0 {
			span := ranks[len(ranks)-1] - ranks[0] + 1
			missing := span - len(ranks)
			// 3-consecutive: span ≤ 3 and gaps fillable by joker
			if span <= 3 && missing <= jokers {
				hasStraight = true
			}
			// ≥4-card 5-window straight
			if !hasStraight && len(ranks)+jokers >= 4 && span <= 5 && missing <= jokers {
				hasStraight = true
			}
		}
		if hasStraight {
			continue
		}
		// 3-flush (joker wild)
		maxSuitCnt := 0
		for _, n := range suitCnt {
			if n > maxSuitCnt {
				maxSuitCnt = n
			}
		}
		if maxSuitCnt+jokers >= 3 {
			continue
		}
		penalty += 5
	}
	return penalty
}

// ============ R1 soft bonus/penalty (替原硬 filter) ============
// 2026-05-17: 用户要求把以下 R1 硬规则改成 score 调整, 让 prerank/MCTS 仍能 override
//
// R1JokerOnTopWithAAPenalty — dealt 含 AA pair + 任一 joker 上顶 → +20 penalty
// (替 r1RuleJokerNotOnTopWithAA; 不再 prune, 但强烈不鼓励)
func R1JokerOnTopWithAAPenalty(p Placement, cards []Card) float32 {
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; !ok || cnt < 2 {
		return 0
	}
	for i, c := range cards {
		if c.IsJoker() && p[i] == RowTop {
			return 20
		}
	}
	return 0
}

// R1JokerWithAOnTopBonus — dealt 含 X + 单 A (非 AA pair) AND 二者都在顶 → +10
// (替 r1RuleJokerWithA_OnTop; 鼓励配 AA fantasy)
func R1JokerWithAOnTopBonus(p Placement, cards []Card) float32 {
	if !dealtHasJoker(cards) {
		return 0
	}
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; ok && cnt >= 2 {
		return 0 // AA pair 走 DealtBigPair_Top
	}
	if !dealtHasA(cards) {
		return 0
	}
	xOnTop := false
	aOnTop := false
	for i, c := range cards {
		if p[i] != RowTop {
			continue
		}
		if c.IsJoker() {
			xOnTop = true
		} else if c.Rank() == RankA {
			aOnTop = true
		}
	}
	if xOnTop && aOnTop {
		return 10
	}
	return 0
}

// R1SingleAOnTopBonus — dealt 单 A 无 joker 无 AA pair, A 上顶 → +10
// (替 r1RuleSingleA_OnTop)
func R1SingleAOnTopBonus(p Placement, cards []Card) float32 {
	if dealtHasJoker(cards) {
		return 0
	}
	pairs := detectDealtPairs(cards)
	if cnt, ok := pairs[RankA]; ok && cnt >= 2 {
		return 0
	}
	if !dealtHasA(cards) {
		return 0
	}
	for i, c := range cards {
		if !c.IsJoker() && c.Rank() == RankA && p[i] == RowTop {
			return 10
		}
	}
	return 0
}

// R1SingleJokerNoAOnTopBonus — dealt 恰好 1 张 joker 且无 A, joker 放顶 → +5
// 用户 2026-06-03 (ypk-178127178-8 R1 [8h X 7c Qc 3c]): 单鬼无 A 时 NN 错把鬼埋中道配低张 (88),
// 应把鬼留顶 (追范/保持灵活). 无 A 限定避开 "鬼+A 配 AA fantasy" (走 R1JokerWithAOnTopBonus).
func R1SingleJokerNoAOnTopBonus(p Placement, cards []Card) float32 {
	if dealtHasA(cards) {
		return 0
	}
	jokers := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokers++
		}
	}
	if jokers != 1 {
		return 0
	}
	for i, c := range cards {
		if c.IsJoker() && p[i] == RowTop {
			return 5
		}
	}
	return 0
}

// R1FlushGroupOnBotBonus — dealt ≥3 同色 (不含 joker, 不含 A) 全部在底 → +5
// (替 r1RuleFlushGroup_OnBot; 去 TT+ 例外, 无条件加分)
func R1FlushGroupOnBotBonus(p Placement, cards []Card) float32 {
	groupIdxs := detectFlushGroup(cards)
	if len(groupIdxs) < 3 {
		return 0
	}
	allBot := true
	for _, i := range groupIdxs {
		if p[i] != RowBottom {
			allBot = false
			break
		}
	}
	if allBot {
		return 5
	}
	return 0
}

// ============ RN soft penalty (替原硬 filter) ============

// botAtLeastTwoPair — 底道成牌 ≥ 两对 (两对/葫芦/三条+/…). pre-guard 用: 底已强就不是"本轮新做".
func botAtLeastTwoPair(row []Card) bool {
	if len(row) == 5 {
		return Evaluate5JokerCap(row, nil).Type >= TypeTwoPair
	}
	var cnt [13]int
	j := 0
	for _, c := range row {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	pairs, maxc := 0, 0
	for _, n := range cnt {
		if n >= 2 {
			pairs++
		}
		if n > maxc {
			maxc = n
		}
	}
	return maxc+j >= 3 || pairs >= 2 // 三条+ 或 两对
}

// RnBotMakeTwoPairBonus — 本轮把底道从 <两对 做成 **≥两对** (如 QQ底 + 发KK → KKQQ) → +8.
// 通用. 2026-06-13 (ypk-88080714-8 R2): KK 该放底凑 KKQQ 强底, 别去丢 K 保 AA 顶干净.
// 关键全靠 pre-guard: 底已 ≥两对(含三条/葫芦, 如实战16/17 底TTT)→ 不奖 (非本轮新做).
//   → 实战16/17 底TTT 被 pre-guard 挡(到不了 post); case44 底顺draw无对不触发.
// post 用 ≥两对 (不限恰两对): 否则奖两对、不奖葫芦/金刚 = 不对称, 可能把更强的成葫芦摆法比下去.
func RnBotMakeTwoPairBonus(postState, preState *GameState) float32 {
	if botAtLeastTwoPair(preState.Bottom) {
		return 0 // 底已 ≥两对, 非本轮新做
	}
	if botAtLeastTwoPair(postState.Bottom) {
		return 8
	}
	return 0
}

// RnMidMakeTwoPairBonus 已删 (2026-06-13): kkMid 删后冗余 — 实测禁用它实战22 照样 KK→中
// (NN te 61.6 > Ks顶 54.7 自己就赢), std63/gamecase 零变化 = 这条没在翻任何 NN 的错.
// 软规则只该"刚好够翻 NN + 小余量", 不留 do-nothing 的死规则.

// partialEvalTP — 两对感知的部分行评估 (中>底 倒置比较专用).
// partialEval (features_v2.go) 只认 单对/三条: 遇两对 (如 [2s 2c Ks Kh]) 会
//   ① 误判成单对; ② 更糟 — 从低位扫 rankCnt 先撞到小对, 报成"22对"漏掉 KK.
// 导致"中两对 vs 底单对"倒置漏罚 (而三条倒置罚得到 = 不对称). 这里补两对.
//   满 5 张: 用 Evaluate5JokerCap (认花/顺/葫芦/四条).
//   <5 张: count-based, joker 优先补三条 (不做第二对), j==0 且 ≥2 真对子才算两对.
func partialEvalTP(cards []Card) HandValue {
	if len(cards) == 5 {
		return Evaluate5JokerCap(cards, nil)
	}
	var cnt [13]int
	j := 0
	for _, c := range cards {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	topRank, topCnt := -1, 0
	for r := 12; r >= 0; r-- { // 并列取高位 rank
		if cnt[r] > topCnt {
			topCnt = cnt[r]
			topRank = r
		}
	}
	if topCnt+j >= 3 { // 三条+ (joker 优先补三条, 不停在两对)
		return HandValue{Type: TypeThreeOfAKind, Value: int64(3000000 + topRank*15)}
	}
	if j == 0 { // 两对只在 j==0 时可能 (j≥1 会去补三条)
		var pairs []int
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 2 {
				pairs = append(pairs, r)
			}
		}
		if len(pairs) >= 2 {
			return HandValue{Type: TypeTwoPair, Value: int64(2000000 + pairs[0]*150 + pairs[1]*15)}
		}
	}
	if topCnt+j >= 2 { // 单对 (真对 或 joker 配最高单张)
		pairRank := topRank
		if topCnt < 2 {
			for r := 12; r >= 0; r-- {
				if cnt[r] >= 1 {
					pairRank = r
					break
				}
			}
		}
		kicker := 0
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 1 && r != pairRank {
				kicker = r
				break
			}
		}
		return HandValue{Type: TypePair, Value: int64(1000000 + pairRank*15 + kicker)}
	}
	top := 0
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 1 {
			top = r
			break
		}
	}
	return HandValue{Type: TypeHighCard, Value: int64(top)}
}

// RnMidExceedsBotPenalty — 候选造成"中道成牌 > 底道成牌"(违反 bot≥mid) → foul 倒置, 罚.
// 通用. 2026-06-13 (ypk-88080714-8 R2): bot=QQ, AI 把 KK→中 → 中KK > 底QQ = 倒置必犯规结构.
// 本质是"中比底大"(不依赖 top); KK 该放底跟 QQ 凑 KKQQ 两对. 只在中/底**都已成对+**时比,
// 避免误伤"中先成对、底还在发展"的正常过程.
// 2026-06-13 用 partialEvalTP (两对感知) 替 partialEval: 修"中两对>底单对"倒置漏罚
//   (编辑 case top=AA mid=2s2c bot=QhQc6h 发2dKsKh: KK→中成KK22两对压底QQ, 原漏罚).
func RnMidExceedsBotPenalty(postState *GameState) float32 {
	mid := partialEvalTP(postState.Middle)
	bot := partialEvalTP(postState.Bottom)
	if mid.Type < TypePair || bot.Type < TypePair {
		return 0 // 至少都成对才算"对梯倒置"
	}
	if HandExceeds5(mid, bot) {
		return 18 // 中 > 底 → 倒置, 罚 (接管 kkMid 删后的 hand1)
	}
	return 0
}

// RnQuadsJokerWastePenalty — 某行 4 张真同 rank (真四条) 且同行有鬼 → 鬼废成 kicker → 罚 (ypk-94634314-14).
func RnQuadsJokerWastePenalty(postState *GameState) float32 {
	var pen float32
	for _, row := range [][]Card{postState.Top, postState.Middle, postState.Bottom} {
		jokers := 0
		var cnt [13]int
		for _, c := range row {
			if c.IsJoker() {
				jokers++
			} else {
				cnt[c.Rank()]++
			}
		}
		if jokers == 0 {
			continue
		}
		for r := 0; r < 13; r++ {
			if cnt[r] >= 4 { // 4 张真同 rank = 真四条, 同行的鬼纯属多余 kicker
				pen += 15
			}
		}
	}
	return pen
}

// RnKKOnMidPenalty 已删 (2026-06-13): 粗暴"dealt KK 别上中" context-blind —
// hand1 KK→中(倒置)该罚, 但实战22 KK→中(凑KK77两对,底KKK>中)是好棋它误罚.
// 由 RnMidExceedsBotPenalty (中>底, 治本倒置, 升 -18) 接管. (detectDealtPairs 随之无用.)

// RnJokersSameRowPenalty — R2-R5 软 penalty (+10):
// post-action mid 或 bot 任一行含 ≥2 鬼牌 → 罚 10.
// 鼓励 X 分散 (不堆 mid 或 bot), 给 top fantasy lock 留余地.
// 2026-06-01 加: ypk-98042186-4 R2 case — NN 把 R2 dealt X 塞进 mid (跟 R1 X 同行),
// 错过 X+Kc 上头锁 AA fantasy 的远期收益. ypk-98042186-5 R2 测 -5 不够翻, 需 -10.
func RnJokersSameRowPenalty(a *RoundNAction, postState *GameState) float32 {
	midJokers, botJokers := 0, 0
	for _, c := range postState.Middle {
		if c.IsJoker() {
			midJokers++
		}
	}
	for _, c := range postState.Bottom {
		if c.IsJoker() {
			botJokers++
		}
	}
	if midJokers >= 2 || botJokers >= 2 {
		return 10
	}
	return 0
}

// RnSingleJokerTopChaseABonus — R2-R5 软 bonus (+8): 孤鬼(或鬼+sub-Q)在顶时, 放 1 张 A 上顶追 AA 范.
// 2026-06-05 (ypk-32571722-17 R3: top=[X] 发 3A, NN 误埋 AA→中 而非单 A 上顶追范).
// 触发:
//   ① pre-top 有鬼, 且"鬼能配出的对子 < QQ" (孤鬼 / 鬼+J以下) —— 即还没法直接进范, 加 A 才升 AA.
//      跳过 X+Q/X+K/XX/XA (鬼配对已 ≥QQ, 可直接进范, 不需 A; 你说的"已锁就不AA中").
//   ② 本轮恰好 1 张真 A 上顶 (post-top realA==1, 不成 AAA foul陷阱).
//   ③ 本轮没往中道加 A (废 A 放底, 不堵中道 —— A 进中变死高张, 挡顶道 AA 范 + 占两对位).
// cap-chain 保护: 中道弱时鬼自动降级 → 不犯规, 纯上行追范.
func RnSingleJokerTopChaseABonus(postState, preState *GameState) float32 {
	// ① pre-top 鬼 + 配对 < QQ
	jt, maxReal, preTopRealA := 0, -1, 0
	for _, c := range preState.Top {
		if c.IsJoker() {
			jt++
			continue
		}
		if int(c.Rank()) > maxReal {
			maxReal = int(c.Rank())
		}
		if c.Rank() == RankA {
			preTopRealA++
		}
	}
	if jt == 0 {
		return 0 // 非鬼顶, 不归这条
	}
	effPair := maxReal // 鬼配最高真牌
	if jt >= 2 {
		effPair = int(RankA) // 双鬼 = AA
	}
	if effPair >= int(RankQ) || preTopRealA > 0 {
		return 0 // 已可直接进 QQ+ 范 (X+Q/K, XX, XA) → 不追加 A
	}
	// ② 本轮恰好 1 张真 A 上顶 (不成 AAA)
	postTopRealA := 0
	for _, c := range postState.Top {
		if !c.IsJoker() && c.Rank() == RankA {
			postTopRealA++
		}
	}
	if postTopRealA != 1 {
		return 0
	}
	// ②b foul-squeeze 防护: top AA 需 mid ≥ 两对才托得住 (AA 是最大对, mid 必须两对+).
	// mid 已满且 < 两对 → top AA 实现不了 (cap-chain 降级成高牌, A 白废) → 不奖.
	// (case 50 R5: mid=KK 满, top 放 As 被 cap 成 A高, 还弃了该留的 7h)
	if len(postState.Middle) == 5 && Evaluate5JokerCap(postState.Middle, nil).Type < TypeTwoPair {
		return 0
	}
	// ③ 本轮没往中道塞 A (废 A 必须放底)
	midA := func(g *GameState) int {
		n := 0
		for _, c := range g.Middle {
			if !c.IsJoker() && c.Rank() == RankA {
				n++
			}
		}
		return n
	}
	if midA(postState) > midA(preState) {
		return 0
	}
	return 8
}

// RnLoneAceMidJokerTopPenalty — R2-R5 软罚 (+8 penalty): 鬼在顶 + 本轮往中道塞 1 张孤 A (中道最终恰 1 张A, 不成对).
// 2026-06-05 (实战16): 鬼+Q在顶升AA时, 废A应放底(留中道干净凑两对托顶AA), 放中是死A高张
// (没第2张A可配对) → 堵两对位 + 顶AA托不住. 正解: AA双进中成对(托顶) 或 A放头+废A放底.
// 只罚"孤A进中"; 双A进中成 AA对 (post mid A==2) 不罚 (那是强中道).
func RnLoneAceMidJokerTopPenalty(postState, preState *GameState) float32 {
	jt := 0
	for _, c := range preState.Top {
		if c.IsJoker() {
			jt++
		}
	}
	if jt == 0 {
		return 0 // 非鬼顶
	}
	midA := func(g *GameState) int {
		n := 0
		for _, c := range g.Middle {
			if !c.IsJoker() && c.Rank() == RankA {
				n++
			}
		}
		return n
	}
	pre, post := midA(preState), midA(postState)
	if post > pre && post == 1 {
		return 8 // 本轮加 A 进中 且 中道最终只 1 张孤 A (死张) → 罚
	}
	return 0
}

// RnTopTripsFantasyBonus — top 凑成 foul-safe 三条 (re-fan 锚 + 最高范 tier) → +bonus.
// 2026-06-11 (ypk-102367562-12 R4): top=[X X 3c]=333三条 vs top=[X X Ts]=AA对(被 mid 888 cap 住).
// NN value head 低估三条: te 158.26 (AA对) > 157.06 (333三条), 只差 1.2 → AI 选了 AA对弃了 3c.
// 但三条 = 17张范 + re-fan 锚 (fantasy.go FindReFanAnchors), AA对 = 16张范且不 re-fan.
// 用 mid cap 算 top 真实牌型 (X X Ts 被 cap 成 AA对不是 TTT, 故不奖; X X 3c 是 333 三条, 奖).
// 仅 top 满 3 张时触发; top 未满或非三条 → 0. 加性 bonus 不罚其它候选.
func RnTopTripsFantasyBonus(postState *GameState) float32 {
	if len(postState.Top) != 3 {
		return 0 // top 未满, 牌型未定
	}
	// top 能否确定成三条: 非鬼牌必须同 rank (鬼补齐). 跨 ≥2 rank 或全鬼 → 跳过.
	jt, distinct, tripRank := 0, 0, -1
	var seen [13]bool
	for _, c := range postState.Top {
		if c.IsJoker() {
			jt++
			continue
		}
		r := int(c.Rank())
		if !seen[r] {
			seen[r] = true
			distinct++
			tripRank = r
		}
	}
	if distinct != 1 {
		return 0 // 非鬼牌跨 ≥2 rank (顶其实是对/高张, X X Ts 被 mid cap 成 AA 对走这里) 或全鬼
	}
	// foul-safe: mid 现成牌型 ≥ 三条 of tripRank (行只增不减 → 当前 floor 即保证 ≤ mid final).
	midType, midTrip := midMadeFloor(postState.Middle)
	if midType > TypeThreeOfAKind || (midType == TypeThreeOfAKind && midTrip >= tripRank) {
		return 5 // foul-safe top 三条 (17张范 + re-fan): 翻过 NN ~1.2 gap
	}
	return 0
}

// midMadeFloor — mid 当前已成牌型下界 (行只增不减, 作 foul-safe 保证). 鬼牌计入最高 count 的 rank.
// 返回 (type, tripRank, 仅三条时有效). 只精确区分 ≥ 三条 (本规则只用这段); 弱于三条统一返回 (TypePair, -1).
//   mid 满 5 张: 用真实 eval (认得 花/顺/葫芦/四条 这些 > 三条但无对子计数的牌型).
//   mid 部分 (<5): 只能靠已落子的 count floor (花/顺 draw 未成, 不算保证) → pair/trips/quads via counts.
func midMadeFloor(mid []Card) (int, int) {
	if len(mid) == 5 {
		me := Evaluate5JokerCap(mid, nil)
		if me.Type == TypeThreeOfAKind {
			return TypeThreeOfAKind, int((me.Value - 3000000) / 50625) // makeValue: trip rank 在 15^4 位
		}
		return me.Type, -1 // 非三条: 花/顺/葫芦/四条 > 三条; 两对/对/高张 < 三条 (caller 据 type 判)
	}
	var cnt [13]int
	j := 0
	for _, c := range mid {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	bestRank, bestCnt := -1, 0
	for r := 12; r >= 0; r-- {
		if cnt[r] > bestCnt {
			bestCnt = cnt[r]
			bestRank = r
		}
	}
	if bestCnt+j >= 4 {
		return TypeFourOfAKind, bestRank // 四条 (> 三条) — rank 无关
	}
	if bestCnt+j >= 3 {
		return TypeThreeOfAKind, bestRank
	}
	return TypePair, -1 // < 三条, 不触发本规则
}

// RnTopTripsOvercommitPenalty — 顶把"已锁的 QQ+ 范对子"升成三条, 但中道现成牌型托不住该三条 → foul 风险, 罚.
// 2026-06-13 (ypk-70123850-2 R4): pre-top KK (已锁 15张范, 且 KK 对 < mid 222 三条 = 安全) + 发 Kd → 凑 KKK,
// KKK 三条 > mid 222 三条 → ~64% foul (mid 只剩 1 空, 要 2222/葫芦才托得住). 升级毫无意义还有害.
// 仅: pre-top 是 QQ+ 对 (范已锁) + post-top 凑成三条 + mid 现成牌型 < 该三条 → 罚 12.
// (注: 这是"风险"不是"必犯规", 故软罚而非 FoulImminentPenalty +20; mid 满且必犯规由 FoulImminent 兜.)
func RnTopTripsOvercommitPenalty(postState, preState *GameState) float32 {
	if len(preState.Top) != 2 || len(postState.Top) != 3 {
		return 0 // 只管"2张顶(QQ+对)本轮加 1 张升 3张"
	}
	// pre-top: 已成 QQ+ 对 (范锁)
	jt, reals := 0, []int{}
	for _, c := range preState.Top {
		if c.IsJoker() {
			jt++
		} else {
			reals = append(reals, int(c.Rank()))
		}
	}
	prePair := -1
	switch {
	case jt == 2:
		prePair = int(RankA)
	case jt == 1 && len(reals) == 1:
		prePair = reals[0]
	case jt == 0 && len(reals) == 2 && reals[0] == reals[1]:
		prePair = reals[0]
	}
	if prePair < int(RankQ) {
		return 0 // pre-top 不是 QQ+ 范锁 → 不归这条 (低对升三条可能是合理 re-fan)
	}
	// post-top: 凑成三条? 取三条 rank
	var cnt [13]int
	pj := 0
	for _, c := range postState.Top {
		if c.IsJoker() {
			pj++
		} else {
			cnt[c.Rank()]++
		}
	}
	topTrip := -1
	for r := 12; r >= 0; r-- {
		if cnt[r]+pj >= 3 {
			topTrip = r
			break
		}
	}
	if topTrip < 0 {
		return 0 // post-top 还是对子/高张 (没升三条) → 不罚
	}
	// mid 现成牌型托得住 top 三条吗? (行只增不减 → 现状是下界)
	midType, midTrip := midMadeFloor(postState.Middle)
	if midType > TypeThreeOfAKind || (midType == TypeThreeOfAKind && midTrip >= topTrip) {
		return 0 // mid 已 ≥ top 三条 → 安全, 升级是 free re-fan, 不罚
	}
	return 10 // mid 托不住 → foul 风险 → 罚 (翻过 NN 对 KKK 升级的高估). 2026-06-13 -12→-10: 余量 ~4.7→~2.7
}

// RnSingleAOnTopBonus 已删 (2026-06-13): case 29 太子自学会 / case 46 过严期望已放宽 / 帮不到手2 鬼+A. 退休.

// RnJokerAOnTopBonus — 本轮鬼+A 上顶锁 AA 范 → +10. 补 NN 对"鬼+A 锁顶范"的系统性低估.
// ⚠️ 这类软规则是针对**当前太子 NN 的具体偏差**校准的 (magnitude/触发都依赖太子的 te).
//    换模型 (尤其 sp24 激进版重奖 AA/范, NN 偏好会变) → 整套软硬规则可能需要**不同的配置**:
//    有的冗余(NN 自纠, 如已删的 RnSingleAOnTopBonus)、有的过火、magnitude 要重调.
//    promote 任何新 ckpt 前, 务必把这些规则 on/off + 重测 testcase/实战, 别假设沿用.
// 2026-06-13 (ypk-70123850-10 R2): top=[Kh]+发[Ah,X] → [Kh Ah X]=AA范锁, NN 排第3 (te 差 6.3).
// 实验证实 NN 恒偏好"X 撑底/中" > "锁顶AA", 牌好牌坏都一样 (跟低估 top-三条/范锁同根).
// 仅: 本轮往 top 加了鬼或A (有贡献) + post-top 恰 1鬼+1真A (=AA对范) + foul-squeeze guard.
// (双鬼/AAA 走 RnTopTripsFantasyBonus 等; 不重复奖.)
func RnJokerAOnTopBonus(a *RoundNAction, postState *GameState) float32 {
	// 必须本轮把鬼"带上顶"(新建 AA 锁). 鬼若已在顶(如 [X Qc]=QQ 已锁), 再加 A 是"追"不是"锁",
	// 该走 AA进中 (实战16/17/18) → 不奖. (孤鬼已在顶+加A 走 RnSingleJokerTopChaseABonus.)
	jokerAddedTop := false
	for k, c := range a.Kept {
		if a.Placement[k] == RowTop && c.IsJoker() {
			jokerAddedTop = true
			break
		}
	}
	if !jokerAddedTop {
		return 0
	}
	jt, ra := 0, 0
	for _, c := range postState.Top {
		if c.IsJoker() {
			jt++
		} else if c.Rank() == RankA {
			ra++
		}
	}
	if jt != 1 || ra != 1 {
		return 0 // 只奖 鬼+A=AA对 (1鬼1A); 双鬼/AAA 别处管
	}
	// foul-squeeze guard: mid 满且 < 两对 → top AA 托不住 (AA 是最大对, 需 mid 两对+) → 不奖
	if len(postState.Middle) == 5 && Evaluate5JokerCap(postState.Middle, nil).Type < TypeTwoPair {
		return 0
	}
	return 12 // 2026-06-13 +8→+12: ypk-174260554-28 R3 (顶[]+发[X As], 中22底KQJ) NN 偏好摊开 10.3, 用户判该锁 AA → 抬到能翻 (范率优先). 代价: 别的 foul-勉强局也会更倾向锁 AA.
}

// ============ FoulImminentPenalty (通用, R1-R5) ============
// 2026-05-17: 老 R4FoulImminentPenalty 只覆 R4 mid+bot 满 + top 缺 1.
// 通用化: 任何 partial state 下检测 foul 必然 → +20 penalty.
//
// 通用判定 (相对位置约束 bot ≥ mid ≥ top):
//   1) Mid/bot 都满 (len 5): mid.Type > bot.Type → 100% foul
//   2) Mid/bot 都满: mid 等 type 但 mid 值 > bot 值 → 100% foul
//   3) Top 满 (3 张) + mid 满 (5 张): top.Type > mid.Type → 100% foul
//   4) Top/mid 都满: top 等 type 但值 > mid → 100% foul
//   5) Mid full (high-card) + top fill 1 张 (任何 R5 卡补满) →
//       若 top 现 max rank > mid max rank → 必 foul (覆盖原 R4 case)
//
// 不要乱估 "未来可能 foul", 只检 100% 必然 case.
func FoulImminentPenalty(state *GameState) float32 {
	topFull := len(state.Top) == 3
	midFull := len(state.Middle) == 5
	botFull := len(state.Bottom) == 5

	// case 1+2: mid 满 + bot 满 → mid > bot ?
	// 2026-06-03: cap-aware. mid 含 joker 时用 Evaluate5JokerCap(mid, &bot) 把 joker 限制到 ≤ bot,
	// 避免 joker 被当最大值 (e.g. 中道 joker 补 heart flush 被算成 A-high flush > bot K-high flush) 误判 foul.
	// (ypk-178127178-8 R4: 中道 [8h X 3h 7h 2h] heart flush 应 ≤ bot K-high club flush, 不 foul)
	if midFull && botFull {
		bot := Evaluate5JokerCap(state.Bottom, nil)
		mid := Evaluate5JokerCap(state.Middle, &bot)
		if mid.Type < 0 {
			// mid 无法降到 ≤ bot (无 joker 且超 cap, 或纯超) → 必 foul
			return 20
		}
		if mid.Value > bot.Value {
			return 20 // 防御, cap 已限制
		}
	}
	// case 3+4: top 满 + mid 满 → top > mid ?
	// 2026-05-20 sp16: cap-aware. 用 Evaluate3JokerCap 传 mid 当 cap, 避免 joker+A 误判 foul (case 50).
	if topFull && midFull {
		mid := Evaluate5(state.Middle)
		// 用 cap-aware 算 top — joker 会被限制到 ≤ mid
		top := Evaluate3JokerCap(state.Top, &mid)
		if top.Type < 0 {
			// 全候选都 over cap (无 valid 配置) → 必 foul
			return 20
		}
		if top.Type > mid.Type {
			return 20 // 应该不会, cap 已限制. 防御.
		}
		if top.Type == mid.Type {
			if top.Type == TypePair {
				tRank := (top.Value - 1000000) / 15
				mRank := (mid.Value - 1000000) / 50625
				if tRank > mRank {
					return 20
				}
			} else if top.Type == TypeThreeOfAKind {
				tRank := (top.Value - 3000000) / 15
				mRank := (mid.Value - 3000000) / 50625
				if tRank > mRank {
					return 20
				}
			}
		}
	}
	// case 5: R4 兼容 — mid 满 high-card + top 2 张, top 最高 rank > mid 最高 rank → R5 必 foul
	if midFull && botFull && len(state.Top) == 2 {
		mid := Evaluate5(state.Middle)
		if mid.Type == TypeHighCard {
			topMaxRank := -1
			for _, c := range state.Top {
				r := int(c.Rank())
				if c.IsJoker() {
					r = 12
				}
				if r > topMaxRank {
					topMaxRank = r
				}
			}
			midMaxRank := -1
			for _, c := range state.Middle {
				r := int(c.Rank())
				if c.IsJoker() {
					r = 12
				}
				if r > midMaxRank {
					midMaxRank = r
				}
			}
			if topMaxRank > midMaxRank {
				return 20
			}
		}
	}
	// case 6: mid 满 + bot 部分 (<5) + bot 不可能凑出 ≥ mid.Type → 必 foul
	// 2026-05-20 sp15: case 45 R4 类 (mid clubs flush + bot 4 张无 flush 潜力 → 必 foul)
	if midFull && !botFull {
		mid := Evaluate5(state.Middle)
		botSlots := 5 - len(state.Bottom)
		if botSlots > 0 && mid.Type > TypeHighCard {
			rankRem, suitRem, jokerRem := computeDeckRemaining(state)
			botMax := maxAchievableHandType(state.Bottom, botSlots, rankRem, suitRem, jokerRem)
			if int(botMax) < mid.Type {
				return 20
			}
		}
	}
	return 0
}

// R1SameSuitInRowBonus — R1 行内 ≥2 张同色 (无 off-suit 稀释) → 加分
// 中/底行越多同色越好 (flush 种子无破)
// 例如: bot [Qs Js] 全 spade → +2; bot [Qs Js 9c] 不纯 → 0
func R1SameSuitInRowBonus(p Placement, cards []Card) float32 {
	rowCards := make(map[Row][]Card)
	for i, c := range cards {
		rowCards[p[i]] = append(rowCards[p[i]], c)
	}
	var bonus float32
	for row, cs := range rowCards {
		if row == RowTop || len(cs) < 2 {
			continue
		}
		var suitCnt [4]int
		hasJoker := false
		for _, c := range cs {
			if c.IsJoker() {
				hasJoker = true
				continue
			}
			suitCnt[c.Suit()]++
		}
		// 统计 placed suits
		placedSuits, maxSuitCount := 0, 0
		for _, n := range suitCnt {
			if n > 0 {
				placedSuits++
			}
			if n > maxSuitCount {
				maxSuitCount = n
			}
		}
		_ = hasJoker
		// 必须全同色 (joker 不计): placedSuits ≤ 1
		if placedSuits == 1 && maxSuitCount >= 2 {
			bonus += float32(maxSuitCount)
		}
	}
	return bonus
}

// RowPotentialScore — 启发式 行潜力分 (粗略概率 × royalty)
//   pair / flush / straight 三类种子 weighted by row royalty
// 思路: 同行 cards 越 coherent (同色/同 rank/连续 rank), 行潜力越大.
// 用于 prerank 加分, 鼓励 placing 让 row 更可能成型.
func RowPotentialScore(rowCards []Card, row Row) float32 {
	var suitCnt [4]int
	var rankCnt [13]int
	jokers := 0
	for _, c := range rowCards {
		if c.IsJoker() {
			jokers++
		} else {
			suitCnt[c.Suit()]++
			rankCnt[c.Rank()]++
		}
	}

	// pair seed (含 joker wild)
	maxPair, pairRank := 0, 0
	for r := 0; r < 13; r++ {
		if rankCnt[r] > maxPair {
			maxPair = rankCnt[r]
			pairRank = r
		}
	}
	pairWithJoker := maxPair + jokers

	// flush seed: 仅当 row 不混色 (placedSuits ≤ 1)
	maxSuit, placedSuits := 0, 0
	for s := 0; s < 4; s++ {
		if suitCnt[s] > maxSuit {
			maxSuit = suitCnt[s]
		}
		if suitCnt[s] > 0 {
			placedSuits++
		}
	}
	flushSeed := maxSuit + jokers
	if placedSuits >= 2 {
		flushSeed = 0 // 混色 → 不可能 flush
	}

	// straight seed: 最长 5-rank 滑动窗口内 distinct ranks + jokers
	maxRun := 0
	for start := 0; start <= 8; start++ {
		run := 0
		for r := start; r <= start+4; r++ {
			if rankCnt[r] > 0 {
				run++
			}
		}
		if run+jokers > maxRun {
			maxRun = run + jokers
		}
	}
	if maxRun > 5 {
		maxRun = 5
	}

	var score float32
	switch row {
	case RowTop:
		// top: QQ+ pair 锁 fantasy (适度奖励, 别太重)
		if pairWithJoker >= 2 && pairRank >= int(RankQ) {
			score += 3
		}
		if pairWithJoker >= 3 {
			score += float32(10+pairRank) * 0.5
		}
		// 顶单 joker: 未来配 high pair 进范的潜力 (joker 灵活)
		if jokers == 1 && len(rowCards) == 1 {
			score += 3
		}
	case RowMiddle:
		if pairWithJoker >= 2 && pairRank >= int(Rank6) {
			score += 1
		}
		if pairWithJoker >= 3 {
			score += 2
		}
		if pairWithJoker >= 4 {
			score += 18
		}
		if flushSeed >= 2 {
			score += 8 * float32(flushSeed) / 5.0
		}
		if maxRun >= 2 {
			score += 4 * float32(maxRun) / 5.0
		}
	case RowBottom:
		// bot pair: high pair (≥T) 是底 anchor 价值高, 低 pair 价值低
		if pairWithJoker >= 2 {
			if pairRank >= int(RankT) {
				score += 3 + float32(pairRank-int(RankT))*0.5
			} else {
				score += 1
			}
		}
		if pairWithJoker >= 3 {
			score += 2
		}
		if pairWithJoker >= 4 {
			score += 8
		}
		if flushSeed >= 2 {
			score += 4 * float32(flushSeed) / 5.0
		}
		if maxRun >= 2 {
			score += 2 * float32(maxRun) / 5.0
		}
	}
	return score
}

// AllRowsPotentialScore — 各行 RowPotentialScore 求和
func AllRowsPotentialScore(p Placement, cards []Card) float32 {
	var top, mid, bot []Card
	for i, c := range cards {
		switch p[i] {
		case RowTop:
			top = append(top, c)
		case RowMiddle:
			mid = append(mid, c)
		case RowBottom:
			bot = append(bot, c)
		}
	}
	return RowPotentialScore(top, RowTop) +
		RowPotentialScore(mid, RowMiddle) +
		RowPotentialScore(bot, RowBottom)
}

// R1FourInRowPenalty — R1 任意 row (mid/bot) 4 张或 5 张全堆, 强 draw / 同 rank 集中 除外 → 扣分
// 例外 (4-row):
//   - 4-flush (4 同色) 或 ≥4-straight (4 连张): 强 draw
//   - ≥3 同 rank (trips 或 quads, 同 row 才合理, 不能拆)
// 例外: top 4 张 不在此列 (top 最多 3 张).
// 触发:
//   - 4 张同行无 hand-type 苗 → -5
//   - 5 张同行 (mid/bot 占满) → -15 (R1 极不平衡, 浪费 R2-5 灵活性; 例外: 同花/顺子 给小幅 penalty)
func R1FourInRowPenalty(p Placement, cards []Card) float32 {
	rowCards := make(map[Row][]Card)
	for i, c := range cards {
		rowCards[p[i]] = append(rowCards[p[i]], c)
	}
	var penalty float32
	for row, cs := range rowCards {
		if row == RowTop {
			continue
		}
		// 5 张全一行: 几乎总是 anti-pattern. 例外: 强 hand-type (顺子/同花) 减轻
		if len(cs) == 5 {
			if isFlush5(cs) || isStraight5(cs) {
				penalty += 5 // 还是 unbalanced, 但有 5-card hand 价值
			} else {
				penalty += 15 // 一般 5-card 无 hand-type → 重罚
			}
			continue
		}
		if len(cs) != 4 {
			continue
		}
		if isFourSameSuit(cs) || isFourConsecutive(cs) || hasThreeSameRank(cs) {
			continue
		}
		penalty += 5
	}
	return penalty
}

// isFlush5 — 5 张全同色 (joker wild 算入)
func isFlush5(cs []Card) bool {
	if len(cs) != 5 {
		return false
	}
	suitCnt := map[uint8]int{}
	jokers := 0
	for _, c := range cs {
		if c.IsJoker() {
			jokers++
			continue
		}
		suitCnt[c.Suit()]++
	}
	for _, n := range suitCnt {
		if n+jokers >= 5 {
			return true
		}
	}
	return false
}

// isStraight5 — 5 张顺子 (含 joker wild fill)
func isStraight5(cs []Card) bool {
	if len(cs) != 5 {
		return false
	}
	v := Evaluate5JokerCap(cs, nil)
	return v.Type == TypeStraight || v.Type == TypeStraightFlush
}

func hasThreeSameRank(cs []Card) bool {
	rankCnt := map[uint8]int{}
	jokers := 0
	for _, c := range cs {
		if c.IsJoker() {
			jokers++
			continue
		}
		rankCnt[c.Rank()]++
	}
	for _, n := range rankCnt {
		if n+jokers >= 3 {
			return true
		}
	}
	return false
}

func isFourSameSuit(cs []Card) bool {
	if len(cs) != 4 {
		return false
	}
	// joker 算 wild (任何 suit OK)
	var suit uint8 = 255
	for _, c := range cs {
		if c.IsJoker() {
			continue
		}
		s := c.Suit()
		if suit == 255 {
			suit = s
		} else if s != suit {
			return false
		}
	}
	return true
}

func isFourConsecutive(cs []Card) bool {
	if len(cs) != 4 {
		return false
	}
	ranks := []int{}
	jokers := 0
	for _, c := range cs {
		if c.IsJoker() {
			jokers++
		} else {
			ranks = append(ranks, int(c.Rank()))
		}
	}
	// 排序
	for i := 0; i < len(ranks); i++ {
		for j := i + 1; j < len(ranks); j++ {
			if ranks[i] > ranks[j] {
				ranks[i], ranks[j] = ranks[j], ranks[i]
			}
		}
	}
	// 计 gap. 4 张 + j jokers. 需要 (max - min + 1) ≤ 5 (最多 1 个空位让 future 补成 5-straight)
	if len(ranks) == 0 {
		return true // 全是 joker
	}
	span := ranks[len(ranks)-1] - ranks[0] + 1
	// 加 joker 可填 1 位 → 检查 span - len(ranks) (内部 gap 总数) ≤ jokers + 1 (允许尾部留 1 个 future card)
	missing := span - len(ranks)
	return missing <= jokers+1 && span <= 5
}

// ConnectorSplitPenalty — straight 潜力 + 中底 hierarchy 扣分 (soft penalty)
//   1. 跨行 split: 低 rank (lower < Rank6) 不罚 — 低连张无 straight 潜力
//      rank diff ≤ 2 dealt 对被 split: d=1 → +5, d=2 → +2
//   2. 每对 (mid_card, bot_card): mid > bot → +3 (违反 bot ≥ mid hierarchy)
// 例外: KA 连张 (K-A): 不扣 (fantasy lock 常分行)
// 2026-05-13 加 (跨行 gap=1 only)
// 2026-05-15 扩到 gap≤4 + 加 mid>bot per-pair 罚
// 2026-05-20 sp15: 跳过 lower rank<Rank6 + 罚值减 (8→5/3→2/5→3) — case 15 R1 误罚 3-4 split
func ConnectorSplitPenalty(p Placement, cards []Card) float32 {
	rankInfo := make(map[uint8][]Row)
	midRanks := []int{}
	botRanks := []int{}
	for i, c := range cards {
		if c.IsJoker() {
			continue
		}
		rankInfo[c.Rank()] = append(rankInfo[c.Rank()], p[i])
		r := int(c.Rank())
		switch p[i] {
		case RowMiddle:
			midRanks = append(midRanks, r)
		case RowBottom:
			botRanks = append(botRanks, r)
		}
	}
	var penalty float32
	// 跨行 split
	// 2026-05-20 sp15: d≥3 已删 (V3 L2 features 已传信号给 NN); 加 lower rank<Rank6 skip;
	// 罚值减 (8→5 / 3→2) 给 NN 更多自由度.
	for r := uint8(0); r < 13; r++ {
		// 跳过低连张: 最低 rank < 6 (即 2-3, 3-4, 4-5, 5-6) — 实际无 straight 潜力, 拆不亏.
		if r < Rank6 {
			continue
		}
		for d := uint8(1); d <= 2; d++ {
			r2 := r + d
			if r2 >= 13 {
				continue
			}
			if r == RankK && r2 == RankA {
				continue
			}
			v1, ok1 := rankInfo[r]
			v2, ok2 := rankInfo[r2]
			if !ok1 || !ok2 {
				continue
			}
			// 2026-06-05: 跳过成对/三条的 rank — 它是 made pair/trips, 不是顺子连张,
			// 拆"三条J + 对Q"不是破顺子 (JJJ+QQ 被误罚 +30 → 避开 QQ追范/葫芦). ypk JcQcJdQdJs.
			if len(v1) >= 2 || len(v2) >= 2 {
				continue
			}
			for _, a := range v1 {
				for _, b := range v2 {
					if a == b {
						continue
					}
					if d == 1 {
						penalty += 5 // adjacent (e.g. 8-9 split), 8→5
					} else {
						penalty += 2 // gap 1 (e.g. 8-T split), 3→2
					}
				}
			}
		}
	}
	// 每对 (mid, bot) mid > bot → +3 (违反 bot ≥ mid hierarchy, sp15: 5→3 减重)
	// 注: 这里按 rank 比较 (不是牌型). 对"低对在底 + 高单在中"是真 foul 风险 (case 26),
	// 保留. JJJ+QQ 的误罚主要在上面连张 split 部分, 已跳过成对/三条 → 这里残留 +18 不影响结果.
	for _, mr := range midRanks {
		for _, br := range botRanks {
			if mr > br {
				penalty += 3
			}
		}
	}
	return penalty
}

// botHasDrawOrPair — bot 是否有 flush/straight/pair (含 joker wild)
//   ≥3 同色 (potential flush) | ≥3 consecutive (potential straight) | 任意 pair
func botHasDrawOrPair(p Placement, cards []Card) bool {
	botCards := []Card{}
	for i, c := range cards {
		if p[i] == RowBottom {
			botCards = append(botCards, c)
		}
	}
	if len(botCards) < 2 {
		return false
	}
	// 统计 suit/rank, joker 当 wild
	suitCnt := map[uint8]int{}
	rankCnt := map[uint8]int{}
	jokerCnt := 0
	ranks := []int{}
	for _, c := range botCards {
		if c.IsJoker() {
			jokerCnt++
			continue
		}
		suitCnt[c.Suit()]++
		rankCnt[c.Rank()]++
		ranks = append(ranks, int(c.Rank()))
	}
	// pair (joker 可凑 1)
	for _, n := range rankCnt {
		if n+jokerCnt >= 2 {
			return true
		}
	}
	// 3+ same suit (joker = wild flush)
	for _, n := range suitCnt {
		if n+jokerCnt >= 3 {
			return true
		}
	}
	// 3+ consecutive (joker 可填 gap)
	if len(ranks) >= 1 {
		for i := 0; i < len(ranks); i++ {
			for j := i + 1; j < len(ranks); j++ {
				if ranks[i] > ranks[j] {
					ranks[i], ranks[j] = ranks[j], ranks[i]
				}
			}
		}
		// 用 jokers 填 gap, 看是否能达 3-window
		span := ranks[len(ranks)-1] - ranks[0] + 1
		missing := span - len(ranks)
		// 至少 3 张组成 span ≤ 5 的窗口
		if len(ranks)+jokerCnt >= 3 && span <= 5 && missing <= jokerCnt {
			return true
		}
	}
	return false
}

// r1RuleSplitDoubleJoker — dealt 有 2+ jokers → 不能都堆同一行 (留 wild 灵活性)
func r1RuleSplitDoubleJoker(p Placement, cards []Card) bool {
	if dealtJokerCount(cards) < 2 {
		return true
	}
	rows := make(map[Row]int)
	for i, c := range cards {
		if c.IsJoker() {
			rows[p[i]]++
		}
	}
	// 任一行 >= 2 jokers → 违反
	for _, n := range rows {
		if n >= 2 {
			return false
		}
	}
	return true
}

// r1RuleLowPair_OnMid — DELETED 2026-05-22.
// 原意: dealt 有 ≤9 小对 → 必上 mid (节省 bot slot 拼 flush/straight).
// 漏洞: dealt 有 ≥2 个小对时 (例: J 5 5 9 9), 强迫所有小对都上 mid → 必然 mid 4 张两对, partial-foul,
//      所有 sensible 摆法 (99 → bot) 被砍, AI 只剩死路候选 → 必爆.
// 决策: 删硬规则. 若需要"小 pair 优 mid" 倾向, 改用软 penalty (NN 学不到再加).

// r1RuleSingleA_OnTop — dealt 有 1 张 A (无 AA pair) AND 无 joker → A 必上顶
// (joker + A 已由 JokerWithA_OnTop 处理)
func r1RuleSingleA_OnTop(p Placement, cards []Card) bool {
	if dealtHasJoker(cards) {
		return true // 留给 JokerWithA_OnTop 处理
	}
	pairs := detectDealtPairs(cards)
	if _, ok := pairs[RankA]; ok {
		return true // AA pair 由 DealtBigPair_Top 处理
	}
	if !dealtHasA(cards) {
		return true
	}
	// 单 A 必须 top
	for i, c := range cards {
		if !c.IsJoker() && c.Rank() == RankA {
			if p[i] != RowTop {
				return false
			}
		}
	}
	return true
}

// r1RuleJokerWithK_OnTop_NoA — dealt 有 X + K AND no available A → X+K 必上顶 (锁 KK fantasy)
// 需要 state 来检查 deck 中 A 是否全用
func r1RuleJokerWithK_OnTop_NoA(p Placement, cards []Card, state *GameState) bool {
	if !dealtHasJoker(cards) {
		return true
	}
	// 有 A 在 dealt 或 deck 中 → 不强制 K 上顶 (用 r1RuleJokerWithA_OnTop)
	if dealtHasA(cards) || !noAvailableAces(state) {
		return true
	}
	// dealt 有 K?
	hasK := false
	for _, c := range cards {
		if !c.IsJoker() && c.Rank() == RankK {
			hasK = true
			break
		}
	}
	if !hasK {
		return true
	}
	// 至少 1 joker + 1 K 在 top
	jokerOnTop := false
	kOnTop := false
	for i, c := range cards {
		if p[i] != RowTop {
			continue
		}
		if c.IsJoker() {
			jokerOnTop = true
		} else if c.Rank() == RankK {
			kOnTop = true
		}
	}
	return jokerOnTop && kOnTop
}

// ApplyHardRulesR1 — 按 rule 顺序逐个 narrow 候选; rule 把候选清空则 skip 该 rule.
type R1Cand struct {
	Placement Placement
	GS        *GameState
}

func ApplyHardRulesR1(candidates []R1Cand, cards []Card, state *GameState) []R1Cand {
	// rules without state
	plainRules := []struct {
		name string
		fn   func(Placement, []Card) bool
	}{
		{"NoSplitDealtPair", r1RuleNoSplitDealtPair},
		{"DealtBigPair_Top", r1RuleDealtBigPair_Top},
		// "LowPair_OnMid" DELETED 2026-05-22: 漏洞 — dealt 有 ≥2 小对时强迫两对都 mid → partial-foul 必爆
		{"SplitDoubleJoker", r1RuleSplitDoubleJoker},
		{"TopMustAllowFantasy", r1RuleTopMustAllowFantasy},
	}
	cur := candidates
	for _, r := range plainRules {
		next := make([]R1Cand, 0, len(cur))
		for _, c := range cur {
			if r.fn(c.Placement, cards) {
				next = append(next, c)
			}
		}
		if len(next) > 0 {
			cur = next
		}
	}
	// state-aware rule
	next := make([]R1Cand, 0, len(cur))
	for _, c := range cur {
		if r1RuleJokerWithK_OnTop_NoA(c.Placement, cards, state) {
			next = append(next, c)
		}
	}
	if len(next) > 0 {
		cur = next
	}
	return cur
}

// ============ R2-R5 rules (RoundNAction) ============

// rnRuleNoDiscardJoker — 不弃 joker
func rnRuleNoDiscardJoker(a *RoundNAction, cards []Card) bool {
	return !cards[a.DiscardIdx].IsJoker()
}

// rnRuleNoDiscardAce — 不弃 A (仅 R2-R3; R4-R5 终局可弃 A 凑底)
// rnRuleNoDiscardAce — DELETED 2026-05-31. R2-R3 不弃 A 规则.
// NN 自然偏好 A (NN 给 A 高 value, 几乎不会弃), 规则冗余. 用户判定多余.
func rnRuleNoDiscardAce_DELETED(a *RoundNAction, cards []Card, state *GameState) bool {
	return true
}

// rnRuleNoDiscardPairMember — DELETED 2026-05-31.
// 原意: dealt 含 ≥T 高对 → 不弃 pair 成员 (保 royalty).
// 漏洞: 不看 cap chain — R5 mid/bot 满时, 强迫 top 加 pair → top > mid → 必 foul.
// case ypk-180814154-1 R5: state top[Ah] mid pair-5 bot pair-T, dealt [8c Jh Jc] →
//   规则砍掉 high-A-J-8 (score 16.35) 只留 JJ pair-A (score 1.06 必 foul). AI 被迫选 foul.
// NN 自己 score 已识别 (JJ pair score 1.06 最低), 但规则砍光不爆候选.
// 第 3 个同模式漏洞: r1RuleLowPair_OnMid / rnRuleJokerWithA_OnTop / 本规则 都是硬规则一刀切忽略 cap chain.

// rnRuleNoSplitKeptPair — kept 中同 rank ≥2 必须同行
// rnRuleNoSplitKeptPair — DELETED 2026-05-31. kept 中同 rank ≥2 必同行 规则.
// NN 自然不拆 pair (拆开两端弱), 规则冗余. 用户判定多余.
func rnRuleNoSplitKeptPair_DELETED(a *RoundNAction, cards []Card) bool {
	return true
}

// rnRuleJokerOnTop_IfSpace — dealt 含 joker 且 state.top 还有空 → joker (或其中之一) 必须放 top
func rnRuleJokerOnTop_IfSpace(a *RoundNAction, cards []Card, state *GameState) bool {
	if !dealtHasJoker(cards) {
		return true
	}
	if len(state.Top) >= 3 {
		return true
	}
	// kept 中至少 1 个 joker 在 top
	for i, c := range a.Kept {
		if c.IsJoker() && a.Placement[i] == RowTop {
			return true
		}
	}
	return false
}

// rnRuleKK_OnTop_NoA — dealt 含 KK pair AND state 无可用 A → KK 必上顶 (锁 fantasy)
func rnRuleKK_OnTop_NoA(a *RoundNAction, cards []Card, state *GameState) bool {
	pairs := detectDealtPairs(cards)
	cnt, ok := pairs[RankK]
	if !ok || cnt < 2 {
		return true
	}
	if !noAvailableAces(state) {
		return true
	}
	// kept 中所有 K 必须 placement = top
	for i, c := range a.Kept {
		if !c.IsJoker() && c.Rank() == RankK {
			if a.Placement[i] != RowTop {
				return false
			}
		}
	}
	return true
}

// rnRuleKK_OnBot_WithA — DELETED 2026-05-31. dealt KK + deck 还有 A → KK 必下底 规则.
// 压抑 NN 判断: R2 dealt[Kh Kc 8d] empty state, NN top-1 = KK 上 mid (score 30.75),
// 规则强制 KK 上 bot (rk 1, score 27.92, -3). 跟 r1RuleLowPair_OnMid 等同模式 (硬规则强制具体位置).
func rnRuleKK_OnBot_WithA_DELETED(a *RoundNAction, cards []Card, state *GameState) bool {
	return true
}

// rnRuleNoCompleteMidTrips — state.middle 已有同 rank pair AND kept 含第三张该 rank → 不能放 mid
// 理由: mid trips royalty 仅 2 分, 但 mid trips ≥ bot 概率高, foul -20 (中小底大), 净 EV 巨亏
// Pattern 5 fix: case 35/38 类 "mid 双 → 三" 陷阱 (5d 上 55 mid; 9c 上 99 mid)
// 例外: state.bot 已有更高 hand type (e.g. set/straight/flush/+) → mid trips 安全
func rnRuleNoCompleteMidTrips(a *RoundNAction, cards []Card, state *GameState) bool {
	if len(state.Middle) < 2 {
		return true
	}
	// detect mid pair rank
	var midPairRank uint8 = 255
	rankCnt := make(map[uint8]int)
	for _, c := range state.Middle {
		if c.IsJoker() {
			continue
		}
		rankCnt[c.Rank()]++
	}
	for r, cnt := range rankCnt {
		if cnt >= 2 {
			midPairRank = r
			break
		}
	}
	if midPairRank == 255 {
		return true
	}
	// 检查 kept 是否有第三张该 rank 放 mid
	for i, c := range a.Kept {
		if c.IsJoker() || c.Rank() != midPairRank {
			continue
		}
		if a.Placement[i] == RowMiddle {
			// 例外: bot 已是 set+ → 安全
			if bothHandTypeAtLeastSet(state.Bottom) {
				return true
			}
			return false
		}
	}
	return true
}

// bothHandTypeAtLeastSet — bot 当前能确定 ≥ trips
func bothHandTypeAtLeastSet(bot []Card) bool {
	if len(bot) < 3 {
		return false
	}
	rankCnt := make(map[uint8]int)
	jokers := 0
	for _, c := range bot {
		if c.IsJoker() {
			jokers++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	maxSame := 0
	for _, cnt := range rankCnt {
		if cnt > maxSame {
			maxSame = cnt
		}
	}
	return maxSame+jokers >= 3
}

// rnRuleNoCompleteMidFlush — state.middle 已有 ≥4 同色, kept 含第 5 张同色 → 不能放 mid
// 理由: mid flush royalty 8 分但 mid flush ≥ bot 概率极高, foul -20 净亏
// Pattern 5 fix: case 40 类 "mid 4 同色 → 5 同色 flush" 陷阱 (8d 上 3d4d5d6d mid)
// 例外: state.bot 已是 flush+ 或 mid 凑 ≥ straight flush (rare)
func rnRuleNoCompleteMidFlush(a *RoundNAction, cards []Card, state *GameState) bool {
	if len(state.Middle) < 4 {
		return true
	}
	// detect mid suit (4 same)
	suitCnt := make(map[uint8]int)
	jokers := 0
	for _, c := range state.Middle {
		if c.IsJoker() {
			jokers++
		} else {
			suitCnt[c.Suit()]++
		}
	}
	var midSuit uint8 = 255
	for s, cnt := range suitCnt {
		if cnt+jokers >= 4 {
			midSuit = s
			break
		}
	}
	if midSuit == 255 {
		return true
	}
	// 检查 kept 是否第 5 张同色放 mid
	for i, c := range a.Kept {
		if c.IsJoker() {
			continue // joker 跳过, 永远完成 flush (强制不挡)
		}
		if c.Suit() != midSuit {
			continue
		}
		if a.Placement[i] == RowMiddle {
			// 例外: bot 已 flush+
			if botIsFlushPlus(state.Bottom) {
				return true
			}
			return false
		}
	}
	return true
}

// botIsFlushPlus — bot 已成 flush 或更高 (粗略检测)
func botIsFlushPlus(bot []Card) bool {
	if len(bot) < 5 {
		return false
	}
	suitCnt := make(map[uint8]int)
	jokers := 0
	for _, c := range bot {
		if c.IsJoker() {
			jokers++
		} else {
			suitCnt[c.Suit()]++
		}
	}
	for _, cnt := range suitCnt {
		if cnt+jokers >= 5 {
			return true
		}
	}
	return false
}

// rnRuleKK_NotOnMid — dealt 有 KK pair → 永不上中 (KK 中是天坑: 顶难压, 底难超)
// 例外: state.top 已有 KK 同 rank (e.g. top 已 K+ joker = KK fantasy 锁), 此时 dealt 第三个 K 应去底
// 通用约束: kept 里所有 K 不能放 mid
// Pattern 3 fix: case 62 (R2 dealt KK + 4d, AI 放 KK 中导致 foul / 中小底大 violation)
func rnRuleKK_NotOnMid(a *RoundNAction, cards []Card, state *GameState) bool {
	pairs := detectDealtPairs(cards)
	cnt, ok := pairs[RankK]
	if !ok || cnt < 2 {
		return true
	}
	for i, c := range a.Kept {
		if !c.IsJoker() && c.Rank() == RankK {
			if a.Placement[i] == RowMiddle {
				return false
			}
		}
	}
	return true
}

// rnRuleJokerWithA_OnTop — DELETED 2026-05-31.
// 原意: dealt 有 X + A → kept 中 joker + A 都必须放 top (锁 AA fantasy).
// 漏洞: 不看 state.top 已有 A/K 等 — state.top=[Ad] 时强迫加 joker+Ah → top 满 3 张 trips A
//       → mid 凑不到 trips A → cap chain 必 foul. case ypk-159252810-11 实战触发.
// NN 自己 know: state.top=[Ad] 下选 X→mid (拼 trips 9) + Ah→top (pair-A fantasy lock) score 116,
//              比规则强迫的 X+Ah→top trips A foul (score 26) 高 +90.
// 删硬规则, 让 NN 自己学. 类似 r1RuleLowPair_OnMid 漏洞.

// RNCand — wrapper for ApplyHardRulesRN
type RNCand struct {
	Action *RoundNAction
	GS     *GameState
}

func ApplyHardRulesRN(candidates []RNCand, cards []Card, state *GameState) []RNCand {
	rules := []struct {
		name string
		fn   func(*RoundNAction, []Card, *GameState) bool
	}{
		{"NoDiscardJoker", func(a *RoundNAction, c []Card, s *GameState) bool { return rnRuleNoDiscardJoker(a, c) }},
		// "NoDiscardAce" DELETED 2026-05-31: NN 自然不弃 A, 规则冗余.
		// "NoDiscardPairMember" DELETED 2026-05-31: dealt ≥T 高对强迫不弃, R5 mid/bot 满时 top 加 pair → cap chain 必 foul. case ypk-180814154-1.
		// "NoSplitKeptPair" DELETED 2026-05-31: NN 自然不拆 pair, 规则冗余.
		{"KK_OnTop_NoA", rnRuleKK_OnTop_NoA},
		// "KK_OnBot_WithA" DELETED 2026-05-31: 压抑 NN — R2 dealt[KK 8d] NN 想 KK 上 mid (score 30.75), 规则强制 KK 上 bot (score 27.92).
		// "JokerWithA_OnTop" DELETED 2026-05-31: 不看 state.top 已有 A → 强迫 X+A 都上头变 trips foul. case ypk-159252810-11.
		{"TopMustAllowFantasy", rnRuleTopMustAllowFantasy}, // 2026-05-20 sp15: 仅 R2-R3 触发, R4-R5 skip
	}
	cur := candidates
	for _, r := range rules {
		next := make([]RNCand, 0, len(cur))
		for _, c := range cur {
			if r.fn(c.Action, cards, state) {
				next = append(next, c)
			}
		}
		if len(next) > 0 {
			cur = next
		}
	}
	return cur
}
