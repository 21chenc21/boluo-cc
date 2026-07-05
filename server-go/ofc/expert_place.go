package ofc

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// placementStr — 把 placement 摆到 state 上, 返回字符串表示 (调试用)
func placementStr(gs *GameState) string {
	t := cardsStr(gs.Top)
	m := cardsStr(gs.Middle)
	b := cardsStr(gs.Bottom)
	return fmt.Sprintf("头[%s] 中[%s] 底[%s]", t, m, b)
}

func cardsStr(cards []Card) string {
	if len(cards) == 0 {
		return ""
	}
	s := ""
	for i, c := range cards {
		if i > 0 {
			s += " "
		}
		s += c.String()
	}
	return s
}

// stateKey — 用于 expertPlace5 候选去重 (top|mid|bot 各自 sort 后 cardId)
func stateKey(gs *GameState) string {
	tids := cardIDs(gs.Top)
	mids := cardIDs(gs.Middle)
	bids := cardIDs(gs.Bottom)
	sort.Strings(tids)
	sort.Strings(mids)
	sort.Strings(bids)
	return joinIDs(tids) + "|" + joinIDs(mids) + "|" + joinIDs(bids)
}

func cardIDs(cards []Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID()
	}
	return out
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

// roundNActionKey — expertPlace3 候选 dedup key
func roundNActionKey(discardCard Card, gs *GameState) string {
	return discardCard.ID() + "|" + stateKey(gs)
}

// ExpertPlace5 — R1 摆 5 张 (修改 state).
//
// 三阶段 MC: 候选 stage1 (TrainedEval 排) → stage2 (rollout 跑) → stage3 (rollout 跑, 选 max).
// 候选量受 cfg.R1Mult 缩放; 鬼自动降 50% mult.
//
// 无 hardcoded filter / anchor boost / simpleEval blend / mono lex sort —
// 全部信号通过 TrainedEval (MLP) 和 rollout 的 fan/foul label-knob 表达.
func (er *ExpertRollout) ExpertPlace5(state *GameState, cards []Card) {
	actions := GenerateRound1Actions(cards, state)
	type cand struct {
		placement Placement
		score     float32
		gs        *GameState
		penalty   float32 // 拆连张扣分 (在 prerank/stage1/2/3 各处一并扣)
		rowPot    float32 // row potential 分 (各 stage 累加)
	}
	var candidates []cand
	seen := make(map[string]bool)
	for _, p := range actions {
		gs := state.Clone()
		for i, c := range cards {
			gs.PlaceCard(c, p[i])
		}
		key := stateKey(gs)
		if seen[key] {
			continue
		}
		seen[key] = true
		// 候选粗排: value (TrainedEval head0) + 可选 policy head3 bias - 连张拆分罚分
		score := TrainedEval(gs)
		if _, _, _, plogit, hasPolicy := TrainedEvalFull(gs); hasPolicy {
			score += PolicyBoost * plogit
		}
		// 净罚 = 总罚 - 加分项 (在 stage1/2/3 都用)
		// 2026-05-17 软化重构: 删 R1TopKWhenJokerAFishPenalty (太硬), 加 4 个 R1 bonus/penalty 替代被删 filter:
		//   - R1JokerOnTopWithAAPenalty -20 (替 r1RuleJokerNotOnTopWithAA)
		//   - R1JokerWithAOnTopBonus +10 (替 r1RuleJokerWithA_OnTop)
		//   - R1SingleAOnTopBonus +10 (替 r1RuleSingleA_OnTop)
		//   - R1FlushGroupOnBotBonus +5 (替 r1RuleFlushGroup_OnBot, 无 TT 例外)
		//   - R1SingleJokerNoAOnTopBonus +5 (2026-06-03: 单鬼无 A 留顶)
		// FoulImminentPenalty 通用到所有 round (R1 这里 + R2-R5 prerank)
		penalty := float32(0)
		radd := func(name string, v float32) {
			if !DisabledRules[name] {
				penalty += v
			}
		}
		radd("ConnectorSplit", ConnectorSplitPenalty(p, cards))
		radd("R1FourInRow", R1FourInRowPenalty(p, cards))
		radd("R1IncoherentRow", R1IncoherentRowPenalty(p, cards))
		radd("R1TopNonAKX", R1TopNonAKXPenalty(p, cards, state))
		radd("R1JokerOnTopWithAA", R1JokerOnTopWithAAPenalty(p, cards))
		radd("R1HighCardBotKicker", R1HighCardShouldBeBotKickerPenalty(p, cards))
		radd("R1LoneKingOnTop", R1LoneKingOnTopPenalty(p, cards))
		radd("MidPlacedOverBot", RnMidPlacedOverBotPlacedPenalty(gs, state))
		radd("Foul", FoulImminentPenalty(gs))
		radd("R1SameSuit", -R1SameSuitInRowBonus(p, cards))
		radd("R1JokerWithAOnTop", -R1JokerWithAOnTopBonus(p, cards))
		radd("R1SingleAOnTop", -R1SingleAOnTopBonus(p, cards))
		radd("R1FlushGroupOnBot", -R1FlushGroupOnBotBonus(p, cards))
		radd("R1BotDraw", -R1BottomDrawBonus(p, cards))
		radd("R1MidOverBotCard", R1MidOverBotCardPenalty(p, cards))
		radd("R1SingleJokerNoAOnTop", -R1SingleJokerNoAOnTopBonus(p, cards))
		radd("R1BigPairOnBot", -R1BigPairOnBotBonus(p, cards))
		if SoftRulesDisabled { // 裸跑: 只留纯 value head
			penalty = 0
		}
		score -= penalty
		candidates = append(candidates, cand{p, score, gs, penalty, 0})
	}

	// === Hard rule filter (打地鼠): 在 ranking 之前 narrow 候选 ===
	if !HardRulesDisabled {
		r1c := make([]R1Cand, len(candidates))
		for i, c := range candidates {
			r1c[i] = R1Cand{Placement: c.placement, GS: c.gs}
		}
		r1c = ApplyHardRulesR1(r1c, cards, state)
		// 重建 candidates (保留 score)
		if len(r1c) < len(candidates) {
			keep := make(map[string]bool, len(r1c))
			for _, c := range r1c {
				keep[stateKey(c.GS)] = true
			}
			filtered := make([]cand, 0, len(r1c))
			for _, c := range candidates {
				if keep[stateKey(c.gs)] {
					filtered = append(filtered, c)
				}
			}
			candidates = filtered
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })

	if MctsDebugTrace {
		fmt.Println("=== R1 Prerank (MLP value head + PolicyBoost) top 12 ===")
		for i := 0; i < 12 && i < len(candidates); i++ {
			c := candidates[i]
			fmt.Printf("  [%d] %s  score=%.4f  (te+pol=%.4f  penalty=%.4f)\n", i+1, placementStr(c.gs), c.score, c.score+c.penalty, c.penalty)
		}
		fmt.Println("  --- rule 拆分 (top 6): CSP/4row/incoh/topNonAKX/jokAA/foul | -bSuit/-bJokA/-bSingleA/-bFlush/-bSingleJok/-bBigPair ---")
		for i := 0; i < 6 && i < len(candidates); i++ {
			c := candidates[i]
			p := c.placement
			fmt.Printf("  [%d] %.1f/%.1f/%.1f/%.1f/%.1f/%.1f | -%.1f/-%.1f/-%.1f/-%.1f/-%.1f/-%.1f\n", i+1,
				ConnectorSplitPenalty(p, cards), R1FourInRowPenalty(p, cards), R1IncoherentRowPenalty(p, cards),
				R1TopNonAKXPenalty(p, cards, state), R1JokerOnTopWithAAPenalty(p, cards), FoulImminentPenalty(c.gs),
				R1SameSuitInRowBonus(p, cards), R1JokerWithAOnTopBonus(p, cards), R1SingleAOnTopBonus(p, cards),
				R1FlushGroupOnBotBonus(p, cards), R1SingleJokerNoAOnTopBonus(p, cards), R1BigPairOnBotBonus(p, cards))
		}
	}

	// === MctsDisabled: 跳过 rollout, 直接 prerank top-1 (纯MLP模式) ===
	if MctsDisabled || er.Cfg.PureMLP {
		if len(candidates) > 0 {
			pick := 0
			// 2026-05-23: per-request er.Cfg.TopKSampleR1 优先, fallback global MctsTopKSample (bench-cases CLI 用)
			topk := er.Cfg.TopKSampleR1
			if topk == 0 {
				topk = MctsTopKSample
			}
			if topk > 1 {
				k := topk
				if k > len(candidates) {
					k = len(candidates)
				}
				pick = er.Rng.Intn(k)
			}
			// 2026-07-05: serve 薄边轻搜索 (只在确定性 top-1 模式介入, 不干扰 topk-sample)
			trig := false
			if ServeSearchMargin > 0 && pick == 0 && len(candidates) >= 2 {
				atomic.AddInt64(&ServeSearchDecCount, 1)
				if candidates[0].score-candidates[1].score < ServeSearchMargin {
					for i := 1; i < len(candidates) && i < ServeSearchTopK; i++ {
						if candidates[0].score-candidates[i].score < ServeSearchMargin &&
							serveSearchConsequential(candidates[0].gs, candidates[i].gs) {
							atomic.AddInt64(&ServeSearchTrigCount, 1)
							trig = true
							break
						}
					}
				}
			}
			if !ServeSearchDryRun && trig && tryAcquireSearchSlot() {
				var sts []*GameState
				for i := 0; i < len(candidates) && i < ServeSearchTopK; i++ {
					if candidates[0].score-candidates[i].score < ServeSearchMargin {
						sts = append(sts, candidates[i].gs)
					}
				}
				pick = er.serveMarginSearchK(sts, 1)
				releaseSearchSlot()
			}
			for i, c := range cards {
				state.PlaceCard(c, candidates[pick].placement[i])
			}
		}
		return
	}

	// stage 大小 — candMult 控候选数 (不动), simsMult 仅放大 sims
	candMult := er.Cfg.R1Mult
	if candMult <= 0 {
		candMult = 1
	}
	hasJoker := false
	for _, c := range cards {
		if c.IsJoker() {
			hasJoker = true
			break
		}
	}
	if hasJoker {
		candMult = candMult * 0.5
		if candMult < 0.15 {
			candMult = 0.15
		}
	}
	simsMult := candMult * MctsSimsMult
	// 2026-05-23: env MCTS_STAGE_MIN override lower bound (测 top-N candidates 加 MCTS 效果)
	s1Min, s2Min, s3Min := 5, 3, 2
	if MctsStage1Min > 0 {
		s1Min = MctsStage1Min
	}
	if MctsStage2Min > 0 {
		s2Min = MctsStage2Min
	}
	if MctsStage3Min > 0 {
		s3Min = MctsStage3Min
	}
	s1c := maxInt(s1Min, int(roundFloat(30*candMult)))
	s1n := maxInt(10, int(roundFloat(30*simsMult)))
	s2c := maxInt(s2Min, int(roundFloat(8*candMult)))
	s2n := maxInt(20, int(roundFloat(60*simsMult)))
	s3c := maxInt(s3Min, int(roundFloat(3*candMult)))
	s3n := maxInt(40, int(roundFloat(150*simsMult)))

	type stageItem struct {
		placement Placement
		gs        *GameState
		avg       float32
		penalty   float32
		rowPot    float32
	}

	// stage 1: 取 top-s1c 候选 (按 TrainedEval), 每个跑 s1n rollout, 按 (prerank, rollout_mean) blend 排
	// 各 stage 一并扣 ConnectorSplitPenalty.
	stage1N := minInt(len(candidates), s1c)
	stage1 := make([]stageItem, 0, stage1N)
	for i := 0; i < stage1N; i++ {
		c := candidates[i]
		var avg float32
		if MctsPrerankW >= 1.0 {
			avg = c.score
		} else {
			var total float32
			for s := 0; s < s1n; s++ {
				total += er.QuickRollout(c.gs, 1)
			}
			rolloutMean := total / float32(s1n)
			if MctsPrerankW <= 0 {
				avg = rolloutMean - c.penalty
			} else {
				avg = MctsPrerankW*c.score + (1-MctsPrerankW)*(rolloutMean-c.penalty)
			}
		}
		stage1 = append(stage1, stageItem{c.placement, c.gs, avg, c.penalty, 0})
	}
	sort.SliceStable(stage1, func(i, j int) bool { return stage1[i].avg > stage1[j].avg })

	if MctsDebugTrace {
		fmt.Printf("=== R1 Stage 1 (sims=%d, %d→%d 候选, by rollout mean) top 5 ===\n", s1n, len(candidates), len(stage1))
		for i := 0; i < 5 && i < len(stage1); i++ {
			fmt.Printf("  [%d] %s  rollout_mean=%.4f\n", i+1, placementStr(stage1[i].gs), stage1[i].avg)
		}
	}

	// stage 2
	stage2N := minInt(len(stage1), s2c)
	stage2 := make([]stageItem, 0, stage2N)
	for i := 0; i < stage2N; i++ {
		it := stage1[i]
		var total float32
		for s := 0; s < s2n; s++ {
			total += er.QuickRollout(it.gs, 1)
		}
		stage2 = append(stage2, stageItem{it.placement, it.gs, total/float32(s2n) - it.penalty, it.penalty, 0})
	}
	sort.SliceStable(stage2, func(i, j int) bool { return stage2[i].avg > stage2[j].avg })

	if MctsDebugTrace {
		fmt.Printf("=== R1 Stage 2 (sims=%d, %d 候选, by rollout mean) ===\n", s2n, len(stage2))
		for i := 0; i < len(stage2); i++ {
			fmt.Printf("  [%d] %s  rollout_mean=%.4f\n", i+1, placementStr(stage2[i].gs), stage2[i].avg)
		}
	}

	// stage 3: 决策按 rollout mean
	stage3N := minInt(len(stage2), s3c)
	bestAvg := float32(-1e30)
	var bestPlacement Placement
	haveBest := false
	type s3Item struct {
		placement Placement
		gs        *GameState
		avg       float32
	}
	stage3 := make([]s3Item, 0, stage3N)
	for i := 0; i < stage3N; i++ {
		it := stage2[i]
		var sumScore float32
		for s := 0; s < s3n; s++ {
			sumScore += er.QuickRollout(it.gs, 1)
		}
		avg := sumScore/float32(s3n) - it.penalty
		stage3 = append(stage3, s3Item{it.placement, it.gs, avg})
		if !haveBest || avg > bestAvg {
			bestAvg = avg
			bestPlacement = it.placement
			haveBest = true
		}
	}

	if MctsDebugTrace {
		// stage3 排序后打印 + 最终选择
		sort.SliceStable(stage3, func(i, j int) bool { return stage3[i].avg > stage3[j].avg })
		fmt.Printf("=== R1 Stage 3 (sims=%d, %d 候选, max 选最终) ===\n", s3n, len(stage3))
		for i := 0; i < len(stage3); i++ {
			marker := "  "
			if stage3[i].avg == bestAvg {
				marker = "★ "
			}
			fmt.Printf("%s[%d] %s  rollout_mean=%.4f\n", marker, i+1, placementStr(stage3[i].gs), stage3[i].avg)
		}
	}

	// R1 fantasy-lost post-filter: 按 avg 排序后跳过 fantasy-lost 候选
	if !FantasyLost(state) {
		sort.SliceStable(stage3, func(i, j int) bool { return stage3[i].avg > stage3[j].avg })
		for _, it := range stage3 {
			if !FantasyLost(it.gs) {
				bestPlacement = it.placement
				haveBest = true
				break
			}
		}
		// 全 lost → 保留原 bestPlacement (max avg)
	}

	// fallback
	if !haveBest && len(candidates) > 0 {
		bestPlacement = candidates[0].placement
		haveBest = true
	}

	if haveBest {
		for i, c := range cards {
			state.PlaceCard(c, bestPlacement[i])
		}
	}
}

// ExpertPlace3 — R2-5 弃 1 摆 2 (修改 state, 与 JS expertPlace3 同语义)
// bottomDomScore — 底道当前成手的可比强度 (joker-aware, 仅 made hand). tier*100 + 主rank.
// 用于"同顶中底大者赢"支配过滤 (用户 2026-06-14, 绕开 value-head 对强成手低估 89X).
func bottomDomScore(row []Card) int {
	var cnt [13]int
	j := 0
	for _, c := range row {
		if c.IsJoker() {
			j++
		} else {
			cnt[c.Rank()]++
		}
	}
	pairs, trips, quads, maxc := 0, 0, 0, 0
	for _, n := range cnt {
		if n > maxc {
			maxc = n
		}
		if n == 2 {
			pairs++
		}
		if n == 3 {
			trips++
		}
		if n >= 4 {
			quads++
		}
	}
	eff := maxc + j // joker 补最大堆
	tier := 0       // 高牌/draw
	switch {
	case eff >= 4 || quads > 0:
		tier = 7 // 金刚
	// 2026-06-18 port v0-dev 修 (用户发现 ypk-129630538-16 手3 R5: straight-blind domScore 把"凑顺底"误删→逼犯规).
	case (trips > 0 && pairs > 0) || (pairs >= 2 && j >= 1):
		tier = 6 // 葫芦 (真三条+对, 或 2对+鬼)
	case eff >= 3:
		tier = 3 // 三条
	case pairs >= 2:
		tier = 2 // 两对
	case pairs >= 1 || (j >= 1 && maxc >= 1):
		tier = 1 // 一对 (含鬼+单)
	}
	// 2026-06-18 port v0-dev: 满5张行检测顺/花/SF (count-based 漏鬼凑的顺 → 误删凑顺底. 顺4/花5/SF8, 取max)
	if len(row) == 5 {
		if t := int(Evaluate5JokerCap(row, nil).Type); t > tier {
			tier = t
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
	return tier*100 + prim + 1
}

// 支配软罚参数 (用户 2026-06-18: dominance 改软加分, 不强过滤). sibling-relative, 小幅 tie-break.
const (
	domSoftScale = 2.0 // dom 分差 → 罚分 斜率
	domSoftCap   = 3.0 // 单候选最大软罚 (防过火破坏 value-head / draw)
)

// domSoftPen — dom 分差(gap>0) 映射到软罚分, 线性封顶.
func domSoftPen(gap float32) float32 {
	p := float32(domSoftScale) * gap
	if p > domSoftCap {
		p = domSoftCap
	}
	return p
}

// rowHasJoker — 行内是否含鬼 (含鬼=wild发育, partial board 支配软罚要豁免, 防手4类发育底被误罚).
func rowHasJoker(row []Card) bool {
	for _, c := range row {
		if c.IsJoker() {
			return true
		}
	}
	return false
}

// domScoreK — kicker-aware 成手支配分 (bottomDomScore + 高kicker小数). 仅 made-pair+(>=100).
// 用于支配软罚 tie-break: 区分 KKQ vs KK3 (同对不同 kicker, bottomDomScore 看不出).
func domScoreK(row []Card) float32 {
	base := bottomDomScore(row)
	if base < 100 {
		return float32(base)
	}
	var cnt [13]int
	for _, c := range row {
		if !c.IsJoker() {
			cnt[c.Rank()]++
		}
	}
	pairRank := -1
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 2 {
			pairRank = r
			break
		}
	}
	kick := -1
	for r := 12; r >= 0; r-- {
		if cnt[r] >= 1 && r != pairRank {
			kick = r
			break
		}
	}
	return float32(base) + 0.1*float32(kick+1)
}

// topMidKey — 顶+中 placement 的归一化 key (排序, 同集合同 key).
func topMidKey(gs *GameState) string {
	t := append([]string(nil), cardIDs(gs.Top)...)
	m := append([]string(nil), cardIDs(gs.Middle)...)
	sort.Strings(t)
	sort.Strings(m)
	return joinIDs(t) + "|" + joinIDs(m)
}

// topMidMadeKey — 顶exact + 中道成手cmp(type*16+rank, 忽略kicker). 底道支配过滤用:
//
//	同顶+同中成手(如都55对, 仅kicker差) → 比底道成手强弱. 治 hand67: Ad该进底成broadway顺,
//	不是进中当55的死kicker (底顺413 严格支配 底KKK312, 但旧 exact-middle key 因kicker(A/2/K)不同没分组).
func topMidMadeKey(gs *GameState) string {
	t := append([]string(nil), cardIDs(gs.Top)...)
	sort.Strings(t)
	return joinIDs(t) + fmt.Sprintf("|M%d", madeHandCmp(gs.Middle))
}

// topBotKey — 顶+底 placement 的归一化 key (mirror topMidKey, 中道支配过滤用).
func topBotKey(gs *GameState) string {
	t := append([]string(nil), cardIDs(gs.Top)...)
	b := append([]string(nil), cardIDs(gs.Bottom)...)
	sort.Strings(t)
	sort.Strings(b)
	return joinIDs(t) + "|" + joinIDs(b)
}

// rnSoftScore — R2-R5 软规则对 base value-head 的净有符号调整 (= teScore - base).
// trace=true 同时返回各非零项明细 (OFC_DEBUG_TRACE 撸规则用). ⚠️ 增删软规则必同步这里.
func rnSoftScore(action *RoundNAction, dealt []Card, gs, state *GameState, trace bool) (float32, string) {
	if SoftRulesDisabled {
		return 0, "纯NN(软规则关)"
	}
	var adj float32
	var sb string
	add := func(name string, v float32) {
		if DisabledRules[name] {
			return
		}
		adj += v
		if trace && v != 0 {
			sb += fmt.Sprintf(" %s%+.1f", name, v)
		}
	}
	add("Foul", -FoulImminentPenalty(gs))
	add("JokerSameRow", -RnJokersSameRowPenalty(action, gs))
	add("SingleJokerTopA", RnSingleJokerTopChaseABonus(gs, state))
	add("LoneAceMidJokerTop", -RnLoneAceMidJokerTopPenalty(gs, state))
	add("TopTripsFan", RnTopTripsFantasyBonus(gs))
	add("TopTripsOver", -RnTopTripsOvercommitPenalty(gs, state))
	add("JokerAOnTop", RnJokerAOnTopBonus(action, gs))
	add("QuadsJokerWaste", -RnQuadsJokerWastePenalty(gs))
	add("MidExceedsBot", -RnMidExceedsBotPenalty(gs, state))
	add("HighCardWrongRow", -RnHighCardWrongRowPenalty(gs, state))
	add("MidPairTwoPair", RnMidPairCompletesTwoPairBonus(gs, state))
	add("MidDrawFace", RnMidDrawFaceGated(dealt, gs))
	add("BotDrawFace", rowDrawFaceBonus(gs.Bottom))
	add("MidTwoPairBotDraw", RnMidTwoPairBotDrawBonus(gs))
	add("JokerHighSeedTop", RnJokerHighSeedOnTopBonus(action, gs))
	add("AceToMidVsTopAA", RnAceToMidSupportTopAABonus(action, gs))
	// "BotMakeTwoPair" DELETED 2026-06-23 (用户) — 见 hard_rules.go. 底凑两对+/金刚分级过火压掉鬼→顶范.
	add("MidMakeTwoPair", RnMidMakeTwoPairBonus(gs, state))
	add("PreserveTopAA", RnPreserveTopAAChaseBonus(gs))
	add("MidHighOverBot", -RnMidHighCardOverBotPenalty(gs, state))
	add("MidPlacedOverBot", -RnMidPlacedOverBotPlacedPenalty(gs, state))
	add("LoneSubQTop", -RnLoneSubQOnTopPenalty(gs, state))
	add("RedundantHighLockedAA", -RnRedundantHighOnLockedAAPenalty(gs, state))
	add("DeadLowKickerFanTop", -RnDeadLowKickerOnFanTopPenalty(gs, state))
	add("R4TripsFanReach", RnR4TripsFantasyReachableBonus(gs, state))
	add("AceToTopSeed", RnAceToTopSeedBonus(gs, state))
	add("R2BotPairMidDraw", R2BotPairMidDrawBonus(gs, state))
	add("MidKickerBotFlush", -RnMidKickerShouldBotFlushPenalty(action, gs, state))
	add("TopPairOvercommit", -RnTopPairOvercommitPenalty(gs, state))
	if trace && sb == "" {
		sb = " 无"
	}
	return adj, sb
}

func (er *ExpertRollout) ExpertPlace3(state *GameState, cards []Card) {
	actions := GenerateRoundNActions(cards, state)

	type cand struct {
		action  *RoundNAction
		gs      *GameState
		teScore float32
		domAdj  float32 // 支配软罚 (sibling-relative, 在 dom 块算, ranking 时并入 teScore)
	}

	// dedup
	seen := make(map[string]bool)
	uniq := make([]cand, 0, len(actions))
	for i := range actions {
		a := &actions[i]
		gs := state.Clone()
		gs.UsedCards[cards[a.DiscardIdx].ID()] = true
		gs.SetDiscard(cards[a.DiscardIdx]) // V3 N/N2 features
		for k, c := range a.Kept {
			gs.PlaceCard(c, a.Placement[k])
		}
		key := roundNActionKey(cards[a.DiscardIdx], gs)
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, cand{a, gs, 0, 0})
	}

	// === R3-R5 无进范可能 → 全听 NN (跳所有软/硬规则, 用户 2026-06-19/20) ===
	// 无候选能保住 foul-free 范 (各候选 post 都 FantasyLost 或 只能靠 foul 成范) → 规则没意义, 纯 value-head top-1.
	// (A.csv局6 R4: 顶[Ad]单A唯一范=AA, 中只能QQ<AA → 全候选假范 → 听NN选 22→中避foul.)
	// (D-nndiff局94 R3: 顶[4c]弱, 任何摆法范都靠 foul (中→底链撑不住) → 听NN选 8h→底凑红桃花draw.)
	pureNN := false
	if state.Round >= 3 && !HardRulesDisabled {
		anyFan := false
		for i := range uniq {
			if !FantasyLost(uniq[i].gs) && !fantasyOnlyViaFoul(uniq[i].gs) {
				anyFan = true
				break
			}
		}
		pureNN = !anyFan
	}

	// === Hard rule filter (打地鼠): 在 ranking 之前 narrow 候选 ===
	if !HardRulesDisabled && !pureNN {
		rnc := make([]RNCand, len(uniq))
		for i, c := range uniq {
			rnc[i] = RNCand{Action: c.action, GS: c.gs}
		}
		rnc = ApplyHardRulesRN(rnc, cards, state)
		if len(rnc) < len(uniq) {
			keep := make(map[string]bool, len(rnc))
			for _, c := range rnc {
				keep[roundNActionKey(cards[c.Action.DiscardIdx], c.GS)] = true
			}
			filtered := make([]cand, 0, len(rnc))
			for _, c := range uniq {
				if keep[roundNActionKey(cards[c.action.DiscardIdx], c.gs)] {
					filtered = append(filtered, c)
				}
			}
			uniq = filtered
		}
	}

	// === 支配过滤 (用户 2026-06-14): 同顶+同中 → 底道成手严格大者支配, 删底小的. ===
	// 绕开 value-head 对强成手低估 (89X: 同顶中, 888三条 严格 > 88-99两对 → 删两对那个).
	// 仅 made-hand 比较; 裸跑 bench 看是否误删 draw.
	if !HardRulesDisabled && !pureNN && len(uniq) > 1 {
		best := make(map[string]int, len(uniq))
		for _, c := range uniq {
			k := topMidMadeKey(c.gs)
			if sc := bottomDomScore(c.gs.Bottom); sc > best[k] {
				best[k] = sc
			}
		}
		kept := make([]cand, 0, len(uniq))
		for _, c := range uniq {
			sc := bottomDomScore(c.gs.Bottom)
			// draw 守护: 只删 made-pair+ 的弱底 (>=100); 高牌型底(顺/花 draw)永不删.
			// 2026-06-18 (手4 R3 ypk-129761610-3): 只在底道满5张(成手定型)才比 — 早轮底道3张就比会
			//   误删"当前对子小但有发育"的底 (底[Qs X 9d]QQ被底[Qs X Ah]AA支配删, 但9d会配9s成99/999).
			// 2026-06-19 (hand67): key 改 topMidMadeKey (中道按成手分组忽略kicker), 让 55+Ad/55+2c 比底道.
			if sc >= 100 && sc < best[topMidMadeKey(c.gs)] && len(c.gs.Bottom) >= 5 {
				continue // 被同顶+同中成手更强底道严格支配
			}
			kept = append(kept, c)
		}
		uniq = kept
	}

	// === 底道支配软罚 (partial board, 用户 2026-06-18: 不用强过滤, 改软加分, 治局16/17) ===
	// 同顶+同中, 底道成手更强(kicker-aware)者支配; 弱者按 dom 差距小幅软罚(≤DomSoftCap), 不删.
	// 局17 R2 底QQ>JJ(NN漏判0.66) / 局16 R3 底KKQ>KK3(kicker, NN偏0.32). sibling-relative→无全局失真.
	// ⚠️ 小心draw: 花draw底(hasNFlushDraw≥3)豁免不罚; 满5张已由上面硬过滤管, 这里只 <5 张.
	if !HardRulesDisabled && !pureNN && len(uniq) > 1 {
		best := make(map[string]float32, len(uniq))
		for i := range uniq {
			b := uniq[i].gs.Bottom
			if len(b) >= 5 || len(b) < 2 || bottomDomScore(b) < 100 || hasNFlushDraw(b, 3) || rowHasJoker(b) {
				continue
			}
			if k := topMidKey(uniq[i].gs); domScoreK(b) > best[k] {
				best[k] = domScoreK(b)
			}
		}
		for i := range uniq {
			b := uniq[i].gs.Bottom
			if len(b) >= 5 || len(b) < 2 || bottomDomScore(b) < 100 || hasNFlushDraw(b, 3) || rowHasJoker(b) {
				continue
			}
			if s := domScoreK(b); s < best[topMidKey(uniq[i].gs)] {
				uniq[i].domAdj -= domSoftPen(best[topMidKey(uniq[i].gs)] - s)
			}
		}
	}

	// === 中道支配软罚 (mirror 底道, 用户 2026-06-18 局75 R5: 硬过滤改软加分) ===
	// 同顶+同底, 中道成手更强且**非foul(中≤底)**者支配; 弱者软罚. 局75 中555>333(NN噪声偏333 1.8, 同13范).
	// 只在整盘定型(中满5+底满5)比 — 弃牌无未来纯结构 tie-break; 花draw中豁免.
	// 中道软罚只在整盘定型(中满5+底满5)比 → 行已成手, 不查 flush draw (hasNFlushDraw 会把含鬼成手误判成花draw).
	midElig := func(c *cand) bool {
		m := c.gs.Middle
		if len(m) < 5 || len(c.gs.Bottom) < 5 || bottomDomScore(m) < 100 {
			return false
		}
		return !IsFoulJoker(c.gs.Top, m, c.gs.Bottom) // foul 中道不当支配者
	}
	if !HardRulesDisabled && !pureNN && len(uniq) > 1 {
		best := make(map[string]float32, len(uniq))
		for i := range uniq {
			if !midElig(&uniq[i]) {
				continue
			}
			if k := topBotKey(uniq[i].gs); domScoreK(uniq[i].gs.Middle) > best[k] {
				best[k] = domScoreK(uniq[i].gs.Middle)
			}
		}
		for i := range uniq {
			if !midElig(&uniq[i]) {
				continue
			}
			if s := domScoreK(uniq[i].gs.Middle); s < best[topBotKey(uniq[i].gs)] {
				uniq[i].domAdj -= domSoftPen(best[topBotKey(uniq[i].gs)] - s)
			}
		}
	}

	// stage1 ranking: value (head0) + 可选 policy (head3) bias - foul-imminent penalty + 支配软罚
	for i := range uniq {
		item := &uniq[i]
		item.teScore = TrainedEval(item.gs)
		if _, _, _, plogit, hasPolicy := TrainedEvalFull(item.gs); hasPolicy {
			item.teScore += PolicyBoost * plogit
		}
		if !pureNN { // R4/R5 无进范可能 → 跳软规则, 纯 value-head
			adj, _ := rnSoftScore(item.action, cards, item.gs, state, false)
			item.teScore += adj + item.domAdj
		}
	}

	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].teScore > uniq[j].teScore })

	if MctsDebugTrace {
		if pureNN {
			fmt.Println("=== RN Prerank: R3-R5 无进范可能 → 全听NN (软/硬规则全跳) ===")
		}
		fmt.Println("=== RN Prerank (value head base + 软规则明细) top 8 ===")
		for i := 0; i < 8 && i < len(uniq); i++ {
			c := uniq[i]
			discCard := cards[c.action.DiscardIdx].String()
			base := TrainedEval(c.gs)
			if _, _, _, plogit, hasPolicy := TrainedEvalFull(c.gs); hasPolicy {
				base += PolicyBoost * plogit
			}
			_, rules := rnSoftScore(c.action, cards, c.gs, state, true)
			fmt.Printf("  [%d] %s 弃 %s  score=%.4f  (base=%.4f 规则:%s)\n", i+1, placementStr(c.gs), discCard, c.teScore, base, rules)
		}
	}

	// === MctsDisabled: R2-R5 跳过 rollout, 直接 prerank top-1 (纯MLP模式) ===
	// 2026-05-23: MctsTopKSampleRN 控制 R2-R5 sample (默认 0 = top-1 deterministic 保 endgame).
	if MctsDisabled || er.Cfg.PureMLP {
		if len(uniq) > 0 {
			pick := 0
			if MctsTopKSampleRN > 1 {
				k := MctsTopKSampleRN
				if k > len(uniq) {
					k = len(uniq)
				}
				pick = er.Rng.Intn(k)
			}
			// 2026-07-05: serve 薄边轻搜索 (同 R1)
			trigN := false
			if ServeSearchMargin > 0 && pick == 0 && len(uniq) >= 2 {
				atomic.AddInt64(&ServeSearchDecCount, 1)
				if uniq[0].teScore-uniq[1].teScore < ServeSearchMargin {
					for i := 1; i < len(uniq) && i < ServeSearchTopK; i++ {
						if uniq[0].teScore-uniq[i].teScore < ServeSearchMargin &&
							serveSearchConsequential(uniq[0].gs, uniq[i].gs) {
							atomic.AddInt64(&ServeSearchTrigCount, 1)
							trigN = true
							break
						}
					}
				}
			}
			if !ServeSearchDryRun && trigN && tryAcquireSearchSlot() {
				var sts []*GameState
				for i := 0; i < len(uniq) && i < ServeSearchTopK; i++ {
					if uniq[0].teScore-uniq[i].teScore < ServeSearchMargin {
						sts = append(sts, uniq[i].gs)
					}
				}
				pick = er.serveMarginSearchK(sts, state.Round)
				releaseSearchSlot()
			}
			action := uniq[pick].action
			state.UsedCards[cards[action.DiscardIdx].ID()] = true
			state.SetDiscard(cards[action.DiscardIdx]) // V3 features
			for k, c := range action.Kept {
				state.PlaceCard(c, action.Placement[k])
			}
		}
		return
	}

	candMult3 := er.Cfg.R1Mult
	if candMult3 <= 0 {
		candMult3 = 1
	}
	simsMult3 := candMult3 * MctsSimsMult
	S1c3 := maxInt(5, int(roundFloat(15*candMult3)))
	S1n3 := maxInt(15, int(roundFloat(50*simsMult3)))
	S2c3 := maxInt(2, int(roundFloat(5*candMult3)))
	S2n3 := maxInt(60, int(roundFloat(300*simsMult3)))

	type stageItem struct {
		action *RoundNAction
		gs     *GameState
		avg    float32
	}

	stage1N := minInt(len(uniq), S1c3)
	stage1 := make([]stageItem, 0, stage1N)
	for i := 0; i < stage1N; i++ {
		it := &uniq[i]
		if it.gs.IsComplete() {
			score := it.gs.Score()
			fb := float32(0)
			if score.Fantasy && !score.Foul {
				te := score.TopEval
				if te.Type == TypeThreeOfAKind {
					fb = er.Cfg.TripsFanBonus
				} else if te.Type == TypePair {
					pr := int((te.Value - 1000000) / 15)
					if pr >= 12 {
						fb = er.Cfg.AAFanBonus
					} else if pr >= 11 {
						fb = er.Cfg.KKFanBonus
					} else {
						fb = er.Cfg.QQFanBonus
					}
				}
			}
			avg := float32(score.Royalties) + fb
			if score.Foul {
				avg = -er.Cfg.FoulCost
			}
			stage1 = append(stage1, stageItem{it.action, it.gs, avg})
			continue
		}
		round := state.Round
		if round == 0 {
			round = 3
		}
		var avg float32
		if MctsPrerankW >= 1.0 {
			avg = it.teScore
		} else {
			var total float32
			for r := 0; r < S1n3; r++ {
				total += er.QuickRollout(it.gs, round)
			}
			rolloutMean := total / float32(S1n3)
			if MctsPrerankW <= 0 {
				avg = rolloutMean
			} else {
				avg = MctsPrerankW*it.teScore + (1-MctsPrerankW)*rolloutMean
			}
		}
		stage1 = append(stage1, stageItem{it.action, it.gs, avg})
	}
	sort.SliceStable(stage1, func(i, j int) bool { return stage1[i].avg > stage1[j].avg })

	if MctsDebugTrace {
		fmt.Printf("=== RN Stage 1 (sims=%d, %d→%d 候选, by rollout mean) top 5 ===\n", S1n3, len(uniq), len(stage1))
		for i := 0; i < 5 && i < len(stage1); i++ {
			discCard := cards[stage1[i].action.DiscardIdx].String()
			fmt.Printf("  [%d] %s 弃 %s  rollout_mean=%.4f\n", i+1, placementStr(stage1[i].gs), discCard, stage1[i].avg)
		}
	}

	bestScore := float32(-1e30)
	var bestAction *RoundNAction
	haveBest := false

	type s2Item struct {
		action *RoundNAction
		gs     *GameState
		avg    float32
	}
	stage2N := minInt(len(stage1), S2c3)
	stage2dbg := make([]s2Item, 0, stage2N)
	for i := 0; i < stage2N; i++ {
		it := stage1[i]
		if it.gs.IsComplete() {
			stage2dbg = append(stage2dbg, s2Item{it.action, it.gs, it.avg})
			if !haveBest || it.avg > bestScore {
				bestScore = it.avg
				bestAction = it.action
				haveBest = true
			}
			continue
		}
		var total float32
		round := state.Round
		if round == 0 {
			round = 3
		}
		for r := 0; r < S2n3; r++ {
			total += er.QuickRollout(it.gs, round)
		}
		avg := total / float32(S2n3)
		stage2dbg = append(stage2dbg, s2Item{it.action, it.gs, avg})
		if !haveBest || avg > bestScore {
			bestScore = avg
			bestAction = it.action
			haveBest = true
		}
	}

	if MctsDebugTrace {
		sort.SliceStable(stage2dbg, func(i, j int) bool { return stage2dbg[i].avg > stage2dbg[j].avg })
		fmt.Printf("=== RN Stage 2 (sims=%d, %d 候选, max 选最终) ===\n", S2n3, len(stage2dbg))
		for i := 0; i < len(stage2dbg); i++ {
			marker := "  "
			if stage2dbg[i].avg == bestScore {
				marker = "★ "
			}
			discCard := cards[stage2dbg[i].action.DiscardIdx].String()
			fmt.Printf("%s[%d] %s 弃 %s  rollout_mean=%.4f\n", marker, i+1, placementStr(stage2dbg[i].gs), discCard, stage2dbg[i].avg)
		}
	}

	// R2+ fantasy-lost post-filter: 按 Q 排序后跳过 fantasy-lost 候选
	if state.Round >= 2 && !FantasyLost(state) {
		sort.SliceStable(stage2dbg, func(i, j int) bool { return stage2dbg[i].avg > stage2dbg[j].avg })
		for _, it := range stage2dbg {
			if !FantasyLost(it.gs) {
				bestAction = it.action
				haveBest = true
				break
			}
		}
		// 全 lost → 保留原 bestAction (max avg)
	}

	if !haveBest && len(uniq) > 0 {
		bestAction = uniq[0].action
		haveBest = true
	}
	if haveBest {
		state.UsedCards[cards[bestAction.DiscardIdx].ID()] = true
		state.SetDiscard(cards[bestAction.DiscardIdx]) // V3 features
		for k, c := range bestAction.Kept {
			state.PlaceCard(c, bestAction.Placement[k])
		}
	}
}

func roundFloat(f float32) float32 {
	if f >= 0 {
		return float32(int(f + 0.5))
	}
	return float32(int(f - 0.5))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ServeSearchMargin/Cap — serve 端薄边轻搜索 (2026-07-05 用户拍板, 预算 200-400ms).
// PureMLP 下, 与 top-1 teScore 差 < margin 的候选(含自身, ≤3个)交替加摸, 批间判停. 0=关.
// 治"陌生板外推失准"(人摆的板/规则改道的板, std63 类) — 与 gen margin 升级同思想的 serve 预算版.
var (
	ServeSearchMargin float32
	ServeSearchCap    = 120 // 每候选 sims 上限
	ServeSearchBatch  = 40  // 每批 sims
	ServeSearchTopK   = 3
	// 2026-07-05 (用户"30并发能扛住吗"): 全局并发闸 — 同时最多 N 个搜索在跑,
	// 抢不到坑位直接退回纯NN top-1 (搜索是增强不是依赖, 优雅降级谁都不等).
	// 单搜索 worker 也封顶, 防一个请求吃满所有核把 5ms 纯NN请求堵在调度队列.
	serveSearchSlots   = make(chan struct{}, 1) // 并发搜索上限 (4核prod默认1, SetServeSearchSlots 可调)
	ServeSearchWorkers = 3                      // 单搜索 worker 上限 (留1核伺候纯NN流量)
)

// SetServeSearchSlots — 部署时按机器核数调并发搜索槽位.
func SetServeSearchSlots(n int) {
	if n < 1 {
		n = 1
	}
	serveSearchSlots = make(chan struct{}, n)
}

// serveSearchCapForRound — 按 round 缩 sims 预算: R2 rollout 深(~15ms), 不缩 4核会 1.8s 爆预算;
// R4/R5 浅(~5ms) 给满. 4核+3worker 口径: R2≈40×2×15/3≈400ms, R4+≈120×2×5/3≈400ms.
func serveSearchCapForRound(round int) int {
	switch {
	case round <= 2:
		return ServeSearchCap / 3
	case round == 3:
		return ServeSearchCap / 2
	default:
		return ServeSearchCap
	}
}

// ServeSearchWait — 抢不到坑位时最多排队等这么久 (2026-07-05 用户"其他触发者不就摆错吗"):
// 薄边手宁可多等 ~0.8s 也要搜 — 排队不烧CPU, 只有超时才降级纯NN. 0=立即降级.
var ServeSearchWait = 800 * time.Millisecond

// ServeSearchDryRun/计数器 — 触发率测量: 只数不搜 (2026-07-05 用户"怕把把都要搜索").
var (
	ServeSearchDryRun    bool
	ServeSearchDecCount  int64 // 决策总数 (margin>0 时)
	ServeSearchTrigCount int64 // 触发数
)

// serveSearchConsequential — 触发第二判据 (2026-07-05 用户"怕把把都搜"实锤: 纯分差触发率52%!).
// 薄边里大量"真平局"(花色互换等)搜了白搜 — 只有 top-2 在 foul风险/范EV 上真分歧才值得搜.
func serveSearchConsequential(a, b *GameState) bool {
	fa, fb := BuildFeaturesV3(a), BuildFeaturesV3(b)
	df := fa[89] - fb[89] // pFoulFinal
	if df < 0 {
		df = -df
	}
	if df > 0.15 {
		return true
	}
	de := fa[168] - fb[168] // FE 范EV
	if de < 0 {
		de = -de
	}
	return de > 0.08
}

// tryAcquireSearchSlot — 先非阻塞抢, 抢不到排队至多 ServeSearchWait. 超时 → 调用方退回纯NN.
func tryAcquireSearchSlot() bool {
	select {
	case serveSearchSlots <- struct{}{}:
		return true
	default:
	}
	if ServeSearchWait <= 0 {
		return false
	}
	t := time.NewTimer(ServeSearchWait)
	defer t.Stop()
	select {
	case serveSearchSlots <- struct{}{}:
		return true
	case <-t.C:
		return false
	}
}
func releaseSearchSlot() { <-serveSearchSlots }

// serveMarginSearchK — K 个 post-state 并行加摸 (worker=NumCPU, 每 worker 独立 ExpertRollout/Rng);
// 每批后若 leader 领先第二名 > 2·SE合 提前停 (真平局烧满也没损失). 返回赢家下标.
func (er *ExpertRollout) serveMarginSearchK(states []*GameState, round int) int {
	K := len(states)
	W := runtime.NumCPU()
	if W > ServeSearchWorkers {
		W = ServeSearchWorkers
	}
	var mu sync.Mutex
	sum := make([]float64, K)
	sumsq := make([]float64, K)
	cnt := make([]int, K)

	runBatch := func(per int) {
		type job struct{ ci int }
		jobs := make(chan job, K*per)
		for ci := 0; ci < K; ci++ {
			for k := 0; k < per; k++ {
				jobs <- job{ci}
			}
		}
		close(jobs)
		var wg sync.WaitGroup
		for w := 0; w < W; w++ {
			wg.Add(1)
			// er.Rng 是 IntnRNG 接口 (无 Int63) — 用 Intn 拼 worker seed
			seed := int64(er.Rng.Intn(1<<30))<<31 | int64(er.Rng.Intn(1<<30))
			go func(seed int64) {
				defer wg.Done()
				wer := &ExpertRollout{Rng: rand.New(rand.NewSource(seed)), Cfg: er.Cfg}
				for j := range jobs {
					wer.QuickRolloutDetailed(states[j.ci].Clone(), round)
					r := wer.LastResult
					var v float64
					switch {
					case r.IsFoul:
						v = -float64(wer.Cfg.FoulCost)
					case r.IsFantasy:
						v = float64(r.RawRoyalty + r.FanBonus)
					default:
						v = float64(r.RawRoyalty)
					}
					mu.Lock()
					sum[j.ci] += v
					sumsq[j.ci] += v * v
					cnt[j.ci]++
					mu.Unlock()
				}
			}(seed)
		}
		wg.Wait()
	}

	capN := serveSearchCapForRound(round)
	for cnt[0] < capN {
		runBatch(ServeSearchBatch)
		// leader vs runner-up 判停
		best, second := 0, -1
		for i := 1; i < K; i++ {
			if sum[i]/float64(cnt[i]) > sum[best]/float64(cnt[best]) {
				second = best
				best = i
			} else if second < 0 || sum[i]/float64(cnt[i]) > sum[second]/float64(cnt[second]) {
				second = i
			}
		}
		if second >= 0 {
			mb := sum[best] / float64(cnt[best])
			ms := sum[second] / float64(cnt[second])
			vb := sumsq[best]/float64(cnt[best]) - mb*mb
			vs := sumsq[second]/float64(cnt[second]) - ms*ms
			se := math.Sqrt(vb/float64(cnt[best]) + vs/float64(cnt[second]))
			if mb-ms > 2*se {
				break
			}
		}
	}
	// 2026-07-05 (std14 教训): 滞回带 — NN top-1 是34万样本的先验, 挑战者须显著更好(>1.5分)才换手.
	// 近等价平局(搜索均值差<1.5)保持 NN 原选, 防 40-sim 噪声掷反硬币推翻正确薄边选择.
	best := 0
	for i := 1; i < len(states); i++ {
		if sum[i]/float64(cnt[i]) > sum[best]/float64(cnt[best]) {
			best = i
		}
	}
	if best != 0 && sum[best]/float64(cnt[best])-sum[0]/float64(cnt[0]) < 1.5 {
		best = 0
	}
	if MctsDebugTrace {
		fmt.Printf("=== serveMarginSearchK: K=%d n=%d/侧 → 选[%d] (means:", K, cnt[0], best)
		for i := 0; i < K; i++ {
			fmt.Printf(" %.2f", sum[i]/float64(cnt[i]))
		}
		fmt.Println(") ===")
	}
	return best
}
