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
		// 2026-07-06 sp46: 保险丝#2 — 必foul过滤 (同 Rn)
		if KeepFiltersPureNN && len(candidates) > 1 {
			kept := candidates[:0]
			for i := range candidates {
				if FoulImminentPenalty(candidates[i].gs) < 20 {
					kept = append(kept, candidates[i])
				}
			}
			if len(kept) > 0 {
				candidates = kept
			}
		}
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
			foulFuse := false
			fanFuse := false
			if ServeSearchMargin > 0 && ServeSearchR1 && pick == 0 && len(candidates) >= 2 {
				atomic.AddInt64(&ServeSearchDecCount, 1)
				if ServeSearchThinMargin && candidates[0].score-candidates[1].score < ServeSearchMargin {
					for i := 1; i < len(candidates) && i < ServeSearchTopK; i++ {
						if candidates[0].score-candidates[i].score < ServeSearchMargin &&
							serveSearchConsequential(candidates[0].gs, candidates[i].gs) {
							atomic.AddInt64(&ServeSearchTrigCount, 1)
							trig = true
							break
						}
					}
				}
				// 保险丝#3: top-1 高foul 且存在安全替代 → 无视 margin 强制验证
				if !trig && BuildFeaturesV3(candidates[0].gs)[89] > ServeSearchFoulFuse {
					all := make([]*GameState, len(candidates))
					for i := range candidates {
						all[i] = candidates[i].gs
					}
					if serveFoulFuseHasSafe(all) {
						atomic.AddInt64(&ServeSearchFoulTrigCount, 1)
						trig, foulFuse = true, true
					}
				}
				// 保险丝#4: 存在保底范候选而 top-1 不是 → 小跑裁决 (#16/#124)
				if !trig && ServeSearchFanFuse && !fanFloorCandidate(candidates[0].gs) {
					for i := 1; i < len(candidates); i++ {
						if fanFloorCandidate(candidates[i].gs) {
							atomic.AddInt64(&ServeSearchFanTrigCount, 1)
							trig, fanFuse = true, true
							break
						}
					}
				}
			}
			// 保险丝#7 pairmid (2026-07-10 用户spec, #129/#130 F11家族): R1 中道放对子且
			// 对子rank > 其余3张(除鬼) = 倒挂债结构 (中道成手对压住待发育底). 独立于 ServeSearchR1
			// 总开关 (R1 margin搜索仍禁, #59判例) — 结构触发 + rollout裁决, 130型实测平局会KEEP.
			pairMidFuse := false
			pairMidRank := -1
			if ServeSearchMargin > 0 && ServeSearchPairMidFuse && !trig && pick == 0 && len(candidates) >= 2 {
				if r := r1PairMidDominant(candidates[0].gs); r >= 0 {
					for i := 1; i < len(candidates); i++ {
						if !r1PairInMiddle(candidates[i].gs, r) {
							atomic.AddInt64(&ServeSearchPairMidTrigCount, 1)
							trig, pairMidFuse, pairMidRank = true, true, r
							break
						}
					}
				}
			}
			if !ServeSearchDryRun && trig && tryAcquireSearchSlot() {
				var sts []*GameState
				var stIdx []int // sts → candidates 索引映射 (fuse 集非前缀)
				if pairMidFuse {
					sts = append(sts, candidates[0].gs)
					stIdx = append(stIdx, 0)
					for i := 1; i < len(candidates) && len(sts) < 3; i++ {
						if !r1PairInMiddle(candidates[i].gs, pairMidRank) {
							sts = append(sts, candidates[i].gs)
							stIdx = append(stIdx, i)
						}
					}
				} else if foulFuse || fanFuse {
					all := make([]*GameState, len(candidates))
					for i := range candidates {
						all[i] = candidates[i].gs
					}
					if foulFuse {
						sts = serveFoulFuseStates(all, ServeSearchTopK)
					} else {
						sts = serveFanFuseStates(all, 1) // 保底线方差≈0, 1条+半预算足够
					}
					for _, st := range sts {
						for i := range candidates {
							if candidates[i].gs == st {
								stIdx = append(stIdx, i)
								break
							}
						}
					}
				} else {
					for i := 0; i < len(candidates) && i < ServeSearchTopK; i++ {
						if candidates[0].score-candidates[i].score < ServeSearchMargin {
							sts = append(sts, candidates[i].gs)
							stIdx = append(stIdx, i)
						}
					}
				}
				var n int
				var means []float64
				otR1 := 4
				if pairMidFuse {
					otR1 = 8 // R1 rollout 方差大 (#59), pairmid 靠加时压 SE
				}
				pick, n, means = er.serveMarginSearchKDivH(sts, 1, 1, ServeSearchHysteresis, otR1)
				releaseSearchSlot()
				tag := "KEEP"
				if pick != 0 {
					tag = "OVERRIDE"
				}
				if foulFuse {
					tag += "-foulfuse"
				}
				if fanFuse {
					tag += "-fanfloor"
				}
				if pairMidFuse {
					tag += "-pairmid"
				}
				pick = stIdx[pick]
				line := fmt.Sprintf("[serve-search] R1 %s n=%d means=%.2f NN=%s → 选=%s",
					tag, n, means, placementStr(candidates[0].gs), placementStr(candidates[pick].gs))
				if ServeSearchLog != nil {
					ServeSearchLog(line)
				}
				if er.SearchAudit != nil {
					*er.SearchAudit = append(*er.SearchAudit, line)
				}
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
		// 2026-07-06 sp46: 保险丝#2 — 必foul过滤 (FoulImminentPenalty 只检100%必然case, 零误伤).
		// 治"自信地必爆"(std45类 中SF>底max): royalty军团骗过NN时, 数学直接除名. 全候选必foul则不滤.
		if KeepFiltersPureNN && len(uniq) > 1 {
			kept := uniq[:0]
			for i := range uniq {
				if FoulImminentPenalty(uniq[i].gs) < 20 {
					kept = append(kept, uniq[i])
				}
			}
			if len(kept) > 0 {
				uniq = kept
			}
		}
		// 2026-07-05: 保险丝#1 — R5 支配过滤 (label盲区 tie-break, #90 555vs333)
		if KeepFiltersPureNN && state.Round == 5 && len(uniq) > 1 {
			sts := make([]*GameState, len(uniq))
			for i := range uniq {
				sts[i] = uniq[i].gs
			}
			km := r5DominanceKeep(sts)
			kept := uniq[:0]
			for i := range uniq {
				if km[i] {
					kept = append(kept, uniq[i])
				}
			}
			uniq = kept
		}
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
			foulFuseN := false
			fanFuseN := false
			jokerTripsN := false
			ceilN := false
			if ServeSearchMargin > 0 && pick == 0 && len(uniq) >= 2 {
				atomic.AddInt64(&ServeSearchDecCount, 1)
				if ServeSearchThinMargin && uniq[0].teScore-uniq[1].teScore < ServeSearchMargin {
					for i := 1; i < len(uniq) && i < ServeSearchTopK; i++ {
						if uniq[0].teScore-uniq[i].teScore < ServeSearchMargin &&
							serveSearchConsequential(uniq[0].gs, uniq[i].gs) {
							atomic.AddInt64(&ServeSearchTrigCount, 1)
							trigN = true
							break
						}
					}
				}
				// 保险丝#3: top-1 高foul 且存在安全替代 → 无视 margin 强制验证
				if !trigN && BuildFeaturesV3(uniq[0].gs)[89] > ServeSearchFoulFuse {
					all := make([]*GameState, len(uniq))
					for i := range uniq {
						all[i] = uniq[i].gs
					}
					if serveFoulFuseHasSafe(all) {
						atomic.AddInt64(&ServeSearchFoulTrigCount, 1)
						trigN, foulFuseN = true, true
					}
				}
				// 保险丝#4: 存在保底范候选而 top-1 不是 → 小跑裁决 (#16/#124)
				if !trigN && ServeSearchFanFuse && !fanFloorCandidate(uniq[0].gs) {
					for i := 1; i < len(uniq); i++ {
						if fanFloorCandidate(uniq[i].gs) {
							atomic.AddInt64(&ServeSearchFanTrigCount, 1)
							trigN, fanFuseN = true, true
							break
						}
					}
				}
				// 保险丝#5: 顶三条含鬼(高杠杆承诺)且非确定范 → 必搜 (#46; #19 确定范免搜)
				if !trigN && ServeSearchFanFuse && jokerTripsTopLock(uniq[0].gs) && !jokerTripsCertain(uniq[0].gs) {
					atomic.AddInt64(&ServeSearchTripsTrigCount, 1)
					trigN, jokerTripsN = true, true
				}
				// 保险丝#6: 范天花板 — top-1 没锁大对上顶而候选能锁且上位rank死绝 (#63)
				if !trigN && ServeSearchFanFuse && topLockedPairRank(uniq[0].gs) < 0 {
					for i := 1; i < len(uniq) && i < ServeSearchTopK*2; i++ {
						if r := topLockedPairRank(uniq[i].gs); r >= 0 && fanCeilingLocked(uniq[i].gs, r) {
							atomic.AddInt64(&ServeSearchCeilTrigCount, 1)
							trigN, ceilN = true, true
							break
						}
					}
				}
			}
			if !ServeSearchDryRun && trigN && tryAcquireSearchSlot() {
				var sts []*GameState
				var stIdx []int // sts → uniq 索引映射 (fuse 集非前缀)
				if ceilN {
					sts = append(sts, uniq[0].gs)
					stIdx = append(stIdx, 0)
					for i := 1; i < len(uniq) && len(sts) < ServeSearchTopK+1; i++ {
						if r := topLockedPairRank(uniq[i].gs); r >= 0 && fanCeilingLocked(uniq[i].gs, r) {
							sts = append(sts, uniq[i].gs)
							stIdx = append(stIdx, i)
						}
					}
				} else if jokerTripsN {
					for i := 0; i < len(uniq) && i < ServeSearchTopK+1; i++ {
						sts = append(sts, uniq[i].gs)
						stIdx = append(stIdx, i)
					}
				} else if foulFuseN || fanFuseN {
					all := make([]*GameState, len(uniq))
					for i := range uniq {
						all[i] = uniq[i].gs
					}
					if foulFuseN {
						sts = serveFoulFuseStates(all, ServeSearchTopK)
					} else {
						sts = serveFanFuseStates(all, 1) // 保底线方差≈0, 1条+半预算足够
					}
					for _, st := range sts {
						for i := range uniq {
							if uniq[i].gs == st {
								stIdx = append(stIdx, i)
								break
							}
						}
					}
				} else {
					for i := 0; i < len(uniq) && i < ServeSearchTopK; i++ {
						if uniq[0].teScore-uniq[i].teScore < ServeSearchMargin {
							sts = append(sts, uniq[i].gs)
							stIdx = append(stIdx, i)
						}
					}
				}
				var n int
				var means []float64
				// 2026-07-06 #22: fanfloor 恢复全预算 — v3免费卷线 fan~72% 方差大, 半预算在 2SE 门槛闪烁
				// 2026-07-08 #63 二审: fanceil 降滞回 2.0 (唯一范路, 教义同向) — 灰区加时随 hyst 一起下探
				hystN := ServeSearchHysteresis
				otN := 4
				if ceilN {
					hystN = ServeSearchCeilHysteresis
					otN = 8 // #63 三审: 真抖加样本
				}
				pick, n, means = er.serveMarginSearchKDivH(sts, state.Round, 1, hystN, otN)
				// 教义带 (#16, 2026-07-07): 挑战者=承诺型保底范(100%范锁定) 且 NN线优势<4 → 取保底线.
				// 账本盲区(范率/re-fan延续)由教义定价; 免费卷型(概率范)不适用.
				if fanFuseN && pick == 0 && len(sts) > 1 && len(means) > 1 {
					// 仅当 NN 线自己不是承诺保底时才反转举证 (#73 教训: 两线同顶同范价值, 教义无差别)
					// 2026-07-08 #73 二审: 同顶硬免死 — 顶完全相同则范差恒为零, f89 行间不对称
					// 会让 commit-certain 误判 (Jc顺draw底 vs 9d对子底), 教义带无权翻案.
					if fanFloorCommitCertain(sts[1]) && !fanFloorCommitCertain(sts[0]) &&
						means[0]-means[1] < 4.0 && !sameTopRow(sts[0], sts[1]) {
						pick = 1
					}
				}
				releaseSearchSlot()
				tag := "KEEP"
				if pick != 0 {
					tag = "OVERRIDE"
				}
				if foulFuseN {
					tag += "-foulfuse"
				}
				if fanFuseN {
					tag += "-fanfloor"
				}
				if jokerTripsN {
					tag += "-jokertrips"
				}
				if ceilN {
					tag += "-fanceil"
				}
				if fanFuseN && pick != 0 && len(sts) > 1 && fanFloorCommitCertain(sts[pick]) && means[pick]-means[0] < ServeSearchHysteresis {
					tag += "-doctrine"
				}
				pick = stIdx[pick]
				line := fmt.Sprintf("[serve-search] R%d %s n=%d means=%.2f NN=%s → 选=%s",
					state.Round, tag, n, means, placementStr(uniq[0].gs), placementStr(uniq[pick].gs))
				if ServeSearchLog != nil {
					ServeSearchLog(line)
				}
				if er.SearchAudit != nil {
					*er.SearchAudit = append(*er.SearchAudit, line)
				}
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

// ServeSearchThinMargin — 薄边路径(纯分差+后果判据触发)是否参与搜索 (2026-07-06 默认关).
// 退役理由: 它为 std63 而生, 现 NN 原生学会; 剩余触发全是品味题 — rollout 账本(foul6/KK30)
// 与范率优先教义有原则分歧 (60语境 1000-sim 账本也认头KK+2.0, 教义要底KK等AA), 加样本无解.
// override 权收归保险丝#3(绝路)/#4(保送) — 数学题, 账本与教义无分歧.
var ServeSearchThinMargin = false

// ServeSearchR1 — R1 是否参与搜索 (2026-07-06 用户拍板: 默认关).
// R1 rollout 深且噪大 (40 sims SE≈2.8), #59 实锤: NN 选对被 fanfloor 噪声翻错 (means 45.73 vs 39.92).
// R1 决策空间小 + 1000-sim 沙盘铁律本就说 R1 裁决要大样本, serve 预算给不起 → 交给 NN.
var ServeSearchR1 = false

// ServeSearchHysteresis — 换手门槛 (搜索均值差). 2026-07-06: 1.5→4.0, 见 serveMarginSearchKDiv 内注释.
var ServeSearchHysteresis = 4.0

// ServeSearchCeilHysteresis — #6 fanceil 专用换手门槛 (2026-07-08 #63 二审, 用户拍板 1+2).
// 天花板锁死 = 唯一范路, 错过不再来; 账本教义同向(+5)但真差骑在通用滞回4.0上 —
// 实战07-06本尊局差0.36险胜, bench差0.18告负 = 掷硬币. 降到2.0 + 灰区加时压SE, 显著性门槛(2SE)仍在.
var ServeSearchCeilHysteresis = 2.0

// ServeSearchDryRun/计数器 — 触发率测量: 只数不搜 (2026-07-05 用户"怕把把都要搜索").
var (
	ServeSearchDryRun    bool
	ServeSearchDecCount  int64 // 决策总数 (margin>0 时)
	ServeSearchTrigCount int64 // 触发数 (薄边+后果判据)
)

// ServeSearchFoulFuse — 保险丝#3 (2026-07-06 用户拍板): NN top-1 是高foul线 (f89>阈值) →
// 无视 margin 强制搜索验证. 42型"自信地错"(te gap 60, 真foul 92%) 绕过薄边触发, 这里兜住.
// 依赖同日的 f89 鬼降级修复 — 修前 46型假警报(鬼顶可缩, 真foul 0%)会天天误触.
var (
	ServeSearchFoulFuse      float32 = 0.7
	ServeSearchFoulTrigCount int64
)

// ServeSearchFanFuse — 保险丝#4 保底范 (2026-07-06 用户拍板): 场上存在"保底范"候选而 NN top-1
// 不是 → 小跑搜索裁决. #16/#124 实锤: 保底线 100%范/0%foul (mean 45/89) 被 NN 排后 (te gap +18.7).
// 探测是粗筛(顶有鬼+天然大牌≥Q + f89≈0), 600-sim 实测 fan% 当裁判 — 保底线 mean 碾压, 搜索必翻.
var (
	ServeSearchFanFuse      = true
	ServeSearchFanTrigCount int64
)

// fanFloorCandidate — 保底范判定: 顶含鬼 + 顶有天然 Q/K/A, 且**鬼承诺配大牌(范形态, 不许降级)后
// foul 仍不可能**. 2026-07-06 v2 (#16 实锤): 粗筛版把 16A(顶🃏QcAc, 鬼一牌两用不可兼得: 当A追范
// 24%foul / 降级保命没范)也放行 → top-1 自己"像"保底范, 保险丝不触发. 承诺检查分得开:
// 16B 承诺 QQ 后中道 AA 恒≥ → f89=0 ✓; 16A 承诺 AA 后中道无A必须两对 → f89 高 ✗.
func fanFloorCandidate(gs *GameState) bool {
	bigRank := -1
	jokerN := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			jokerN++
		} else if int(c.Rank()) > bigRank && c.Rank() >= RankQ {
			bigRank = int(c.Rank())
		}
	}
	if jokerN == 0 || bigRank < 0 {
		return false
	}
	// 承诺范形态: 鬼 → 大牌rank 的替身牌 (花色取顶内未用的, 仅供eval, 不动UsedCards)
	g2 := gs.Clone()
	var suitUsed [4]bool
	for _, c := range gs.Top {
		if !c.IsJoker() && int(c.Rank()) == bigRank {
			suitUsed[c.Suit()] = true
		}
	}
	top2 := make([]Card, 0, len(gs.Top))
	for _, c := range gs.Top {
		if c.IsJoker() {
			sub := Card(0)
			for st := uint8(0); st < 4; st++ {
				if !suitUsed[st] {
					sub = MakeCard(uint8(bigRank), st)
					suitUsed[st] = true
					break
				}
			}
			top2 = append(top2, sub)
		} else {
			top2 = append(top2, c)
		}
	}
	g2.Top = top2
	if BuildFeaturesV3(g2)[89] < 0.05 {
		return true // 承诺保底 (16B型): 现在锁范也恒安全
	}
	// v3 (2026-07-06 #22): 条件承诺 — 顶留空位时真实玩法是"中道成了才让鬼配大牌, 不成降级保命".
	// 降级逃生铁的条件: 中道已有天然成对(对子恒压任何高牌顶) 且 原状态 foul 链≈0.
	// 16A 反例: 降级后顶 A-Q 高 vs 中 A92 无对 → kicker 战会输(真 24% foul), 中道无对 → 拒.
	if 3-len(gs.Top) >= 1 {
		var midRc [13]int
		for _, c := range gs.Middle {
			if !c.IsJoker() {
				midRc[c.Rank()]++
			}
		}
		midPair := false
		for _, n := range midRc {
			if n >= 2 {
				midPair = true
				break
			}
		}
		if midPair && BuildFeaturesV3(gs)[89] < 0.05 {
			return true // 免费卷 (22B型): 降级恒活 + 范大概率
		}
	}
	return false
}

// serveFanFuseStates — 保险丝#4 对比集: top-1 + 排名最高的保底范候选 (≤topK).
func serveFanFuseStates(states []*GameState, topK int) []*GameState {
	out := []*GameState{states[0]}
	for i := 1; i < len(states) && len(out) < topK+1; i++ {
		if fanFloorCandidate(states[i]) {
			out = append(out, states[i])
		}
	}
	return out
}

// ServeSearchTripsTrigCount — 保险丝#5 (2026-07-06 用户拍板): 顶上放三条且含鬼 → 必搜.
// #46 型: 锁 🃏22=222 追窄范(4.5%) vs 留鬼自由行(21.7%) — #4 只认 ≥Q 对子范, 三条范(任意rank)
// 在它视野外. 鬼三条顶 = 高杠杆承诺(鬼被锁死), 值得一次二次验证; 对比集=NN前K名(替代线形态多样).
var ServeSearchTripsTrigCount int64

// jokerTripsCertain — 鬼三条顶是否"确定范": 承诺三条形态(鬼→trips rank替身)后 foul 链≈0
// = 中底已托死, 范数学锁定. 2026-07-06 #19 (用户裁): 确定范不容账本翻案 (范率优先教义) —
// 19 锁333(中888成/底TTT成, 100%范) 被#5按EV翻成留双鬼(91%范+higher EV) = 错.
// 46A 反例: 锁222 中88托不住 → 承诺后 f89 高 → 范不确定 → 该搜.
func jokerTripsCertain(gs *GameState) bool {
	var rankCnt [13]int
	jokerN := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			jokerN++
		} else {
			rankCnt[c.Rank()]++
		}
	}
	tripsRank := -1
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 1 {
			tripsRank = r
			break
		}
	}
	if tripsRank < 0 {
		return false // 全鬼顶, 交给搜索
	}
	g2 := gs.Clone()
	var suitUsed [4]bool
	for _, c := range gs.Top {
		if !c.IsJoker() && int(c.Rank()) == tripsRank {
			suitUsed[c.Suit()] = true
		}
	}
	top2 := make([]Card, 0, 3)
	for _, c := range gs.Top {
		if c.IsJoker() {
			for st := uint8(0); st < 4; st++ {
				if !suitUsed[st] {
					top2 = append(top2, MakeCard(uint8(tripsRank), st))
					suitUsed[st] = true
					break
				}
			}
		} else {
			top2 = append(top2, c)
		}
	}
	g2.Top = top2
	return BuildFeaturesV3(g2)[89] < 0.05
}

// jokerTripsTopLock — 顶满 + 含鬼 + 鬼打满成三条.
func jokerTripsTopLock(gs *GameState) bool {
	if len(gs.Top) != 3 {
		return false
	}
	hasJoker := false
	for _, c := range gs.Top {
		if c.IsJoker() {
			hasJoker = true
		}
	}
	if !hasJoker {
		return false
	}
	return Evaluate3Joker(gs.Top).Type == TypeThreeOfAKind
}

// ServeSearchCeilTrigCount — 保险丝#6 范天花板 (2026-07-07, #63): top-1 没锁大对上顶,
// 而场上存在"锁对P≥Q于顶"的候选, 且 P 之上所有 rank 已死绝(等待无 rank 上行) → 搜一次.
// 触发=纯甲板数学(rankRem), 裁决=账本 — 63 对照实验账本教义同向(+5.2), 无 #19 型冲突.
// 注: 不看鬼(双鬼理论可AA), 鬼路径由搜索的 rollout 自然定价.
var ServeSearchCeilTrigCount int64

// topLockedPairRank — 候选顶上已锁的天然大对 rank (≥Q); 无则 -1.
func topLockedPairRank(gs *GameState) int {
	var rc [13]int
	for _, c := range gs.Top {
		if !c.IsJoker() {
			rc[c.Rank()]++
		}
	}
	for r := 12; r >= int(RankQ); r-- {
		if rc[r] >= 2 {
			return r
		}
	}
	return -1
}

// fanCeilingLocked — rank r 之上的所有 rank 是否死绝 (等待无上行).
func fanCeilingLocked(gs *GameState, r int) bool {
	var seen [13]int
	for id := range gs.UsedCards {
		if c, ok := ParseCard(id); ok && !c.IsJoker() {
			seen[c.Rank()]++
		}
	}
	for hr := r + 1; hr <= 12; hr++ {
		if seen[hr] < 4 {
			return false
		}
	}
	return true
}

// fanFloorCommitCertain — 仅承诺型保底范 (鬼承诺配大牌后 foul 链≈0 = 100%范数学锁定).
// 教义带(#16, 2026-07-07 用户"只要可达就必须治")专用: 这类线的范率/延续价值是账本已知盲区
// (扁平bonus不价re-fan/范率, #19/#63判例), 举证责任反转 — NN线须好4分以上才保留.
// ServeSearchPairMidFuse — 保险丝#7 (2026-07-10 用户spec): R1 中道对子压秤 (对子>其余3张除鬼).
// #129/#130 F11家族的栈层兜底: 死牌×对子行位轴的特征响应倒挂, NN裸摆会把封顶对留中垫烂底.
// ⛔ 2026-07-10 默认关 (用户"保险丝也没作用?"定论): F11病在续局功力里, 自我rollout裁决循环失明
// (serve环境量129仅+1.5 vs 深度听证+12) + R1搜索5~40s延迟. 代码保留, 测量层升级后再启用.
var ServeSearchPairMidFuse = false
var ServeSearchPairMidTrigCount int64

// r1PairMidDominant — R1候选: 中道含天然对(rank r) 且 其余非鬼牌全 < r → 返回 r; 否则 -1.
func r1PairMidDominant(gs *GameState) int {
	var midRc [13]int
	for _, c := range gs.Middle {
		if !c.IsJoker() {
			midRc[c.Rank()]++
		}
	}
	pr := -1
	for r := 12; r >= 0; r-- {
		if midRc[r] >= 2 {
			pr = r
			break
		}
	}
	if pr < 0 {
		return -1
	}
	pairLeft := 2
	for _, row := range [][]Card{gs.Top, gs.Middle, gs.Bottom} {
		for _, c := range row {
			if c.IsJoker() {
				continue
			}
			if int(c.Rank()) == pr && pairLeft > 0 {
				pairLeft--
				continue
			}
			if int(c.Rank()) >= pr {
				return -1
			}
		}
	}
	return pr
}

// r1PairInMiddle — rank r 的天然对是否在中道.
func r1PairInMiddle(gs *GameState, r int) bool {
	n := 0
	for _, c := range gs.Middle {
		if !c.IsJoker() && int(c.Rank()) == r {
			n++
		}
	}
	return n >= 2
}

// sameTopRow — 两候选顶行牌完全一致 (multiset). 同顶 = 范价值零差异, 教义带不适用 (#73 二审).
func sameTopRow(a, b *GameState) bool {
	if len(a.Top) != len(b.Top) {
		return false
	}
	cnt := map[string]int{}
	for _, c := range a.Top {
		cnt[c.ID()]++
	}
	for _, c := range b.Top {
		cnt[c.ID()]--
		if cnt[c.ID()] < 0 {
			return false
		}
	}
	return true
}

func fanFloorCommitCertain(gs *GameState) bool {
	bigRank := -1
	jokerN := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			jokerN++
		} else if int(c.Rank()) > bigRank && c.Rank() >= RankQ {
			bigRank = int(c.Rank())
		}
	}
	if jokerN == 0 || bigRank < 0 {
		return false
	}
	g2 := gs.Clone()
	var suitUsed [4]bool
	for _, c := range gs.Top {
		if !c.IsJoker() && int(c.Rank()) == bigRank {
			suitUsed[c.Suit()] = true
		}
	}
	top2 := make([]Card, 0, len(gs.Top))
	for _, c := range gs.Top {
		if c.IsJoker() {
			for st := uint8(0); st < 4; st++ {
				if !suitUsed[st] {
					top2 = append(top2, MakeCard(uint8(bigRank), st))
					suitUsed[st] = true
					break
				}
			}
		} else {
			top2 = append(top2, c)
		}
	}
	g2.Top = top2
	return BuildFeaturesV3(g2)[89] < 0.05
}

// serveFoulFuseHasSafe — 保险丝#3 第二判据: 是否存在安全替代线 (f89<0.3).
// 被迫foul场 (全候选都危) 不触发 — 搜了白搜还烧预算; fuse 的本意是"有安路不走选了绝路".
// 首测无此判据 fuse 触发 11.3% (尾轮被迫场大量误触), 加后应显著降.
func serveFoulFuseHasSafe(states []*GameState) bool {
	for i := 1; i < len(states); i++ {
		if BuildFeaturesV3(states[i])[89] < 0.3 {
			return true
		}
	}
	return false
}

// serveFoulFuseStates — 保险丝#3 的对比集: top-1 + 排名最高的安全替代线 (f89<0.3), 至多 topK 条.
// 不能用 margin 过滤 — 高foul误选的 gap 往往巨大(42=60), 安全线在 margin 外.
// (触发已保证有安全线; 空安全集是竞态外的兜底, 回退 top-K.)
func serveFoulFuseStates(states []*GameState, topK int) []*GameState {
	out := []*GameState{states[0]}
	for i := 1; i < len(states) && len(out) < topK+1; i++ {
		if BuildFeaturesV3(states[i])[89] < 0.3 {
			out = append(out, states[i])
		}
	}
	if len(out) == 1 {
		for i := 1; i < len(states) && i < topK; i++ {
			out = append(out, states[i])
		}
	}
	return out
}

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

// KeepFiltersPureNN — 2026-07-05 (用户: 硬规则只留两根保险丝, 适配纯NN+搜索栈):
// pureMLP 模式下也启用 R5 支配过滤 (label 盲区: 555vs333 同royalty, NN/搜索都分不出, 见#90).
// env OFC_KEEP_FILTERS=1 开.
var KeepFiltersPureNN bool

// r5DominanceKeep — R5 收官广义支配过滤 (2026-07-05 用户: 支配过滤只应用到 R5).
// 完局板逐行比牌力: A 三行 ≥ B 且至少一行 > (或 A 不foul B foul) → B 必劣删除.
// 计分(royalty+对战行胜负)对每行牌力单调 → 数学上零误伤. 治 label 盲区 (#90 555vs333, h2h价值不在solo-reward里).
func r5DominanceKeep(states []*GameState) []bool {
	n := len(states)
	type ev struct {
		foul    bool
		t, m, b int64
	}
	evs := make([]ev, n)
	for i, gs := range states {
		sc := gs.Score()
		evs[i] = ev{foul: sc.Foul, t: int64(sc.TopEval.Value), m: int64(sc.MidEval.Value), b: int64(sc.BotEval.Value)}
	}
	dominates := func(a, b ev) bool {
		if a.foul {
			return false
		}
		if b.foul {
			return true
		}
		if a.t < b.t || a.m < b.m || a.b < b.b {
			return false
		}
		return a.t > b.t || a.m > b.m || a.b > b.b
	}
	keep := make([]bool, n)
	any := false
	for i := 0; i < n; i++ {
		dominated := false
		for j := 0; j < n; j++ {
			if i != j && dominates(evs[j], evs[i]) {
				dominated = true
				break
			}
		}
		if !dominated {
			keep[i] = true
			any = true
		}
	}
	if !any {
		for i := range keep {
			keep[i] = true
		}
	}
	return keep
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

// ServeSearchLog — 搜索审计钩子 (2026-07-05 用户: 排锅要能分清搜索的锅还是NN的锅).
// 每次搜索记一行(含未换手). server main 接 log.Printf; nil=关.
var ServeSearchLog func(line string)

// serveMarginSearchK — K 个 post-state 并行加摸 (worker=NumCPU, 每 worker 独立 ExpertRollout/Rng);
// 每批后若 leader 领先第二名 > 2·SE合 提前停 (真平局烧满也没损失). 返回赢家下标+统计.
func (er *ExpertRollout) serveMarginSearchK(states []*GameState, round int) (int, int, []float64) {
	return er.serveMarginSearchKDiv(states, round, 1)
}

// serveMarginSearchKDiv — capDiv>1 缩减 sims 预算 (保险丝#4: 保底线方差≈0, 半预算足够裁决).
func (er *ExpertRollout) serveMarginSearchKDiv(states []*GameState, round, capDiv int) (int, int, []float64) {
	return er.serveMarginSearchKDivH(states, round, capDiv, ServeSearchHysteresis, 4)
}

// serveMarginSearchKDivH — 换手门槛/灰区加时可调版 (#6 fanceil: 滞回2.0+加时8×, 其余走默认4×).
// 2026-07-09 #63 三审(用户拍板"真抖加样本"): R2 cap80×4=320 下 2SE≈4.2 盖过真差~4 → 显著性关掷硬币;
// 8×(n=640) 2SE≈3.0, 真差稳定显著. 只有 ceil 触发的万分位手付这 1-2s.
func (er *ExpertRollout) serveMarginSearchKDivH(states []*GameState, round, capDiv int, hyst float64, overtimeMult int) (int, int, []float64) {
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

	capN := serveSearchCapForRound(round) / capDiv
	if capN < ServeSearchBatch {
		capN = ServeSearchBatch
	}
	// 灰区加时 (2026-07-06 #16 教训): 挑战者对 top-1 的优势落在 [滞回, 2SE) = 有信号没辨清 —
	// 与其放弃, 加倍预算把 SE 压下来 (16 的 gap 7.9 在 40-sim SE4.5 下不显著, 80-sim 即显著).
	// 只有灰区付双倍, 铁案(>2SE)照旧早停, 噪声(<滞回)照旧放弃.
	maxN := capN
	for cnt[0] < maxN {
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
		if best != 0 && maxN == capN {
			mb := sum[best] / float64(cnt[best])
			m0 := sum[0] / float64(cnt[0])
			vb := sumsq[best]/float64(cnt[best]) - mb*mb
			v0 := sumsq[0]/float64(cnt[0]) - m0*m0
			seD := math.Sqrt(vb/float64(cnt[best]) + v0/float64(cnt[0]))
			if gap := mb - m0; gap > hyst && gap < 2*seD {
				// 2026-07-06 账本对齐后: 范bonus方差↑(0~140摆动) + 真gap缩(16: 17→7.3),
				// 辨清 gap≈7 需 n≈225. 加时上限 2×→4× — 到显著即早停, 只有真灰区付满.
				maxN = capN * overtimeMult
			}
		}
	}
	// 2026-07-05 (std14 教训): 滞回带 — NN top-1 是34万样本的先验, 挑战者须显著更好才换手.
	// 2026-07-06 加严 1.5→4.0 (#63 实锤) 再叠显著性门槛 (#60/#63 对照实验):
	// rollout 确带 usedCards (60/63 语境 1000-sim 均值差 -2.0 vs +5.2, deck-aware ✓),
	// 但 40-sim 下均值差的 SE≈4 — 真实差距 2~5 分的品味题在噪声底之下, "裁决"=掷硬币.
	// 换手须 gap > max(固定滞回, 2×SE_diff): 搜索不知道时保持 NN, 只有统计显著的大灾才出手.
	best := 0
	for i := 1; i < len(states); i++ {
		if sum[i]/float64(cnt[i]) > sum[best]/float64(cnt[best]) {
			best = i
		}
	}
	if best != 0 {
		mb := sum[best] / float64(cnt[best])
		m0 := sum[0] / float64(cnt[0])
		vb := sumsq[best]/float64(cnt[best]) - mb*mb
		v0 := sumsq[0]/float64(cnt[0]) - m0*m0
		seDiff := math.Sqrt(vb/float64(cnt[best]) + v0/float64(cnt[0]))
		thr := hyst
		if 2*seDiff > thr {
			thr = 2 * seDiff
		}
		if mb-m0 < thr {
			best = 0
		}
	}
	means := make([]float64, K)
	for i := 0; i < K; i++ {
		means[i] = sum[i] / float64(cnt[i])
	}
	if MctsDebugTrace {
		fmt.Printf("=== serveMarginSearchK: K=%d n=%d/侧 → 选[%d] (means:", K, cnt[0], best)
		for i := 0; i < K; i++ {
			fmt.Printf(" %.2f", means[i])
		}
		fmt.Println(") ===")
	}
	return best, cnt[0], means
}
