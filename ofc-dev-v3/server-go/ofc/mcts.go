package ofc

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
)

// MCTS — OFC 单 player MCTS, 处理随机 deal 用 chance-node sampling.
//
// Tree node = AI 决策点 (state + dealt cards). 子节点 = 候选 action.
// 每 simulation:
//   1. Selection: 从 root 沿 UCB/PUCT 选最优 action 一路下
//   2. 当走到叶子 (未扩展): 扩展 + NN evaluate value
//   3. 若叶子是终局 (R5 done, state.IsComplete): 直接 ScoreHand 当 value
//   4. Backup: value 沿路径累加 Q
//
// Chance node: 当从 child 进入下一 round 但还没 dealt 时, 随机从 remaining deck
// 抽 3 张 (R2-R5) 或 5 张 (不应发生, R1 在 root) 当 dealt, 继续 sim.
// 不同 sim 抽不同 dealt, 自然平均出真实 EV (无 oracle 的 future-info bias).
//
// MLP 接入: 叶子 NN(state) → value (royalty 预测). 当前用 TrainedEval, 不用 policy head.
// PUCT 退化为 UCB1 (no NN policy prior).

// MCTSConfig — search 参数
type MCTSConfig struct {
	Sims       int     // 每决策 simulation 数
	CPuct      float32 // UCB exploration constant
	UseValue   bool    // true = NN value eval; false = random rollout (fallback)
	RolloutCfg *RolloutConfig // foul-cost / fan-bonus knobs
	Rng        *rand.Rand
}

// DefaultMCTSConfig — 推理用默认
func DefaultMCTSConfig() MCTSConfig {
	cfg := DefaultRolloutConfig
	return MCTSConfig{
		Sims:       200,
		CPuct:      1.5,
		UseValue:   true,
		RolloutCfg: &cfg,
		Rng:        rand.New(rand.NewSource(1)),
	}
}

// mctsNode — 决策树节点. 代表 "AI 在 state + dealt 状态下要做决策" 这个时刻.
type mctsNode struct {
	state    *GameState
	dealt    []Card
	round    int        // 当前 round (即将做决策的 round)
	terminal bool       // state 已完整
	score    float32    // 终局 score (terminal=true 时有效)

	// Children expansion
	expanded     bool
	actions      []mctsAction
	children     []*mctsNode  // index 对应 actions[i]; nil = 未访问
	q            []float32    // 每 action 累计 value 平均
	n            []int        // 每 action 访问次数
	nTotal       int          // 总访问数
	policyPrior  []float32    // NN policy (Phase 3+, 暂全均匀)
	penalties    []float32    // ConnectorSplitPenalty 等 (selectAction 时减去)

	value float32 // NN 给的 state value (cached)
}

// mctsAction — 一个决策的内容
//   - R1: placement = 长度 5, 对应 dealt[0..4] 的 row
//   - R2-R5: discardIdx + kept (长度 2) + placement (长度 2)
type mctsAction struct {
	round       int       // 1=R1, 2-5=Rn
	placement   []Row     // R1 长 5, R2-R5 长 2
	discardIdx  int       // R2-R5
	kept        []Card    // R2-R5 长 2
}

// MCTSSearch — 在给定 state + dealt 上跑 MCTS, 返回最优 action.
//
// 输入:
//   state — 当前 partial state (round 应等于 round 参数)
//   dealt — 当前 round 发的 cards (R1=5, R2-R5=3)
//   round — 当前 round (1-5)
//   cfg   — MCTS 配置
//
// 返回:
//   bestAction — 访问数最高的 action
//   stats      — 每 action 的 (visits, q) 用于 debug or self-play data collection
type MCTSStats struct {
	Visits []int
	Q      []float32
	Action mctsAction
}

func MCTSSearch(state *GameState, dealt []Card, round int, cfg MCTSConfig) (mctsAction, []MCTSStats) {
	root := newMCTSNode(state.Clone(), dealt, round, cfg.RolloutCfg)
	root.expand(cfg)

	for sim := 0; sim < cfg.Sims; sim++ {
		simulate(root, cfg)
	}

	// Pick best by visit count
	bestIdx := 0
	bestN := -1
	for i, n := range root.n {
		if n > bestN {
			bestN = n
			bestIdx = i
		}
	}

	if MctsDebugTrace {
		// 打印 top 5 by visits + 全部 action 摘要
		type item struct {
			idx int
			n   int
			q   float32
			p   float32
			pen float32
		}
		items := make([]item, len(root.actions))
		for i := range root.actions {
			items[i] = item{i, root.n[i], root.q[i], root.policyPrior[i], root.penalties[i]}
		}
		// sort by visits desc
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if items[j].n > items[i].n {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
		fmt.Printf("=== MCTS R%d %d candidates, sims=%d ===\n", round, len(root.actions), cfg.Sims)
		fmt.Printf("%-45s %6s %8s %8s %8s\n", "placement", "visits", "Q", "P", "penalty")
		for i := 0; i < len(items); i++ {
			it := items[i]
			tmp := root.state.Clone()
			applyAction(tmp, root.dealt, root.actions[it.idx])
			marker := "  "
			if it.idx == bestIdx {
				marker = "★ "
			}
			fmt.Printf("%s%-43s %6d %8.3f %8.4f %8.2f\n", marker, placementStr(tmp), it.n, it.q, it.p, it.pen)
		}
	}

	stats := make([]MCTSStats, len(root.actions))
	for i := range root.actions {
		var q float32
		if root.n[i] > 0 {
			q = root.q[i]
		}
		stats[i] = MCTSStats{Visits: []int{root.n[i]}, Q: []float32{q}, Action: root.actions[i]}
	}

	return root.actions[bestIdx], stats
}

// newMCTSNode — 构造节点; 不展开
func newMCTSNode(state *GameState, dealt []Card, round int, rolloutCfg *RolloutConfig) *mctsNode {
	n := &mctsNode{
		state: state,
		dealt: dealt,
		round: round,
	}
	if state.IsComplete() {
		n.terminal = true
		n.score = scoreTerminal(state, rolloutCfg)
		n.value = n.score
	}
	return n
}

// expand — 列出当前节点的所有 actions, 初始化 q/n + NN policy prior
func (n *mctsNode) expand(cfg MCTSConfig) {
	if n.expanded || n.terminal {
		return
	}
	n.actions = enumerateActions(n.state, n.dealt, n.round)
	n.children = make([]*mctsNode, len(n.actions))
	n.q = make([]float32, len(n.actions))
	n.n = make([]int, len(n.actions))
	n.policyPrior = make([]float32, len(n.actions))
	n.penalties = make([]float32, len(n.actions))

	if len(n.actions) == 0 {
		n.expanded = true
		return
	}

	// Compute policy prior + value via NN.
	// 对每个候选: apply action 得 post-state, 评 NN 得 (value, policyLogit).
	// 跨候选 softmax(policyLogit) → policy prior.
	// 当前节点 value = root candidate values 的 max (or NN of pre-action state).
	// 同时记录 ConnectorSplitPenalty (R1 only), 在 PUCT q 里减去.
	policyLogits := make([]float32, len(n.actions))
	hasPolicy := false
	bestPostValue := float32(-1e18)
	for i := range n.actions {
		tmp := n.state.Clone()
		applyAction(tmp, n.dealt, n.actions[i])
		v, _, _, plogit, hp := TrainedEvalFull(tmp)
		policyLogits[i] = plogit
		if hp {
			hasPolicy = true
		}
		// R1 only: 跟 ExpertPlace5 一致 penalty + bonus
		if n.round == 1 {
			pl := n.actions[i].placement
			n.penalties[i] = ConnectorSplitPenalty(pl, n.dealt) + R1FourInRowPenalty(pl, n.dealt) + R1IncoherentRowPenalty(pl, n.dealt) + R1TopNonAKXPenalty(pl, n.dealt, n.state) + R1TopKWhenJokerAFishPenalty(pl, n.dealt, n.state) - R1SameSuitInRowBonus(pl, n.dealt)
		}
		vNet := v - n.penalties[i]
		if vNet > bestPostValue {
			bestPostValue = vNet
		}
	}
	if hasPolicy {
		// Softmax across candidates
		maxL := policyLogits[0]
		for _, l := range policyLogits[1:] {
			if l > maxL {
				maxL = l
			}
		}
		var sumExp float32
		for i := range policyLogits {
			policyLogits[i] = expf32(policyLogits[i] - maxL) // numerical stability
			sumExp += policyLogits[i]
		}
		for i := range policyLogits {
			n.policyPrior[i] = policyLogits[i] / sumExp
		}
	} else {
		// Uniform prior fallback (no policy head in ckpt)
		uniform := float32(1.0) / float32(len(n.actions))
		for i := range n.policyPrior {
			n.policyPrior[i] = uniform
		}
	}
	n.expanded = true

	// Node value: NN value of best post-action state (for selectAction default Q)
	n.value = bestPostValue

	// MctsInitRollouts: 每候选跑 N0 rollouts 给稳定 starting Q (避免 PUCT 早期锁死)
	if MctsInitRollouts > 0 && len(n.actions) > 0 {
		if MctsParallelInit {
			n.parallelInitRollouts(cfg)
		} else {
			er := &ExpertRollout{Rng: cfg.Rng, Cfg: *cfg.RolloutCfg}
			for i := range n.actions {
				tmp := n.state.Clone()
				applyAction(tmp, n.dealt, n.actions[i])
				var sum float32
				for k := 0; k < MctsInitRollouts; k++ {
					if tmp.IsComplete() {
						sum += scoreTerminal(tmp, cfg.RolloutCfg)
						continue
					}
					rollState := tmp.Clone()
					sum += er.QuickRollout(rollState, n.round)
				}
				n.q[i] = sum / float32(MctsInitRollouts)
				n.n[i] = MctsInitRollouts
			}
			n.nTotal = MctsInitRollouts * len(n.actions)
		}
	}
}

// parallelInitRollouts — 并行跑每候选 K rollouts. goroutine pool 数 = NumCPU.
// 每 worker 自己 RNG (避 math/rand 非线程安全), 跑完汇总平均.
func (n *mctsNode) parallelInitRollouts(cfg MCTSConfig) {
	K := MctsInitRollouts
	N := len(n.actions)
	totalJobs := K * N

	// (action_idx, rollout_idx) pair → 跑后存 results[i][k]
	type job struct {
		actionIdx, rolloutIdx int
	}
	jobs := make(chan job, totalJobs)
	for i := 0; i < N; i++ {
		for k := 0; k < K; k++ {
			jobs <- job{i, k}
		}
	}
	close(jobs)

	results := make([][]float32, N)
	for i := range results {
		results[i] = make([]float32, K)
	}

	numWorkers := runtime.NumCPU()
	if numWorkers > totalJobs {
		numWorkers = totalJobs
	}

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		// 用 cfg.Rng derive 每 worker 独立 seed (保 deterministic 跨 worker)
		seed := cfg.Rng.Int63()
		go func(workerSeed int64) {
			defer wg.Done()
			workerRng := rand.New(rand.NewSource(workerSeed))
			workerEr := &ExpertRollout{Rng: workerRng, Cfg: *cfg.RolloutCfg}
			for j := range jobs {
				tmp := n.state.Clone()
				applyAction(tmp, n.dealt, n.actions[j.actionIdx])
				if tmp.IsComplete() {
					results[j.actionIdx][j.rolloutIdx] = scoreTerminal(tmp, cfg.RolloutCfg)
				} else {
					results[j.actionIdx][j.rolloutIdx] = workerEr.QuickRollout(tmp, n.round)
				}
			}
		}(seed)
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		var sum float32
		for _, v := range results[i] {
			sum += v
		}
		n.q[i] = sum / float32(K)
		n.n[i] = K
	}
	n.nTotal = K * N
}

// expf32 — float32 wrapper of math.Exp
func expf32(x float32) float32 {
	if x > 30 {
		return 1e13
	}
	if x < -30 {
		return 0
	}
	// Taylor 起步 (跟 sigmoid expNeg 同级精度即可)
	return float32(expNegMath(-float64(x)))
}

func expNegMath(x float64) float64 {
	// 复用现有 expNeg
	return 1.0 / expNeg(x)
}

// simulate — 跑一次 MCTS simulation: select → expand → ROLLOUT → backup
//
// 关键: 在 leaf (newly expanded child) 不直接返回 NN value, 而是跑 random rollout
// 到终局, 返回 rollout 终局 score. 这是经典 MCTS 做法, NN 只在 root level 提供 prior
// (Phase 1-2 因为 prior uniform, NN 实际在 leaf 不参与; Phase 3+ 加 NN policy 后 prior 起作用).
//
// 为啥不用 NN value 当 leaf return: NN 训练数据有限, 对 partial state 预测可能 biased
// (e.g., v14 NN 给 KK→mid 高分但实际必爆). Rollout 直接看 game 终局, 无 bias.
func simulate(node *mctsNode, cfg MCTSConfig) float32 {
	// Terminal: 直接返回 score
	if node.terminal {
		return node.score
	}

	// Not expanded: expand + ROLLOUT to terminal (经典 MCTS leaf evaluation)
	if !node.expanded {
		node.expand(cfg)
		return rolloutToTerminal(node, cfg)
	}

	// Select action by PUCT (or UCB1 since uniform prior)
	actionIdx := selectAction(node, cfg)
	action := node.actions[actionIdx]

	// 应用 action 得新 state
	childState := node.state.Clone()
	applyAction(childState, node.dealt, action)
	nextRound := node.round + 1

	// 检查是否已完整 (R5 done)
	if childState.IsComplete() {
		v := scoreTerminal(childState, cfg.RolloutCfg)
		updateNode(node, actionIdx, v)
		return v
	}

	// 进入下一 round: 需要随机抽 dealt
	var nextDealt []Card
	if nextRound > 5 {
		updateNode(node, actionIdx, -cfg.RolloutCfg.FoulCost)
		return -cfg.RolloutCfg.FoulCost
	}

	nextDealt = sampleNextDealt(childState, nextRound, cfg.Rng)
	if len(nextDealt) == 0 {
		updateNode(node, actionIdx, -cfg.RolloutCfg.FoulCost)
		return -cfg.RolloutCfg.FoulCost
	}

	// Recurse on new child (chance-node sampling each time, no tree reuse for chance branches)
	child := newMCTSNode(childState, nextDealt, nextRound, cfg.RolloutCfg)
	v := simulate(child, cfg)

	updateNode(node, actionIdx, v)
	return v
}

// rolloutToTerminal — 从给定 node 起, RANDOM first action + MLP-greedy 后续, 跑到 game end.
//
// 关键设计: First action 用 RANDOM (从合法 actions 均匀采样), 不是 MLP-greedy.
// Why: 如果 MLP 有偏见 (e.g., v14 给 KK→mid 高分), MLP-greedy first 会把偏见传到 rollout 终局.
// Random first 让每个 root candidate 在 leaf 看到无偏的 future, MCTS Q 估计可信.
//
// 后续 round 用 MLP-greedy (QuickRollout) 因为 deep tree exploration 不可能, MLP 是合理 baseline policy.
// Chance-deal 随机性自然提供变量.
// MctsLeafRollouts — 每 leaf 跑 K 次 rollout 平均, 降单次噪声 (σ≈50 → σ/√K)
// 默认 1 (单 rollout, 老行为). env MCTS_LEAF_ROLLOUTS 设. 推荐 3-5.
var MctsLeafRollouts = 1

// MctsInitRollouts — expand() 时给每个 candidate 跑 N0 rollouts 当 starting Q
// 避免单次 unlucky rollout 把候选永久埋葬 (PUCT lock-out 问题).
// 默认 0 (关闭). 推荐 20-50 让 starting Q 稳.
var MctsInitRollouts = 0

// MctsParallelInit — 并行 init rollouts (goroutine pool). 默认 true (Mac 8 核 4-6x 加速).
// 关掉用于诊断 (false → 串行).
var MctsParallelInit = true

func rolloutToTerminal(node *mctsNode, cfg MCTSConfig) float32 {
	er := &ExpertRollout{
		Rng: cfg.Rng,
		Cfg: *cfg.RolloutCfg,
	}
	K := MctsLeafRollouts
	if K < 1 {
		K = 1
	}
	var sum float32
	for k := 0; k < K; k++ {
		tmpState := node.state.Clone()
		// RANDOM first action (uniform 在合法 actions 中)
		if !node.terminal && len(node.actions) > 0 {
			idx := cfg.Rng.Intn(len(node.actions))
			applyAction(tmpState, node.dealt, node.actions[idx])
		}
		if tmpState.IsComplete() {
			sum += scoreTerminal(tmpState, cfg.RolloutCfg)
		} else {
			// QuickRollout 从 node.round 后续 (MLP-greedy + structural rules + deal sampling)
			sum += er.QuickRollout(tmpState, node.round)
		}
	}
	return sum / float32(K)
}

// selectAction — PUCT 选 action: argmax(Q - penalty + cPuct * P * sqrt(N) / (1+n))
// penalty: ConnectorSplitPenalty 等 (R1), 让 MCTS 跟 ExpertPlace5 一致避开拆连张候选.
func selectAction(node *mctsNode, cfg MCTSConfig) int {
	bestIdx := 0
	bestScore := float32(-1e18)
	sqrtN := float32(math.Sqrt(float64(node.nTotal + 1)))

	for i := range node.actions {
		var q float32
		if node.n[i] > 0 {
			q = node.q[i]
		} else {
			// Optimistic init: 用父节点 value 当未访问 action 的预期值
			q = node.value
		}
		exploration := cfg.CPuct * node.policyPrior[i] * sqrtN / float32(1+node.n[i])
		score := q - node.penalties[i] + exploration
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

// updateNode — 把 value v backup 到 node 的 action[idx]
func updateNode(node *mctsNode, idx int, v float32) {
	node.n[idx]++
	node.nTotal++
	// Running mean
	node.q[idx] += (v - node.q[idx]) / float32(node.n[idx])
}

// enumerateActions — 列出 round 的所有合法 actions
func enumerateActions(state *GameState, dealt []Card, round int) []mctsAction {
	var actions []mctsAction
	if round == 1 {
		placements := GenerateRound1Actions(dealt, state)
		seen := make(map[string]bool, len(placements))
		// 候选去重 + 收集 (placement, gs) pairs 给 hard rule filter
		type cand struct {
			placement []Row
			gs        *GameState
		}
		cands := make([]cand, 0, len(placements))
		for _, p := range placements {
			tmp := state.Clone()
			for i, c := range dealt {
				tmp.PlaceCard(c, p[i])
			}
			key := stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			pCopy := make([]Row, len(p))
			copy(pCopy, p)
			cands = append(cands, cand{pCopy, tmp})
		}
		// Hard rule filter (跟 ExpertPlace5 一致)
		if !HardRulesDisabled && len(cands) > 0 {
			r1c := make([]R1Cand, len(cands))
			for i, c := range cands {
				r1c[i] = R1Cand{Placement: c.placement, GS: c.gs}
			}
			r1c = ApplyHardRulesR1(r1c, dealt, state)
			if len(r1c) < len(cands) {
				keep := make(map[string]bool, len(r1c))
				for _, c := range r1c {
					keep[stateKey(c.GS)] = true
				}
				filtered := make([]cand, 0, len(r1c))
				for _, c := range cands {
					if keep[stateKey(c.gs)] {
						filtered = append(filtered, c)
					}
				}
				cands = filtered
			}
		}
		for _, c := range cands {
			actions = append(actions, mctsAction{round: 1, placement: c.placement})
		}
	} else {
		raw := GenerateRoundNActions(dealt, state)
		seen := make(map[string]bool, len(raw))
		type candN struct {
			discardIdx int
			kept       []Card
			placement  []Row
			gs         *GameState
		}
		cands := make([]candN, 0, len(raw))
		for i := range raw {
			a := &raw[i]
			tmp := state.Clone()
			tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
			tmp.SetDiscard(dealt[a.DiscardIdx]) // V3 N/N2 features
			for k, c := range a.Kept {
				tmp.PlaceCard(c, a.Placement[k])
			}
			key := dealt[a.DiscardIdx].ID() + "|" + stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			keptCopy := make([]Card, len(a.Kept))
			copy(keptCopy, a.Kept)
			placeCopy := make([]Row, len(a.Placement))
			copy(placeCopy, a.Placement)
			cands = append(cands, candN{a.DiscardIdx, keptCopy, placeCopy, tmp})
		}
		// Hard rule filter R2-R5
		if !HardRulesDisabled && len(cands) > 0 {
			rnc := make([]RNCand, len(cands))
			for i, c := range cands {
				rnc[i] = RNCand{
					Action: &RoundNAction{DiscardIdx: c.discardIdx, Kept: c.kept, Placement: c.placement},
					GS:     c.gs,
				}
			}
			rnc = ApplyHardRulesRN(rnc, dealt, state)
			if len(rnc) < len(cands) {
				keep := make(map[string]bool, len(rnc))
				for _, c := range rnc {
					keep[dealt[c.Action.DiscardIdx].ID()+"|"+stateKey(c.GS)] = true
				}
				filtered := make([]candN, 0, len(rnc))
				for _, c := range cands {
					key := dealt[c.discardIdx].ID() + "|" + stateKey(c.gs)
					if keep[key] {
						filtered = append(filtered, c)
					}
				}
				cands = filtered
			}
		}
		for _, c := range cands {
			actions = append(actions, mctsAction{
				round:      round,
				discardIdx: c.discardIdx,
				kept:       c.kept,
				placement:  c.placement,
			})
		}
	}
	return actions
}

// applyAction — 把 action 应用到 state (会修改 state)
func applyAction(state *GameState, dealt []Card, action mctsAction) {
	if action.round == 1 {
		for i, c := range dealt {
			state.PlaceCard(c, action.placement[i])
		}
	} else {
		state.UsedCards[dealt[action.discardIdx].ID()] = true
		state.SetDiscard(dealt[action.discardIdx]) // V3 features
		for k, c := range action.kept {
			state.PlaceCard(c, action.placement[k])
		}
	}
}

// ApplyMCTSAction — 跟 applyAction 同, 但 export 给 server 调用.
// 注意 mctsAction 是 unexported, server 通过 MCTSSearch 拿到后用此函数 apply.
// 接受 mctsAction 是因为 caller 通常是 ofc package 外, 但 type 又是 unexported.
// 折中: caller 把 search 结果 (mctsAction) 直接传回这里.
type MCTSAction = mctsAction

func ApplyMCTSAction(state *GameState, dealt []Card, action MCTSAction) {
	applyAction(state, dealt, action)
}

// sampleNextDealt — 从 state.UsedCards 之外随机抽 numCards (R2-R5 = 3)
func sampleNextDealt(state *GameState, round int, rng *rand.Rand) []Card {
	numCards := 3
	if round == 1 {
		numCards = 5
	}
	deck := MakeDeck(state.NumJokers)
	avail := make([]Card, 0, len(deck))
	for _, c := range deck {
		if !state.UsedCards[c.ID()] {
			avail = append(avail, c)
		}
	}
	if len(avail) < numCards {
		return nil
	}
	// Fisher-Yates 前 numCards 张
	for i := 0; i < numCards; i++ {
		j := i + rng.Intn(len(avail)-i)
		avail[i], avail[j] = avail[j], avail[i]
	}
	return avail[:numCards]
}
