// v0 训练 CLI — Go 实现的 silver-label 训练管线.
// 跟 v7/train-loop.js 等价 (R1 stage1 simpleEval 排序 + N-sim rollout label),
// 但用 Go 跑 (Mac M 系列单核已经比 Node 快 ~3-5x).
//
// 用法:
//   ./ofc-train -hours 4 -workers 4 -sims 200 -jokers 2 \
//     -ckpt-out checkpoints/round-NNN.json \
//     -weights start.json
//
// 阶段 1: 单核, 单头 (跟 v7/train-loop.js parity).
// 阶段 2-3: 加 -workers + 多头 + 扩 hidden, 同一 binary 不同 flag.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/boluo/v0-server/ofc"
)

// ============================================================
// CLI args
// ============================================================
var (
	totalHours  = flag.Float64("hours", 4, "total training hours")
	roundMins   = flag.Float64("round-min", 60, "minutes per round")
	rolloutsPC  = flag.Int("sims", 100, "MC rollouts per candidate (silver label)")
	numJokers   = flag.Int("jokers", 2, "joker count in deck (0/2/4)")
	workers     = flag.Int("workers", 1, "parallel game generation workers (1=sequential)")
	weightsIn   = flag.String("weights", "", "starting MLP weights (default: ofc embed)")
	ckptOutDir  = flag.String("ckpt-dir", "checkpoints", "ckpt output dir")
	samplesDir  = flag.String("samples-dir", "samples", "samples persistence dir")
	policyVer   = flag.String("policy", "v0-go-v1", "POLICY_VERSION (排除其它版本 priors)")
	maxSamples  = flag.Int("max-samples", 500000, "cap total samples in MLP train")
	trainEpochs = flag.Int("epochs", 80, "MLP train epochs per round")
	verbose     = flag.Bool("v", false, "verbose")

	// 阶段 3: 架构
	hiddenH1   = flag.Int("h1", 128, "hidden layer 1 size (default 128, 256 推荐多头时, 512 大模型)")
	hiddenH2   = flag.Int("h2", 64, "hidden layer 2 size (default 64, 128 推荐多头时, 256 大模型)")
	hiddenH3   = flag.Int("h3", 0, "hidden layer 3 size (0 = 2-hidden legacy; 128 = 3-hidden 大模型)")
	outDim       = flag.Int("outdim", 1, "output heads: 1=royalty only (legacy), 3=royalty+fan+foul, 4=+policy (AlphaZero)")
	fanWeight    = flag.Float64("fan-w", 0.15, "multi-head: fan BCE loss weight (vs royalty MSE 1.0)")
	foulWeight   = flag.Float64("foul-w", 0.15, "multi-head: foul BCE loss weight")
	policyWeight = flag.Float64("policy-w", 0.30, "AlphaZero: policy BCE loss weight (only if outdim >= 4)")

	// 用户价值函数 — fan/foul bonus 直接编码进 silver-label.
	// 调高某 fan 类型 → 训练 label 偏好该路径; foul-cost 控制风险厌恶.
	foulCost      = flag.Float64("foul-cost", 6, "foul penalty in rollout label")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 20, "QQ Fantasy bonus in rollout label")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 40, "KK Fantasy bonus in rollout label")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 100, "AA Fantasy bonus in rollout label")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 120, "Trips top Fantasy bonus in rollout label")

	// Rollout ε-greedy 探索率 — 训练分布广度. 0=纯 MLP-greedy (推理默认), 0.1=10% 随机探索 (训练推荐).
	// 让 rollout policy 不被 MLP 当前认知锁死, 让 KK→bot 等"当前 MLP 低估"路径有机会被采样.
	rolloutEpsilon = flag.Float64("rollout-epsilon", 0.1, "rollout policy ε-greedy exploration rate (0=pure greedy, 0.1=recommended for training)")

	// Phantom usedCards 注入 — 结构化模拟多人游戏对手可见牌, 修 deck-aware feature OOD.
	//
	// Pineapple OFC 弃牌面朝下 → 对手只能看到 placed 牌 (R1=5, R2-R5=2/round).
	// 每局随机选: opponents ∈ [0, max], slot ∈ [0, opponents].
	//   opponents=0 → phantom=0 (单 player / 对手都在 Fantasyland 不可见)
	//   opponents=1 → 2-player 顺序模型
	//   opponents=2 → 3-player 顺序模型 (default max)
	//
	// phantom 公式 (slot s, opponents O, round R):
	//   决策时 phantom = before*cardsDoneR + after*cardsDoneR1
	//     before = s (在我之前已完成 R 的对手数)
	//     after  = O - s (在我之后, 只完成 R-1 的对手数)
	//     cardsDoneR  = 5 + 2*(R-1)        (对手 R 完成的可见牌; R1=5, R≥2 加 2/轮)
	//     cardsDoneR1 = 5 + 2*(R-2) (R≥2)  (R-1 完成的可见牌)
	//                 = 0 (R=1)
	//
	// 例 (max=2):
	//   3-player: R1: 0/5/10 | R2: 10/12/14 | R5: 22/24/26
	//   2-player: R1: 0/5    | R2: 5/7      | R5: 13/15
	phantomOpponentsMax = flag.Int("phantom-opponents", 2, "max opponents to simulate per game (0=off; per-game ~Uniform[0, max])")

	// 输入维度. 78=v11 +AKQ-remaining, 89=v12 +全 rank +joker remaining, 90=v13 +partial-foul
	inDim = flag.Int("indim", 90, "input feature dim (90=v13 +partial-state foul; 89=v12; 78=v11)")

	// Oracle dataset mode: 若设置, 跳过 collectSamples, 从 dir 读 JSONL.gz oracle samples.
	// dir 结构: dir/round{1..5}/shard-NNNNN.jsonl.gz (gen-oracle-dataset 输出格式).
	// 默认自动 disable warm-start (oracle label 跟 rollout label 分布不同).
	// rollout-dataset (gen-rollout-dataset) label 兼容, 应该传 -dataset-keep-warm-start 覆盖.
	datasetDir = flag.String("dataset-dir", "", "if set, load samples from dir (skip rollout); 默认 auto-disables warm-start (oracle), 见 -dataset-keep-warm-start")

	// dataset-mode 下保留 warm-start (rollout-dataset 用; oracle-dataset 不要打开)
	datasetKeepWarm = flag.Bool("dataset-keep-warm-start", false, "dataset-dir 模式下保留 warm-start (rollout-compatible labels); oracle-dataset 必须留 false")

	// Warm-start: round 2+ 用前一 round 的 ckpt 当 MLP 初值, 而非 from-scratch.
	// 让训练真正 "持续迭代" — 每 round 在前 round 权重基础上 finetune.
	warmStart = flag.Bool("warm-start", true, "load previous round's ckpt as MLP init (round 2+); false = fresh MLP each round (legacy)")

	// Init-from-ckpt: round 1 时 MLP 从指定 ckpt warm-start (而非 NewMLP).
	// AlphaZero orchestrator 用此跨 iteration 累积学习 (每 iter 单 round, 但 init 自上 iter ckpt).
	initFromCkpt = flag.String("init-from-ckpt", "", "round 1 MLP init from this ckpt (default: fresh NewMLP)")

	// Warm-start LR multiplier — finetune 应该用更小 LR 不覆盖前 round 学习
	warmLRMult = flag.Float64("warm-lr-mult", 0.5, "warm-start finetune LR multiplier (default 0.5; round 2+ 用 base_lr * 此值)")
	baseLRFlag = flag.Float64("lr", 0.005, "base learning rate (online SGD); 大数据集 (>100K samples) 建议降到 0.001 或更低避免发散")
	yRecompute = flag.Bool("y-recompute", false, "warm-start 时直接替换 YMean/YStd (true) — 注意 W3 校准会暂时失效")
	yEMA       = flag.Float64("y-ema", 0, "warm-start 时 EMA 更新 YMean/YStd (0=不用 EMA; 0.3=新 30% 老 70%, 平滑过渡). 跟 -y-recompute 互斥 (优先用 EMA)")
)

// ============================================================
// Sample record
//   单头: 仅 Features + McScore (跟 v7-patient-v1 兼容)
//   多头: 加 FanRate / FoulRate (来自 ER.LastResult 的 N-rollout 累计)
// ============================================================
type Sample struct {
	Features     []float32 `json:"features"`
	McScore      float32   `json:"mcScore"`                // royalty mean (含 fantasy bonus, 跟 v7 一致)
	FanRate      float32   `json:"fanRate,omitempty"`      // 0-1, N-rollout 中 fantasy 比率
	FoulRate     float32   `json:"foulRate,omitempty"`     // 0-1, N-rollout 中 foul 比率
	PolicyTarget float32   `json:"policyTarget,omitempty"` // 0-1, AlphaZero policy: visit_count/total_visits at decision (per-candidate)
}

// ============================================================
// Game generation (1 worker, sequential)
// ============================================================

// phantomCountFor — 计算 (round, slot, opponents) 决策时 phantom usedCards 数量.
// Pineapple 规则: 弃牌面朝下 → 对手只 placed 可见 (R1=5/张, R2-R5=2/张).
// Slot 顺序 0..opponents (=opponents+1 player game), 每 round 内 P0→...→Pn.
//
// 通用公式 (任意 #opponents):
//   slot s, opponents O, round R, 决策时 phantom = sum over op != s:
//     if op < s (already played round R): 5 + 2*(R-1)
//     if op > s (last action was round R-1): 5 + 2*(R-2)  for R>=2; 0 for R=1
func phantomCountFor(round, slot, opponents int) int {
	if opponents <= 0 {
		return 0
	}
	totalPlayers := opponents + 1
	if slot >= totalPlayers {
		slot = totalPlayers - 1
	}
	cardsDoneR := 5 + 2*(round-1) // 对手 round R 完成的可见牌数
	cardsDoneR1 := 0              // 对手 R-1 完成 (R=1 时 = 0)
	if round >= 2 {
		cardsDoneR1 = 5 + 2*(round-2)
	}
	before := slot               // 在我之前的对手数量 (已完成 round R)
	after := opponents - before  // 在我之后的对手数量 (只完成 R-1)
	return before*cardsDoneR + after*cardsDoneR1
}

// genOneGame — 跑 5 round, 收集 R1-R5 决策 sample.
// 当前 ckpt 当 rollout policy. 每个候选 N-sim rollout, label = mean.
//
// Phantom 注入: 模拟 multi-player 游戏中其他玩家可见牌 (placed only, discard 面朝下隐藏).
// 每局随机 opponents ∈ [0, max], slot ∈ [0, opponents]:
//   opponents=0 → phantom=0 (单 player / 对手在 Fantasy 不可见)
//   opponents>0 → 顺序模型, 每 round 决策前增量加 phantom 牌进 UsedCards.
func genOneGame(rng *rand.Rand, evalCfg ofc.RolloutConfig) []Sample {
	state := ofc.NewGameState(*numJokers)
	deck := ofc.MakeDeck(*numJokers)
	shuffleDeck(deck, rng)
	out := make([]Sample, 0, 60)

	// 每局随机选 opponents 和 slot
	opponents := 0
	slot := 0
	if *phantomOpponentsMax > 0 {
		opponents = rng.Intn(*phantomOpponentsMax + 1) // [0, max]
		if opponents > 0 {
			slot = rng.Intn(opponents + 1) // [0, opponents]
		}
	}

	// 预算: R5 决策时最大 phantom 数. 从 deck 末尾保留这么多张.
	maxPhantom := phantomCountFor(5, slot, opponents)
	if len(deck)-maxPhantom < 17 { // player 需要 17 张 (R1=5, R2-R5 各 3)
		maxPhantom = len(deck) - 17
		if maxPhantom < 0 {
			maxPhantom = 0
		}
	}
	phantomReserveStart := len(deck) - maxPhantom
	phantomAdded := 0

	deckIdx := 0
	er := &ofc.ExpertRollout{Rng: rng, Cfg: evalCfg}

	for round := 1; round <= 5; round++ {
		state.Round = round

		// 增量注入当前 round 需要的 phantom 数
		want := phantomCountFor(round, slot, opponents)
		if want > maxPhantom {
			want = maxPhantom
		}
		for phantomAdded < want {
			state.UsedCards[deck[phantomReserveStart+phantomAdded].ID()] = true
			phantomAdded++
		}

		numCards := 3
		if round == 1 {
			numCards = 5
		}
		if deckIdx+numCards > phantomReserveStart {
			break
		}
		dealt := deck[deckIdx : deckIdx+numCards]
		deckIdx += numCards

		samplesFromR := genSamplesAndPlay(state, dealt, round, er, rng)
		out = append(out, samplesFromR...)
	}
	return out
}

// genSamplesAndPlay — 给定 round 摆牌. 收集每候选 sample. 同时把"最佳候选"应用到 state.
func genSamplesAndPlay(gs *ofc.GameState, dealt []ofc.Card, round int, er *ofc.ExpertRollout, rng *rand.Rand) []Sample {
	// 候选生成: R1 用 GenerateRound1Actions; R2-R5 用 GenerateRoundNActions (含 discard)
	type cand struct {
		applyFn func(*ofc.GameState) // 在 state clone 上应用此候选
		simEval *ofc.GameState       // 已应用候选的 partial state
		simple  float32              // simpleEval 排序
	}
	var cands []cand
	seen := make(map[string]bool)

	if round == 1 {
		actions := ofc.GenerateRound1Actions(dealt, gs)
		for _, p := range actions {
			tmp := gs.Clone()
			for i, c := range dealt {
				tmp.PlaceCard(c, p[i])
			}
			key := stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			pCopy := p
			cands = append(cands, cand{
				applyFn: func(s *ofc.GameState) {
					for i, c := range dealt {
						s.PlaceCard(c, pCopy[i])
					}
				},
				simEval: tmp,
				// 候选粗排用 MLP (TrainedEval), 跟 inference stage1 一致信号. 不再用 SimpleEval (heuristic).
				simple: ofc.TrainedEval(tmp),
			})
		}
	} else {
		actions := ofc.GenerateRoundNActions(dealt, gs)
		// 全部候选都丢进 rollout (含 joker-discard) — 让 MLP 自己学到 joker-discard = -EV.
		for i := range actions {
			a := &actions[i]
			tmp := gs.Clone()
			tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
			tmp.SetDiscard(dealt[a.DiscardIdx]) // V3 N/N2 features
			for k, c := range a.Kept {
				tmp.PlaceCard(c, a.Placement[k])
			}
			key := stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			aCopy := a
			cands = append(cands, cand{
				applyFn: func(s *ofc.GameState) {
					s.UsedCards[dealt[aCopy.DiscardIdx].ID()] = true
					s.SetDiscard(dealt[aCopy.DiscardIdx])
					for k, c := range aCopy.Kept {
						s.PlaceCard(c, aCopy.Placement[k])
					}
				},
				simEval: tmp,
				simple:  ofc.TrainedEval(tmp),
			})
		}
	}

	// 排序 TrainedEval, 留 top 50 (R1) / 全部 (R2-R5)
	// R1 候选可达 252 个, top-50 给"边界强 case" (A+joker→top 等当前 MLP 低估的) 进 sample pool 的机会.
	sort.Slice(cands, func(i, j int) bool { return cands[i].simple > cands[j].simple })
	keepN := len(cands)
	if round == 1 && keepN > 50 {
		keepN = 50
	}
	cands = cands[:keepN]

	// 每候选跑 N-sim rollout, 收集 royalty mean + fan/foul rate (多头用)
	out := make([]Sample, 0, len(cands))
	type scoredCand struct {
		c     cand
		score float32
	}
	scored := make([]scoredCand, len(cands))
	N := float32(*rolloutsPC)
	for i, c := range cands {
		var sum float32
		fanCount, foulCount := 0, 0
		for s := 0; s < *rolloutsPC; s++ {
			sum += er.QuickRollout(c.simEval, round)
			r := er.LastResult
			if r.IsFantasy {
				fanCount++
			}
			if r.IsFoul {
				foulCount++
			}
		}
		mean := sum / N
		fanRate := float32(fanCount) / N
		foulRate := float32(foulCount) / N
		// label = pure rollout mean (含 fan/foul bonus 通过 cfg knob 编码).
		// 不做 reshape — best-cand 选路 / MLP 训练 同一信号, 无 hack.
		scored[i] = scoredCand{c, mean}
		feat := ofc.BuildFeatures(c.simEval, *inDim)
		out = append(out, Sample{
			Features: feat, McScore: mean,
			FanRate: fanRate, FoulRate: foulRate,
		})
	}

	// 选最佳 → apply 到 state
	if len(scored) > 0 {
		sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
		scored[0].c.applyFn(gs)
	}
	return out
}

// ============================================================
// MLP training (支持单头 / 多头)
//
// 单头 (OutDim=1): output = royalty (跟 v7/train-loop.js 等价).
// 多头 (OutDim=3): output = [royalty, fan_logit, foul_logit].
//   royalty: MSE loss (target normalized by yMean/yStd)
//   fan/foul: BCE with logits (target = sample.FanRate / FoulRate, [0,1])
//   total loss = mse + fan_w*bce_fan + foul_w*bce_foul
//
// 推理时 (ofc/trained_eval.go) 自动 combine: royalty + α*sigmoid(fan) - β*sigmoid(foul)
// ============================================================

type MLP struct {
	InDim, H1, H2, H3, OutDim int // H3=0 → 2-hidden legacy; H3>0 → 3-hidden big
	Means, Stds               []float32
	W1                        [][]float32 // H1 × IN
	B1                        []float32
	W2                        [][]float32 // H2 × H1
	B2                        []float32
	W3                        [][]float32 // H3>0: H3 × H2 (middle). H3==0: OutDim × H2 (output, legacy).
	B3                        []float32   // H3>0: [H3]; H3==0: [OutDim]
	W4                        [][]float32 // H3>0: OutDim × H3 (output). H3==0: nil.
	B4                        []float32   // H3>0: [OutDim]; H3==0: nil
	YStd, YMean               float32
	TaskWeights               []float32

	// 训练用的 scratch buffers
	bufXn      []float32
	bufH1      []float32
	bufH2      []float32
	bufH3      []float32 // H3>0 only
	bufOut     []float32
	bufDOut    []float32
	bufGradH1  []float32
	bufGradH2  []float32
	bufGradH3  []float32 // H3>0 only
	bufGradW1  [][]float32
	bufGradW2  [][]float32
	bufGradW3  [][]float32
	bufGradW4  [][]float32 // H3>0 only
}

// allocTrainBufs — 在训练前一次性 alloc, 重用于每个 sample
func (m *MLP) allocTrainBufs() {
	m.bufXn = make([]float32, m.InDim)
	m.bufH1 = make([]float32, m.H1)
	m.bufH2 = make([]float32, m.H2)
	m.bufOut = make([]float32, m.OutDim)
	m.bufDOut = make([]float32, m.OutDim)
	m.bufGradH1 = make([]float32, m.H1)
	m.bufGradH2 = make([]float32, m.H2)
	m.bufGradW1 = make([][]float32, m.H1)
	for i := range m.bufGradW1 {
		m.bufGradW1[i] = make([]float32, m.InDim)
	}
	m.bufGradW2 = make([][]float32, m.H2)
	for i := range m.bufGradW2 {
		m.bufGradW2[i] = make([]float32, m.H1)
	}
	if m.H3 > 0 {
		m.bufH3 = make([]float32, m.H3)
		m.bufGradH3 = make([]float32, m.H3)
		// W3 grads: H3 × H2 (middle layer)
		m.bufGradW3 = make([][]float32, m.H3)
		for i := range m.bufGradW3 {
			m.bufGradW3[i] = make([]float32, m.H2)
		}
		// W4 grads: OutDim × H3 (output layer)
		m.bufGradW4 = make([][]float32, m.OutDim)
		for i := range m.bufGradW4 {
			m.bufGradW4[i] = make([]float32, m.H3)
		}
	} else {
		// 2-hidden legacy: W3 grads = OutDim × H2 (output)
		m.bufGradW3 = make([][]float32, m.OutDim)
		for i := range m.bufGradW3 {
			m.bufGradW3[i] = make([]float32, m.H2)
		}
	}
}

// NewMLP — 初始化 (Xavier init). h3=0 → 2-hidden 老架构; h3>0 → 3-hidden 大模型.
func NewMLP(inDim, h1, h2, h3, out int, rng *rand.Rand) *MLP {
	scale := func(fan int) float32 { return float32(1.0 / float64(fan)) }
	rf := func(s float32) float32 { return (rng.Float32()*2 - 1) * s }
	m := &MLP{
		InDim: inDim, H1: h1, H2: h2, H3: h3, OutDim: out,
		Means: make([]float32, inDim), Stds: make([]float32, inDim),
		W1: make([][]float32, h1), B1: make([]float32, h1),
		W2: make([][]float32, h2), B2: make([]float32, h2),
		TaskWeights: make([]float32, out),
	}
	for i := range m.Stds {
		m.Stds[i] = 1
	}
	for i := range m.TaskWeights {
		m.TaskWeights[i] = 1.0
	}
	s1 := scale(inDim)
	for i := range m.W1 {
		m.W1[i] = make([]float32, inDim)
		for j := range m.W1[i] {
			m.W1[i][j] = rf(s1)
		}
	}
	s2 := scale(h1)
	for i := range m.W2 {
		m.W2[i] = make([]float32, h1)
		for j := range m.W2[i] {
			m.W2[i][j] = rf(s2)
		}
	}
	if h3 > 0 {
		// 3-hidden: W3 = H3 × H2 (middle), W4 = OutDim × H3 (output)
		m.W3 = make([][]float32, h3)
		m.B3 = make([]float32, h3)
		s3 := scale(h2)
		for i := range m.W3 {
			m.W3[i] = make([]float32, h2)
			for j := range m.W3[i] {
				m.W3[i][j] = rf(s3)
			}
		}
		m.W4 = make([][]float32, out)
		m.B4 = make([]float32, out)
		s4 := scale(h3)
		for o := 0; o < out; o++ {
			m.W4[o] = make([]float32, h3)
			for j := range m.W4[o] {
				m.W4[o][j] = rf(s4)
			}
		}
	} else {
		// 2-hidden legacy: W3 = OutDim × H2 (output)
		m.W3 = make([][]float32, out)
		m.B3 = make([]float32, out)
		s3 := scale(h2)
		for o := 0; o < out; o++ {
			m.W3[o] = make([]float32, h2)
			for j := range m.W3[o] {
				m.W3[o][j] = rf(s3)
			}
		}
	}
	return m
}

// Forward — 返回 (out, h1, h2). out 长度 = OutDim.
// 注意: head 0 (royalty) 的输出**没有** YStd/YMean denormalize, 训练 loss 用 normalized 标度.
// 推理 (ofc/trained_eval.go) 才做 denormalize + combined.
// 3-hidden 时 h3 隐藏在内部, h2 返回值跟 2-hidden 兼容.
func (m *MLP) Forward(x []float32) (out, h1, h2 []float32) {
	xn := make([]float32, m.InDim)
	for i, v := range x {
		s := m.Stds[i]
		if s == 0 {
			s = 1
		}
		xn[i] = (v - m.Means[i]) / s
	}
	h1 = make([]float32, m.H1)
	for i := 0; i < m.H1; i++ {
		s := m.B1[i]
		for j := 0; j < m.InDim; j++ {
			s += m.W1[i][j] * xn[j]
		}
		if s < 0 {
			s = 0
		}
		h1[i] = s
	}
	h2 = make([]float32, m.H2)
	for i := 0; i < m.H2; i++ {
		s := m.B2[i]
		for j := 0; j < m.H1; j++ {
			s += m.W2[i][j] * h1[j]
		}
		if s < 0 {
			s = 0
		}
		h2[i] = s
	}
	if m.H3 > 0 {
		// 3-hidden: h2 → h3 via W3 middle, then h3 → out via W4
		h3 := make([]float32, m.H3)
		for i := 0; i < m.H3; i++ {
			s := m.B3[i]
			for j := 0; j < m.H2; j++ {
				s += m.W3[i][j] * h2[j]
			}
			if s < 0 {
				s = 0
			}
			h3[i] = s
		}
		out = make([]float32, m.OutDim)
		for o := 0; o < m.OutDim; o++ {
			v := m.B4[o]
			for j := 0; j < m.H3; j++ {
				v += m.W4[o][j] * h3[j]
			}
			out[o] = v
		}
		return
	}
	// 2-hidden legacy: h2 → out via W3 (= output)
	out = make([]float32, m.OutDim)
	for o := 0; o < m.OutDim; o++ {
		v := m.B3[o]
		for j := 0; j < m.H2; j++ {
			v += m.W3[o][j] * h2[j]
		}
		out[o] = v
	}
	return
}

// forwardInto — 训练用 forward, 写到 preallocated buf, 不分配新 slice.
func (m *MLP) forwardInto(x []float32) {
	for i, v := range x {
		s := m.Stds[i]
		if s == 0 {
			s = 1
		}
		m.bufXn[i] = (v - m.Means[i]) / s
	}
	for i := 0; i < m.H1; i++ {
		s := m.B1[i]
		for j := 0; j < m.InDim; j++ {
			s += m.W1[i][j] * m.bufXn[j]
		}
		if s < 0 {
			s = 0
		}
		m.bufH1[i] = s
	}
	for i := 0; i < m.H2; i++ {
		s := m.B2[i]
		for j := 0; j < m.H1; j++ {
			s += m.W2[i][j] * m.bufH1[j]
		}
		if s < 0 {
			s = 0
		}
		m.bufH2[i] = s
	}
	if m.H3 > 0 {
		// h2 → h3 via W3 middle
		for i := 0; i < m.H3; i++ {
			s := m.B3[i]
			for j := 0; j < m.H2; j++ {
				s += m.W3[i][j] * m.bufH2[j]
			}
			if s < 0 {
				s = 0
			}
			m.bufH3[i] = s
		}
		// h3 → out via W4 output
		for o := 0; o < m.OutDim; o++ {
			v := m.B4[o]
			for j := 0; j < m.H3; j++ {
				v += m.W4[o][j] * m.bufH3[j]
			}
			m.bufOut[o] = v
		}
		return
	}
	// 2-hidden legacy
	for o := 0; o < m.OutDim; o++ {
		v := m.B3[o]
		for j := 0; j < m.H2; j++ {
			v += m.W3[o][j] * m.bufH2[j]
		}
		m.bufOut[o] = v
	}
}

// sigmoid 跟 trained_eval.go 一致, 数值稳定
func sigmoidf(x float32) float32 {
	if x > 30 {
		return 1
	}
	if x < -30 {
		return 0
	}
	if x >= 0 {
		ex := expf(-x)
		return 1.0 / (1.0 + ex)
	}
	ex := expf(x)
	return ex / (1.0 + ex)
}

func expf(x float32) float32 {
	// 快速近似 (训练对精度不敏感, 误差 < 1e-4 在 [-30, 30])
	if x > 30 {
		return 1e13
	}
	if x < -30 {
		return 0
	}
	N := float32(1024)
	r := 1.0 + x/N
	for i := 0; i < 10; i++ {
		r *= r
	}
	return r
}

// TrainOne — 单 sample 训, targets 长度 = OutDim.
//   targets[0] = royalty (normalized)
//   targets[1] = fan rate ∈ [0,1]   (BCE)
//   targets[2] = foul rate ∈ [0,1]  (BCE)
// 返回联合 loss. 使用 m.bufXn/H1/H2/Out/DOut/GradH1/GradH2/GradW1/2/3 (调用 allocTrainBufs).
func (m *MLP) TrainOne(x []float32, targets []float32, lr float32) float32 {
	m.forwardInto(x) // 写入 m.bufXn / bufH1 / bufH2 / bufOut

	// 各 head 的 d_loss / d_out (预乘 task weight)
	var totalLoss float32
	for o := 0; o < m.OutDim; o++ {
		w := m.TaskWeights[o]
		if o == 0 {
			err := m.bufOut[o] - targets[o]
			m.bufDOut[o] = w * err
			totalLoss += w * 0.5 * err * err
		} else {
			t := targets[o]
			z := m.bufOut[o]
			s := sigmoidf(z)
			m.bufDOut[o] = w * (s - t)
			loss := float32(0)
			if z >= 0 {
				loss = z - z*t + logSafe(1+expf(-z))
			} else {
				loss = -z*t + logSafe(1+expf(z))
			}
			totalLoss += w * loss
		}
	}

	// Backprop — 分 3-hidden 跟 2-hidden 两路
	if m.H3 > 0 {
		// === 3-hidden backprop ===
		// dh3 = sum_o(dOut[o] * W4[o][j]) * ReLU'(h3) → m.bufGradH3
		for j := 0; j < m.H3; j++ {
			m.bufGradH3[j] = 0
		}
		for o := 0; o < m.OutDim; o++ {
			for j := 0; j < m.H3; j++ {
				m.bufGradH3[j] += m.bufDOut[o] * m.W4[o][j]
			}
		}
		for j := 0; j < m.H3; j++ {
			if m.bufH3[j] <= 0 {
				m.bufGradH3[j] = 0
			}
		}
		// W4 grads (output)
		for o := 0; o < m.OutDim; o++ {
			row := m.bufGradW4[o]
			dO := m.bufDOut[o]
			for j := 0; j < m.H3; j++ {
				row[j] = dO * m.bufH3[j]
			}
		}
		// dh2 = sum_i(gradH3[i] * W3[i][j]) * ReLU'(h2)
		for j := 0; j < m.H2; j++ {
			var s float32
			for i := 0; i < m.H3; i++ {
				s += m.bufGradH3[i] * m.W3[i][j]
			}
			if m.bufH2[j] <= 0 {
				s = 0
			}
			m.bufGradH2[j] = s
		}
		// W3 grads (middle)
		for i := 0; i < m.H3; i++ {
			gh := m.bufGradH3[i]
			row := m.bufGradW3[i]
			for j := 0; j < m.H2; j++ {
				row[j] = gh * m.bufH2[j]
			}
		}
	} else {
		// === 2-hidden legacy backprop ===
		for j := 0; j < m.H2; j++ {
			m.bufGradH2[j] = 0
		}
		for o := 0; o < m.OutDim; o++ {
			for j := 0; j < m.H2; j++ {
				m.bufGradH2[j] += m.bufDOut[o] * m.W3[o][j]
			}
		}
		for j := 0; j < m.H2; j++ {
			if m.bufH2[j] <= 0 {
				m.bufGradH2[j] = 0
			}
		}
		for o := 0; o < m.OutDim; o++ {
			row := m.bufGradW3[o]
			dO := m.bufDOut[o]
			for j := 0; j < m.H2; j++ {
				row[j] = dO * m.bufH2[j]
			}
		}
	}

	// dh1 = sum_i(gradH2[i] * W2[i]) (after ReLU)
	for j := 0; j < m.H1; j++ {
		var s float32
		for i := 0; i < m.H2; i++ {
			s += m.bufGradH2[i] * m.W2[i][j]
		}
		if m.bufH1[j] <= 0 {
			s = 0
		}
		m.bufGradH1[j] = s
	}

	// W2 grads
	for i := 0; i < m.H2; i++ {
		gh := m.bufGradH2[i]
		row := m.bufGradW2[i]
		for j := 0; j < m.H1; j++ {
			row[j] = gh * m.bufH1[j]
		}
	}

	// W1 grads
	for i := 0; i < m.H1; i++ {
		gh := m.bufGradH1[i]
		row := m.bufGradW1[i]
		for j := 0; j < m.InDim; j++ {
			row[j] = gh * m.bufXn[j]
		}
	}

	// Apply SGD
	if m.H3 > 0 {
		// W4 (output)
		for o := 0; o < m.OutDim; o++ {
			gw := m.bufGradW4[o]
			w := m.W4[o]
			for j := 0; j < m.H3; j++ {
				w[j] -= lr * gw[j]
			}
			m.B4[o] -= lr * m.bufDOut[o]
		}
		// W3 (middle)
		for i := 0; i < m.H3; i++ {
			gw := m.bufGradW3[i]
			w := m.W3[i]
			for j := 0; j < m.H2; j++ {
				w[j] -= lr * gw[j]
			}
			m.B3[i] -= lr * m.bufGradH3[i]
		}
	} else {
		// W3 (output, legacy)
		for o := 0; o < m.OutDim; o++ {
			gw := m.bufGradW3[o]
			w := m.W3[o]
			for j := 0; j < m.H2; j++ {
				w[j] -= lr * gw[j]
			}
			m.B3[o] -= lr * m.bufDOut[o]
		}
	}
	for i := 0; i < m.H2; i++ {
		gw := m.bufGradW2[i]
		w := m.W2[i]
		for j := 0; j < m.H1; j++ {
			w[j] -= lr * gw[j]
		}
		m.B2[i] -= lr * m.bufGradH2[i]
	}
	for i := 0; i < m.H1; i++ {
		gw := m.bufGradW1[i]
		w := m.W1[i]
		for j := 0; j < m.InDim; j++ {
			w[j] -= lr * gw[j]
		}
		m.B1[i] -= lr * m.bufGradH1[i]
	}

	return totalLoss
}

// 简单 log (避免 math import)
func logSafe(x float32) float32 {
	if x <= 0 {
		return -30
	}
	// Newton's method on x = e^y, 即 y = ln(x). 起点 y=0 ln(1)=0
	y := float32(0.0)
	for i := 0; i < 20; i++ {
		ey := expf(y)
		y -= 1 - x/ey
	}
	return y
}

// TrainBatch — 1 epoch
//   targets[0] = royalty (MSE)
//   targets[1] = fanRate (BCE)
//   targets[2] = foulRate (BCE)
//   targets[3] = policyTarget (BCE) — AlphaZero per-candidate visit ratio
func (m *MLP) TrainBatch(samples []Sample, lr float32, rng *rand.Rand) float32 {
	var totalLoss float32
	rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
	targets := make([]float32, m.OutDim)
	for _, s := range samples {
		targets[0] = (s.McScore - m.YMean) / m.YStd
		if m.OutDim >= 2 {
			targets[1] = s.FanRate
		}
		if m.OutDim >= 3 {
			targets[2] = s.FoulRate
		}
		if m.OutDim >= 4 {
			targets[3] = s.PolicyTarget
		}
		totalLoss += m.TrainOne(s.Features, targets, lr)
	}
	return totalLoss / float32(len(samples))
}

// ============================================================
// ckpt save (兼容 v7 schema, 含 outDim=1 for 单头)
// ============================================================

// ckpt schema 兼容 v7 + 加 outDim 多头
//
// 单头 (outDim=1): w3 写 [[h2-vec]], b3 写 [b3-scalar]
//   trained_eval.go LoadWeightsFromFile 自动 handle [][] schema
//
// 多头 (outDim=3): w3 [[h2],[h2],[h2]], b3 [r,fan,foul]
// ckpt schema:
//   2-hidden: inDim/h1Dim/h2Dim/outDim, w1/b1, w2/b2, w3/b3 (w3=output)
//   3-hidden: + h3Dim, w3/b3=middle, w4/b4=output
type ckptOut struct {
	InDim      int         `json:"inDim"`
	H1Dim      int         `json:"h1Dim"`
	H2Dim      int         `json:"h2Dim"`
	H3Dim      int         `json:"h3Dim,omitempty"`
	OutDim     int         `json:"outDim"`
	Means      []float32   `json:"means"`
	Stds       []float32   `json:"stds"`
	W1         [][]float32 `json:"w1"`
	B1         []float32   `json:"b1"`
	W2         [][]float32 `json:"w2"`
	B2         []float32   `json:"b2"`
	W3         [][]float32 `json:"w3"`           // H3>0: H3×H2 (middle); H3==0: OutDim×H2 (output)
	B3         []float32   `json:"b3"`           // H3>0: [H3]; H3==0: [OutDim]
	W4         [][]float32 `json:"w4,omitempty"` // H3>0: OutDim×H3 (output)
	B4         []float32   `json:"b4,omitempty"` // H3>0: [OutDim]
	YStd       float32     `json:"yStd"`
	YMean      float32     `json:"yMean"`
	Round      int         `json:"round"`
	Accuracy   float32     `json:"accuracy"`
	SamplesCnt int         `json:"samplesCount"`
	GamesCnt   int         `json:"gamesPlayed"`
	Timestamp  string      `json:"timestamp"`
	PolicyVer  string      `json:"policyVersion"`
	// 2026-06-12: 模型自带 fan-bonus scale → server/bench/duel 加载时自动对齐 (见 ofc.SetFanBonusScale).
	// 必写: 否则 feature dim-0 在 train↔serve 错位 (sp21/sp23 那个坑).
	FanBonusQQ    float64 `json:"fanBonusQQ"`
	FanBonusKK    float64 `json:"fanBonusKK"`
	FanBonusAA    float64 `json:"fanBonusAA"`
	FanBonusTrips float64 `json:"fanBonusTrips"`
	FoulCost      float64 `json:"foulCost"`
}

// LoadMLPFromCkpt — 解析 ckpt JSON 回到 MLP struct (warm-start 用).
// 跟 NewMLP 不同, 这个保留训练好的 W/B, 让下一 round 在此基础上 SGD finetune.
// Means/Stds/YMean/YStd 也会从 ckpt 读, 但每 round 训练前会被新计算的 sample 统计覆盖.
func LoadMLPFromCkpt(path string) (*MLP, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ckptOut
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.InDim == 0 || c.H1Dim == 0 || c.H2Dim == 0 {
		return nil, fmt.Errorf("ckpt 缺少维度信息: inDim=%d h1=%d h2=%d", c.InDim, c.H1Dim, c.H2Dim)
	}
	outDim := c.OutDim
	if outDim == 0 {
		outDim = 1
	}
	// 自动识别 3-hidden: 若 h3Dim>0 或 w4 存在
	h3 := c.H3Dim
	if h3 == 0 && len(c.W4) > 0 {
		h3 = len(c.B3)
	}
	m := &MLP{
		InDim: c.InDim, H1: c.H1Dim, H2: c.H2Dim, H3: h3, OutDim: outDim,
		Means: c.Means, Stds: c.Stds,
		W1: c.W1, B1: c.B1, W2: c.W2, B2: c.B2,
		W3: c.W3, B3: c.B3,
		W4: c.W4, B4: c.B4,
		YStd: c.YStd, YMean: c.YMean,
		TaskWeights: make([]float32, outDim),
	}
	for i := range m.TaskWeights {
		m.TaskWeights[i] = 1.0
	}
	return m, nil
}

func saveCkpt(m *MLP, path string, meta ckptOut) error {
	out := ckptOut{
		InDim: m.InDim, H1Dim: m.H1, H2Dim: m.H2, H3Dim: m.H3, OutDim: m.OutDim,
		Means: m.Means, Stds: m.Stds,
		W1: m.W1, B1: m.B1, W2: m.W2, B2: m.B2,
		W3: m.W3, B3: m.B3,
		W4: m.W4, B4: m.B4,
		YStd: m.YStd, YMean: m.YMean,
		Round: meta.Round, Accuracy: meta.Accuracy,
		SamplesCnt: meta.SamplesCnt, GamesCnt: meta.GamesCnt,
		Timestamp: meta.Timestamp, PolicyVer: meta.PolicyVer,
		FanBonusQQ: meta.FanBonusQQ, FanBonusKK: meta.FanBonusKK, FanBonusAA: meta.FanBonusAA,
		FanBonusTrips: meta.FanBonusTrips, FoulCost: meta.FoulCost,
	}
	b, err := json.Marshal(&out)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// ============================================================
// helpers
// ============================================================

func stateKey(gs *ofc.GameState) string {
	tids, mids, bids := make([]string, 0, len(gs.Top)), make([]string, 0, len(gs.Middle)), make([]string, 0, len(gs.Bottom))
	for _, c := range gs.Top {
		tids = append(tids, c.ID())
	}
	for _, c := range gs.Middle {
		mids = append(mids, c.ID())
	}
	for _, c := range gs.Bottom {
		bids = append(bids, c.ID())
	}
	sort.Strings(tids)
	sort.Strings(mids)
	sort.Strings(bids)
	key := ""
	for _, s := range tids {
		key += s
	}
	key += "|"
	for _, s := range mids {
		key += s
	}
	key += "|"
	for _, s := range bids {
		key += s
	}
	return key
}

func shuffleDeck(deck []ofc.Card, rng *rand.Rand) {
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
}

// ============================================================
// main
// ============================================================

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	// 加载起始 weights (如有)
	if *weightsIn != "" {
		if err := ofc.LoadWeightsFromFile(*weightsIn); err != nil {
			log.Fatalf("load -weights failed: %v", err)
		}
		log.Printf("[train] loaded weights from %s", *weightsIn)
	} else {
		log.Print("[train] using embed default weights")
	}

	// rollout config — 用户价值函数 fan/foul bonus 全 knob 化, 直入 silver-label
	evalCfg := ofc.DefaultRolloutConfig
	evalCfg.FoulCost = float32(*foulCost)
	evalCfg.QQFanBonus = float32(*fanBonusQQ)
	evalCfg.KKFanBonus = float32(*fanBonusKK)
	evalCfg.AAFanBonus = float32(*fanBonusAA)
	evalCfg.TripsFanBonus = float32(*fanBonusTrips)
	evalCfg.Epsilon = float32(*rolloutEpsilon)
	// 2026-06-12: flag 驱动 feature dim-0 的 var (warm-start LoadMLPFromCkpt 不设 var → 否则 train 算 feature 用 default).
	ofc.SetFanBonusScale(fanBonusQQ, fanBonusKK, fanBonusAA, fanBonusTrips, foulCost)

	// MLP warm-start tracking: 每 round 后保 ckpt path, round+1 用作 init
	var prevCkptPath string

	// 输出目录
	os.MkdirAll(*ckptOutDir, 0755)
	os.MkdirAll(*samplesDir, 0755)

	// 算 round 数
	roundCount := int(*totalHours * 60 / *roundMins)
	if roundCount < 1 {
		roundCount = 1
	}
	log.Printf("[train] hours=%.1f round-min=%.1f sims=%d jokers=%d workers=%d → %d rounds",
		*totalHours, *roundMins, *rolloutsPC, *numJokers, *workers, roundCount)
	log.Printf("[train] policy=%s out=%s", *policyVer, *ckptOutDir)
	log.Printf("[train] label fan-bonus QQ=%.0f KK=%.0f AA=%.0f trips=%.0f, foul-cost=%.0f, rollout-epsilon=%.2f",
		*fanBonusQQ, *fanBonusKK, *fanBonusAA, *fanBonusTrips, *foulCost, *rolloutEpsilon)
	if *phantomOpponentsMax > 0 {
		log.Printf("[train] phantom usedCards inject ON: per-game opponents ∈ [0,%d] / slot ∈ [0, opponents]. Max R5 phantom = %d (修 deck-aware OOD; opp=0 模拟 Fantasy 不可见)",
			*phantomOpponentsMax, phantomCountFor(5, *phantomOpponentsMax, *phantomOpponentsMax))
	}

	// Dataset mode: 默认 auto-disable warm-start (oracle label scale 跟 rollout 不同).
	// 但若 -dataset-keep-warm-start (rollout-dataset 用) 则保留 warm-start.
	if *datasetDir != "" {
		log.Printf("[train] DATASET MODE: loading samples from %s (skip rollout)", *datasetDir)
		if *warmStart && !*datasetKeepWarm {
			log.Printf("[train] auto-disable warm-start (oracle label scale 跟 rollout 不同; rollout-dataset 加 -dataset-keep-warm-start 覆盖)")
			*warmStart = false
		} else if *warmStart && *datasetKeepWarm {
			log.Printf("[train] -dataset-keep-warm-start: 保留 warm-start (适用 rollout-dataset, label 兼容)")
		}
	}

	startT := time.Now()
	for r := 1; r <= roundCount; r++ {
		log.Printf("=== Round %d/%d (elapsed %.1f min) ===", r, roundCount, time.Since(startT).Minutes())

		// Sample 收集: dataset (从 disk 读, oracle 或 rollout 都可) 或 collectSamples (实时 rollout 生)
		var newSamples []Sample
		var gamesCount int
		if *datasetDir != "" {
			loadStart := time.Now()
			loaded, err := loadDatasetSamples(*datasetDir, *maxSamples, mlpRngForLoad())
			if err != nil {
				log.Fatalf("load dataset samples: %v", err)
			}
			newSamples = loaded
			gamesCount = -1
			log.Printf("  loaded %d dataset samples from %s in %.1fs",
				len(newSamples), *datasetDir, time.Since(loadStart).Seconds())
			// Sample feature dim 必须跟 inDim 完全一致 — 否则 fatal
			if len(newSamples) > 0 && len(newSamples[0].Features) != *inDim {
				log.Fatalf("feature dim mismatch: samples %d-d but inDim %d. 需重新生成 samples 用相同 inDim",
					len(newSamples[0].Features), *inDim)
			}
		} else {
			newSamples, gamesCount = collectSamples(*workers, *roundMins, *maxSamples, evalCfg, *verbose)
			log.Printf("  collected %d samples in %d games (%d workers, %.1f min)",
				len(newSamples), gamesCount, *workers, *roundMins)
		}

		// 标准化
		var sumScore, sumScoreSq float32
		for _, s := range newSamples {
			sumScore += s.McScore
			sumScoreSq += s.McScore * s.McScore
		}
		yMean := sumScore / float32(len(newSamples))
		variance := sumScoreSq/float32(len(newSamples)) - yMean*yMean
		yStd := float32(1.0)
		if variance > 0 {
			yStd = float32(sqrtf(variance))
		}

		// 训 MLP — warm-start 来源 (优先级: round-2+ prev > round-1 init-from-ckpt > NewMLP)
		var mlpRng = rand.New(rand.NewSource(time.Now().UnixNano() + 1))
		var mlp *MLP
		isWarmStart := false
		extensionStartIdx := 0 // >0 = feature-extension warm-start, 新 feature 起始 idx
		var warmCkpt string
		if *warmStart && r > 1 && prevCkptPath != "" {
			warmCkpt = prevCkptPath
		} else if r == 1 && *initFromCkpt != "" {
			warmCkpt = *initFromCkpt
		}
		if warmCkpt != "" {
			loaded, err := LoadMLPFromCkpt(warmCkpt)
			if err != nil {
				log.Printf("  warm-start load failed: %v, falling back to NewMLP", err)
				mlp = NewMLP(*inDim, *hiddenH1, *hiddenH2, *hiddenH3, *outDim, mlpRng)
			} else if loaded.H1 != *hiddenH1 || loaded.H2 != *hiddenH2 || loaded.H3 != *hiddenH3 || loaded.OutDim != *outDim {
				log.Printf("  warm-start arch mismatch (ckpt=%d/%d/%d/%d/%d, want=%d/%d/%d/%d/%d), falling back to NewMLP",
					loaded.InDim, loaded.H1, loaded.H2, loaded.H3, loaded.OutDim, *inDim, *hiddenH1, *hiddenH2, *hiddenH3, *outDim)
				mlp = NewMLP(*inDim, *hiddenH1, *hiddenH2, *hiddenH3, *outDim, mlpRng)
			} else if loaded.InDim < *inDim {
				// FEATURE EXTENSION warm-start: ckpt inDim < train inDim
				// W1 行扩展, 新 feature 列 zero-init (新 feature 初始 contribution = 0; 训练学起来)
				// 用于 round-004 (56-d) → 加新 feature 后 (e.g. 64-d) 继续训练.
				oldInDim := loaded.InDim
				log.Printf("  warm-start FEATURE EXTENSION: ckpt inDim=%d → train inDim=%d (新 %d feature, W1 列 zero-init, 旧 %d 列权重保留)",
					oldInDim, *inDim, *inDim-oldInDim, oldInDim)
				for i := range loaded.W1 {
					ext := make([]float32, *inDim)
					copy(ext, loaded.W1[i])
					loaded.W1[i] = ext
				}
				extMeans := make([]float32, *inDim)
				extStds := make([]float32, *inDim)
				copy(extMeans, loaded.Means)
				copy(extStds, loaded.Stds)
				for i := oldInDim; i < *inDim; i++ {
					extStds[i] = 1
				}
				loaded.Means = extMeans
				loaded.Stds = extStds
				loaded.InDim = *inDim
				mlp = loaded
				isWarmStart = true
				extensionStartIdx = oldInDim
			} else if loaded.InDim > *inDim {
				log.Printf("  warm-start inDim shrink not supported (ckpt=%d > train=%d), falling back to NewMLP",
					loaded.InDim, *inDim)
				mlp = NewMLP(*inDim, *hiddenH1, *hiddenH2, *hiddenH3, *outDim, mlpRng)
			} else {
				log.Printf("  warm-start: loaded MLP from %s (%d epochs accumulating)", warmCkpt, *trainEpochs*r)
				mlp = loaded
				isWarmStart = true
			}
		} else {
			mlp = NewMLP(*inDim, *hiddenH1, *hiddenH2, *hiddenH3, *outDim, mlpRng)
		}
		// 多头 task weights (warm-start 时也覆盖, 保证当前 round 的 fan-w/foul-w 即时生效)
		if *outDim >= 2 {
			mlp.TaskWeights[1] = float32(*fanWeight)
		}
		if *outDim >= 3 {
			mlp.TaskWeights[2] = float32(*foulWeight)
		}
		if *outDim >= 4 {
			mlp.TaskWeights[3] = float32(*policyWeight)
		}
		// 标准化 — warm-start 必须保留 loaded ckpt 的 Means/Stds + YMean/YStd
		// (W1 / W3[0] 权重已对其校准). from-scratch 才重新算.
		// 特殊情况: feature-extension warm-start (extensionStartIdx>0) — 旧 feature dims 保 ckpt
		// Means/Stds (老 W1 权重对其), 仅新 feature dims 重新算.
		if extensionStartIdx > 0 {
			log.Printf("  feature-extension: preserving Means/Stds[0:%d] from ckpt, recomputing [%d:%d] from samples",
				extensionStartIdx, extensionStartIdx, *inDim)
			means := make([]float32, *inDim)
			stds := make([]float32, *inDim)
			copy(means, mlp.Means) // 旧 dims 保留 (Means[0:extensionStartIdx])
			copy(stds, mlp.Stds)
			for _, s := range newSamples {
				for i := extensionStartIdx; i < *inDim && i < len(s.Features); i++ {
					means[i] += s.Features[i]
				}
			}
			for i := extensionStartIdx; i < *inDim; i++ {
				means[i] /= float32(len(newSamples))
			}
			for _, s := range newSamples {
				for i := extensionStartIdx; i < *inDim && i < len(s.Features); i++ {
					d := s.Features[i] - means[i]
					stds[i] += d * d
				}
			}
			for i := extensionStartIdx; i < *inDim; i++ {
				stds[i] = float32(sqrtf(stds[i] / float32(len(newSamples))))
				if stds[i] < 0.01 {
					stds[i] = 1
				}
			}
			mlp.Means = means
			mlp.Stds = stds
			// YMean/YStd 默认保留. -y-ema (EMA 平滑) 或 -y-recompute (直接替换)
			if *yEMA > 0 {
				alpha := float32(*yEMA)
				newYMean := (1-alpha)*mlp.YMean + alpha*yMean
				newYStd := (1-alpha)*mlp.YStd + alpha*yStd
				log.Printf("  -y-ema=%.2f (ext-warm): YMean %.3f → %.3f, YStd %.3f → %.3f",
					alpha, mlp.YMean, newYMean, mlp.YStd, newYStd)
				mlp.YMean = newYMean
				mlp.YStd = newYStd
			} else if *yRecompute {
				log.Printf("  -y-recompute (ext-warm): YMean %.3f → %.3f, YStd %.3f → %.3f",
					mlp.YMean, yMean, mlp.YStd, yStd)
				mlp.YMean = yMean
				mlp.YStd = yStd
			}
		} else if !isWarmStart {
			mlp.YMean = yMean
			mlp.YStd = yStd
			means := make([]float32, *inDim)
			stds := make([]float32, *inDim)
			for _, s := range newSamples {
				for i, v := range s.Features {
					means[i] += v
				}
			}
			for i := range means {
				means[i] /= float32(len(newSamples))
			}
			for _, s := range newSamples {
				for i, v := range s.Features {
					d := v - means[i]
					stds[i] += d * d
				}
			}
			for i := range stds {
				stds[i] = float32(sqrtf(stds[i] / float32(len(newSamples))))
				if stds[i] < 0.01 {
					stds[i] = 1
				}
			}
			mlp.Means = means
			mlp.Stds = stds
		} else if *yEMA > 0 {
			// regular warm-start + EMA 平滑过渡
			alpha := float32(*yEMA)
			newYMean := (1-alpha)*mlp.YMean + alpha*yMean
			newYStd := (1-alpha)*mlp.YStd + alpha*yStd
			log.Printf("  -y-ema=%.2f (warm): YMean %.3f → %.3f, YStd %.3f → %.3f",
				alpha, mlp.YMean, newYMean, mlp.YStd, newYStd)
			mlp.YMean = newYMean
			mlp.YStd = newYStd
		} else if *yRecompute {
			// regular warm-start (无 feature extension) + -y-recompute: 直接替换
			log.Printf("  -y-recompute (warm): YMean %.3f → %.3f, YStd %.3f → %.3f",
				mlp.YMean, yMean, mlp.YStd, yStd)
			mlp.YMean = yMean
			mlp.YStd = yStd
		}

		baseLR := float32(*baseLRFlag)
		if isWarmStart {
			baseLR *= float32(*warmLRMult) // finetune LR 减半 (默认 0.0025), 避免覆盖前 round 学习
			log.Printf("  yMean=%.3f yStd=%.3f (preserved from prev) | sample yMean=%.3f yStd=%.3f, training %d epochs (warm-start LR=%.4f)",
				mlp.YMean, mlp.YStd, yMean, yStd, *trainEpochs, baseLR)
		} else {
			log.Printf("  yMean=%.3f yStd=%.3f, training %d epochs (fresh LR=%.4f)", mlp.YMean, mlp.YStd, *trainEpochs, baseLR)
		}
		// 训练前一次性 alloc 训练 scratch buffers (TrainOne 重用, 避免 4M 次 allocation/round)
		mlp.allocTrainBufs()
		nanDetected := false
		for ep := 0; ep < *trainEpochs; ep++ {
			lr := baseLR * (1.0 - float32(ep)/float32(*trainEpochs)*0.5)
			loss := mlp.TrainBatch(newSamples, lr, mlpRng)
			// 2026-05-19: NaN 检测早退 — 之前 silver label cap bug + warm-start 累积梯度爆,
			// 训练 30 epochs 跑完才发现 ckpt 全 NaN. 检测到立即 abort, 不浪费时间.
			if math.IsNaN(float64(loss)) || math.IsInf(float64(loss), 0) {
				log.Printf("  ⚠ NaN/Inf loss at epoch %d (loss=%.4f). 训练中止, 当前 ckpt 不可用.",
					ep, loss)
				log.Printf("  常见根因: silver label 异常大 (检查 gen-rollout-dataset cap-aware), LR 太高, 或 feature 含 Inf.")
				nanDetected = true
				break
			}
			if *verbose && ep%10 == 0 {
				log.Printf("    epoch %d: loss=%.4f", ep, loss)
			}
		}
		if nanDetected {
			// 退出 round 循环 — 不保 NaN ckpt 不评估. 上层判断 prevCkpt 还在, warm-start 链断在这.
			log.Printf("  跳过本 round 的 acc 评估 + ckpt 保存 (NaN abort)")
			break
		}

		// 计算 ranking accuracy (取 head 0 = royalty 跟 v7/train-loop.js 一致)
		correct := 0
		total := 0
		for i := 0; i+1 < len(newSamples); i++ {
			y0, _, _ := mlp.Forward(newSamples[i].Features)
			y1, _, _ := mlp.Forward(newSamples[i+1].Features)
			// out[0] 是 normalized royalty, denormalize 比较 (虽然 unnormalized 比较结果一样)
			if (newSamples[i].McScore > newSamples[i+1].McScore) == (y0[0] > y1[0]) {
				correct++
			}
			total++
		}
		acc := float32(correct) / float32(total)
		log.Printf("  ranking acc: %.4f", acc)

		// 保存 ckpt
		ckptName := fmt.Sprintf("round-%03d-acc%d.json", r, int(acc*100))
		ckptPath := filepath.Join(*ckptOutDir, ckptName)
		err := saveCkpt(mlp, ckptPath, ckptOut{
			Round: r, Accuracy: acc,
			SamplesCnt: len(newSamples), GamesCnt: gamesCount,
			Timestamp: time.Now().Format(time.RFC3339),
			PolicyVer: *policyVer,
			// 2026-06-12: 把训练用的 fan-bonus scale 写进 ckpt → serve 时自动对齐
			FanBonusQQ: *fanBonusQQ, FanBonusKK: *fanBonusKK, FanBonusAA: *fanBonusAA,
			FanBonusTrips: *fanBonusTrips, FoulCost: *foulCost,
		})
		if err != nil {
			log.Printf("save ckpt failed: %v", err)
		} else {
			log.Printf("  saved ckpt: %s", ckptPath)
			prevCkptPath = ckptPath // warm-start: 下 round 用此 ckpt 当 MLP init
		}

		// 应用新 weights 到 evaluator (下一 round 用此 ckpt 跑 rollout)
		if err := ofc.LoadWeightsFromFile(ckptPath); err != nil {
			log.Printf("load new ckpt failed: %v", err)
		}
	}

	log.Printf("Training complete in %.1f hours", time.Since(startT).Hours())
}

// 简单 sqrt (avoid math import)
func sqrtf(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton's method, 10 iter 够准
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) * 0.5
	}
	return z
}

// ============================================================
// Multi-worker sample collection (阶段 2)
// ============================================================

// collectSamples — 并发跑 N 个 worker 收集 sample, 直到时间到或达 maxSamples.
//
// 架构: producer-consumer
//   - workers 个 goroutine 各跑 genOneGame, 把 sample 推 channel
//   - main goroutine 收集 channel, 监控时间
//   - 时间到/cap 满则发 done 信号, workers 退出
//
// 每 worker 用独立 RNG (worker_id seed offset), 保证不同 worker 跑不同 trajectory.
func collectSamples(numWorkers int, roundMin float64, maxSamples int,
	cfg ofc.RolloutConfig, verbose bool) ([]Sample, int) {
	if numWorkers < 1 {
		numWorkers = 1
	}
	type gameOut struct {
		samples []Sample
	}
	ch := make(chan gameOut, numWorkers*4)
	done := make(chan struct{})
	var wg sync.WaitGroup

	// 启 worker
	baseSeed := time.Now().UnixNano()
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(baseSeed + int64(workerID*1009)))
			for {
				select {
				case <-done:
					return
				default:
				}
				samples := genOneGame(rng, cfg)
				select {
				case ch <- gameOut{samples}:
				case <-done:
					return
				}
			}
		}(w)
	}

	// 主 goroutine 收集
	allSamples := make([]Sample, 0, maxSamples)
	gamesCount := 0
	roundT := time.Now()
	deadline := roundT.Add(time.Duration(roundMin * float64(time.Minute)))

collectLoop:
	for {
		select {
		case g := <-ch:
			allSamples = append(allSamples, g.samples...)
			gamesCount++
			if verbose && gamesCount%50 == 0 {
				log.Printf("  [%.1f min] %d games, %d samples (%.0f s/g)",
					time.Since(roundT).Minutes(), gamesCount, len(allSamples),
					time.Since(roundT).Seconds()/float64(gamesCount))
			}
			if len(allSamples) >= maxSamples {
				break collectLoop
			}
		case <-time.After(time.Until(deadline)):
			break collectLoop
		}
		if time.Now().After(deadline) {
			break
		}
	}

	// 通知 workers 退出
	close(done)
	wg.Wait()
	close(ch)
	// drain channel (剩余 in-flight)
	for g := range ch {
		allSamples = append(allSamples, g.samples...)
		gamesCount++
	}
	return allSamples, gamesCount
}
// =====================================================================
// Oracle dataset loader — 从 dir/round{N}/shard-NNNNN.jsonl.gz 读 Sample
// =====================================================================

// mlpRngForLoad — 给 loadDatasetSamples 用的随机源 (shuffle samples)
func mlpRngForLoad() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano() ^ 0xDEADBEEF))
}

// loadDatasetSamples — 递归读 dir 下所有 .jsonl.gz, 返回 []Sample.
// 若总 sample 数 > cap, reservoir sample 到 cap.
// 加载完 shuffle.
func loadDatasetSamples(dir string, cap int, rng *rand.Rand) ([]Sample, error) {
	// 收集所有 shard 路径
	var shards []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl.gz") {
			shards = append(shards, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	if len(shards) == 0 {
		return nil, fmt.Errorf("no .jsonl.gz shards under %s", dir)
	}
	log.Printf("  dataset: %d shards", len(shards))

	// Pass 1: count total. Corrupt shard → warn + skip (不 fatal, ctrl-c 留下的截断 shard 不该杀整次训练).
	totalCount := 0
	goodShards := make([]string, 0, len(shards))
	skipped := 0
	for _, p := range shards {
		n, err := countShardLines(p)
		if err != nil {
			log.Printf("  WARN: skip corrupt shard %s (count failed: %v)", p, err)
			skipped++
			continue
		}
		totalCount += n
		goodShards = append(goodShards, p)
	}
	if skipped > 0 {
		log.Printf("  dataset: %d total samples (%d good shards, %d skipped corrupt)", totalCount, len(goodShards), skipped)
	} else {
		log.Printf("  dataset: %d total samples", totalCount)
	}
	shards = goodShards

	// Pass 2: load (with optional reservoir sampling). 单 shard 中途坏 → 跳剩余, 保已加载.
	useReservoir := totalCount > cap
	if useReservoir {
		log.Printf("  reservoir sampling to cap %d (from %d total)", cap, totalCount)
	}
	out := make([]Sample, 0, minInt2(totalCount, cap))
	seen := 0
	for _, p := range shards {
		f, err := os.Open(p)
		if err != nil {
			log.Printf("  WARN: skip shard %s (open failed: %v)", p, err)
			continue
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			log.Printf("  WARN: skip shard %s (gzip open failed: %v)", p, err)
			f.Close()
			continue
		}
		sc := bufio.NewScanner(gz)
		sc.Buffer(make([]byte, 1<<20), 1<<22) // 4MB max line
		shardCorrupt := false
		for sc.Scan() {
			line := sc.Bytes()
			var s Sample
			if err := json.Unmarshal(line, &s); err != nil {
				log.Printf("  WARN: skip rest of shard %s (unmarshal line %d failed: %v)", p, seen, err)
				shardCorrupt = true
				break
			}
			seen++
			if !useReservoir {
				out = append(out, s)
			} else {
				if len(out) < cap {
					out = append(out, s)
				} else {
					j := rng.Intn(seen)
					if j < cap {
						out[j] = s
					}
				}
			}
		}
		if !shardCorrupt {
			if err := sc.Err(); err != nil {
				log.Printf("  WARN: shard %s scan ended with error: %v (已加载部分保留)", p, err)
			}
		}
		gz.Close()
		f.Close()
	}

	// Shuffle
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, nil
}

// countShardLines — 数 shard 内 line 数 (gzip + jsonl).
func countShardLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 1<<16), 1<<22)
	count := 0
	for sc.Scan() {
		count++
	}
	return count, sc.Err()
}

func minInt2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 防止 io 包 unused
var _ = io.EOF
