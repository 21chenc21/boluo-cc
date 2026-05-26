package ofc

import (
	"math/rand"
)

// RolloutConfig — MC 模拟 + label 形成参数
//
// Fan bonus 按 top 进范牌型分级 (rollout 里, 用户价值函数):
//   - Trips on top: TripsFanBonus (默认 400)
//   - AA pair:      AAFanBonus    (默认 200)
//   - KK pair:      KKFanBonus    (默认 100)
//   - QQ pair:      QQFanBonus    (默认 50)
// Foul 扣分: FoulCost (默认 20)
//
// 这些 knob 直接编码到 silver-label 里 (mcScore = Royalties + fanBonus 或 -FoulCost),
// 训练时调高某类 → MLP 学到偏好. 不是 rollout policy bias, 是用户价值函数声明.
//
// Epsilon — rollout policy ε-greedy 探索率 (训练用, 推理建议 0).
//   每 step ε 概率 random pick action (而非 max-MLP), 让训练分布不被 MLP 当前认知锁死.
//   推荐 0.1 (10%); 0=纯 greedy.
type RolloutConfig struct {
	R1Mult        float32
	FoulCost      float32 // foul 扣分 (默认 6, 真实 head-to-head -6 net loss 对齐)
	QQFanBonus    float32 // QQ Fantasy 进范奖励 (默认 20, calibrated 24)
	KKFanBonus    float32 // KK Fantasy 进范奖励 (默认 40, calibrated 39)
	AAFanBonus    float32 // AA Fantasy 进范奖励 (默认 80, calibrated 64)
	TripsFanBonus float32 // Trips top 进范奖励 (默认 90, calibrated ~80-120)
	Epsilon       float32 // rollout ε-greedy 探索率 (0=纯 greedy)
	PureMLP       bool    // true → 跳过 MCTS rollout, 用纯 MLP prerank top-1 (per-request override). 2026-05-22 加, 修复 server 忽略 pureMLP 字段.
	// 2026-05-23: per-request top-K sample (R1 only; R2-R5 永远 top-1 保 endgame).
	// 0=top-1 deterministic (最强 = 难度 1), 2=top-2 随机 (中等 = 难度 2), 3=top-3 (简单 = 难度 3).
	TopKSampleR1 int
}

// DefaultRolloutConfig — 推理 / 老 ckpt 加载默认.
// 2026-05-15 重校 — calibration via pineapple-ofc/v7_fan/fantasy-calibrate.js
var DefaultRolloutConfig = RolloutConfig{
	R1Mult:        1.0,
	FoulCost:      6,
	QQFanBonus:    20,
	KKFanBonus:    40,
	AAFanBonus:    80,
	TripsFanBonus: 90,
	Epsilon:       0, // 推理 0 = 纯 greedy; 训练 CLI 可调 0.1
}

// IntnRNG — RNG 接口, 兼容 *rand.Rand 和测试用 LCG
type IntnRNG interface {
	Intn(n int) int
}

// LCG — 与 JS Math.random 替换可对齐的 (a=1664525, c=1013904223, m=2^32)
// 用于 parity 测试 (Go vs JS 同种子 → 同决策)
type LCG struct {
	State uint32
}

func NewLCG(seed uint32) *LCG { return &LCG{State: seed} }

// NextFloat — [0, 1)
func (l *LCG) NextFloat() float64 {
	l.State = l.State*1664525 + 1013904223
	return float64(l.State) / 4294967296.0
}

// Intn — Math.floor(NextFloat() * n), 与 JS `Math.floor(lcg() * n)` 一致
func (l *LCG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(l.NextFloat() * float64(n))
}

// ExpertRollout — 持有 RNG (interface 形式, 支持 LCG 注入)
type ExpertRollout struct {
	Rng     IntnRNG
	Cfg     RolloutConfig
	Verbose func(format string, args ...interface{}) // optional trace; nil 关闭

	// LastResult — Path X (v8-fan): 每次 QuickRollout 结束后填,
	// 让 stage3 能拿到 isFan / isFoul flag, 不只 royalty 数值.
	// 不并发安全 — 单 goroutine 串行 stage3 use.
	LastResult RolloutResult
}

// RolloutResult — 单次 rollout 的详细结果 (Path X)
type RolloutResult struct {
	RawRoyalty float32 // 不含 fantasy bonus 的纯 royalty (foul=0, incomplete=-10)
	IsFantasy  bool
	IsFoul     bool
	// FanBonus: cap-chain aware fantasy bonus (2026-05-19 加). IsFantasy=true 时填,
	// 否则 0. 替代 caller 重算 classifyFanBonus (旧版 cap-down 误算).
	FanBonus float32
}

func (er *ExpertRollout) trace(format string, args ...interface{}) {
	if er.Verbose != nil {
		er.Verbose(format, args...)
	}
}

// NewExpertRollout — 用默认 cfg + 当前时间种子的 rng
func NewExpertRollout() *ExpertRollout {
	return &ExpertRollout{
		Rng: rand.New(rand.NewSource(rand.Int63())),
		Cfg: DefaultRolloutConfig,
	}
}

func (er *ExpertRollout) shuffle(deck []Card) {
	for i := len(deck) - 1; i > 0; i-- {
		j := er.Rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

func max3(a, b, c int) int {
	if a >= b && a >= c {
		return a
	}
	if b >= c {
		return b
	}
	return c
}

// QuickRollout — 从 state 跑一局到结束, 返回 royalty 终局分.
// foul → -20, 进范分级奖励, 未完成 → -10
// (与 JS quickRollout 完全 parity)
func (er *ExpertRollout) QuickRollout(state *GameState, currentRound int) float32 {
	gs := state.Clone()
	deck := gs.GetRemainingDeck()
	er.shuffle(deck)
	deckIdx := 0

	for round := currentRound + 1; round <= 5; round++ {
		if gs.IsComplete() {
			break
		}
		numCards := 3
		if deckIdx+numCards > len(deck) {
			break
		}
		dealt := deck[deckIdx : deckIdx+numCards]
		deckIdx += numCards
		if er.Verbose != nil {
			ds := make([]string, len(dealt))
			for i, c := range dealt {
				ds[i] = c.String()
			}
			er.trace("R%d dealt=%v ", round, ds)
		}

		// 边界: 不可能完成
		slotsNow := gs.TotalSlots()
		roundsLeft := 5 - round
		if slotsNow > roundsLeft*2+2 {
			er.trace("→ EARLY-FOUL slotsNow=%d roundsLeft=%d\n", slotsNow, roundsLeft)
			er.LastResult = RolloutResult{RawRoyalty: 0, IsFantasy: false, IsFoul: true}
			return -er.Cfg.FoulCost
		}

		// 边界: 只剩 1-2 个空位, 只放 1 张
		if slotsNow <= 1 {
			row := RowMiddle
			if gs.MidSlots() == 0 {
				row = RowBottom
			}
			if gs.MidSlots() == 0 && gs.BotSlots() == 0 {
				row = RowTop
			}
			// ε-greedy: 探索时随机选 dealt 一张
			if er.Cfg.Epsilon > 0 && er.Rng.Intn(1000) < int(er.Cfg.Epsilon*1000) {
				pickIdx := er.Rng.Intn(len(dealt))
				gs.PlaceCard(dealt[pickIdx], row)
				for _, c := range dealt {
					gs.UsedCards[c.ID()] = true
				}
				continue
			}
			var bestCard Card
			bestS := float32(-1e30)
			haveBest := false
			for _, c := range dealt {
				tmp := gs.Clone()
				tmp.PlaceCard(c, row)
				s := TrainedEval(tmp)
				if !haveBest || s > bestS {
					bestS = s
					bestCard = c
					haveBest = true
				}
			}
			if haveBest {
				gs.PlaceCard(bestCard, row)
				for _, c := range dealt {
					gs.UsedCards[c.ID()] = true
				}
				er.trace("→ slotsNow<=1 path: placed %s on %s (s=%.4f)\n", bestCard.String(), row, bestS)
			}
			continue
		}

		// 贪心放置: 纯 MLP-greedy + ε-greedy 探索 (训练分布扩展).
		// 用户价值函数通过 fan/foul bonus 编码到 label, 不在此干预.
		actions := GenerateRoundNActions(dealt, gs)
		var best *RoundNAction
		bestS := float32(-1e30)

		// ε-greedy: 探索时随机选 action, 否则跑 MLP 全 candidates 选 max
		if er.Cfg.Epsilon > 0 && len(actions) > 0 && er.Rng.Intn(1000) < int(er.Cfg.Epsilon*1000) {
			best = &actions[er.Rng.Intn(len(actions))]
		} else {
			for i := range actions {
				action := &actions[i]
				tmp := gs.Clone()
				tmp.UsedCards[dealt[action.DiscardIdx].ID()] = true
				tmp.SetDiscard(dealt[action.DiscardIdx]) // V3 features
				for k, c := range action.Kept {
					tmp.PlaceCard(c, action.Placement[k])
				}
				s := TrainedEval(tmp)
				if s > bestS {
					bestS = s
					best = action
				}
			}
		}
		if best != nil {
			gs.UsedCards[dealt[best.DiscardIdx].ID()] = true
			gs.SetDiscard(dealt[best.DiscardIdx]) // V3 features
			for k, c := range best.Kept {
				gs.PlaceCard(c, best.Placement[k])
			}
			if er.Verbose != nil {
				ks := make([]string, len(best.Kept))
				for k, c := range best.Kept {
					ks[k] = c.String() + "→" + best.Placement[k].String()
				}
				er.trace("→ disc=%s, kept=%v (s=%.4f)\n", dealt[best.DiscardIdx].String(), ks, bestS)
			}
		}
	}

	if gs.IsComplete() {
		score := gs.Score()
		if score.Foul {
			er.LastResult = RolloutResult{RawRoyalty: 0, IsFantasy: false, IsFoul: true}
			return -er.Cfg.FoulCost
		}
		raw := float32(score.Royalties)
		// cap-chain aware fan bonus (2026-05-19 fix, 跟 game.js 同 bug 修复).
		// 旧版手算 jokerCnt + realMax 找 pairR (rank-greedy), joker 被 cap 时多算 bonus.
		fanBonus := float32(0)
		if score.Fantasy {
			fanBonus, _ = FantasyBonusFromBoard(gs.Top, gs.Middle, gs.Bottom,
				er.Cfg.QQFanBonus, er.Cfg.KKFanBonus, er.Cfg.AAFanBonus, er.Cfg.TripsFanBonus)
		}
		er.LastResult = RolloutResult{RawRoyalty: raw, IsFantasy: score.Fantasy, IsFoul: false, FanBonus: fanBonus}
		if score.Fantasy {
			return raw + fanBonus
		}
		return raw
	}
	er.LastResult = RolloutResult{RawRoyalty: -10, IsFantasy: false, IsFoul: false}
	return -10
}

// QuickRolloutDetailed — Path X (v8-fan): 直接调用 QuickRollout 然后读 LastResult.
// 注意: 单 goroutine 内调用; 并发用每个 goroutine 各自的 ExpertRollout 实例.
// 在 foul 提前返回路径 (slotsNow > roundsLeft*2+2) 也覆盖了 — return -20 路径.
func (er *ExpertRollout) QuickRolloutDetailed(state *GameState, currentRound int) (rawRoyalty float32, isFan bool, isFoul bool) {
	er.QuickRollout(state, currentRound)
	r := er.LastResult
	return r.RawRoyalty, r.IsFantasy, r.IsFoul
}

