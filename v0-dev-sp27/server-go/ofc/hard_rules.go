package ofc

// hard_rules.go — 打地鼠用 candidate filter (R1 + R2-R5).
// 在 candidate 枚举之后, rollout-Q 排序之前应用. 不修 rollout 内部.
// 任何 rule 把候选清空则跳过该 rule (保留前一步结果) — 即"宽容"应用.

// HardRuleVerbose — 可选 log (用于 debug 看哪个 rule 触发)
var HardRuleVerbose = false

// HardRulesDisabled — 若 true, 跳过所有 candidate filter (env DISABLE_HARD_RULES=1)
var HardRulesDisabled = false

// SoftRulesDisabled — 若 true, ExpertPlace5/3 跳过所有软 bonus/penalty (含 FoulImminent),
// score = 纯 TrainedEval(value head) + PolicyBoost*plogit. env DISABLE_SOFT_RULES=1.
// 2026-06-14 加: "裸跑"看 NN value head 自己摆什么, 诊断哪些软规则对当前 NN 已冗余/过火.
var SoftRulesDisabled = false

// DisabledRules — 按名字单独关某条软规则 (ablation 审计用, env DISABLE_RULES=name1,name2).
// add()/radd() 里查; 名字见 rnSoftScore 的 add("X",..) 和 ExpertPlace5 的 radd("X",..).
// 2026-07-05 sp45 退役五连 (用户逐条裁定): R1 结构先验已被 NN 原生学会 (JJ/QQ+杂牌裸NN放底实测✓),
// 残余救赎(#94型 0.43分刀尖)不值危害(R1BigPairOnBot 连 KK/AA 也往底推, 和范线对着干 — 前范时代化石;
// R1JokerWithAOnTop 垃圾侧牌时被 MCTS 600sims 深摸否决, 见 122-R1 探源).
// #94 型转种子家族 backlog (F5). 复活方法: env DISABLE_RULES 机制反向不支持, 删本条目即可.
var DisabledRules = map[string]bool{
	"ConnectorSplit":    true,
	"R1TopNonAKX":       true,
	"R1JokerWithAOnTop": true,
	"R1FlushGroupOnBot": true,
	"R1BigPairOnBot":    true,
}

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
//
//	0 = 纯 rollout (默认, 老行为)
//	1 = 纯 prerank (跳过 stage 1 rollout, 直接 value-head 排 → 喂 stage 2)
//	0.5 = blend
//
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
//
//	TT 已锁 royalty, ♦ 不必强压底, 让 6d 中保 mid draw)
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
	boardJokers := 0
	for _, c := range state.Top {
		seen[c.ID()] = true
		if c.IsJoker() {
			boardJokers++
		}
	}
	for _, c := range state.Middle {
		seen[c.ID()] = true
		if c.IsJoker() {
			boardJokers++
		}
	}
	for _, c := range state.Bottom {
		seen[c.ID()] = true
		if c.IsJoker() {
			boardJokers++
		}
	}
	for cid := range seen {
		c, ok := ParseCard(cid)
		if !ok {
			continue
		}
		if c.IsJoker() {
			if cid == "X" && boardJokers >= 1 {
				continue
			} // 鬼双计fix: raw "X"是盘上鬼冗余
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
//
// handCmp 编码: int(HandType)*16 + 主rank(0-12). rank-aware, 跨行可比. (Ht*/Type* 数值对齐)
// madeHandCmp — row 当前成手的可比值 (floor/min).
func madeHandCmp(row []Card) int {
	hv := partialEvalTP(row)
	var cnt [13]int
	for _, c := range row {
		if !c.IsJoker() {
			cnt[c.Rank()]++
		}
	}
	prim := -1
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 2 {
			prim = r
			break
		}
	}
	if prim < 0 {
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 1 {
				prim = r
				break
			}
		}
	}
	if prim < 0 {
		prim = 0
	}
	return int(hv.Type)*16 + prim
}

// midMadeFloorReal — 中道 foul-floor: 鬼是活的(可压低当kicker), 不该锁成三条+. 按真牌成手算.
//
//	2026-06-23 (用户 ypk-141164874-8 R4): 中[2s 🃏 2h] 真牌=22对(floor), 旧 madeHandCmp 把鬼当2读成
//	222三条 → 底JJ99(两对)误判 < 中(三条) → FantasyLost/fantasyOnlyViaFoul 判倒置过滤掉正确的 J→底.
//	鬼实际可取 J/T 让中道两对(TT22/JJ22)≤底JJ99 不foul. floor 用真牌, 让上层 midFanNeed 升级逻辑接管.
//	(全真牌行 → 等于 madeHandCmp 原值, 无变化; 只 partial 含鬼行受影响.)
func midMadeFloorReal(row []Card) int {
	var real []Card
	for _, c := range row {
		if !c.IsJoker() {
			real = append(real, c)
		}
	}
	if len(real) == 0 {
		return madeHandCmp(row) // 全鬼行(极罕见) → 退回原逻辑
	}
	return madeHandCmp(real)
}

// maxAchievableCmp — row+slots 能达到的最高可比手值 (rank-aware, accurate).
//
//	对/两对/三条/金刚 逐rank精算 (不用会高估两对的 maxAchievableHandType); 顺/花/葫芦+ 用 tier(rank=12估).
func maxAchievableCmp(row []Card, slots int, rankRem [13]int, suitRem [4]int, jokerRem int) int {
	var rc [13]int
	j := 0
	for _, c := range row {
		if c.IsJoker() {
			j++
		} else {
			rc[c.Rank()]++
		}
	}
	best := madeHandCmp(row)
	add := func(t HandTypeEnum, rank int) {
		if v := int(t)*16 + rank; v > best {
			best = v
		}
	}
	for r := 12; r >= 0; r-- {
		have := rc[r] + j
		if have >= 2 || (rc[r] >= 1 && slots >= 1 && rankRem[r] >= 1) {
			add(HtPair, r)
		}
		if have >= 2 {
			if need := 3 - have; need <= 0 || (need <= slots && rankRem[r] >= need) {
				add(HtThreeKind, r)
			}
			if need := 4 - have; need <= 0 || (need <= slots && rankRem[r] >= need) {
				add(HtFourKind, r)
			}
		}
	}
	// 两对: 已成对免费 + 单张升对花 1 预算(slots+jokers), 够 2 个对才算
	budget := slots + j
	var pr []int
	for r := 12; r >= 0; r-- {
		if rc[r] >= 2 {
			pr = append(pr, r)
		} else if rc[r] == 1 && budget > 0 && rankRem[r] >= 1 {
			pr = append(pr, r)
			budget--
		}
	}
	if len(pr) >= 2 {
		add(HtTwoPair, pr[0])
	}
	// 顺/花/葫芦/金刚/同花顺: 用 maxAchievableHandType (这些 tier 它算得准), rank=12 估
	if t := maxAchievableHandType(row, slots, rankRem, suitRem, jokerRem); t >= HtStraight {
		add(t, 12)
	}
	return best
}

func FantasyLost(state *GameState) bool {
	// 2026-06-19 用户重写: 链式 rank-aware max-vs-min (治 hand20: 范跟避foul互斥但旧版独立判误活).
	//   ① 头最大 < QQ → 追不了范 (canTopReachPairQPlus 含 QQ2/KK/AA2 已成 + XA3/XA4 鬼配)
	//   ② 中max ≥ 头min 且 ≥ QQ (rank-aware: QQ3≥QQ2 算同级可托; 但中只够 66 对 < QQ → 托不住)
	//   ③ 底max ≥ 中min 且 ≥ QQ
	//   三个都成立才可追范. 用 maxAchievableCmp(rank精算)不是高估的 maxAchievableHandType.
	if !canTopReachPairQPlus(state) {
		return true // ①
	}
	// 已成牌型直接犯规 (相邻满行) → 范死. 例2: 顶AA-K > 中AA-8 (kicker + 3vs5 cross-count, type+rank漏).
	//   用 OFC 真比较 topFoulVsMid / HandExceeds5 (不是会跨牌数错位的 raw Value).
	//   topFoulVsMid 内部 re-eval 真牌 (topTripRank等), 含鬼会 panic → 仅无鬼行用; HandExceeds5 只比 HandValue 安全.
	noJoker := func(cs []Card) bool {
		for _, c := range cs {
			if c.IsJoker() {
				return false
			}
		}
		return true
	}
	if len(state.Top) == 3 && len(state.Middle) == 5 && noJoker(state.Top) && noJoker(state.Middle) &&
		topFoulVsMid(state.Top, partialEvalTP(state.Top), state.Middle, partialEvalTP(state.Middle)) {
		return true
	}
	// 注: 中>底 满行犯规交给下方 ③ 链式 (botMax≥midMin). 不用 HandExceeds5 — 鬼cap过的中道flush会
	//   误判压过底道(实战9: 中鬼红花 vs 底梅花), 且 kicker级中底foul 罕见, 不值得为它引回归.
	rankRem, suitRem, jokerRem := computeDeckRemaining(state)
	qq := int(HtPair)*16 + int(RankQ)
	topMin := madeHandCmp(state.Top)
	midMax := maxAchievableCmp(state.Middle, 5-len(state.Middle), rankRem, suitRem, jokerRem)
	if midMax < topMin || midMax < qq {
		return true // ②
	}
	midMin := midMadeFloorReal(state.Middle) // 鬼活的, foul-floor 按真牌(别锁三条+) — ypk-141164874-8 R4
	botMax := maxAchievableCmp(state.Bottom, 5-len(state.Bottom), rankRem, suitRem, jokerRem)
	// 2026-06-22 (用户 ypk-84869450-8 R3): 进范顶必≥QQ → 中也必≥QQ. 中已有对P(<Q)要≥QQ 只能升两对
	//   (high=P, = HtTwoPair+P), 底要撑的是这个升级后的中, 不是中当前 midMin. 旧 ③ 用 midMin 漏判:
	//   6622-play 底36 ≥ 中JJ对25 误判可进范, 实则中需升 JJ两对41 > 底36 倒置 = 真不可进范.
	midFanNeed := midMin
	if midFanNeed < qq {
		midPairRank := -1
		for pr := range midPairedRanks(state.Middle) {
			if pr > midPairRank {
				midPairRank = pr
			}
		}
		if midPairRank >= 0 {
			midFanNeed = int(HtTwoPair)*16 + midPairRank // 中有对 → 升两对(high=P)
		} else {
			midFanNeed = qq // 中无对 → 单对Q
		}
	}
	if botMax < midFanNeed {
		return true // ③
	}
	return false
}

// rnRuleFantasyPossible — RN 应用候选后, 若 fantasy lost AND 当前 state 还没 lost → reject
// midCanSinglePairAtLeast — 中道能否成 rank ≥ rankFloor 的单对 (现有牌+鬼+空位+deck).
func midCanSinglePairAtLeast(state *GameState, rankFloor int) bool {
	rankRem, _, _ := computeDeckRemaining(state)
	var cnt [13]int
	nJoker := 0
	for _, c := range state.Middle {
		if c.IsJoker() {
			nJoker++
		} else {
			cnt[c.Rank()]++
		}
	}
	slots := 5 - len(state.Middle)
	for r := rankFloor; r < 13; r++ {
		have := cnt[r] + nJoker
		if have >= 2 {
			return true
		}
		if needDraw := 2 - have; slots >= needDraw && rankRem[r] >= needDraw {
			return true
		}
	}
	return false
}

// fantasyOnlyViaFoul — post-state 的"范"是否只能靠 foul 成立 (假范, 无 foul-free 进范路径).
//
//	遍历 QQ/KK/AA: 找一个 ① 顶能成 pair-r ② pair-r ≤ 中max ③ 底能撑住"中为达 ≥pair-r 需升的手牌" 的范级.
//	一个都没有 → 范只能 foul → true.
//	2026-06-19 (A6 R4): ①② — 顶[Ad 2s]唯一AA(28)>中max QQ(26) → 假范.
//	2026-06-20 (game94 R3): ③ 中→底链 — 顶要QQ, 中88<QQ 必须升两对(中单对够不着Q), 底[9h Kh Qh 7s]剩1空只成一对KK(27)<两对(32) → 撑不住 = 假范. FantasyLost 逐行独立查会漏这个.
//	用在 ExpertPlace3 的 pureNN 判定: R3-R5 全候选都 (FantasyLost || fantasyOnlyViaFoul) → 无进范可能 → 听NN.
func fantasyOnlyViaFoul(state *GameState) bool {
	rankRem, suitRem, jokerRem := computeDeckRemaining(state)
	midMax := maxAchievableCmp(state.Middle, 5-len(state.Middle), rankRem, suitRem, jokerRem)
	botMax := maxAchievableCmp(state.Bottom, 5-len(state.Bottom), rankRem, suitRem, jokerRem)
	var cnt [13]int
	nJoker := 0
	for _, c := range state.Top {
		if c.IsJoker() {
			nJoker++
		} else {
			cnt[c.Rank()]++
		}
	}
	slots := 3 - len(state.Top)
	for _, r := range []int{int(RankQ), int(RankK), int(RankA)} {
		pairCmp := int(HtPair)*16 + r
		if pairCmp > midMax {
			continue // 中撑不到这范对 → 成立即 foul
		}
		// ① 顶能成 pair r?
		have := cnt[r] + nJoker // 鬼可当此 rank
		topCan := have >= 2
		if !topCan {
			if needDraw := 2 - have; slots >= needDraw && rankRem[r] >= needDraw {
				topCan = true
			}
		}
		if !topCan {
			continue
		}
		// ③ 中→底链: 中为达 ≥pair-r 的最弱手牌, 底必须撑得住.
		// 2026-06-22 (用户 ypk-84869450-8 R3): 中已有对 P 时, 再配任何对成"两对(high≥P)"非单对 →
		//   底要撑 P-两对 不是单 pair-r. 旧 midCanSinglePairAtLeast 没看中已有对 → midNeed 低估漏判.
		midCur := midMadeFloorReal(state.Middle) // 鬼活的, foul-floor 按真牌(别锁三条+) — ypk-141164874-8 R4
		midPairRank := -1
		for pr := range midPairedRanks(state.Middle) {
			if pr > midPairRank {
				midPairRank = pr
			}
		}
		var midNeed int
		switch {
		case midCur >= pairCmp:
			midNeed = midCur // 中现成手已 ≥pair-r → 底需 ≥ 中现手
		case midPairRank >= 0:
			midNeed = int(HtTwoPair)*16 + midPairRank // 中有对P(<r) → 升两对(high≥P), 底需 ≥ P-两对
		case midCanSinglePairAtLeast(state, r):
			midNeed = pairCmp // 中无对, 能成单对r → 底需 ≥ pair-r
		default:
			midNeed = int(HtTwoPair) * 16 // 中无对且单对够不着r → 升最低两对
		}
		if botMax >= midNeed {
			return false // foul-free 范在 (顶pair-r ≤ 中 ≤ 底)
		}
	}
	return true // 无任何范级能 foul-free → 假范
}

func rnRuleFantasyPossible(a *RoundNAction, cards []Card, state *GameState) bool {
	// 只在 R2-R4 应用 (2026-06-17 用户: R5 末轮范已定, 强制保范可能误杀放弃范避foul)
	if state.Round < 2 || state.Round > 4 {
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

// canFantasyTopFinal — top 是否还可能 fantasy (考虑现有牌 + 剩余空位的可达性).
// 2026-06-17 bug fix: 原版 len<3 无脑 return true ("未满可补"), 漏判"已塞≥2张废牌+空位不足凑不出范".
//
//	顶[7c 2h]+1空位永进不了范(配不出QQ+/凑不齐三条)却被放行 → 规则从不拦 R2-R3 把废牌埋顶杀范.
//	改: 按"现有 maxCnt/鬼/≥Q单张 + 剩余空位"算三条范/对范可达性. 满3张退化回原(空位0)判定.
func canFantasyTopFinal(topCards []Card) bool {
	jokers := 0
	var rankCnt [13]int
	for _, c := range topCards {
		if c.IsJoker() {
			jokers++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	maxCnt := 0
	hasHighQ := false // 现有 ≥Q 真单张 (空位补1张即可配成 QQ+)
	for r := 0; r < 13; r++ {
		if rankCnt[r] > maxCnt {
			maxCnt = rankCnt[r]
		}
		if r >= int(RankQ) && rankCnt[r] > 0 {
			hasHighQ = true
		}
	}
	slots := 3 - len(topCards)
	if slots < 0 {
		slots = 0
	}
	// 三条范可达: 现有最大同rank + 鬼 + 空位(补同rank) ≥ 3 (实战19 [X X 3c]=333 / 77+空→777)
	if maxCnt+jokers+slots >= 3 {
		return true
	}
	// QQ+ 对范可达: 空位≥2(摸QQ) 或 空位≥1且(有鬼 或 现有≥Q单张可配)
	if slots >= 2 || (slots >= 1 && (jokers >= 1 || hasHighQ)) {
		return true
	}
	// 满3张(空位0): 现有 ≥Q 真牌 + 鬼 ≥2 = 对范
	for r := int(RankQ); r <= int(RankA); r++ {
		if rankCnt[r]+jokers >= 2 {
			return true
		}
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
			// 2026-06-14 sp26 recal: 3 张落 ≤5 rank 窗口 = 顺draw (5-8-9 = 5-9窗, 补6,7成顺) = coherent,
			//   非杂行 (原只认 span≤3 或 ≥4张, 漏 3张5窗). mid+bot 都认 (实战45 中[8 5 9] 正解被误罚 +5).
			if !hasStraight && len(ranks) >= 3 && span <= 5 {
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
		return 16 // 2026-06-17 port v0-dev值: 10→16 (std63-13 鬼+A锁AA顶, 裸NN偏好鬼进底差12)
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
// splitsDealtTrips — dealt 含某rank 3张(三条) 却被 p 拆到不同行 → true. 用于"别为花组/同花奖拆三条".
// 2026-06-18 (s99局10 R1): 发888+7s Qs(7s8sQs=3黑桃), prod为黑桃花奖(+8)把888拆了(88进中 8s进底).
//
//	value-head 重重首选不拆(888三条底 te+pol 38.17 >> 拆的 30.20). 花奖拆三条是捡芝麻丢西瓜.
func splitsDealtTrips(p Placement, cards []Card) bool {
	var cnt [13]int
	rows := map[int]map[Row]bool{}
	for i, c := range cards {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		cnt[r]++
		if rows[r] == nil {
			rows[r] = map[Row]bool{}
		}
		rows[r][p[i]] = true
	}
	for r := 0; r < 13; r++ {
		if cnt[r] >= 3 && len(rows[r]) > 1 {
			return true
		}
	}
	return false
}

// R1BottomDrawBonus — R1 底道有成型 3-card draw → +2. 鼓励把 draw 放最强行(底).
//
//	治实战44: 567 该整组进底(顺draw)别拆. 覆盖 3条 + 3连张(5-window内3+张).
//	3花已由 R1FlushGroupOnBot(+5)管, 不重复. gap 小时(44=0.34)+2 足够反转拆法.
func R1BottomDrawBonus(p Placement, cards []Card) float32 {
	if DisabledRules["R1BotDraw"] {
		return 0
	}
	var rankCnt [13]int
	var present [13]bool
	n := 0
	for i, c := range cards {
		if p[i] != RowBottom || c.IsJoker() {
			continue
		}
		rankCnt[c.Rank()]++
		present[c.Rank()] = true
		n++
	}
	if n < 3 {
		return 0
	}
	// 4连顺 (4张连续, 如4-5-6-7 开口顺draw) → +5 (强draw, 2026-06-20 用户; 盖过 generic 3连张 +2)
	for lo := 0; lo <= 9; lo++ { // lo..lo+3 四连
		if present[lo] && present[lo+1] && present[lo+2] && present[lo+3] {
			return 5
		}
	}
	for r := 0; r < 13; r++ {
		if rankCnt[r] >= 3 {
			return 2 // 3条
		}
	}
	for lo := 0; lo <= 8; lo++ { // 5-window [lo, lo+4]: 3+张落同窗 = 3 to a straight
		cnt := 0
		for r := lo; r <= lo+4; r++ {
			if present[r] {
				cnt++
			}
		}
		if cnt >= 3 {
			return 2 // 3连张 (顺 draw)
		}
	}
	return 0
}

// R1MidOverBotCardPenalty — R1 1-2-2 结构, 中道任一真牌 > 底道任一真牌 → +2.
//
//	强制 4 张非顶牌里最高 2 张进底(底=最强行). 治实战54: 8 该进底凑(9,8), 不是 5 进底.
//	比 RnMidPlacedOverBotPlaced(只比 maxMid vs maxBot)更严: 逐张比, 防"高牌偷进中".
func R1MidOverBotCardPenalty(p Placement, cards []Card) float32 {
	if DisabledRules["R1MidOverBotCard"] {
		return 0
	}
	var mid, bot []int
	nTop, nMid, nBot := 0, 0, 0
	for i, c := range cards {
		switch p[i] {
		case RowTop:
			nTop++
		case RowMiddle:
			nMid++
			if !c.IsJoker() {
				mid = append(mid, int(c.Rank()))
			}
		case RowBottom:
			nBot++
			if !c.IsJoker() {
				bot = append(bot, int(c.Rank()))
			}
		}
	}
	if nTop != 1 || nMid != 2 || nBot != 2 {
		return 0 // 只管 1-2-2
	}
	// 豁免: 底道2张同花(花draw) → 底承诺做花, 花 > 任何对, 中高牌威胁不到 → 不罚.
	{
		var botCards []Card
		for i, c := range cards {
			if p[i] == RowBottom && !c.IsJoker() {
				botCards = append(botCards, c)
			}
		}
		if len(botCards) == 2 && botCards[0].Suit() == botCards[1].Suit() {
			return 0
		}
	}
	for _, m := range mid {
		for _, b := range bot {
			if m > b {
				return 2 // 中某真牌 > 底某真牌 → 行序没排好
			}
		}
	}
	return 0
}

func R1FlushGroupOnBotBonus(p Placement, cards []Card) float32 {
	if splitsDealtTrips(p, cards) {
		return 0 // 别为底花组奖拆 dealt 三条
	}
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

// R1BigPairOnBotBonus — R1 大对(≥T)放底道 +2 (用户 2026-06-18 局32). 底是最强行, 大对锚底稳行序 +
//
//	留中道/顶道灵活. 小幅 prior, 只 tip 近平局(局32 TT→中 vs →底 gap1.7), 不压 fantasy/flush 强信号. 只 R1.
func R1BigPairOnBotBonus(p Placement, cards []Card) float32 {
	var cnt [13]int
	botN := 0
	for i, c := range cards {
		if p[i] == RowBottom {
			botN++
			if !c.IsJoker() {
				cnt[c.Rank()]++
			}
		}
	}
	if botN < 3 {
		return 0 // 底道未成锚行(光秃2张对, kicker散顶/中) 不奖 — 防 std22 把K丢顶当lone seed
	}
	pairRank := -1
	for r := 12; r >= 8; r-- { // r>=8 → T/J/Q/K/A
		if cnt[r] >= 2 {
			pairRank = r
			break
		}
	}
	if pairRank < 0 {
		return 0
	}
	// 非成对底牌必须都是高张(≥8) — 别把低张(可成中道顺draw, 实战75 2c做2-4-5)埋底
	for i, c := range cards {
		if p[i] == RowBottom && !c.IsJoker() && int(c.Rank()) != pairRank && c.Rank() < 6 {
			return 0
		}
	}
	return 2
}

// R1LoneKingOnTopPenalty — R1 顶道放孤 K(非成对/无鬼配)-2 (用户 2026-06-18: "KQ上头-2").
//
//	K 上顶是弱范种子(配 KK 概率低), 进底/中当高张更值; 只 A 上顶才强(R1SingleAOnTopBonus).
//	(Q 上顶已被 R1TopNonAKXPenalty 罚 +5, 故只补 K.) 顶 KK 成对 或 K+鬼(范) 不罚.
func R1LoneKingOnTopPenalty(p Placement, cards []Card) float32 {
	cntTopK, hasJokerTop := 0, false
	for i, c := range cards {
		if p[i] != RowTop {
			continue
		}
		if c.IsJoker() {
			hasJokerTop = true
		} else if c.Rank() == RankK {
			cntTopK++
		}
	}
	if cntTopK == 1 && !hasJokerTop {
		return 2 // 孤 K 上顶 (无鬼配 KK)
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

// "RnBotMakeTwoPairBonus" DELETED 2026-06-23 (用户): 底凑两对+/分级(金刚18/葫芦14/其它8) 过火 —
//
//	对"鬼放底凑四条" over-reward, 压掉"鬼→顶 fantasy 种子"正解 (ypk-127795530-21 R3, gamecase 123).
//	进范优先于金刚 royalty; 裸 NN 自纠. case97(底顺)已加 exp 接受删后摆法.
//
// (下面 helpers highestRealPairRank/botMadeTier 保留 — RnMidExceedsBot/MidDrawFace 等其它规则在用.)
// highestRealPairRank — row 里最高的"真对子"(cnt>=2) rank; 不把 joker 凑的对算进去
// (joker 凑对/三条由外层 botAtLeastTwoPair 处理); 无真对返回 -1.
func highestRealPairRank(row []Card) int {
	var cnt [13]int
	for _, c := range row {
		if !c.IsJoker() {
			cnt[c.Rank()]++
		}
	}
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 2 {
			return r
		}
	}
	return -1
}

// botMadeTier — 底道当前成手档位 (joker-aware). 5 张用 Evaluate5JokerCap; <5 张按 rank-count 估已成最强.
func botMadeTier(row []Card) int {
	if len(row) == 5 {
		return Evaluate5JokerCap(row, nil).Type
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
	pairs, trips, maxc := 0, 0, 0
	for _, n := range cnt {
		if n > maxc {
			maxc = n
		}
		if n == 2 {
			pairs++
		}
		if n >= 3 {
			trips++
		}
	}
	eff := maxc + j
	switch {
	case eff >= 4:
		return TypeFourOfAKind
	case trips >= 1 && pairs >= 1:
		return TypeFullHouse
	case eff >= 3:
		return TypeThreeOfAKind
	case pairs >= 2:
		return TypeTwoPair
	default:
		return TypePair
	}
}

// RnMidMakeTwoPairBonus — 本轮把中道做成 ≥两对, 且底道 > 中道 (维持 bot>mid 不倒置) → +8.
// 通用. 底已比中强时(底三条/顺/更高两对), 中凑两对是安全的强中. 用 partialEvalTP(两对感知).
// 底 ≤ 中 不奖 → 防 case9(弃鬼凑中两对) / 防 mid>bot 倒置.
// 2026-06-13 曾删(以为冗余, 只在实战22 验过 NN 自赢); 2026-06-14 恢复 — ypk-459082-16 R5:
//
//	底=顺子, 发 Jh 该进中凑 JJ22 两对(底顺>中), AI 却弃 Jh 保 22 (gap 仅 0.10). 删早了.
func RnMidMakeTwoPairBonus(postState, preState *GameState) float32 {
	if botAtLeastTwoPair(preState.Middle) {
		return 0 // 中已≥两对, 非本轮新做
	}
	if !botAtLeastTwoPair(postState.Middle) {
		return 0 // 中没成两对
	}
	if HandExceeds5(partialEvalTP(postState.Bottom), partialEvalTP(postState.Middle)) {
		return 8 // 底 > 中 → 安全的强中, 奖
	}
	return 0
}

// RnMidHighCardOverBotPenalty — 用户提案 (2026-06-14): 本轮往中道放的真牌 > 底道锚 且 底未成三条 → 罚.
// 含义: 大于底锚的高牌该进底道(强行), 别浪费在中道. 底锚 = 底成对→最高对子rank; 底未成对→底max真牌.
// ypk-459082-15 R2 (底99, Jd 进中) / ypk-459082-16 R2 (底6789draw, Jc进中).
// 2026-06-14 验证(bot-333): "中道高牌 > 底锚" 其实 ≈ "中道draw成形会超底 → foul苗头", 罚是对的,
//
//	不是瞎压高牌. 底弱时中高牌draw成形必犯规(中顺>底低顺); 底≥三条 guard 放过底真强局
//	(底333能升葫芦撑住中顺, 实测 NN 自选 Qh→中 score==te 不罚). 残留误伤窄(中既不超底、底又发展更高).
func RnMidHighCardOverBotPenalty(postState, preState *GameState) float32 {
	if DisabledRules["MidHighCardOverBot"] {
		return 0
	}
	bot := partialEvalTP(postState.Bottom)
	if bot.Type >= TypeThreeOfAKind {
		return 0 // 底已三条+ → 不管
	}
	midType := partialEvalTP(postState.Middle).Type
	if midType >= TypeThreeOfAKind {
		return 0 // 中已三条+ → 高牌只是无害 kicker, 不算浪费 (实战20: mid 222 + K)
	}
	// 2026-06-19 (实战101): 底已两对 + 中≤单对 → 中任何单对(含高对JJ) < 底两对, foul 不了.
	//   rule 只比对子 rank (botAnchor) 会误杀: 中JJ vs 底77/55, J>7 触发 -10, 但 JJ(单对)<两对 安全.
	//   (区别 std28/std9: 底是高牌/单对, 中高牌成对确实越底 → 仍罚.) 中发育成两对超底是玩家可控, 不强罚.
	if bot.Type >= TypeTwoPair && midType <= TypePair {
		return 0
	}
	for _, c := range postState.Middle {
		if c.IsJoker() {
			// 2026-06-18 (s99局39 R4): 中道有鬼 → 本轮进的高牌跟鬼配成对(非死kicker, 如Ac+鬼=AA托顶AA范) → 豁免.
			//   (区别 std-28: Kc进中没配对=死高牌, 该罚→上顶.)
			return 0
		}
	}
	// 2026-06-18 (s99局40 R2): 顶已范级对(QQ+, 含鬼) → 中道必须发育成两对+才托得住顶(否则顶>中foul),
	//   本轮进中的高牌是**必要发育**(非死kicker); 赶它上顶反而是 DeadLowKicker 浪费(顶范已锁). 豁免.
	//   (区别 std-28: 顶非范级对时高牌进中=死高牌, 该罚→上顶.) 纯NN首选 7h→中 base48.17 被旧-10压错.
	if effTopPairRank(postState.Top) >= RankQ {
		return 0
	}
	var botCnt [13]int
	botMax := -1
	for _, c := range postState.Bottom {
		if c.IsJoker() {
			continue
		}
		botCnt[c.Rank()]++
		if int(c.Rank()) > botMax {
			botMax = int(c.Rank())
		}
	}
	botAnchor := botMax
	if bot.Type >= TypePair { // 底成对 → 锚 = 最高对子 rank
		for r := 12; r >= 0; r-- {
			if botCnt[r] >= 2 {
				botAnchor = r
				break
			}
		}
	}
	// 本轮新进中道的真牌最高 rank (multiset diff pre vs post)
	preMid := map[string]int{}
	for _, c := range preState.Middle {
		preMid[c.ID()]++
	}
	maxAdded := -1
	for _, c := range postState.Middle {
		if preMid[c.ID()] > 0 {
			preMid[c.ID()]--
			continue
		}
		if !c.IsJoker() && int(c.Rank()) > maxAdded {
			maxAdded = int(c.Rank())
		}
	}
	if maxAdded > botAnchor {
		return 10 // 高牌进中越过底锚 → 罚. 2026-06-17 用户要求 5→10 (局41: 高中牌过弱底55 foul-prone, 压下去)
	}
	return 0
}

// RnAceToTopSeedBonus — RN 本轮把单 A 放上"非范级顶"(<QQ) → +8 (seed AA 追范, 别弃/埋A).
//
//	局56 R3 (s99): 顶[Qd], As该→顶做AQ(后续Ac来成AA范). 范特西率优先.
//	守护: 顶pre非范级对(QQ+已锁不需seed) + 本轮真进单A到顶(非鬼).
//
// R2BotPairMidDrawBonus — 用户规则 2026-06-19: R2 + 底有对/三条且==3张 + 中有3+同花draw或三连张 → +3.
//
//	治 hand63 R2: 底QQ(3张) + 中3黑桃flush draw, value-head漏看同花弃了黑桃(低估保花2.23).
//	底==3张 = 没往已成对的底道加死kicker(保持发育中道draw). flush 或 strict 3连(5-6-7式).
func R2BotPairMidDrawBonus(postState, preState *GameState) float32 {
	if DisabledRules["R2BotPairMidDraw"] || postState.Round != 2 {
		return 0
	}
	if len(postState.Bottom) != 3 || partialEvalTP(postState.Bottom).Type < TypePair {
		return 0 // 底不是 (对/三条 且 ==3张)
	}
	var suitCnt [4]int
	var present [13]bool
	for _, c := range postState.Middle {
		if c.IsJoker() {
			continue
		}
		suitCnt[c.Suit()]++
		present[c.Rank()] = true
	}
	flush := false
	for _, n := range suitCnt {
		if n >= 3 {
			flush = true
		}
	}
	straight := false // strict 3 连续 rank (5-6-7 式)
	run := 0
	for r := 0; r < 13; r++ {
		if present[r] {
			run++
			if run >= 3 {
				straight = true
			}
		} else {
			run = 0
		}
	}
	if flush || straight {
		return 3
	}
	return 0
}

func RnAceToTopSeedBonus(gs, state *GameState) float32 {
	if state.Round < 2 || state.Round > 4 {
		return 0 // 只在 R2-R4 seed (R5 终局无未来配对, A→顶若AA>中会foul→鬼压低浪费A, 见std-50)
	}
	if len(gs.Top) > 3 || len(gs.Top) <= len(state.Top) {
		return 0 // 本轮顶没新增
	}
	if effTopPairRank(state.Top) >= RankQ {
		return 0 // 顶pre已范级对, 不需seed
	}
	added, ok := rowAddedCard(gs.Top, state.Top)
	if !ok || added.IsJoker() || int(added.Rank()) != RankA {
		return 0
	}
	return 8
}

// effTopPairRank — 顶道有效成对 rank (真对 或 鬼配最高单张), 无对返回 -1.
func effTopPairRank(row []Card) int {
	var cnt [13]int
	j := 0
	for _, c := range row {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 2 {
			return r
		}
	}
	if j >= 1 {
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 1 {
				return r
			}
		}
	}
	return -1
}

// RnDeadLowKickerOnFanTopPenalty — 顶已是范级对(QQ+, 含鬼配A/K/Q)时, 本轮把**低死 kicker(≤9)**
// 拍上顶把顶填满(2→3张) → 罚 -2.5. 范早锁死, 第3张低牌零增益; 它进底(配对/凑葫芦)或中(成对种子)更值.
// 局30 R3 (seed99): 顶[Ad X]=AA, 2h该进底配999凑99922, AI却拍2h上顶填死 → value-head偏好填顶(无规则), 此罚翻正.
// 守护: ①顶pre必范级对(QQ+, 低对顶要留弱不罚) ②第3张不改进顶(非升三条) ③低牌≤9(高kicker T+可能安全dump不罚) ④非鬼.
func RnDeadLowKickerOnFanTopPenalty(postState, preState *GameState) float32 {
	if len(preState.Top) != 2 || len(postState.Top) != 3 {
		return 0 // 只管本轮把顶从2张填到3张(定型)
	}
	pre := partialEvalTP(preState.Top)
	if pre.Type < TypePair {
		return 0 // 顶pre未成对 → 第3张可能在配对, 不算死kicker
	}
	if pre.Type == TypePair && effTopPairRank(preState.Top) < RankQ {
		return 0 // 低对顶(<QQ)要留弱避免压中, 不罚
	}
	added, ok := rowAddedCard(postState.Top, preState.Top)
	if !ok || added.IsJoker() {
		return 0
	}
	if partialEvalTP(postState.Top).Type > pre.Type {
		return 0 // 第3张改进了顶(成三条等) → 非死kicker
	}
	if added.Rank() > 7 { // rank index: 9=7. 高kicker(T+ idx≥8)可能安全dump, 只罚低死kicker(2..9)
		return 0
	}
	// ⚠️ 守护: 仅当底道已成**三条+**(低牌进底凑葫芦更值)才罚. 否则低牌在底无发育(如实战72 底[Tc]单张,
	//   低牌该上顶当AA kicker让位 5c 留花draw) → 拍顶是对的, 不罚.
	if botMadeTier(postState.Bottom) < TypeThreeOfAKind {
		return 0
	}
	return 4.0 // 2026-06-20: 2.5→4 (鬼jokerRem修干净后, 实战93 2h→顶base更高需更强罚)
}

// RnR4TripsFantasyReachableBonus — 正向版 (用户 2026-06-22, 实战110/48; 取代旧 RnLowCardOnLockedTop 罚版):
//
//	R4 + 中道恰=三条(rank M) + 底>中(盘锁死) 时, 若顶 placement 后**仍能成 ≤M 的合法 trips 范**
//	(顶范种子没被占死) → +25. 奖"留鬼/留低种子保合法trips范" 而非罚"占死".
//	留鬼(110: 顶[鬼]+空位能配≤6低对成 trips≤6) → +25;
//	3d种(48: 顶[鬼3]能成333≤6 合法范) → +25;
//	7h占顶(110: 顶[鬼7h]只能成777>6 倒置 foul) → 0 (输给留鬼25分).
func RnR4TripsFantasyReachableBonus(postState, preState *GameState) float32 {
	// 2026-06-23 (用户): R4-only → 开 R3 (ypk-93192522-6 R3: 中666底888顶joker, 6h压顶堵XXX范种子).
	//   R5 不开 (末轮无未来发育, joker trips种子无意义).
	if postState.Round != 3 && postState.Round != 4 {
		return 0
	}
	midV := partialEvalTP(postState.Middle)
	if midV.Type != TypeThreeOfAKind {
		return 0 // 中道非恰三条 → 顶trips范语境不成立
	}
	// 2026-06-23 (用户发现bug): 旧用 HandExceeds5(partialEvalTP) — 底满5张 partialEvalTP 走 Evaluate5JokerCap
	//   (makeValue 15^4编码), 中<5张走 partialEval *15编码, 跨编码比 → 底666<中888 误判底>中 返30该0.
	//   改 madeHandCmp(type*16+rank 一致编码).
	if madeHandCmp(postState.Bottom) <= madeHandCmp(postState.Middle) {
		return 0 // 底未>中 (盘未锁)
	}
	var midRC [13]int
	midJokers := 0
	for _, c := range postState.Middle {
		if c.IsJoker() {
			midJokers++
		} else {
			midRC[int(c.Rank())]++
		}
	}
	M := -1
	for r := 12; r >= 0; r-- {
		if midRC[r]+midJokers >= 3 {
			M = r
			break
		}
	}
	if M < 0 {
		return 0
	}
	var topRC [13]int
	topJokers := 0
	for _, c := range postState.Top {
		if c.IsJoker() {
			topJokers++
		} else {
			topRC[int(c.Rank())]++
		}
	}
	slots := 3 - len(postState.Top)
	rankRem, _, _ := computeDeckRemaining(postState)
	// 顶成 trips RRR 要求所有真牌同 rank R (异rank真牌挡死单一trips); R≤M 才合法不倒置.
	canReach := func(r int) bool {
		if r > M {
			return false
		}
		need := 3 - topRC[r] - topJokers
		if need <= 0 {
			return true // 已是 ≤M 的 trips
		}
		return slots >= need && rankRem[r] >= need
	}
	distinctReal := 0
	theRank := -1
	for r := 0; r < 13; r++ {
		if topRC[r] > 0 {
			distinctReal++
			theRank = r
		}
	}
	if distinctReal >= 2 {
		return 0 // 顶 2+ 不同真牌 → 成不了单一 trips
	}
	// 2026-06-23 (用户): 底4张 + 中4张 (都发育中, 各留1空位) → 再+5, 偏好平衡发育摆法
	//   (手1: Th→中 使 中666Th+底888J 各4张, 优于 Th→底 锁底5留中3).
	bonus := float32(30)
	if len(postState.Bottom) == 4 && len(postState.Middle) == 4 {
		bonus += 5
	}
	if distinctReal == 1 {
		if canReach(theRank) {
			return bonus
		}
		return 0
	}
	for r := 0; r <= M; r++ { // 顶全鬼/空: 任一 r≤M 可凑
		if canReach(r) {
			return bonus
		}
	}
	return 0
}

// RnLoneSubQOnTopPenalty — 太子专属 (2026-06-14, 实战28 ypk-185336138-28): 本轮起手往**空顶**放
// 1 张 **≥中道最大真牌 且 <Q** 的牌, 而底道已成对+未满 → 罚 -2. 该牌在顶**零范路径 + foul险**:
// ① 自配对 < QQ 不是对范; ② 升三条范又会犯规(需 mid ≥ 该三条, 弱中托不住);
// ③ 自配对 > 中max → 顶对压中 foul. (比中max小的牌上顶: 中能匹配, 不foul, 不罚.)
// 该牌进底更值 (底有对 → 添两对 draw, 实战28 底TT + Jd(≥mid max6) → 催 TTJJ). ⚠️ 模型特定, 换 sp24/sp25 须重评.
func RnLoneSubQOnTopPenalty(postState, preState *GameState) float32 {
	if len(preState.Top) != 0 || len(postState.Top) != 1 {
		return 0 // 只管"本轮往空顶起手放 1 张"
	}
	c := postState.Top[0]
	if c.IsJoker() {
		return 0
	}
	r := int(c.Rank())
	midMaxIdx := -1 // 中道现有最大真牌 rank
	for _, mc := range postState.Middle {
		if !mc.IsJoker() && int(mc.Rank()) > midMaxIdx {
			midMaxIdx = int(mc.Rank())
		}
	}
	if midMaxIdx < 0 || r < midMaxIdx || r >= int(RankQ) {
		return 0 // 只罚 [中道最大牌, Q): 比中max小不foul, Q+有范苗头, 中无真牌不比
	}
	// 升三条范是否被弱中堵死: mid 现成牌型 < 该 rank 三条 → 升三条必犯规 → 顶零范路径
	midType, midTrip := midMadeFloor(postState.Middle)
	if midType > TypeThreeOfAKind || (midType == TypeThreeOfAKind && midTrip >= r) {
		return 0 // 中能托住该三条 → 顶牌还有三条范苗头, 不罚
	}
	bot := partialEvalTP(postState.Bottom)
	if bot.Type < TypePair || len(postState.Bottom) >= 5 {
		return 0 // 底须已成对且未满 (放底能添两对 draw)
	}
	return 2
}

// partialEvalTP — 两对感知的部分行评估 (中>底 倒置比较专用).
// partialEval (features_v2.go) 只认 单对/三条: 遇两对 (如 [2s 2c Ks Kh]) 会
//
//	① 误判成单对; ② 更糟 — 从低位扫 rankCnt 先撞到小对, 报成"22对"漏掉 KK.
//
// 导致"中两对 vs 底单对"倒置漏罚 (而三条倒置罚得到 = 不对称). 这里补两对.
//
//	满 5 张: 用 Evaluate5JokerCap (认花/顺/葫芦/四条).
//	<5 张: count-based, joker 优先补三条 (不做第二对), j==0 且 ≥2 真对子才算两对.
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
//
//	(编辑 case top=AA mid=2s2c bot=QhQc6h 发2dKsKh: KK→中成KK22两对压底QQ, 原漏罚).
func RnMidExceedsBotPenalty(postState, preState *GameState) float32 {
	mid := partialEvalTP(postState.Middle)
	bot := partialEvalTP(postState.Bottom)
	if mid.Type < TypePair || bot.Type < TypePair {
		return 0 // 至少都成对才算"对梯倒置"
	}
	// 2026-06-14 sp26 recal: 底道含鬼 + 未满 → 鬼给巨大发育潜力(成花/顺/三条追上), 不罚 mid>bot.
	//   实战1: 中666 > 底[X Jd 8d](鬼配JJ), 底鬼会追上 → 别罚 666 三条. 实战23(底无鬼 QQ)仍罚.
	botHasJoker := false
	for _, c := range postState.Bottom {
		if c.IsJoker() {
			botHasJoker = true
			break
		}
	}
	if botHasJoker && len(postState.Bottom) < 5 {
		return 0
	}
	// 2026-06-17 实战9 bug fix: 中道满5张含鬼时, partialEvalTP 用 nil-cap 把鬼当最大(A高花)→ 误判中>底.
	//   实际鬼可压低 → 用 cap=&bot 重评中道, 鬼压低后中 ≤底 就无强制倒置, 不罚.
	//   (中[8h X 3h 7h 2h]红心花: 真张max8, 鬼即便当Kh仍 < 底 KQ763梅花 → 不foul.)
	midHasJoker := false
	for _, c := range postState.Middle {
		if c.IsJoker() {
			midHasJoker = true
			break
		}
	}
	if midHasJoker && len(postState.Middle) == 5 {
		if midCap := Evaluate5JokerCap(postState.Middle, &bot); !HandExceeds5(midCap, bot) {
			return 0 // 鬼压低后中 ≤底 → 无强制 foul
		}
	}
	// 2026-06-17 实战17(ypk-111870282-17): 中2266两对 > 底KK单对, 但底KK(rank高于中两对)会发育成
	//   KK两对反超 → 假倒置, 不罚. (中两对 + 底单对 rank > 中最高对 + 底未满. KK→中overQQ 仍罚:
	//   中KK是单对非两对; 实战23 中KK22>底QQ 仍罚: 底Q < 中最高对K.)
	if mid.Type == TypeTwoPair && bot.Type == TypePair && len(postState.Bottom) < 5 &&
		highestRealPairRank(postState.Bottom) > highestRealPairRank(postState.Middle) {
		return 0
	}
	// 2026-06-18 局12(seed99 R2): 中道鬼借成三条(真牌仅单对55) vs 底道更高真对(KK)未满 →
	//   真牌看底对(K) > 中对(5), 鬼只是把中道吹成三条. 底KK必发育成KK葫芦/KKK 反超锁死的555, 不罚.
	//   纯NN base=88.7(全局最高)选此线, 旧 -18 把它压到 70.7 → AI 改放 Kd Jd→底反而 foul.
	//   (vs 真555无鬼: 中道真实承诺厚, 不豁免; 必底对rank > 中对rank, 否则小对追不上.)
	if midHasJoker && mid.Type == TypeThreeOfAKind && bot.Type == TypePair && len(postState.Bottom) < 5 &&
		highestRealPairRank(postState.Bottom) > highestRealPairRank(postState.Middle) {
		return 0
	}
	// 2026-06-17 实战18(ypk-111870282-18): 中333锁死, 本轮 Js→底[KsJdTh]→[KsJdTh Js]=JJ 真牌发育追赶中道.
	//   中道本轮**成手没变强**(锁死, 或只加 kicker) + 底未满 + 底道 rank 类型本轮提升(高牌→对) → 是"底道发育"
	//   不是"中道膨胀超底", 不罚. (vs KK→中 倒置: 中道膨胀 madeHandCmp↑, 仍罚.)
	//   2026-06-22 (用户 ypk-84869450-8 R3): 旧版要求中 len 不变, 漏了"中加 kicker 但成手没变"(中JJ+3h still JJ);
	//     正解 3中6底(底66发育能成KK66反超JJ) 被这条 -18 误压(NN base 4.44 全局最高). 放宽成 madeHandCmp 不增.
	if len(postState.Bottom) < 5 && madeHandCmp(postState.Middle) <= madeHandCmp(preState.Middle) &&
		bot.Type > partialEvalTP(preState.Bottom).Type {
		return 0
	}
	if HandExceeds5(mid, bot) {
		return 18 // 中 > 底 → 倒置, 罚 (接管 kkMid 删后的 hand1)
	}
	return 0
}

// rowAddedCard — post 比 pre 多出的那张牌 (post = pre + 本轮1张). 找不到返回 false.
func rowAddedCard(post, pre []Card) (Card, bool) {
	cnt := map[Card]int{}
	for _, c := range pre {
		cnt[c]++
	}
	for _, c := range post {
		if cnt[c] > 0 {
			cnt[c]--
		} else {
			return c, true
		}
	}
	return 0, false
}

// rowHasDrawOrPair — 行(部分牌)有对 / 3+同花draw / 3+连顺draw (鬼当wild). 行级版 botHasDrawOrPair.
func rowHasDrawOrPair(row []Card) bool {
	suitCnt := map[uint8]int{}
	var rankCnt [13]int
	jokers := 0
	var ranks []int
	for _, c := range row {
		if c.IsJoker() {
			jokers++
			continue
		}
		suitCnt[c.Suit()]++
		rankCnt[c.Rank()]++
		ranks = append(ranks, int(c.Rank()))
	}
	for _, n := range rankCnt {
		if n+jokers >= 2 {
			return true // 对 (鬼可凑)
		}
	}
	for _, n := range suitCnt {
		if n+jokers >= 3 {
			return true // 3+ 同花 draw
		}
	}
	for lo := 0; lo <= 12; lo++ { // 3+ 连续 (含鬼填 gap): 任意 5-rank 窗口真牌≥1 且 真牌+鬼≥3
		cnt := 0
		for _, r := range ranks {
			if r >= lo && r <= lo+4 {
				cnt++
			}
		}
		if cnt >= 1 && cnt+jokers >= 3 {
			return true
		}
	}
	return false
}

// RnHighCardWrongRowPenalty — R2-R5 本轮"中底各放1张真牌", 两行都死(无对无花draw无顺draw),
// 且放中的牌 rank > 放底的牌 → 高牌该进底(bot≥mid 梯度), 罚. 2026-06-17 实战16(ypk-111870282-16):
// 8c→中 5s→底 错(8>5 高牌埋中), 该 5→中 8→底 (或 5中J底). 守卫条件防误伤 draw/pair 发展.
func RnHighCardWrongRowPenalty(postState, preState *GameState) float32 {
	if len(postState.Top) != len(preState.Top) { // top 本轮不能动
		return 0
	}
	if len(postState.Middle) != len(preState.Middle)+1 || len(postState.Bottom) != len(preState.Bottom)+1 {
		return 0 // 必须中底各恰好新增1张
	}
	midNew, ok1 := rowAddedCard(postState.Middle, preState.Middle)
	botNew, ok2 := rowAddedCard(postState.Bottom, preState.Bottom)
	if !ok1 || !ok2 || midNew.IsJoker() || botNew.IsJoker() {
		return 0 // 鬼不比大小
	}
	if rowHasDrawOrPair(postState.Middle) || rowHasDrawOrPair(postState.Bottom) {
		return 0 // 任一行有对/花/顺发展 → 非纯高牌梯度, 不罚
	}
	if midNew.Rank() > botNew.Rank() {
		return 4 // 高牌埋中道 → 罚
	}
	return 0
}

// R1HighCardShouldBeBotKickerPenalty — R1: 底恰成一对 + 中道有松高牌 > 底kicker → 该高牌该当底对kicker, 罚1.
// 2026-06-17 用户(局96 seed11 FOUL): 发KK+Q23, AI Q埋中(中Q-2)+3当底kicker(KK3); 应 Q进底当kicker(KKQ强)+23留中.
//
//	高牌进底梯度的R1版. (RnHighCardWrongRow是RN且要"两行都死无对", 这里底有对故另写.) guard: 中道有对/花/顺draw就跳.
func R1HighCardShouldBeBotKickerPenalty(p Placement, cards []Card) float32 {
	var mid, bot []Card
	for i, c := range cards {
		switch p[i] {
		case RowMiddle:
			mid = append(mid, c)
		case RowBottom:
			bot = append(bot, c)
		}
	}
	if partialEvalTP(bot).Type != TypePair { // 底必须恰成一对 (三条+ kicker无关)
		return 0
	}
	if rowHasDrawOrPair(mid) { // 中道有对/3+花/顺draw → 高牌可能是draw一部分, 不罚
		return 0
	}
	// 2张同花 或 连张(5-window内) 也算"有花/有顺"潜力 → 跳 (用户: 单张无对无顺无花才罚)
	{
		var suits [4]int
		var mranks []int
		for _, c := range mid {
			if c.IsJoker() {
				return 0 // 鬼牌灵活, 不罚
			}
			suits[c.Suit()]++
			mranks = append(mranks, int(c.Rank()))
		}
		for _, n := range suits {
			if n >= 2 {
				return 0 // 2+ 同花潜力
			}
		}
		for i := 0; i < len(mranks); i++ {
			for j := i + 1; j < len(mranks); j++ {
				d := mranks[i] - mranks[j]
				if d < 0 {
					d = -d
				}
				if d <= 4 {
					return 0 // 5-window 内连张潜力
				}
			}
		}
	}
	var botCnt, midCnt [13]int
	for _, c := range bot {
		if !c.IsJoker() {
			botCnt[c.Rank()]++
		}
	}
	for _, c := range mid {
		if !c.IsJoker() {
			midCnt[c.Rank()]++
		}
	}
	botKick := -1 // 底最高 kicker (非对单张)
	for r := 12; r >= 0; r-- {
		if botCnt[r] == 1 {
			botKick = r
			break
		}
	}
	if botKick < 0 {
		return 0 // 底无 kicker
	}
	for r := 12; r > botKick; r-- {
		if midCnt[r] == 1 { // 中道有松高牌 > 底kicker → 底散牌<中散牌, 该高牌该进底当kicker
			return 2 // 2026-06-17 用户: 扣2分 (底<中, 单张无对无顺无花时)
		}
	}
	return 0
}

// RnMidPairCompletesTwoPairBonus — 本轮往中道放的真牌配对中道已有单张, 把中道做成两对 → 奖.
// 2026-06-17 用户明确要求 (实战14 ypk-111870282-14: 7s 配中道 7d 成 4477 两对).
// ⚠️ 用户要这个**即便底道托不住** — 单人 solver 中道两对 royalty=0 + 底 gutshot ~26% 会 foul,
//
//	用户按"对手局牌力"直觉优先中道两对, 故**不设 bot>mid guard**(区别于 RnMidMakeTwoPairBonus).
//	安全网: 必 foul 的变体仍被 FoulImminentPenalty(-20) 压住, 本 +3 只在"有风险非必死"时翻.
func RnMidPairCompletesTwoPairBonus(postState, preState *GameState) float32 {
	if botAtLeastTwoPair(preState.Middle) {
		return 0 // 中道本来就 ≥两对, 非本轮新做
	}
	if partialEvalTP(postState.Middle).Type != TypeTwoPair {
		return 0 // 没做成恰两对
	}
	added, ok := rowAddedCard(postState.Middle, preState.Middle)
	if !ok || added.IsJoker() {
		return 0 // 鬼配对另算
	}
	cnt := 0 // added 必须配对中道已有的"单张"(pre 该 rank 恰 1 张真牌)
	for _, c := range preState.Middle {
		if !c.IsJoker() && c.Rank() == added.Rank() {
			cnt++
		}
	}
	if cnt == 1 {
		return 3
	}
	return 0
}

// has3InStraightWindow — distinct ranks 里任意 5-rank 窗口含 ≥3 张 (3张顺draw). A 兼顾低位(轮子).
func has3InStraightWindow(ranks []int) bool {
	seen := map[int]bool{}
	for _, r := range ranks {
		seen[r] = true
		if r == RankA {
			seen[-1] = true // A 当低 (A-2-3-4-5)
		}
	}
	for lo := -1; lo <= int(RankT); lo++ {
		cnt := 0
		for r := lo; r <= lo+4; r++ {
			if seen[r] {
				cnt++
			}
		}
		if cnt >= 3 {
			return true
		}
	}
	return false
}

// has3ConsecutiveRanks — distinct ranks 里是否有 3 张相连 (A 可当低 A-2-3). 用于区分"连张顺面"(返true) vs "卡顺"(返false).
func has3ConsecutiveRanks(ranks []int) bool {
	seen := map[int]bool{}
	for _, r := range ranks {
		seen[r] = true
		if r == RankA {
			seen[-1] = true // A 当低
		}
	}
	for lo := -1; lo <= int(RankA)-2; lo++ {
		if seen[lo] && seen[lo+1] && seen[lo+2] {
			return true
		}
	}
	return false
}

// rowDrawFaceBonus — 行全单张(无对无鬼) + 有顺面(3+在5-rank窗口)或花面(3+同花) → +2.
// 2026-06-17 用户准则: "都是单张有顺面花面加分". 单张攒顺/花draw 有价值(顺/花royalty + outs多).
// 用于中道(实战11: 4s接3-4-6顺托顶范)和底道(局23: 底8-9-T-J两头顺8outs > 配99单对垫中两对→foul).
func rowDrawFaceBonus(row []Card) float32 {
	if len(row) < 3 {
		return 0
	}
	var rc [13]int
	suits := map[uint8]int{}
	ranks := []int{}
	for _, c := range row {
		if c.IsJoker() {
			return 0 // 有鬼另算, 只奖纯单张 draw
		}
		rc[c.Rank()]++
		if rc[c.Rank()] >= 2 {
			return 0 // 有对 → 走配对路线非 draw
		}
		suits[c.Suit()]++
		ranks = append(ranks, int(c.Rank()))
	}
	flushFace := false
	for _, n := range suits {
		if n >= 3 {
			flushFace = true
		}
	}
	if has3InStraightWindow(ranks) || flushFace {
		return 2
	}
	return 0
}

// RnMidDrawFaceBonus — 中道 draw 面 (向后兼容/测试入口)
func RnMidDrawFaceBonus(postState *GameState) float32 {
	return rowDrawFaceBonus(postState.Middle)
}

// RnMidDrawFaceGated — 中道draw面奖, 但本轮把"能配中道对子的真牌"放到了中道以外(顶/底), 留中道弱draw → 不奖.
// 2026-06-18 (seed99局25 R4): 中[7d 8d 5c], 8h能配8d成88(中道能成的最大对), prod却8h→顶留中道5-7-8弱顺draw吃+2.
//
//	value-head本要8h→中做88(base更高). 这gate去掉"放弃成对、追弱draw"的MidDrawFace+2.
//
// RnMidTwoPairBotDrawBonus — 中道已成两对+(定型) + 底道还能成花/顺(没被低牌填死) → +2.
//
//	2026-06-20 (用户, 实战72): 中[7766]两对锁定时, 别把低牌都塞底道杀掉底花draw —
//	底[Tc 5c]留3空可成梅花花 vs 底[Tc 5c 4h]填死(2空只剩4花). 奖"保住底花/顺draw"的摆法.
func RnMidTwoPairBotDrawBonus(postState *GameState) float32 {
	if DisabledRules["MidTwoPairBotDraw"] {
		return 0
	}
	if partialEvalTP(postState.Middle).Type < TypeTwoPair {
		return 0 // 中道没成两对+
	}
	bot := postState.Bottom
	slots := 5 - len(bot)
	if slots <= 0 {
		return 0 // 底道已满, 无draw
	}
	suitCnt := map[uint8]int{}
	var ranks []int
	for _, c := range bot {
		if c.IsJoker() {
			return 0 // 有鬼另算
		}
		suitCnt[c.Suit()]++
		ranks = append(ranks, int(c.Rank()))
	}
	for _, n := range suitCnt {
		if n+slots >= 5 {
			return 2 // 底道还能补成花
		}
	}
	if has3InStraightWindow(ranks) {
		return 2 // 底道还能成顺
	}
	return 0
}

func RnMidDrawFaceGated(dealt []Card, gs *GameState) float32 {
	b := rowDrawFaceBonus(gs.Middle)
	if b == 0 {
		return 0
	}
	// 2026-06-23 (用户, ypk-19398986-5 R2): 底仅2张(刚成对) 且 底对rank < 本轮放进中道的真牌rank → 不奖.
	//   底单薄时就拿高牌建中道draw, 中道易后续反超底道 (本局 8c→中, 底77, 8>7 → 到R4双向foul死局).
	if len(gs.Bottom) == 2 {
		if botPair := highestRealPairRank(gs.Bottom); botPair >= 0 {
			midCnt := map[Card]int{}
			for _, c := range gs.Middle {
				midCnt[c]++
			}
			midPlaced := -1
			for _, c := range dealt { // 本轮发牌进了中道的真牌, 取最大 rank
				if !c.IsJoker() && midCnt[c] > 0 && int(c.Rank()) > midPlaced {
					midPlaced = int(c.Rank())
				}
			}
			if midPlaced >= 0 && botPair < midPlaced {
				// 2026-06-23 (用户): 此时中道若是"卡顺"(顺面但非3连张, 如4-6-8 有内部gap) → 不止不奖,
				//   还扣 -2 (弱 gutshot draw + 底单薄被反超, 双重坏). 连张顺面(6-7-8)仅 return 0.
				var mr []int
				for _, c := range gs.Middle {
					if !c.IsJoker() {
						mr = append(mr, int(c.Rank()))
					}
				}
				if has3InStraightWindow(mr) && !has3ConsecutiveRanks(mr) {
					return -2
				}
				return 0
			}
		}
	}
	// 2026-06-18 (s99局78 R2): 4张中道"卡顺"(gutshot 直draw, 且非4-flush强draw) 是弱draw, 别奖 →
	//   防为卡顺弃KK(底). 用户: 4张中道卡顺不加. (开口顺/4-flush 仍奖.)
	if len(gs.Middle) == 4 && !hasNFlushDraw(gs.Middle, 4) {
		if open, gut := classifyStraightDraw4(gs.Middle); gut && !open {
			return 0
		}
	}
	var midRank [13]bool
	for _, c := range gs.Middle {
		if !c.IsJoker() {
			midRank[c.Rank()] = true
		}
	}
	midCnt := map[Card]int{}
	for _, c := range gs.Middle {
		midCnt[c]++
	}
	for _, c := range dealt { // 本轮发的牌, 没进中道却能配中道对(放别处/弃掉) → 放弃成对追弱draw, 不奖
		if c.IsJoker() {
			continue
		}
		if midCnt[c] > 0 {
			midCnt[c]--
			continue // 这张进了中道
		}
		if midRank[c.Rank()] {
			return 0
		}
	}
	// 2026-06-23 (用户, 手2 ypk-93192522-2): 底已成花+(花/葫芦/金刚/同花顺) + 中道有【纯顺draw】→ 再+5.
	//   不含底顺: 底顺时中道draw成顺可能比底顺大 → 倒置; 花+必>任何中道顺, 才安心建中顺draw(89T).
	//   ⚠️中道是花draw(≥3同花)时不给: 中往花发育可能 > 底花 = 倒置 (2h4h6h 既顺窗又花draw, 排除).
	if botMadeTier(gs.Bottom) >= TypeFlush {
		var mr []int
		var seen [13]bool
		var suitCnt [4]int
		flushDraw := false
		for _, c := range gs.Middle {
			if c.IsJoker() {
				continue
			}
			suitCnt[c.Suit()]++
			if suitCnt[c.Suit()] >= 3 {
				flushDraw = true
			}
			if !seen[c.Rank()] {
				seen[c.Rank()] = true
				mr = append(mr, int(c.Rank()))
			}
		}
		if has3InStraightWindow(mr) && !flushDraw {
			return b + 5
		}
	}
	return b
}

// maxAddedRealRank — 本轮往某行新增的真牌里最大 rank (无真牌/仅鬼 → -1); n=新增牌数(含鬼).
func maxAddedRealRank(post, pre []Card) (maxR, n int) {
	cnt := map[Card]int{}
	for _, c := range pre {
		cnt[c]++
	}
	maxR = -1
	for _, c := range post {
		if cnt[c] > 0 {
			cnt[c]--
			continue
		}
		n++
		if !c.IsJoker() && int(c.Rank()) > maxR {
			maxR = int(c.Rank())
		}
	}
	return
}

// RnMidPlacedOverBotPlacedPenalty — R1-R4: 本轮"放中道的最大牌 > 放底道的最大牌" → 大牌放中了 → 底<中 → 罚2.
// 2026-06-17 用户(局80 R2): 放中道8h > 放底道5d → 该把大牌放底. 底道有花/顺draw 或对/成手 → 豁免.
//
//	跟 局96 R1HighCardShouldBeBotKicker(底成对管kicker)互补; 跟 RnHighCardWrongRow(要求两行都死)区别: 这条只看底豁免, 不管中有无draw.
func RnMidPlacedOverBotPlacedPenalty(postState, preState *GameState) float32 {
	if postState.Round < 1 || postState.Round > 4 {
		return 0
	}
	if rowHasDrawOrPair(postState.Bottom) || partialEvalTP(postState.Bottom).Type >= TypePair {
		return 0 // 用户spec: 底道有花/顺draw 或 对/成手 → 豁免
	}
	midMax, midN := maxAddedRealRank(postState.Middle, preState.Middle)
	botMax, botN := maxAddedRealRank(postState.Bottom, preState.Bottom)
	if midN == 0 || botN == 0 || midMax < 0 || botMax < 0 {
		return 0 // 中底本轮没都放真牌 → 不比
	}
	if midMax > botMax {
		// 2026-06-22 (用户): 中道该最大真牌若成对(含鬼 joker+A=AA) → 合理成对摆放非威胁, 豁免.
		if midPairedRanks(postState.Middle)[midMax] {
			return 0
		}
		return 2 // 本轮放中道的最大真牌 > 放底道的最大真牌 → 底<中
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
//
//	① pre-top 有鬼, 且"鬼能配出的对子 < QQ" (孤鬼 / 鬼+J以下) —— 即还没法直接进范, 加 A 才升 AA.
//	   跳过 X+Q/X+K/XX/XA (鬼配对已 ≥QQ, 可直接进范, 不需 A; 你说的"已锁就不AA中").
//	② 本轮恰好 1 张真 A 上顶 (post-top realA==1, 不成 AAA foul陷阱).
//	③ 本轮没往中道加 A (废 A 放底, 不堵中道 —— A 进中变死高张, 挡顶道 AA 范 + 占两对位).
//
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
	jt, topA := 0, 0
	for _, c := range preState.Top {
		if c.IsJoker() {
			jt++
		} else if int(c.Rank()) == RankA {
			topA++
		}
	}
	if jt == 0 {
		return 0 // 非鬼顶
	}
	if topA >= 1 && botMadeTier(postState.Bottom) >= TypeStraight {
		// 2026-06-18 (s99局76 R4): 顶已 鬼+A=AA 锁范 **且底已成顺+(锁强)** → 中道的 A 不是"该上顶配鬼"的死张,
		//   而是合法发育(托住AA顶的中道顺, 夹在AA与底顺之间). 旧-8压错纯NN首选(Ac→中).
		//   ⚠️必须底强(顺+): 实战51 底[X6c]弱 → A该进底不进中, 仍罚.
		return 0
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
//
//	mid 满 5 张: 用真实 eval (认得 花/顺/葫芦/四条 这些 > 三条但无对子计数的牌型).
//	mid 部分 (<5): 只能靠已落子的 count floor (花/顺 draw 未成, 不算保证) → pair/trips/quads via counts.
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
	if prePair < 0 {
		return 0 // pre-top 不是对子 → 不是"对→三条" overcommit
	}
	// 2026-06-17 port v0-dev: 去掉原 prePair>=RankQ 守护. 低对升三条(22→222)照样冒顶(实战46 中88托不住).
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
	// 2026-06-17 port v0-dev 分级: 中道<三条 → 比 top 三条低一整 tier → near-certain foul 大罚16;
	// 中道是三条但 rank 低 → 按 rank gap. (实战46 中88对 → 16.)
	var pen float32
	if midType < TypeThreeOfAKind {
		pen = 16
	} else {
		pen = 10.0 + float32(topTrip-midTrip)*0.6
	}
	if pen > 18 {
		pen = 18
	}
	return pen
}

// RnRedundantHighOnLockedAAPenalty — 2026-06-17 port v0-dev (实战51). R2-R4 顶已锁AA:
// ① 底"鬼弱"(含鬼+无真对, 如51的X6c)时 K/Q 进顶/中 → 罚12 (该进底配鬼成高对); 底已真对(实战6 JJ)不罚.
// ② 本轮往底加 A → 罚5 (AA锁时别浪费A进底).
// cardHasBotValue — C 进底道是否有价值: 凑花 / 凑对 / 凑顺 (底含鬼当 wild). 用户准则: 有价值就该进底.
//
//	同花: 底同色真张 + C + 鬼 ≥3.  配对: C rank 已在底.  顺: 底真ranks + C 某5-rank窗口(含C) ≥3张(含鬼).
func cardHasBotValue(c Card, bottom []Card) bool {
	botJoker := 0
	var suit [4]int
	var hasRank [13]bool
	for _, b := range bottom {
		if b.IsJoker() {
			botJoker = 1
		} else {
			suit[b.Suit()]++
			hasRank[b.Rank()] = true
		}
	}
	cs, cr := int(c.Suit()), int(c.Rank())
	if suit[cs]+1+botJoker >= 3 {
		return true // 凑花 (同花draw)
	}
	// 2026-06-18 (手2 R4 ypk-129630538-5): 底已近成花(某花色4张, 或3张+鬼) 但本牌非该花色
	//   → 进底会破花(花 > 对/顺), 反而降级底道 → 无底价值, 别罚"封中道三条". 凑对/凑顺检查跳过.
	maxSuit := 0
	for _, n := range suit {
		if n > maxSuit {
			maxSuit = n
		}
	}
	if maxSuit+botJoker >= 4 {
		return false // 底近成花 + 本牌非该花色(凑花检查已过滤) → 破花, 无底价值
	}
	if hasRank[cr] {
		return true // 凑对 (C rank 已在底)
	}
	hasRank[cr] = true
	for s := 0; s <= 8; s++ {
		if cr < s || cr > s+4 {
			continue // C 必须在该顺窗口内
		}
		cnt := 0
		for r := s; r <= s+4; r++ {
			if hasRank[r] {
				cnt++
			}
		}
		if cnt+botJoker >= 3 {
			return true // 凑顺 (5窗口内≥3张含鬼)
		}
	}
	return false
}

// RnMidKickerShouldBotFlushPenalty — 2026-06-17 实战1 (sp26 value-head弱, 太子原生留花).
// 用户思路: 中道恰成三条(死kicker场景) + 本轮往中塞的非鬼牌 C, 若 C 对底道有价值
//
//	(底靠它能成花/成顺) → 罚3 (C 在中是死kicker浪费, 该进底). ⚠️ stopgap, 重训让 value head 自学估draw.
func RnMidKickerShouldBotFlushPenalty(action *RoundNAction, postState, preState *GameState) float32 {
	if len(preState.Bottom) >= 5 {
		return 0 // 底满, C 无处放
	}
	if partialEvalTP(postState.Middle).Type != TypeThreeOfAKind {
		return 0 // 只管"中道恰三条"(死kicker场景); 葫芦/四条说明该牌有用, 不罚
	}
	var pen float32
	for k, c := range action.Kept {
		if action.Placement[k] != RowMiddle || c.IsJoker() {
			continue
		}
		if cardHasBotValue(c, preState.Bottom) {
			pen += 3
		}
	}
	return pen
}

// topPairRank — 顶道 made pair 的 rank (joker-aware): 真对取最高; 无真对但有鬼则鬼+最高单=对. 无→-1.
func topPairRank(top []Card) int {
	var cnt [13]int
	j := 0
	for _, c := range top {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 2 {
			return r
		}
	}
	if j >= 1 {
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 1 {
				return r
			}
		}
	}
	return -1
}

// rowSupportsPair — row 当前成牌能否 ≥ 一对 P (两对+ 恒 > 单对; 同/高 rank 对也行).
func rowSupportsPair(row []Card, p int) bool {
	hv := partialEvalTP(row)
	if hv.Type > TypePair {
		return true
	}
	if hv.Type == TypePair {
		var cnt [13]int
		jk := 0
		for _, c := range row {
			if c.IsJoker() {
				jk++
			} else {
				cnt[c.Rank()]++
			}
		}
		for r := 12; r >= 0; r-- {
			if cnt[r] >= 2 {
				return r >= p
			}
		}
		if jk >= 1 {
			for r := 12; r >= 0; r-- {
				if cnt[r] >= 1 {
					return r >= p
				}
			}
		}
	}
	return false
}

// RnTopPairOvercommitPenalty — 2026-06-17 (std63-61): 本轮把顶做成 QQ+/KK made对(非AA), 且牌堆
// A+鬼 ≥3 (升 AA 有望, 该留顶等 AA 范 > KK 范) → 罚6. 安全阀: 中底都已稳托住该对 = 锁对是稳范, 不罚.
func RnTopPairOvercommitPenalty(postState, preState *GameState) float32 {
	if preState.Round < 2 || preState.Round > 4 {
		return 0
	}
	pre := topPairRank(preState.Top)
	post := topPairRank(postState.Top)
	if post < int(RankQ) || post >= int(RankA) || post <= pre {
		return 0 // 只管本轮新建 QQ/KK 顶对 (AA 无可升, 不管)
	}
	// 2026-06-17 局70: 顶对靠鬼凑(该 rank 真牌<2)= 灵活范种子(鬼可配未来 A 成 AA, 非锁死 QQ/KK) → 不罚.
	realCnt := 0
	for _, c := range postState.Top {
		if !c.IsJoker() && int(c.Rank()) == post {
			realCnt++
		}
	}
	if realCnt < 2 {
		return 0
	}
	rankRem, _, jokerRem := computeDeckRemaining(postState)
	if rankRem[RankA]+jokerRem < 3 {
		return 0 // 牌堆 A+鬼 <3 → 升 AA 无望, 锁现对 OK
	}
	if rowSupportsPair(postState.Middle, post) && rowSupportsPair(postState.Bottom, post) {
		return 0 // 中底都已稳托住 → 锁对是稳范, 别罚
	}
	return 6
}

// RnJokerHighSeedOnTopBonus — R2-R4 本轮鬼→顶 + post-top = 鬼+恰1张真≥Q(K/Q, 种 KK/QQ范) + 顶未满 → +4.
// 2026-06-17 局70: 鬼+Kh→顶 博 KK 范种子(鬼灵活, 可配未来A成AA), value head 偏埋鬼(gap~3) → 奖翻过.
// A 走 RnJokerAOnTopBonus(+16, AA锁), 这条专管 K/Q 种子. 低 foul 风险(鬼灵活不锁死).
func RnJokerHighSeedOnTopBonus(action *RoundNAction, postState *GameState) float32 {
	jokerToTop := false // 本轮必须有鬼往顶放
	for k, c := range action.Kept {
		if action.Placement[k] == RowTop && c.IsJoker() {
			jokerToTop = true
		}
	}
	if !jokerToTop || len(postState.Top) >= 3 {
		return 0 // 没鬼上顶 / 顶满(锁死非种子)
	}
	jokers, realCnt, highCnt := 0, 0, 0 // post-top: 鬼 / 真牌 / ≥Q真牌(K或Q, 不含A)
	for _, c := range postState.Top {
		if c.IsJoker() {
			jokers++
		} else {
			realCnt++
			if int(c.Rank()) >= int(RankQ) && int(c.Rank()) < int(RankA) {
				highCnt++
			}
		}
	}
	if jokers == 1 && realCnt == 1 && highCnt == 1 {
		return 4 // 鬼 + 恰1张K/Q = 灵活 KK/QQ 范种子
	}
	return 0
}

// RnAceToMidSupportTopAABonus — 顶成AA + 本轮Ace→中道 + 中道无≥T高牌杂(保轮子/低位向) + 中道还没成对
//
//	→ +4. 2026-06-17 局91: 顶AA成+底顺成, 中道必须≥AA否则foul; Ad该进中(凑中AA 或 A-4轮子顺 A2345 压顶AA),
//	别弃Ace塞Kc(K高压不过AA必foul). value head 偏弃Ace(gap3.4), 奖翻过. 精准命中A-4(无K/Q杂), 不奖A-K.
func RnAceToMidSupportTopAABonus(action *RoundNAction, postState *GameState) float32 {
	topAces, topJokers := 0, 0
	for _, c := range postState.Top {
		if c.IsJoker() {
			topJokers++
		} else if c.Rank() == RankA {
			topAces++
		}
	}
	if !(topAces >= 2 || (topAces >= 1 && topJokers >= 1)) {
		return 0 // 顶非 AA
	}
	aceToMid := false
	for k, c := range action.Kept {
		if action.Placement[k] == RowMiddle && !c.IsJoker() && c.Rank() == RankA {
			aceToMid = true
		}
	}
	if !aceToMid {
		return 0 // 本轮没把 Ace 放中道
	}
	for _, c := range postState.Middle { // 中道除ace外别有≥T高牌杂 (保轮子/低位, 别塞K/Q废kicker)
		if !c.IsJoker() && c.Rank() != RankA && int(c.Rank()) >= int(RankT) {
			return 0
		}
	}
	if partialEvalTP(postState.Middle).Type >= TypePair {
		return 0 // 中道已成对+ → 非 developing 催Ace阶段
	}
	return 4
}

func RnRedundantHighOnLockedAAPenalty(postState, preState *GameState) float32 {
	if preState.Round < 2 || preState.Round > 4 {
		return 0
	}
	// 2026-06-19 (hand67, 用户): 底道(本轮)成三条/顺/花/金刚+ → 高张(A/K/Q)是成手的料, 不 redundant. 整条豁免.
	//   底 X Qd Kh Js + Ad = 鬼T → T-J-Q-K-A broadway顺; 或鬼配高张成三条/金刚, 都非locked-AA多余高张.
	if partialEvalTP(postState.Bottom).Type >= TypeThreeOfAKind {
		return 0
	}
	rA, jt := 0, 0
	for _, c := range preState.Top {
		if c.IsJoker() {
			jt++
		} else if c.Rank() == RankA {
			rA++
		}
	}
	if !(rA >= 2 || (rA >= 1 && jt >= 1)) {
		return 0 // pre-top 非 AA 锁
	}
	var pen float32
	preBotA, postBotA := 0, 0
	for _, c := range preState.Bottom {
		if !c.IsJoker() && c.Rank() == RankA {
			preBotA++
		}
	}
	for _, c := range postState.Bottom {
		if !c.IsJoker() && c.Rank() == RankA {
			postBotA++
		}
	}
	if postBotA > preBotA {
		pen += 5
	}
	botJoker, botPair := false, false
	var bc [13]int
	for _, c := range preState.Bottom {
		if c.IsJoker() {
			botJoker = true
		} else {
			bc[c.Rank()]++
			if bc[c.Rank()] >= 2 {
				botPair = true
			}
		}
	}
	if botJoker && !botPair {
		kq := func(cards []Card) int {
			n := 0
			for _, c := range cards {
				if !c.IsJoker() && (c.Rank() == RankK || c.Rank() == RankQ) {
					n++
				}
			}
			return n
		}
		added := (kq(postState.Top) + kq(postState.Middle)) - (kq(preState.Top) + kq(preState.Middle))
		if added > 0 {
			pen += 12 * float32(added)
		}
	}
	return pen
}

// RnSingleAOnTopBonus 已删 (2026-06-13): case 29 太子自学会 / case 46 过严期望已放宽 / 帮不到手2 鬼+A. 退休.

// RnJokerAOnTopBonus — 本轮鬼+A 上顶锁 AA 范 → +10. 补 NN 对"鬼+A 锁顶范"的系统性低估.
// ⚠️ 这类软规则是针对**当前太子 NN 的具体偏差**校准的 (magnitude/触发都依赖太子的 te).
//
//	换模型 (尤其 sp24 激进版重奖 AA/范, NN 偏好会变) → 整套软硬规则可能需要**不同的配置**:
//	有的冗余(NN 自纠, 如已删的 RnSingleAOnTopBonus)、有的过火、magnitude 要重调.
//	promote 任何新 ckpt 前, 务必把这些规则 on/off + 重测 testcase/实战, 别假设沿用.
//
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
	return 16 // 2026-06-13 +8→+12: ypk-174260554-28 R3 (顶[]+发[X As], 中22底KQJ) NN 偏好摊开 10.3, 用户判该锁 AA → 抬到能翻 (范率优先). 代价: 别的 foul-勉强局也会更倾向锁 AA.
}

// RnPreserveTopAAChaseBonus — top 恰 鬼+1真(QQ/KK, 已是范对)且留 1 空位 + deck 还有 A 或鬼
// (可补上顶升 AA/KKK) → +2. 鼓励"K上头留空位等A 升 AA 范", 别用废 kicker 填满 top 锁死 KK.
// 2026-06-14 (ypk-185336138-22 R2: top=[X] 发 Ks, AI 把 2h 也塞顶填满锁 KK + 弃 5s;
//
//	该 Ks 上头留空, 另一张进中(凑顺draw撑中), 保留催 AA 范潜力). 含义: 保留 > 追 A 范潜力.
func RnPreserveTopAAChaseBonus(postState *GameState) float32 {
	if len(postState.Top) != 2 {
		return 0 // 只奖"鬼+1真"两张(留1空位); 满3张已锁死无空位
	}
	jt, realRank := 0, -1
	for _, c := range postState.Top {
		if c.IsJoker() {
			jt++
		} else {
			realRank = int(c.Rank())
		}
	}
	if jt != 1 || realRank < int(RankQ) || realRank >= int(RankA) {
		return 0 // 必须 鬼+1真 QQ/KK (已成范对, 留空位有意义升 AA; 已是 AA 无可升)
	}
	rankRem, _, jokerRem := computeDeckRemaining(postState)
	if rankRem[RankA] < 1 && jokerRem < 1 {
		return 0 // deck 无 A 也无鬼 → 升不了 AA, 锁现对即可, 不奖留空
	}
	return 2
}

// ============ FoulImminentPenalty (通用, R1-R5) ============
// 2026-05-17: 老 R4FoulImminentPenalty 只覆 R4 mid+bot 满 + top 缺 1.
// 通用化: 任何 partial state 下检测 foul 必然 → +20 penalty.
//
// 通用判定 (相对位置约束 bot ≥ mid ≥ top):
//  1. Mid/bot 都满 (len 5): mid.Type > bot.Type → 100% foul
//  2. Mid/bot 都满: mid 等 type 但 mid 值 > bot 值 → 100% foul
//  3. Top 满 (3 张) + mid 满 (5 张): top.Type > mid.Type → 100% foul
//  4. Top/mid 都满: top 等 type 但值 > mid → 100% foul
//  5. Mid full (high-card) + top fill 1 张 (任何 R5 卡补满) →
//     若 top 现 max rank > mid max rank → 必 foul (覆盖原 R4 case)
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
// sameSuit2SplitsStraight — 行内恰2张同色, 缺中间张(gap=2 的 inside-straight)的那张在别行 → 拆顺.
func sameSuit2SplitsStraight(cs []Card, row Row, p Placement, cards []Card) bool {
	var rs []int
	for _, c := range cs {
		if !c.IsJoker() {
			rs = append(rs, int(c.Rank()))
		}
	}
	if len(rs) != 2 {
		return false
	}
	ra, rb := rs[0], rs[1]
	if ra > rb {
		ra, rb = rb, ra
	}
	if rb-ra != 2 {
		return false // 只管缺中间张(gap=2)的 inside-straight (实战57: 5h3h缺4)
	}
	mid := ra + 1
	for i, c := range cards {
		if !c.IsJoker() && int(c.Rank()) == mid && p[i] != row {
			return true // 中间张在别行 → 这2张同色正在拆3连顺
		}
	}
	return false
}

func R1SameSuitInRowBonus(p Placement, cards []Card) float32 {
	if splitsDealtTrips(p, cards) {
		return 0 // 2026-06-18 别为同花奖拆 dealt 三条 (s99局10)
	}
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
		// 必须全同色 (joker 不计): placedSuits ≤ 1.
		// 2026-06-17 (实战57, 用户"判断顺"): 行内恰2张同色, 若缺中间张(inside-straight)的那张在别行
		//   = 这2张正在拆一个3连顺 → 不奖 (顺比2花值钱; 5h3h缺4d在底=拆345). 相邻无缺张(4d5d实战36)照奖.
		// 2026-06-17 用户"底触发中不触发"(局45): 中道2张同花是弱draw, 占着中道不如建顺/留灵活 → 不奖;
		//   底道2张同花照奖(flush 是底道目标, royalty高). 3+张真同花两行都奖.
		minSuit := 2
		if row == RowMiddle {
			minSuit = 3
		}
		if placedSuits == 1 && maxSuitCount >= minSuit {
			if maxSuitCount == 2 && sameSuit2SplitsStraight(cs, row, p, cards) {
				continue
			}
			bonus += float32(maxSuitCount)
		}
	}
	return bonus
}

// RowPotentialScore — 启发式 行潜力分 (粗略概率 × royalty)
//
//	pair / flush / straight 三类种子 weighted by row royalty
//
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
//
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
//  1. 跨行 split: 低 rank (lower < Rank6) 不罚 — 低连张无 straight 潜力
//     rank diff ≤ 2 dealt 对被 split: d=1 → +5, d=2 → +2
//  2. 每对 (mid_card, bot_card): mid > bot → +3 (违反 bot ≥ mid hierarchy)
//
// 例外: KA 连张 (K-A): 不扣 (fantasy lock 常分行)
// 2026-05-13 加 (跨行 gap=1 only)
// 2026-05-15 扩到 gap≤4 + 加 mid>bot per-pair 罚
// 2026-05-20 sp15: 跳过 lower rank<Rank6 + 罚值减 (8→5/3→2/5→3) — case 15 R1 误罚 3-4 split
// midPairedRanks — 中道哪些真牌 rank 算"成对": ≥2 同rank真牌, 或 1真牌+鬼(鬼配最高真牌).
// 2026-06-22 (用户): 中道成对(含鬼 joker+A=AA)是合理布局, 非被拆散的散连张 → 不算"中>底"威胁,
//
//	CSP / MidPlacedOverBot 对该 rank 豁免不罚 (底可发育追上). 治 4条A手 R1 等.
func midPairedRanks(midCards []Card) map[int]bool {
	cnt := make(map[int]int)
	jokers := 0
	maxReal := -1
	for _, c := range midCards {
		if c.IsJoker() {
			jokers++
			continue
		}
		r := int(c.Rank())
		cnt[r]++
		if r > maxReal {
			maxReal = r
		}
	}
	res := make(map[int]bool)
	for r, n := range cnt {
		if n >= 2 || (jokers >= 1 && r == maxReal) {
			res[r] = true
		}
	}
	return res
}

func ConnectorSplitPenalty(p Placement, cards []Card) float32 {
	if DisabledRules["ConnectorSplit"] {
		return 0
	}
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
	// 2026-06-19 (用户): Part1 连张跨行拆分罚 已删 — 误杀实战102(Kd│89│ThQh), 且96已加K→顶expected.
	// 连张结构信号交给 value-head/L2 features. 保留下方 Part2 (中>底 rank 倒置, foul-risk).
	// 每对 (mid, bot) mid > bot → +3 (违反 bot ≥ mid hierarchy, sp15: 5→3 减重)
	// 注: 这里按 rank 比较 (不是牌型). 对"低对在底 + 高单在中"是真 foul 风险 (case 26), 保留.
	// 2026-06-14: 底道该 rank 已成三条+(count>=3)时跳过 — made set 远强于中道单张, 无 foul 威胁;
	// 按 raw rank 罚会把"set 进底 + 高牌进中"误推成"set 进中" (ypk-9109834-4: 底222 NN TE 33
	// 想放底, 本罚 +9~12 反推到中道 29). 低对(count==2)仍走 case 26 逻辑.
	botRankCnt := make(map[int]int)
	botMax := -1
	for _, br := range botRanks {
		botRankCnt[br]++
		if br > botMax {
			botMax = br
		}
	}
	// 2026-06-17 port v0-dev (实战44): 底道成 3-连张顺(draw)时, 中道高单张威胁不到底顺(整体强,
	//   非botMax高张). 按 raw rank 罚会把"567进底+J进中"正解误罚+9 → 输给 567摆中. 跳过 hierarchy 罚.
	botStraightRun := false
	{
		var bp [13]bool
		for _, br := range botRanks {
			bp[br] = true
		}
		for lo := 0; lo+2 < 13; lo++ {
			if bp[lo] && bp[lo+1] && bp[lo+2] {
				botStraightRun = true
				break
			}
		}
	}
	if botStraightRun {
		return penalty // 底顺 → 不按 raw rank 罚中>底
	}
	var midCards []Card
	for i, c := range cards {
		if p[i] == RowMiddle {
			midCards = append(midCards, c)
		}
	}
	midPaired := midPairedRanks(midCards)
	for _, mr := range midRanks {
		// 2026-06-22 (用户): 中道成对(含鬼 joker+A=AA)的 rank = 合理成对摆放, 非被拆散连张;
		//   该牌"中>底"不算威胁 (底可发育追上). 治 4条A手 R1 (中AA vs 底孤3h 旧误罚6). 中道单张仍罚.
		if midPaired[mr] {
			continue
		}
		// 2026-06-14: mid>bot hierarchy 只在"真威胁"时罚, 不再每张两两比.
		//   (a) 中牌 > 底道最强张(botMax) → 中道可能整体压过底道 (std21: 中9h>底7).
		//   (b) 中牌 > 底道某"成对"的牌 → 威胁 made pair = 真 foul 险 (case26/std26: 中5>底33).
		// 否则底道的"低单张"(既非最强, 又非成对) 不罚 — 否则一张无关低牌凭空造罚, 逼 AI 拆同花
		// 连张 (ypk X9s2s4d5d: 中[4 5] vs 底[9 2], 9压过4/5且2是无关低单 → 0 罚).
		midToppsWhole := mr > botMax
		for _, br := range botRanks {
			if botRankCnt[br] >= 3 {
				continue // 底成三条+: set 远强于中道单张, 与 rank 无关 (gamecase 35 底222)
			}
			paired := botRankCnt[br] >= 2
			if !midToppsWhole && !paired {
				continue // 底道低单张, 无 foul 威胁
			}
			if mr > br {
				penalty += 3
			}
		}
	}
	return penalty
}

// botHasDrawOrPair — bot 是否有 flush/straight/pair (含 joker wild)
//
//	≥3 同色 (potential flush) | ≥3 consecutive (potential straight) | 任意 pair
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
		// "NoSplitDealtPair" DELETED 2026-06-20 (用户): 硬删"拆发牌对子"的候选, 但 value-head 其实
		//   会主动拆对做更强 draw (game2 R1 ypk-91554122-29: 拆77 做底4连顺4-5-6-7, 纯NN排#6, 被这条删掉).
		//   拆对该交给 value-head 判, 不硬禁. 函数保留(向后兼容)但不再 wire.
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

// "KK_OnTop_NoA" DELETED 2026-06-23: 规则冗余 — 裸NN(硬软全关)在"dealt KK+无A+顶无种子"3场景
//   全自己把KK上顶追范, 规则没增量. 而且原版"所有keptK必上顶"在顶槽不够时(顶Ac+鬼仅1空)强制弃1K+
//   另一张进中 → QQQ22葫芦>底顺=爆牌 (prod ypk-100467018-5 R5). 治0个case, 删. 用户拍板.

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
		// "KK_OnTop_NoA" DELETED 2026-06-23: NN自然追KK范, 规则冗余且顶槽不够时致爆牌 (ypk-100467018-5 R5).
		// "KK_OnBot_WithA" DELETED 2026-05-31: 压抑 NN — R2 dealt[KK 8d] NN 想 KK 上 mid (score 30.75), 规则强制 KK 上 bot (score 27.92).
		// "JokerWithA_OnTop" DELETED 2026-05-31: 不看 state.top 已有 A → 强迫 X+A 都上头变 trips foul. case ypk-159252810-11.
		{"TopMustAllowFantasy", rnRuleTopMustAllowFantasy}, // 2026-05-20 sp15: 仅 R2-R3 触发, R4-R5 skip
		// 2026-06-17 用户"保住进范机会": 接上一直没注册的 rnRuleFantasyPossible (用修好的 FantasyLost 三层链).
		//   候选若把"还可能的范"变lost → reject. 已lost的局(如std44 底顺<中花)不管.
		{"FantasyPossible", rnRuleFantasyPossible},
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
