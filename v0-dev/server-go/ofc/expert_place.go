package ofc

import (
	"fmt"
	"sort"
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
		penalty := ConnectorSplitPenalty(p, cards) + R1FourInRowPenalty(p, cards) + R1IncoherentRowPenalty(p, cards) + R1TopNonAKXPenalty(p, cards, state) + R1JokerOnTopWithAAPenalty(p, cards) + FoulImminentPenalty(gs) - R1SameSuitInRowBonus(p, cards) - R1JokerWithAOnTopBonus(p, cards) - R1SingleAOnTopBonus(p, cards) - R1FlushGroupOnBotBonus(p, cards) - R1SingleJokerNoAOnTopBonus(p, cards)
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
		fmt.Println("=== R1 Prerank (MLP value head + PolicyBoost) top 5 ===")
		for i := 0; i < 5 && i < len(candidates); i++ {
			c := candidates[i]
			fmt.Printf("  [%d] %s  score=%.4f\n", i+1, placementStr(c.gs), c.score)
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
func (er *ExpertRollout) ExpertPlace3(state *GameState, cards []Card) {
	actions := GenerateRoundNActions(cards, state)

	type cand struct {
		action  *RoundNAction
		gs      *GameState
		teScore float32
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
		uniq = append(uniq, cand{a, gs, 0})
	}

	// === Hard rule filter (打地鼠): 在 ranking 之前 narrow 候选 ===
	if !HardRulesDisabled {
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

	// stage1 ranking: value (head0) + 可选 policy (head3) bias - foul-imminent penalty
	for i := range uniq {
		item := &uniq[i]
		item.teScore = TrainedEval(item.gs)
		if _, _, _, plogit, hasPolicy := TrainedEvalFull(item.gs); hasPolicy {
			item.teScore += PolicyBoost * plogit
		}
		// FoulImminent — apply 后 partial state foul 必然 → 大 penalty (R2-R5 都生效, 2026-05-17 通用化)
		foul := FoulImminentPenalty(item.gs)
		item.teScore -= foul
		// KK on mid penalty (替原 rnRuleKK_NotOnMid filter)
		item.teScore -= RnKKOnMidPenalty(item.action, cards, state)
		// 2026-05-20 sp15: Rn top fantasy lock 软推 (case 29 — NN R2-R5 低估 A 上 top)
		// 2026-05-31 删 RnJokerWithHighOnTopBonus + RnTopCapBlockedFantasyPenalty:
		//   cap-aware guard 漏了 Type<0 (over cap) 情况, top AAA over mid pair cap 时还 +10,
		//   推翻 NN 真选 (NN 让 X 进 bot 凑 trips/Ad 进 bot), 致 ypk-97780042-1 R4 foul.
		item.teScore += RnSingleAOnTopBonus(item.action, item.gs, foul)
		// 2026-06-01 加: 鬼同行罚 (mid/bot 任一行 ≥2 鬼) → -5
		item.teScore -= RnJokersSameRowPenalty(item.action, item.gs)
		// 2026-06-05 加: 孤鬼(或鬼+sub-Q)在顶 + 放 1 A 上顶追 AA 范 (废 A 放底) → +8
		item.teScore += RnSingleJokerTopChaseABonus(item.gs, state)
		// 2026-06-05 加: 鬼在顶 + 孤 A 进中 (死张堵两对) → -8 (废 A 应放底或双A成对)
		item.teScore -= RnLoneAceMidJokerTopPenalty(item.gs, state)
	}

	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].teScore > uniq[j].teScore })

	if MctsDebugTrace {
		fmt.Println("=== RN Prerank (MLP value head + PolicyBoost) top 5 ===")
		for i := 0; i < 5 && i < len(uniq); i++ {
			c := uniq[i]
			discCard := cards[c.action.DiscardIdx].String()
			fmt.Printf("  [%d] %s 弃 %s  score=%.4f\n", i+1, placementStr(c.gs), discCard, c.teScore)
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
