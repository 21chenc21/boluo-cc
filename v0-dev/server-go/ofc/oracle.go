package ofc

import "strconv"

// OracleSolve — 完美信息 oracle: 给定 partial state + 已知未来 dealt 序列 + 价值函数 cfg,
// 返回最优摆法的最终分 (royalty + fanBonus[type] (if !foul) - foulCost (if foul)).
//
// 用 recursive max-search + per-call memoization (cache 跨整个 search 共享).
//
// futureRounds[i] = round (current_round+i+1) 发的 3 张牌. 长度应等于剩余 round 数.
//   - 若 partial state 当前 round=R, 则 futureRounds 应有 5-R 个 entry (R+1..5).
//   - 若已 complete, futureRounds 可为空.
//
// 返回的 score = ScoreHand royalty (foul=0) + fanBonus or -foulCost.
//
// Joker: 完全交给 ScoreHand 的 cap-chain 处理. Oracle 内部不做特殊 wild 逻辑.
func OracleSolve(gs *GameState, futureRounds [][]Card, cfg *RolloutConfig) float32 {
	cache := newOracleCache()
	return solveImpl(gs, futureRounds, cache, cfg)
}

// oracleCache — per-OracleSolve 内部 memoize. Key = stateKey + future-tail-len.
// 因为 futureRounds 在 solve 调用内固定, 同一 partial state 在不同分支重现时
// 后续 future 相同, 所以 (stateKey, futureLen) 可唯一定 sub-problem.
type oracleCache struct {
	table map[string]float32
}

func newOracleCache() *oracleCache {
	return &oracleCache{table: make(map[string]float32, 4096)}
}

// solveImpl — 递归 max search.
func solveImpl(gs *GameState, futureRounds [][]Card, cache *oracleCache, cfg *RolloutConfig) float32 {
	// 终止: 完整 state
	if gs.IsComplete() {
		return scoreTerminal(gs, cfg)
	}
	// 防御: 没有未来牌但 state 不完整 (理论上不该发生)
	if len(futureRounds) == 0 {
		return -cfg.FoulCost
	}

	// Memoize check
	key := stateKey(gs) + "|" + strconv.Itoa(len(futureRounds))
	if v, ok := cache.table[key]; ok {
		return v
	}

	dealt := futureRounds[0]
	rest := futureRounds[1:]

	best := float32(-1e9)

	// 判断当前是 R1 (5 cards dealt) 还是 R2-R5 (3 cards dealt)
	if len(dealt) == 5 {
		// R1: 全摆, 无 discard
		placements := GenerateRound1Actions(dealt, gs)
		seen := make(map[string]bool, len(placements))
		for _, p := range placements {
			child := gs.Clone()
			for i, c := range dealt {
				child.PlaceCard(c, p[i])
			}
			k := stateKey(child)
			if seen[k] {
				continue
			}
			seen[k] = true
			v := solveImpl(child, rest, cache, cfg)
			if v > best {
				best = v
			}
		}
	} else {
		// R2-R5: 弃 1 摆 2
		actions := GenerateRoundNActions(dealt, gs)
		seen := make(map[string]bool, len(actions))
		for i := range actions {
			a := &actions[i]
			child := gs.Clone()
			child.UsedCards[dealt[a.DiscardIdx].ID()] = true
			child.SetDiscard(dealt[a.DiscardIdx]) // V3 features
			for k, c := range a.Kept {
				child.PlaceCard(c, a.Placement[k])
			}
			// dedup key 含 discard card (不同 discard → 不同 future deck, 但 future 已知所以
			// state 相同的话 score 一定相同; 用 stateKey + discard-id 避免重复 search)
			dk := dealt[a.DiscardIdx].ID() + "|" + stateKey(child)
			if seen[dk] {
				continue
			}
			seen[dk] = true
			v := solveImpl(child, rest, cache, cfg)
			if v > best {
				best = v
			}
		}
	}

	cache.table[key] = best
	return best
}

// scoreTerminal — 完整 state 的最终评分.
// 跟 rollout.go QuickRollout 末尾逻辑一致 (royalty + fan tier bonus or -foulCost).
func scoreTerminal(gs *GameState, cfg *RolloutConfig) float32 {
	score := gs.Score()
	if score.Foul {
		return -cfg.FoulCost
	}
	raw := float32(score.Royalties)
	if !score.Fantasy {
		return raw
	}
	// Fan tier: 分级奖励 (跟 rollout.go:246-291 一致)
	var realCnt [13]int
	jokerCnt := 0
	for _, c := range gs.Top {
		if c.IsJoker() {
			jokerCnt++
		} else {
			realCnt[c.Rank()]++
		}
	}
	realMax := 0
	for _, v := range realCnt {
		if v > realMax {
			realMax = v
		}
	}
	effMax := realMax + jokerCnt
	if effMax >= 3 {
		return raw + cfg.TripsFanBonus
	}
	pairR := -1
	if jokerCnt >= 1 {
		for r := 12; r >= 0; r-- {
			if realCnt[r] > 0 {
				pairR = r
				break
			}
		}
		if pairR < 0 && jokerCnt >= 2 {
			pairR = 12
		}
	} else {
		for r := 12; r >= 0; r-- {
			if realCnt[r] >= 2 {
				pairR = r
				break
			}
		}
	}
	if pairR >= 12 {
		return raw + cfg.AAFanBonus
	}
	if pairR >= 11 {
		return raw + cfg.KKFanBonus
	}
	return raw + cfg.QQFanBonus
}

// OracleResult — 单次 oracle solve 的详细结果 (label + 终局信息).
// 用于 dataset gen 时同时收集 fanRate / foulRate.
type OracleResult struct {
	Score     float32
	IsFantasy bool
	IsFoul    bool
}

// OracleSolveDetailed — 跟 OracleSolve 同, 但额外返回最优终局的 fan/foul flag.
// 这是 single-future 版的辅助 API; multi-future 时 caller 自己累计 K 次结果.
func OracleSolveDetailed(gs *GameState, futureRounds [][]Card, cfg *RolloutConfig) OracleResult {
	cache := newOracleCache()
	score := solveImpl(gs, futureRounds, cache, cfg)
	// 找到最优终局的具体状态: 通过 reconstruction (再 trace 一遍)
	bestState := traceBest(gs, futureRounds, cache, cfg)
	if bestState != nil && bestState.IsComplete() {
		s := bestState.Score()
		return OracleResult{Score: score, IsFantasy: s.Fantasy, IsFoul: s.Foul}
	}
	// 不完整 (异常) 或追溯失败, 用 score 推 fan/foul
	return OracleResult{Score: score, IsFantasy: false, IsFoul: score <= -cfg.FoulCost+0.001}
}

// traceBest — 从 cache 中追溯最优路径, 返回最优终局 state.
// Memoize cache 已存了每个 sub-problem 最优分, 这里 recursive 找 argmax.
func traceBest(gs *GameState, futureRounds [][]Card, cache *oracleCache, cfg *RolloutConfig) *GameState {
	if gs.IsComplete() {
		return gs
	}
	if len(futureRounds) == 0 {
		return nil
	}
	dealt := futureRounds[0]
	rest := futureRounds[1:]

	if len(dealt) == 5 {
		placements := GenerateRound1Actions(dealt, gs)
		seen := make(map[string]bool, len(placements))
		var bestState *GameState
		bestScore := float32(-1e9)
		for _, p := range placements {
			child := gs.Clone()
			for i, c := range dealt {
				child.PlaceCard(c, p[i])
			}
			k := stateKey(child)
			if seen[k] {
				continue
			}
			seen[k] = true
			v := solveImpl(child, rest, cache, cfg)
			if v > bestScore {
				bestScore = v
				bestState = child
			}
		}
		if bestState == nil {
			return nil
		}
		return traceBest(bestState, rest, cache, cfg)
	}

	actions := GenerateRoundNActions(dealt, gs)
	seen := make(map[string]bool, len(actions))
	var bestState *GameState
	bestScore := float32(-1e9)
	for i := range actions {
		a := &actions[i]
		child := gs.Clone()
		child.UsedCards[dealt[a.DiscardIdx].ID()] = true
		for k, c := range a.Kept {
			child.PlaceCard(c, a.Placement[k])
		}
		dk := dealt[a.DiscardIdx].ID() + "|" + stateKey(child)
		if seen[dk] {
			continue
		}
		seen[dk] = true
		v := solveImpl(child, rest, cache, cfg)
		if v > bestScore {
			bestScore = v
			bestState = child
		}
	}
	if bestState == nil {
		return nil
	}
	return traceBest(bestState, rest, cache, cfg)
}
