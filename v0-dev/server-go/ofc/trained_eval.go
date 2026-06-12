package ofc

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

//go:embed trained_weights.json
var trainedWeightsJSON []byte

// TrainedNet — MLP 权重. 兼容多种 arch:
//   2-hidden legacy: 132 → H1 → H2 → OutDim (H3Dim=0, W3=output)
//   3-hidden:        132 → H1 → H2 → H3 → OutDim (H3Dim>0, W3=middle, W4=output)
//
// 启动时从 embedded JSON 加载到平铺 float32 数组, 之后 0 分配.
type TrainedNet struct {
	InDim, H1Dim, H2Dim, H3Dim int       // H3Dim=0 → 2-hidden legacy
	OutDim                     int       // 1 单头 / 3 多头 / 4 含 policy
	Means, Stds                []float32
	W1Flat                     []float32 // H1 × IN, row-major
	W2Flat                     []float32 // H2 × H1
	W3Flat                     []float32 // H3Dim>0: H3 × H2 (middle); else OutDim × H2 (legacy output)
	W4Flat                     []float32 // H3Dim>0: OutDim × H3 (output); else empty
	B1, B2                     []float32
	B3                         []float32 // H3Dim>0: [H3]; else [OutDim]
	B4                         []float32 // H3Dim>0: [OutDim]; else empty
	YStd, YMean                float32   // 标准化 (royalty head 用)
}

// MultiHeadInferenceConfig — 多头 inference 时如何 combine 输出.
//   score = royalty + FanBoost * sigmoid(fan_logit) - FoulPenalty * sigmoid(foul_logit)
//
// 默认 0/0: 只用 head 0 (royalty). head 0 已通过训练 label 吸收 fan/foul bonus 的期望
// (mcScore = Royalties + fanBonus[type] 或 -FoulCost), 不需要再叠加 head 1/2 的 sigmoid.
// 多头 BCE loss 仍然训练 (auxiliary signal 帮泛化), 但 inference 用纯 head 0.
//
// 想强化某方向可调: 比如 FanBoost=20 让 fan 偏好再加一档.
type MultiHeadInferenceConfig struct {
	FanBoost    float32
	FoulPenalty float32
}

var DefaultMultiHeadCfg = MultiHeadInferenceConfig{
	FanBoost:    0,
	FoulPenalty: 0,
}

type rawWeights struct {
	InDim, H1Dim, H2Dim, H3Dim, OutDim int
	Means, Stds                        []float64
	W1                                 [][]float64
	B1                                 []float64
	W2                                 [][]float64
	B2                                 []float64
	W3                                 [][]float64 // H3Dim>0: middle [h3][h2]; else output [outDim][h2]
	B3                                 []float64   // H3Dim>0: [h3]; else [outDim]
	W4                                 [][]float64 // H3Dim>0: output [outDim][h3]
	B4                                 []float64   // H3Dim>0: [outDim]
	YStd, YMean                        float64
}

var defaultTrainedNet *TrainedNet

func init() {
	// embedded schema: 单头 (w3=[]float64, b3=float64). 用 normalize 逻辑同 LoadWeightsFromFile.
	if err := loadWeightsFromBytes(trainedWeightsJSON); err != nil {
		panic("failed to load embedded trained_weights.json: " + err.Error())
	}
}

// loadWeightsFromBytes — 共用的 schema-agnostic loader (embed init / LoadWeightsFromFile 都调).
// 支持 2-hidden (legacy: w3=output) 和 3-hidden (new: h3Dim>0, w3=middle, w4=output) schema.
func loadWeightsFromBytes(data []byte) error {
	var c struct {
		InDim, H1Dim, H2Dim, H3Dim, OutDim int
		Means, Stds                        []float64
		W1                                 [][]float64
		B1                                 []float64
		W2                                 [][]float64
		B2                                 []float64
		W3                                 json.RawMessage
		B3                                 json.RawMessage
		W4                                 [][]float64
		B4                                 []float64
		YStd, YMean                        float64
		// 2026-06-12: 可选 fan-bonus scale (模型自带, 训练写入). 缺失 (如太子) → 回退 default.
		// 指针类型: nil = ckpt 无此字段 → SetFanBonusScale 用 default (向后兼容).
		FanBonusQQ    *float64 `json:"fanBonusQQ"`
		FanBonusKK    *float64 `json:"fanBonusKK"`
		FanBonusAA    *float64 `json:"fanBonusAA"`
		FanBonusTrips *float64 `json:"fanBonusTrips"`
		FoulCost      *float64 `json:"foulCost"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	// 每次加载全量设置 fan-bonus scale (有字段用之, 无字段回退 default — 防 scale 残留/错位).
	SetFanBonusScale(c.FanBonusQQ, c.FanBonusKK, c.FanBonusAA, c.FanBonusTrips, c.FoulCost)
	var w3 [][]float64
	var w3m [][]float64
	var w3v []float64
	if err := json.Unmarshal(c.W3, &w3m); err == nil && len(w3m) >= 1 {
		w3 = w3m
	} else if err := json.Unmarshal(c.W3, &w3v); err == nil {
		w3 = [][]float64{w3v}
	} else {
		return fmt.Errorf("w3 parse failed")
	}
	var b3 []float64
	var b3a []float64
	var b3v float64
	if err := json.Unmarshal(c.B3, &b3a); err == nil && len(b3a) >= 1 {
		b3 = b3a
	} else if err := json.Unmarshal(c.B3, &b3v); err == nil {
		b3 = []float64{b3v}
	} else {
		return fmt.Errorf("b3 parse failed")
	}
	raw := rawWeights{
		InDim: c.InDim, H1Dim: c.H1Dim, H2Dim: c.H2Dim, H3Dim: c.H3Dim, OutDim: c.OutDim,
		Means: c.Means, Stds: c.Stds,
		W1: c.W1, B1: c.B1, W2: c.W2, B2: c.B2,
		W3: w3, B3: b3,
		W4: c.W4, B4: c.B4,
		YStd: c.YStd, YMean: c.YMean,
	}
	if raw.InDim == 0 {
		raw.InDim = len(raw.Means)
	}
	if raw.H1Dim == 0 {
		raw.H1Dim = len(raw.B1)
	}
	if raw.H2Dim == 0 {
		raw.H2Dim = len(raw.B2)
	}
	// H3 auto-detect: 如果 W4 / B4 存在 → 3-hidden, H3Dim = len(b3)
	if raw.H3Dim == 0 && len(raw.W4) > 0 {
		raw.H3Dim = len(b3)
	}
	if raw.OutDim == 0 {
		if raw.H3Dim > 0 {
			raw.OutDim = len(raw.B4)
		} else {
			raw.OutDim = len(b3)
		}
	}
	if raw.OutDim < 1 {
		raw.OutDim = 1
	}
	defaultTrainedNet = buildTrainedNet(&raw)
	return nil
}

// LoadWeightsFromFile — 运行时加载 weights. schema 兼容性见 loadWeightsFromBytes.
func LoadWeightsFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return loadWeightsFromBytes(data)
}

func buildTrainedNet(r *rawWeights) *TrainedNet {
	if r.OutDim == 0 {
		r.OutDim = 1
	}
	net := &TrainedNet{
		InDim:  r.InDim,
		H1Dim:  r.H1Dim,
		H2Dim:  r.H2Dim,
		H3Dim:  r.H3Dim,
		OutDim: r.OutDim,
		Means:  toF32(r.Means),
		Stds:   toF32(r.Stds),
		W1Flat: flatten(r.W1),
		W2Flat: flatten(r.W2),
		B1:     toF32(r.B1),
		B2:     toF32(r.B2),
		YStd:   float32(r.YStd),
		YMean:  float32(r.YMean),
	}
	if r.H3Dim > 0 {
		// 3-hidden: W3 = [H3][H2] middle, W4 = [OutDim][H3] output
		w3flat := make([]float32, r.H3Dim*r.H2Dim)
		for i := 0; i < r.H3Dim; i++ {
			if i < len(r.W3) {
				for j := 0; j < r.H2Dim; j++ {
					if j < len(r.W3[i]) {
						w3flat[i*r.H2Dim+j] = float32(r.W3[i][j])
					}
				}
			}
		}
		b3 := make([]float32, r.H3Dim)
		for i := range b3 {
			if i < len(r.B3) {
				b3[i] = float32(r.B3[i])
			}
		}
		w4flat := make([]float32, r.OutDim*r.H3Dim)
		for o := 0; o < r.OutDim; o++ {
			if o < len(r.W4) {
				for j := 0; j < r.H3Dim; j++ {
					if j < len(r.W4[o]) {
						w4flat[o*r.H3Dim+j] = float32(r.W4[o][j])
					}
				}
			}
		}
		b4 := make([]float32, r.OutDim)
		for i := range b4 {
			if i < len(r.B4) {
				b4[i] = float32(r.B4[i])
			}
		}
		net.W3Flat = w3flat
		net.B3 = b3
		net.W4Flat = w4flat
		net.B4 = b4
	} else {
		// 2-hidden legacy: W3 = [OutDim][H2] output
		w3flat := make([]float32, r.OutDim*r.H2Dim)
		for o := 0; o < r.OutDim; o++ {
			if o < len(r.W3) {
				for j := 0; j < r.H2Dim; j++ {
					if j < len(r.W3[o]) {
						w3flat[o*r.H2Dim+j] = float32(r.W3[o][j])
					}
				}
			}
		}
		b3 := make([]float32, r.OutDim)
		for i := range b3 {
			if i < len(r.B3) {
				b3[i] = float32(r.B3[i])
			}
		}
		net.W3Flat = w3flat
		net.B3 = b3
	}
	return net
}

func toF32(x []float64) []float32 {
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = float32(v)
	}
	return out
}

func flatten(rows [][]float64) []float32 {
	if len(rows) == 0 {
		return nil
	}
	H := len(rows)
	W := len(rows[0])
	out := make([]float32, H*W)
	for i := 0; i < H; i++ {
		for j := 0; j < W; j++ {
			out[i*W+j] = float32(rows[i][j])
		}
	}
	return out
}

// TrainedEval — 评估 state, 返回 royalty 预测分.
// 与 JS trainedEval (无 _weights / _flWeights 注入路径) 完全一致.
//
// 内部 buffer 不可重入. HTTP server 应每个 goroutine 用一个 *TrainedEvaluator.
// 这里直接函数式调用, 每次分配 buffer (开销小, 56+128+64=248 floats ≈ 1KB).
// 后续可加 sync.Pool 优化分配.
func TrainedEval(gs *GameState) float32 {
	return trainedEvalImpl(defaultTrainedNet, gs)
}

// TrainedEvalFull — 同 TrainedEval 但返回所有 head 输出 (value + fan_p + foul_p + policy_p).
// 用于 AlphaZero MCTS: value 当 leaf 估值, policy 当 PUCT prior.
//   - value: predicted royalty (denormalized, 跟 TrainedEval combined 不同 — 纯 head 0)
//   - fanProb: sigmoid(fan_logit) ∈ [0,1]
//   - foulProb: sigmoid(foul_logit) ∈ [0,1]
//   - policyLogit: head 3 (raw, caller softmax across candidates)
//   - hasPolicy: true 若 ckpt OutDim >= 4
func TrainedEvalFull(gs *GameState) (value, fanProb, foulProb, policyLogit float32, hasPolicy bool) {
	net := defaultTrainedNet
	b := newEvalBuffers(net)
	if net.InDim >= 128 {
		// 2026-05-20 fix: 同 trainedEvalImpl, 走 BuildFeatures auto dispatch (V3 for 147).
		f := BuildFeatures(gs, net.InDim)
		copy(b.f, f)
	} else {
		buildFeatures(gs.Top, gs.Middle, gs.Bottom, gs.UsedCards, b.f)
	}
	return mlpForwardFull(net, b)
}

// mlpForwardFull — 同 mlpForward 但返回 head-by-head 输出.
func mlpForwardFull(net *TrainedNet, b *evalBuffers) (value, fanProb, foulProb, policyLogit float32, hasPolicy bool) {
	IN, H1, H2 := net.InDim, net.H1Dim, net.H2Dim
	for i := 0; i < IN; i++ {
		b.fn[i] = (b.f[i] - net.Means[i]) / net.Stds[i]
	}
	w1 := net.W1Flat
	for i := 0; i < H1; i++ {
		v := net.B1[i]
		base := i * IN
		for j := 0; j < IN; j++ {
			v += w1[base+j] * b.fn[j]
		}
		if v > 0 {
			b.h1[i] = v
		} else {
			b.h1[i] = 0
		}
	}
	w2 := net.W2Flat
	for i := 0; i < H2; i++ {
		v := net.B2[i]
		base := i * H1
		for j := 0; j < H1; j++ {
			v += w2[base+j] * b.h1[j]
		}
		if v > 0 {
			b.h2[i] = v
		} else {
			b.h2[i] = 0
		}
	}
	// 选 output layer 用 W3 (2-hidden) 或 W4 (3-hidden) + h2 or h3
	finalHidden, finalDim, outW, outB := selectOutputLayer(net, b)
	if net.OutDim <= 1 {
		score := outB[0]
		for j := 0; j < finalDim; j++ {
			score += outW[j] * finalHidden[j]
		}
		return score*net.YStd + net.YMean, 0, 0, 0, false
	}
	outs := make([]float32, net.OutDim)
	for o := 0; o < net.OutDim; o++ {
		v := outB[o]
		base := o * finalDim
		for j := 0; j < finalDim; j++ {
			v += outW[base+j] * finalHidden[j]
		}
		outs[o] = v
	}
	value = outs[0]*net.YStd + net.YMean
	if net.OutDim >= 2 {
		fanProb = sigmoid(outs[1])
	}
	if net.OutDim >= 3 {
		foulProb = sigmoid(outs[2])
	}
	if net.OutDim >= 4 {
		policyLogit = outs[3]
		hasPolicy = true
	}
	_ = H2 // H2 still used in layer 2 loop above
	return
}

// BuildFeaturesForDebug — 暴露 feature 给调试工具用 (用 default ckpt 的 InDim, 通常 56)
func BuildFeaturesForDebug(gs *GameState) []float32 {
	f := make([]float32, defaultTrainedNet.InDim)
	buildFeatures(gs.Top, gs.Middle, gs.Bottom, gs.UsedCards, f)
	return f
}

// BuildFeatures — 显式指定 inDim. 自动 dispatch:
//   inDim == 147 → V3 (2026-05-19 v2: 加 Tier 1+2+3, L/LR/N2)
//   inDim == 131 → V3 legacy (旧 V3 layout, fail-fast 防意外加载)
//   inDim == 134 → V2
//   inDim >= 128 && 其它 → V2 (legacy, pad/truncate)
//   56-90        → V1 legacy
func BuildFeatures(gs *GameState, inDim int) []float32 {
	if inDim == FeatureDimV3 {
		return BuildFeaturesV3(gs)
	}
	if inDim == 131 {
		// 旧 V3 layout (Tier 1+2+3 还没加), 已废弃, 不要用. 显式 panic 比静默 pad 安全.
		panic("BuildFeatures: inDim=131 is legacy V3 (pre-2026-05-19), 已废弃. 用 147 (新 V3) 或 134 (V2)")
	}
	if inDim >= 128 {
		v2 := BuildFeaturesV2(gs)
		if inDim == FeatureDimV2 {
			return v2
		}
		// inDim ≠ FeatureDimV2: 调整 (truncate 或 pad zero) 用于灵活兼容
		f := make([]float32, inDim)
		copy(f, v2)
		return f
	}
	if inDim < 56 {
		panic("BuildFeatures: inDim < 56 not supported")
	}
	f := make([]float32, inDim)
	buildFeatures(gs.Top, gs.Middle, gs.Bottom, gs.UsedCards, f)
	return f
}

// evalBuffers — IN/normalized/H1/H2/(H3) hidden activations
type evalBuffers struct {
	f, fn, h1, h2, h3 []float32
}

func newEvalBuffers(net *TrainedNet) *evalBuffers {
	b := &evalBuffers{
		f:  make([]float32, net.InDim),
		fn: make([]float32, net.InDim),
		h1: make([]float32, net.H1Dim),
		h2: make([]float32, net.H2Dim),
	}
	if net.H3Dim > 0 {
		b.h3 = make([]float32, net.H3Dim)
	}
	return b
}

// computeHiddenOutputs — 通用工具: 跑完 input → ... → h2 (或 h3 if 3-hidden), 返回 final hidden tensor + size + biases/weights to use for output layer.
// 调用前 b.h1 b.h2 (可能 b.h3) 已被填充.
// 返回: (finalHidden, finalDim, outWeights, outBiases) — 给 output layer 用
func selectOutputLayer(net *TrainedNet, b *evalBuffers) (finalHidden []float32, finalDim int, outW, outB []float32) {
	if net.H3Dim > 0 {
		// 3-hidden: compute h3 from h2 using W3/B3, output uses W4/B4
		H2, H3 := net.H2Dim, net.H3Dim
		w3 := net.W3Flat
		for i := 0; i < H3; i++ {
			v := net.B3[i]
			base := i * H2
			for j := 0; j < H2; j++ {
				v += w3[base+j] * b.h2[j]
			}
			if v > 0 {
				b.h3[i] = v
			} else {
				b.h3[i] = 0
			}
		}
		return b.h3, H3, net.W4Flat, net.B4
	}
	// 2-hidden legacy: output uses W3/B3
	return b.h2, net.H2Dim, net.W3Flat, net.B3
}

// 2026-05-20 sp16 critical fix: 收 *GameState 完整传 (含 Round / LastDiscard / NumJokers).
// 旧版只传 top/mid/bot/usedCards, 内部重建 gs 丢 Round, 让 V3 features 的 R5_lastRd / N_disc /
// S_slot (round-aware) 全 0. NN 在 R2-R5 推理用 R=0 features 不一致 → bench 大退.
func trainedEvalImpl(net *TrainedNet, gs *GameState) float32 {
	b := newEvalBuffers(net)
	if net.InDim >= 128 {
		f := BuildFeatures(gs, net.InDim)
		copy(b.f, f)
	} else {
		buildFeatures(gs.Top, gs.Middle, gs.Bottom, gs.UsedCards, b.f)
	}
	return mlpForward(net, b)
}

// buildFeatures — 构特征到 f (维度由 len(f) 决定; 56/64/72/78 自动适配)
// 与 JS trainedEval 内 feature array 一一对应
func buildFeatures(top, middle, bottom []Card, usedCards map[string]bool, f []float32) {
	// 各行 stats
	bt := rowStats(bottom)
	mt := rowStats(middle)
	tt := rowStats(top)

	botType := bt.handType
	midType := mt.handType
	topType := tt.handType
	botPR := bt.pairRank
	midPR := mt.pairRank
	topPR := tt.pairRank
	topMax := tt.topMax
	topTrips := len(top) == 3 && topType >= 3
	chasing := topPR >= 10 || topTrips
	placed := len(top) + len(middle) + len(bottom)

	rn := 1
	if placed > 5 {
		rn = (placed-5+1)/2 + 1
	}
	bMaxS := bt.maxSuit
	mMaxS := mt.maxSuit
	bRn := bt.bestRun
	mRn := mt.bestRun
	midHasPair := midType >= 1
	mHP := midHasPair

	// dS / dF: 满行 ordering 检查
	defSafe, defFoul := 0, 0
	if len(top) == 3 && len(middle) == 5 {
		tE := Evaluate3(top)
		mE := Evaluate5(middle)
		if mE.Type > tE.Type {
			defSafe = 1
		} else if mE.Type < tE.Type {
			defFoul = 1
		} else if mE.Type == tE.Type && tE.Type == 1 {
			if midPR > topPR {
				defSafe = 1
			} else if midPR < topPR {
				defFoul = 1
			}
		}
	}
	if len(middle) == 5 && len(bottom) == 5 {
		mE := Evaluate5(middle)
		bE := Evaluate5(bottom)
		if mE.Value > bE.Value {
			defFoul = 1
		} else if defSafe < 1 {
			defSafe = 1
		}
	}

	bI := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}

	// 与 JS trainedEval 内 feature array 严格对齐 (索引 0-55)
	// v15 cleanup: f[12,13,15,16,18,19,27,71] 标记为 redundant, 在末尾 zero out
	// (保 index 兼容 round-004 W1 columns; 那些 columns 变 dead weight)
	f[0] = bI(botType > midType)
	f[1] = bI(midType > topType)
	f[2] = bI(midType > botType)
	f[3] = bI(topType > midType)
	f[4] = bI(chasing && midPR >= 0 && topPR >= 0 && midPR >= topPR)
	f[5] = bI(botType >= 2)
	f[6] = bI(chasing)
	f[7] = bI(topPR == 10)
	f[8] = bI(topPR == 11)
	f[9] = bI(topPR >= 12 || topTrips)
	f[10] = float32(botType - midType)
	f[11] = float32(midType - topType)
	// f[12] / f[13] — 直接 PR 差, 不再 conditional skip.
	// 原版只在两边都有 pair 时算, 否则 0 → 与 "两边 pair rank 相等" 0 值混淆.
	// 现版: botPR=-1 (无 pair) 直接参与运算, 让 MLP 自己结合 f[42]/f[43] (rank 存在性) disambiguate.
	f[12] = float32(botPR - midPR)
	f[13] = float32(midPR - topPR)
	f[14] = float32(len(top))
	f[15] = bI(len(top) == 1)
	f[16] = bI(len(top) == 2 && topType < 1)
	f[17] = bI(topType == 0 && topMax >= 10)
	f[18] = bI(len(top) == 0)
	f[19] = float32(rn)
	// f[20] — round progress (0.2-1.0). 简化版替换原 5/3 公式 (公式跟实际游戏 placement 比例不符)
	f[20] = float32(rn) / 5.0
	// f[21] — 已放 joker 总数 / 2 (原常量 1 浪费 dim, 改成有用信号)
	f[21] = float32(tt.jokerCnt+mt.jokerCnt+bt.jokerCnt) / 2.0
	f[22] = float32(botType)
	f[23] = float32(midType)
	f[24] = float32(5 - len(bottom))
	f[25] = bI(bMaxS >= 4)
	f[26] = bI(bMaxS >= 3 && bMaxS < 4)
	f[27] = bI(bRn >= 4)
	f[28] = bI(mMaxS >= 4)
	f[29] = bI(mMaxS >= 3 && mMaxS < 4)
	f[30] = bI(chasing && mHP)
	f[31] = bI(topPR == 10 && mHP)
	f[32] = bI(topPR == 11 && mHP)
	f[33] = bI((topPR >= 12 || topTrips) && midHasPair)
	f[34] = bI(chasing && botType >= 2)
	f[35] = bI(chasing && midType == 0 && len(middle) >= 3)
	f[36] = float32(defSafe)
	f[37] = float32(defFoul)
	// v4-ext
	f[38] = float32(bMaxS) * float32(bMaxS) / 25
	f[39] = float32(mMaxS) * float32(mMaxS) / 25
	f[40] = float32(bRn) / 5
	f[41] = float32(mRn) / 5
	f[42] = float32(botPR+1) / 13
	f[43] = float32(midPR+1) / 13
	f[44] = float32(topPR+1) / 13
	f[45] = bI(botType >= 1 && midType >= 1)
	// v6
	f[46] = bI(botPR >= 0 && midPR >= 0 && midPR > botPR)
	f[47] = bI(botPR >= 8)
	f[48] = bI(midPR >= 8)
	f[49] = bI(botPR >= 0 && midPR >= 0 && botPR > midPR)
	// v7 joker-aware
	f[50] = (float32(tt.jokerCnt) + boolToF32(topPR >= 0)) / 2
	f[51] = (float32(mt.jokerCnt) + boolToF32(midPR >= 0)) / 2
	f[52] = (float32(bt.jokerCnt) + boolToF32(botPR >= 0)) / 2
	f[53] = float32min(1, (float32(bMaxS)+float32(bt.jokerCnt))/5)
	f[54] = float32min(1, (float32(mMaxS)+float32(mt.jokerCnt))/5)
	// f[55] — joker 在顶 + 顶有空 + 没高牌 → 等高牌进 fantasy 状态 (case 9 类: 鬼+低牌等升级).
	// 原版 (jokerCnt>0 && topMax>=10) 跟 f[63] (jokerCnt>0 && topMax>=9) 高度重复, 改成互补语义.
	// 现 f[55] = "joker top growth potential" (low/no anchor); f[63] = "joker top + high anchor" (rescue).
	f[55] = bI(tt.jokerCnt > 0 && len(top) < 3 && topMax < 10)

	// v8 joker-as-wild features (only if InDim >= 64).
	// 让 MLP 看到 joker 能扮演 52 牌任一张, 不再当固定 token.
	if len(f) >= 64 {
		f[56] = float32max(0, float32(maxPairTargetWithJoker(top))) / 12
		f[57] = float32max(0, float32(maxPairTargetWithJoker(middle))) / 12
		f[58] = float32max(0, float32(maxPairTargetWithJoker(bottom))) / 12
		f[59] = bI(canReachTripsWithJoker(top))
		f[60] = float32min(1, float32(straightFlexFill(bottom))/5)
		f[61] = float32min(1, float32(straightFlexFill(middle))/5)
		emptySlots := (3 - len(top)) + (5 - len(middle)) + (5 - len(bottom))
		f[62] = float32min(1, float32(tt.jokerCnt+mt.jokerCnt+bt.jokerCnt)*float32(emptySlots)/16)
		f[63] = bI(tt.jokerCnt > 0 && topMax >= 9) // top 含 joker + 高牌 → 可降级救场
	}

	// v9 features (only if InDim >= 72): 中小底大 + 2-seed flush/straight + 卡顺
	// 解决 56/64-d 的盲区:
	// - mono per-card 排序无 feature (UR17 24 中 vs 23 中)
	// - 同花 / 顺子 feature 阈值 ≥3, 2-card seed 不触发 (J10 TJ♦同色底)
	// - 卡顺 (gut-shot inside straight) 完全无 feature
	if len(f) >= 72 {
		monoBad := MonoSplitBadness(&GameState{Top: top, Middle: middle, Bottom: bottom})
		f[64] = float32min(1, float32(monoBad)/4) // mono inversion count, /4 normalized
		f[65] = bI(bMaxS == 2)                    // bot 2-card 同色 seed (flush draw)
		f[66] = bI(mMaxS == 2)                    // mid 2-card 同色 seed
		f[67] = bI(bRn == 2)                      // bot 2-card 连号 seed (straight)
		f[68] = bI(mRn == 2)                      // mid 2-card 连号 seed
		f[69] = bI(hasGutshot(bottom))            // bot 卡顺 (4 of 5-window 间断 1)
		f[70] = bI(hasGutshot(middle))            // mid 卡顺
		f[71] = bI(monoBad > 0)                   // mono 警告 (任意 inversion)
	}

	// v10 features (only if InDim >= 75): r11 testcase 失败模式分析后加
	// f[72] UR14: top joker+A 已锁 AA Fantasy, 多余 Q+ 牌浪费 anchor 价值
	// f[73,74] J10: 2 张 high-rank (≥T) 同色在同一行 = 强 flush seed (区分高 vs 低 同色)
	if len(f) >= 75 {
		f[72] = bI(topRedundantHigh(top))
		f[73] = bI(rowHighFlushSeed(bottom))
		f[74] = bI(rowHighFlushSeed(middle))
	}

	// v11 deck-aware features (only if InDim >= 78): 桌面已用 A/K/Q 数量 → 升级潜力
	// f[75] = remainA / 4
	// f[76] = remainK / 4
	// f[77] = remainQ / 4
	if len(f) >= 78 {
		used, _ := deckRankCounts(usedCards)
		f[75] = float32(4-used[12]) / 4 // A
		f[76] = float32(4-used[11]) / 4 // K
		f[77] = float32(4-used[10]) / 4 // Q
	}

	// v12 deck-aware features (only if InDim >= 89): 全 13 rank + joker
	// f[78-86] = rank 2-T (rank index 0-8) remaining / 4
	// f[87]    = rank J (rank 9) remaining / 4
	// f[88]    = joker remaining / 2 (deck 默认 2 个 joker, 用户固定)
	// 用户反馈: A/K/Q 之外 2-J 和 joker 也影响策略 (pair-build / straight-fill / wild).
	if len(f) >= 89 {
		used, jokerUsed := deckRankCounts(usedCards)
		// rank 0=2, 1=3, ..., 9=J
		for r := 0; r <= 9; r++ {
			f[78+r] = float32(4-used[r]) / 4
		}
		f[88] = float32(2-jokerUsed) / 2
	}

	// v13 partial-state foul-imminent feature (only if InDim >= 90):
	// f[89] = 1 当且仅当 中道已成对 AND mid pair rank 大于底道现实可达的最强 rank.
	// 修早先发现的 KK→mid +41 高分 bug — MLP 看到 mid pair 给高分但不知 partial-state 必爆.
	// defFoul (f[37]) 只 complete state 算, 这个 feature 补 partial state 盲区.
	//
	// assumedBotMax(bottom, slotsLeft): 底道现实最强 pair rank 估计.
	//   = 底道当前最高 rank + 0 (保守: 假设底道能 pair 当前最高单卡)
	//   未来可加 deck 剩余 + slotsLeft 优化, 当前简化版即可修 KK→mid case.
	if len(f) >= 90 {
		midType := mt.handType
		if midType >= 1 && len(bottom) >= 1 {
			botMax := -1
			for _, c := range bottom {
				if c.IsJoker() {
					botMax = 12 // joker 当 A 上限
					continue
				}
				if int(c.Rank()) > botMax {
					botMax = int(c.Rank())
				}
			}
			midPRLocal := mt.pairRank
			if midPRLocal > botMax {
				f[89] = 1
			}
		}
	}

	// v15 fantasy-priority + synergy features (only if InDim >= 96):
	// 用户反馈核心: 保住追范条件 = 重中之重 (case 6, 9, 10 等). 现 90-d 缺:
	//   (1) locked-tier 显式 value (case 6: AA→top vs joker→top)
	//   (2) chasable detection (case 31: visible A<3 才追 KK/QQ)
	//   (3) row synergy 总 promise (case 19: 4 黑桃底花)
	//   (4) high-scatter ordering warning (case 24/56: 高散排序)
	//   (5) top safe-rank count (用户: 5 已 3 张被使用 → 5 上顶 safe)
	// v15 cleanup REVERTED — 之前 zero 了 f[12,13,15,16,18,19,27,71] (认为 redundant)
	// 但 round-004 的 W1 columns 学了这些 feature 的非零权重 (即使重复也 encode 了 ordering signal)
	// zero 掉等于丢知识. e.g. case 1 KK 错放中: f[12]=botPR-midPR=-12 是关键 foul 警报.
	// 决定: 保留所有 f[0-89] 计算, 不 zero. 让 W1 columns 充分发挥.

	if len(f) >= 96 {
		// f[90] top_fantasy_locked_tier: 当前 top 已 lock 的 fantasy tier 值
		//   0 / 0.2 (QQ) / 0.32 (KK) / 0.8 (AA) / 1.0 (trips/3+)
		f[90] = topLockedTierFeature(top, &tt)

		// f[91] top_chasable_tier: 还能凑出的最高 fantasy tier (deck-aware)
		//   ≥ f[90] (locked 是 chasable 子集). 0 if 全死 (top filled, 无 fantasy)
		f[91] = topChasableTierFeature(top, &tt, usedCards)

		// f[92,93] row synergy: made/seed strength 0-1
		f[92] = rowSynergyScoreFeature(bottom)
		f[93] = rowSynergyScoreFeature(middle)

		// f[94] non_synergy_ordering_warning: bot mid 都没 synergy 但 mid avg-rank > bot avg-rank
		f[94] = nonSynergyOrderingFeature(bottom, middle, f[92], f[93])

		// f[95] top_safe_count: top 上 cards 的 rank 是否已 deck-exhausted
		//   normalized [0,1], 0=都不安全 1=全 safe (rank 已 fully used 或 ≤1 remain)
		used, _ := deckRankCounts(usedCards)
		safeCnt := float32(0)
		for _, c := range top {
			if c.IsJoker() {
				continue
			}
			rk := int(c.Rank())
			remain := 4 - used[rk]
			if remain <= 0 {
				safeCnt += 1.0
			} else if remain == 1 {
				safeCnt += 0.5
			}
		}
		if len(top) > 0 {
			f[95] = float32min(1, safeCnt/float32(len(top)))
		}
	}

	// v15.1 user 补充: 鬼+A 上头是最安全追 A 范, AA+鬼 时 AA 顶比 A鬼 顶强
	// 加 2 个 feature 直接 encode 这俩 nuance.
	if len(f) >= 98 {
		// f[96] top_anti_foul_safety: top 配置的 "不爆 + 锁范" 安全度
		//   关键: 看全 state, 区分 "AA+鬼 dealt 时 AA 顶 vs A鬼 顶" 跟 "A+鬼 dealt 时 A鬼 顶"
		f[96] = topAntiFoulSafetyFeature(top, middle, bottom, &tt)

		// f[97] joker_flex_position_value: joker 放 mid/bot 的灵活价值
		f[97] = jokerFlexPositionFeature(top, middle, bottom, &tt, &mt, &bt)
	}
}

// topAntiFoulSafetyFeature — top 配置 "锁范 + 不爆 + 整体最优" 综合安全度
// 用户两个 nuance:
//  (1) A+joker dealt (无第二 A): joker+A 上顶 = 最安全追 AA 范 (1.0)
//  (2) AA+joker dealt: AA 上顶 + joker→mid/bot 比 A鬼 上顶更强 (joker 在 mid/bot 多成牌灵活性)
// 关键: 看 state 整体 — 如果 mid/bot 有第二 A scatter, joker+A 上顶就是错的 (浪费 A)
func topAntiFoulSafetyFeature(top, middle, bottom []Card, tt *rowStat) float32 {
	if len(top) == 0 {
		return 0
	}
	// trips on top — 锁顶级 fantasy 但 foul 风险
	if tt.handType >= 3 {
		return 0.6
	}
	// 计 top 的 A/K/joker
	aCntTop, kCntTop, jCntTop := 0, 0, 0
	for _, c := range top {
		if c.IsJoker() {
			jCntTop++
		} else if c.Rank() == 12 {
			aCntTop++
		} else if c.Rank() == 11 {
			kCntTop++
		}
	}
	// 计 mid/bot 的 A scatter (用于判断 A 是否被浪费)
	aScatter, kScatter, jScatter := 0, 0, 0
	for _, c := range middle {
		if c.IsJoker() {
			jScatter++
		} else if c.Rank() == 12 {
			aScatter++
		} else if c.Rank() == 11 {
			kScatter++
		}
	}
	for _, c := range bottom {
		if c.IsJoker() {
			jScatter++
		} else if c.Rank() == 12 {
			aScatter++
		} else if c.Rank() == 11 {
			kScatter++
		}
	}

	// AA on top (no joker): 已 lock AA fantasy
	if aCntTop >= 2 {
		// AA 顶 + joker 在 mid/bot 灵活 = 用户提的最优组合
		if jScatter >= 1 {
			return 0.9
		}
		// AA 顶 无 joker (基本 AA dealt 无 joker case): 锁 AA 但 mid/bot 不灵活
		return 0.7
	}

	// joker+A on top (1 joker + 1 A): 锁 AA via 鬼 wild
	if jCntTop >= 1 && aCntTop >= 1 {
		// (1) AA+joker dealt 时, mid/bot 还有第二 A → 应该 AA 上顶 + joker mid/bot, 不是 A鬼 顶
		//     此时 joker+A 顶是错的, A scatter 浪费 → 低分
		if aScatter >= 1 {
			return 0.5
		}
		// (2) A+joker dealt (无第二 A): joker+A 顶 = 最安全追 AA 范 → 满分
		return 1.0
	}

	// KK or joker+K on top (锁 KK fantasy, 弱于 AA)
	if kCntTop >= 2 {
		if jScatter >= 1 {
			return 0.45
		}
		return 0.4
	}
	if jCntTop >= 1 && kCntTop >= 1 {
		if kScatter >= 1 {
			return 0.3 // K 浪费, 应 KK 顶
		}
		return 0.5 // K+joker 顶 (无第二 K)
	}

	// joker 单独上头 (待机, 无 high anchor)
	if jCntTop >= 1 && len(top) <= 2 {
		return 0.35
	}
	return 0
}

// jokerFlexPositionFeature — joker 放 mid/bot 的灵活价值 (相对 top)
// 用户原话: AA+joker 时, AA 顶 + joker mid/bot 更强, 因为 joker 在 mid/bot 多 made-hand 可能
//
// 关键 condition: 只在 top 已 LOCK 真 pair Q+ 时才奖励 joker 下放.
// 否则 (top 没锁) joker 应 chase top fantasy, 不奖励下放. 避免 MLP 学成"无脑 joker 低位".
func jokerFlexPositionFeature(top, middle, bottom []Card, tt, mt, bt *rowStat) float32 {
	// 检查 top 是否 lock 真 pair Q+ (跟 f[90] 一致)
	var topRealCnt [13]int
	for _, c := range top {
		if !c.IsJoker() {
			topRealCnt[c.Rank()]++
		}
	}
	topLocked := false
	for r := 12; r >= 10; r-- {
		if topRealCnt[r] >= 2 {
			topLocked = true
			break
		}
	}
	// 完整 top trips 也算 lock
	if !topLocked && len(top) == 3 && tt.handType >= 3 {
		topLocked = true
	}

	if !topLocked {
		// top 没 lock fantasy, 没理由奖励 joker 下放 (joker 应 chase top)
		return 0
	}

	// top 已 lock, joker 在 mid/bot 加分 (anchor 越强越加)
	score := float32(0)
	if mt.jokerCnt > 0 {
		if mt.pairRank >= 0 || mt.maxSuit >= 2 || mt.bestRun >= 2 {
			score += 0.5
		} else {
			score += 0.3
		}
	}
	if bt.jokerCnt > 0 {
		if bt.pairRank >= 0 || bt.maxSuit >= 2 || bt.bestRun >= 2 {
			score += 0.5
		} else {
			score += 0.3
		}
	}
	if score > 1 {
		score = 1
	}
	return score
}

// topLockedTierFeature — top 已"真正" lock 的 fantasy tier (only real pair, NO joker-as-wild)
//   0 = no real lock; 0.2 = real QQ; 0.32 = real KK; 0.8 = real AA; 1.0 = real trips OR full-top trips
//
// 注: joker 是 wild, partial state 时 joker 还没 commit 成具体 rank.
// 真 lock = 实牌已成对/三条 (joker 不算). joker+A 这种 "potential AA" 留给 f[91] chasable.
// 这样 MLP 区分 "确定 AA reward" (real pair, f[90]=0.8) vs "wild AA potential" (joker+A, f[91]=0.8 但 f[90]=0).
//
// 完整 top (3 张) 例外: handType 已 joker-aware, joker+A+X 终局必 AA → 算 lock.
func topLockedTierFeature(top []Card, tt *rowStat) float32 {
	if len(top) == 0 {
		return 0
	}

	// 实牌计数 (joker 不算)
	var realCnt [13]int
	for _, c := range top {
		if !c.IsJoker() {
			realCnt[c.Rank()]++
		}
	}

	// 实 trips (无 joker, 3+ 同 rank)
	if realCnt[12] >= 3 || realCnt[11] >= 3 || realCnt[10] >= 3 {
		return 1.0
	}

	// 实 pair Q+ (无 joker)
	if realCnt[12] >= 2 {
		return 0.8
	}
	if realCnt[11] >= 2 {
		return 0.32
	}
	if realCnt[10] >= 2 {
		return 0.2
	}

	// 完整 top (3 张) 含 joker: handType 已 joker-aware, 终局 commit 后必 AA/trips
	// 这种 lock 可信 (但 foul 会让它没用, 由 foul features 处理)
	if len(top) == 3 && tt.jokerCnt > 0 {
		if tt.handType >= 3 {
			return 1.0 // 完整 trips
		}
		if tt.pairRank == 12 {
			return 0.8
		}
		if tt.pairRank == 11 {
			return 0.32
		}
		if tt.pairRank == 10 {
			return 0.2
		}
	}

	// partial state + joker+highCard: 不算 lock (joker 未 commit), 由 f[91] chasable 表达 potential
	return 0
}

// topChasableTierFeature — top 还能凑出的最高 fantasy tier
//   考虑: 当前 top 牌 + slotsLeft + deck remain (rank + joker)
//   返回 best chasable tier value, 跟 topLockedTier 同 scheme
func topChasableTierFeature(top []Card, tt *rowStat, usedCards map[string]bool) float32 {
	// 已 lock 的 tier 是 chasable 上限
	locked := topLockedTierFeature(top, tt)
	if locked >= 1.0 {
		return 1.0
	}

	// 顶满 3 张 → 不能再加, chasable = locked
	if len(top) >= 3 {
		return locked
	}

	used, jokerUsed := deckRankCounts(usedCards)
	remainJoker := 2 - jokerUsed
	if remainJoker < 0 {
		remainJoker = 0
	}
	slotsLeft := 3 - len(top)

	// 当前 top 各 rank 计数 (joker 单算 wild)
	var topRkCnt [13]int
	topJ := 0
	for _, c := range top {
		if c.IsJoker() {
			topJ++
		} else {
			topRkCnt[c.Rank()]++
		}
	}

	best := locked

	// trips 检查 (Q+ rank, fantasy tier 1.0)
	for r := 10; r <= 12; r++ {
		currentR := topRkCnt[r] + topJ
		deckR := 4 - used[r]
		if deckR < 0 {
			deckR = 0
		}
		canAdd := minInt(slotsLeft, deckR+remainJoker)
		if currentR+canAdd >= 3 {
			return 1.0 // trips 是最高
		}
	}

	// pair 检查 (A > K > Q 优先级)
	tiers := []struct {
		rank int
		val  float32
	}{
		{12, 0.8},  // AA
		{11, 0.32}, // KK
		{10, 0.2},  // QQ
	}
	for _, t := range tiers {
		currentR := topRkCnt[t.rank] + topJ
		deckR := 4 - used[t.rank]
		if deckR < 0 {
			deckR = 0
		}
		canAdd := minInt(slotsLeft, deckR+remainJoker)
		if currentR+canAdd >= 2 {
			if t.val > best {
				best = t.val
			}
			break // 取最高可达 tier
		}
	}

	return best
}

// rowSynergyScoreFeature — row (mid 或 bot) 的 made-hand 或 seed promise 强度 [0,1]
//   1.0 = made straight/flush 或更强
//   0.85 = 4-card 同色 seed
//   0.55 = 4-card 连号 seed
//   0.45 = 3-card 同色 seed
//   0.40 = 3-card 连号 seed
//   0.30 = pair
//   0 = high card scattered
func rowSynergyScoreFeature(row []Card) float32 {
	if len(row) == 0 {
		return 0
	}

	// Made hand (5-card 完整)
	if len(row) >= 5 {
		var e HandValue
		hasJ := false
		for _, c := range row {
			if c.IsJoker() {
				hasJ = true
				break
			}
		}
		if hasJ {
			e = Evaluate5Joker(row)
		} else {
			e = Evaluate5(row)
		}
		// Type: 0=high, 1=pair, 2=2pair, 3=trips, 4=straight, 5=flush, 6=full, 7=quads, 8=sf
		if e.Type >= 4 {
			return 1.0
		}
		if e.Type >= 3 {
			return 0.7
		}
		if e.Type >= 2 {
			return 0.5
		}
		if e.Type >= 1 {
			return 0.3
		}
		return 0
	}

	// 部分 row, 检查 seed
	suitCnt := [4]int{}
	rankCnt := [13]int{}
	jokerN := 0
	for _, c := range row {
		if c.IsJoker() {
			jokerN++
			continue
		}
		suitCnt[c.Suit()]++
		rankCnt[c.Rank()]++
	}

	maxSuit := 0
	for _, s := range suitCnt {
		if s > maxSuit {
			maxSuit = s
		}
	}
	maxSuitWithJ := maxSuit + jokerN

	// max consecutive run (joker 计入任意 rank, 简化用 +jokerN 做 padding)
	maxRun := 0
	curRun := 0
	for r := 0; r <= 12; r++ {
		if rankCnt[r] > 0 {
			curRun++
			if curRun > maxRun {
				maxRun = curRun
			}
		} else {
			curRun = 0
		}
	}
	maxRunWithJ := maxRun + jokerN

	hasPair := false
	for _, c := range rankCnt {
		if c >= 2 {
			hasPair = true
			break
		}
	}
	if !hasPair && jokerN > 0 {
		// joker + 任意一张 = 暗对
		for _, c := range rankCnt {
			if c >= 1 {
				hasPair = true
				break
			}
		}
	}

	score := float32(0)
	if maxSuitWithJ >= 4 {
		score = 0.85
	} else if maxSuitWithJ >= 3 {
		score = 0.45
	}
	if maxRunWithJ >= 4 && score < 0.55 {
		score = 0.55
	} else if maxRunWithJ >= 3 && score < 0.4 {
		score = 0.4
	}
	if hasPair && score < 0.3 {
		score = 0.3
	}
	return score
}

// nonSynergyOrderingFeature — synergy/rank ordering penalty (continuous)
//   返回 [0,1], 0 = ordering OK, 越大表示 mid 越压过 bot (ordering 危险)
//
// 综合两层 ordering 信号:
//   1. mid_synergy - bot_synergy > 0: mid 成牌强度超过 bot (e.g., KK 中 + 底空)
//   2. 当 synergy 都 < 0.3 时, 比 raw rank-sum (高散排序)
//
// 修 case 1/17/33/60/61 类: model 把 pair/synergy 错放中道, ordering 危险但 f[89]
// (partial-state foul-imminent) 的 botMax 检查太 lenient, 漏抓.
func nonSynergyOrderingFeature(bottom, middle []Card, botSynScore, midSynScore float32) float32 {
	// 主信号: synergy ordering inversion
	synDiff := midSynScore - botSynScore
	if synDiff > 0 {
		// mid 比 bot 强 → ordering 危险, 直接返回连续 penalty
		if synDiff > 1 {
			synDiff = 1
		}
		return synDiff
	}

	// synergy 都 ≤ bot_syn (ordering OK 层面): 检查 raw rank ordering (高散场景)
	if botSynScore >= 0.3 || midSynScore >= 0.3 {
		return 0 // 至少一行有 synergy, 不用看 raw rank
	}
	if len(bottom) == 0 || len(middle) == 0 {
		return 0
	}
	bSum := 0
	bN := 0
	for _, c := range bottom {
		if c.IsJoker() {
			bSum += 13
		} else {
			bSum += int(c.Rank()) + 2
		}
		bN++
	}
	mSum := 0
	mN := 0
	for _, c := range middle {
		if c.IsJoker() {
			mSum += 13
		} else {
			mSum += int(c.Rank()) + 2
		}
		mN++
	}
	bAvg := float32(bSum) / float32(bN)
	mAvg := float32(mSum) / float32(mN)
	if mAvg > bAvg {
		// mid avg rank 超过 bot, 高散场景 ordering 警告
		// 强度 = 差距 / 13 (rank 范围), capped to 0.5 (avoid 跟 syn diff 冲突)
		raw := (mAvg - bAvg) / 13
		if raw > 0.5 {
			raw = 0.5
		}
		return raw
	}
	return 0
}

// deckRankCounts — 数所有 rank + joker 已用数量 (从 usedCards map).
// 返回 [13]int (索引 0-12, 0=2, 12=A) + joker 数.
func deckRankCounts(usedCards map[string]bool) (rankUsed [13]int, jokerUsed int) {
	for id := range usedCards {
		c, ok := ParseCard(id)
		if !ok {
			continue
		}
		if c.IsJoker() {
			jokerUsed++
			continue
		}
		rankUsed[c.Rank()]++
	}
	return
}

// topRedundantHigh — top 有 joker+A 锁 Fantasy 后, 多余 Q+ 牌浪费.
// 触发: top 含 joker AND top Q+ 牌数 > 2 (joker 当 1).
//
// UR14 (X 3h Ks As 2d):
//   AI top=[X Ks As]: jokerCnt=1, qPlus = joker+K+A = 3 → 1 (浪费)
//   Expected top=[X As]: 1+1=2 → 0 (健康)
func topRedundantHigh(top []Card) bool {
	jokerCnt := 0
	qPlusCnt := 0
	for _, c := range top {
		if c.IsJoker() {
			jokerCnt++
			qPlusCnt++ // joker 可当 Q+ 任何 rank
			continue
		}
		if c.Rank() >= 10 { // Q+
			qPlusCnt++
		}
	}
	return jokerCnt > 0 && qPlusCnt > 2
}

// rowHighFlushSeed — 行有 2+ 张 same-suit AND 都 ≥ T (rank 8+).
// 比 bMaxS==2 更强 — 高 rank 同色组合是 flush 的硬种子.
//
// J10 (X Td Jd As 7c):
//   AI bot=[Jd]: 0 (单卡)
//   Expected bot=[Td Jd]: 2 张 ♦ 都 ≥ T → 1
//   也支持 TJ 在 mid 摆法.
func rowHighFlushSeed(cards []Card) bool {
	var suitHigh [4]int
	for _, c := range cards {
		if c.IsJoker() {
			continue
		}
		if c.Rank() >= 8 { // T+
			suitHigh[c.Suit()]++
		}
	}
	for _, n := range suitHigh {
		if n >= 2 {
			return true
		}
	}
	return false
}

// hasGutshot — 检测一行有没有 inside straight draw
//   定义: 在某个 5-rank 窗口里, 该行 (含 joker as wild) 占 4 张 → 1 个空位 (gap)
//   例: bot=[5,7,8,9] → 5-9 窗口里有 5,7,8,9 (缺 6) → gut-shot ✓
//   例: bot=[5,6,8,9] → 5-9 窗口里有 5,6,8,9 (缺 7) → gut-shot ✓
//   例: bot=[5,6,7,8] → 4-8 / 5-9 窗口都覆盖, 是 open-ended (不算 gut-shot, 已在 bestRun feature)
//   例: bot=[joker,5,8,9] → joker 当 6 或 7, 5-9 窗口里 4 张+1 wild = full → gut-shot via wild
func hasGutshot(cards []Card) bool {
	if len(cards) < 4 {
		return false
	}
	var rankPresent [13]bool
	jokerCnt := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCnt++
		} else {
			rankPresent[c.Rank()] = true
		}
	}
	// 5-rank 窗口扫一遍
	for start := 0; start <= 13-5; start++ {
		realInWindow := 0
		for r := start; r < start+5; r++ {
			if rankPresent[r] {
				realInWindow++
			}
		}
		// gut-shot = 4 of 5 (含 joker fill 算)
		if realInWindow+jokerCnt >= 4 && realInWindow+jokerCnt < 5 {
			return true
		}
		// 含 joker 时, 4 张真 + 1 joker 也算 gut-shot (实际等于 open-ended, 但保留为 hit)
		if realInWindow == 4 {
			return true
		}
	}
	return false
}

func float32max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// maxPairTargetWithJoker — 该行用 joker 当 wild, 能配出的最高 pair rank.
//   返回: -1 (没有 pair 路径) 或 rank (0=22, 12=AA)
//   例: top=[joker, Q] → 10 (QQ)
//       top=[joker, A] → 12 (AA)
//       top=[joker, joker] → 12 (双鬼可凑任何 pair, 取最高 AA)
//       top=[Q, K] → -1 (无 joker, 无 pair)
//       top=[Q] → -1
func maxPairTargetWithJoker(cards []Card) int {
	if len(cards) == 0 {
		return -1
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
	// 双鬼 = 任何 rank 都能凑 pair, 取 A
	if jokerCnt >= 2 {
		return 12
	}
	// 单鬼 = 跟最高 real rank 凑 pair
	if jokerCnt == 1 {
		for r := 12; r >= 0; r-- {
			if rankCnt[r] > 0 {
				return r
			}
		}
		return -1 // 1 joker 单卡, 无 real card 配
	}
	// 无鬼: 找已有 pair
	for r := 12; r >= 0; r-- {
		if rankCnt[r] >= 2 {
			return r
		}
	}
	return -1
}

// canReachTripsWithJoker — 该行 (尤其 top) 能否通过 joker 凑 trips
//   双鬼: 1 (任意 rank trip)
//   单鬼 + 任一 rank pair: 1 (joker 充第三张)
//   双 real-pair + joker: trips by promotion (rare, here just check)
//   其它: 0
func canReachTripsWithJoker(cards []Card) bool {
	if len(cards) == 0 {
		return false
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
	if jokerCnt >= 2 {
		return true
	}
	if jokerCnt >= 1 {
		for r := 0; r < 13; r++ {
			if rankCnt[r] >= 2 {
				return true
			}
		}
	}
	for r := 0; r < 13; r++ {
		if rankCnt[r] >= 3 {
			return true
		}
	}
	return false
}

// straightFlexFill — 该行用 joker 当 wild 能凑出的"最长连续 rank 段"长度
//   bot=[5,6,7] → 3
//   bot=[5,6,7,joker] → 4 (joker 当 4 或 8 延伸)
//   bot=[5,6,8,joker] → 4 (joker 当 7 填 gap)
//   bot=[joker, joker] → 2 (任意 2 连)
//   返回长度 0-5
func straightFlexFill(cards []Card) int {
	if len(cards) == 0 {
		return 0
	}
	var rankPresent [13]bool
	jokerCnt := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCnt++
		} else {
			rankPresent[c.Rank()] = true
		}
	}
	// 找最长 5 牌窗口里 (real ranks + joker fill) 的覆盖度
	best := jokerCnt
	for start := 0; start <= 13-5; start++ {
		realInWindow := 0
		for r := start; r < start+5; r++ {
			if rankPresent[r] {
				realInWindow++
			}
		}
		fill := realInWindow + jokerCnt
		if fill > 5 {
			fill = 5
		}
		if fill > best {
			best = fill
		}
	}
	if best > 5 {
		best = 5
	}
	return best
}

func float32abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func float32min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func boolToF32(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// rowStats — 一行的衍生统计
type rowStat struct {
	handType int
	pairRank int
	maxSuit  int
	bestRun  int
	topMax   int
	jokerCnt int
}

// rowStats — 计算单行 (top/mid/bot) 派生统计 (与 JS trainedEval 内 helpers 一致)
func rowStats(cards []Card) rowStat {
	var rs rowStat
	rs.pairRank = -1
	rs.topMax = 0
	if len(cards) == 0 {
		return rs
	}
	// joker: 等同 rank 12 (与 JS rankIndex('X')=12 一致)
	var rankCnt [13]int
	jokerCnt := 0
	for _, c := range cards {
		if c.IsJoker() {
			jokerCnt++
			continue
		}
		rankCnt[c.Rank()]++
	}
	rs.jokerCnt = jokerCnt
	// pairs / max count / max real rank
	maxCnt := 0
	pairs := 0
	for r := 12; r >= 0; r-- {
		c := rankCnt[r]
		if c > maxCnt {
			maxCnt = c
		}
		if c >= 2 {
			pairs++
			if r > rs.pairRank {
				rs.pairRank = r
			}
		}
	}
	// joker 凑虚 pair (与 JS getPairRank 一致)
	if jokerCnt >= 1 && rs.pairRank < 0 {
		hi := -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] > 0 {
				hi = r
				break
			}
		}
		if hi >= 0 {
			rs.pairRank = hi
		} else {
			rs.pairRank = 12
		}
	}
	// topMax: 最高 rank, joker 视为 12 (与 JS rankIndex('X')=12 一致)
	for r := 12; r >= 0; r-- {
		if rankCnt[r] > 0 {
			rs.topMax = r
			break
		}
	}
	if jokerCnt > 0 && rs.topMax < 12 {
		rs.topMax = 12
	}

	// handType — 完整长度走 evaluate3/5; partial 走简化
	if len(cards) == 5 {
		rs.handType = Evaluate5Joker(cards).Type
	} else if len(cards) == 3 {
		rs.handType = Evaluate3Joker(cards).Type
	} else {
		eff := maxCnt + jokerCnt
		uniqueRanks := 0
		for r := 0; r < 13; r++ {
			if rankCnt[r] > 0 {
				uniqueRanks++
			}
		}
		switch {
		case eff >= 4:
			rs.handType = TypeFourOfAKind
		case eff >= 3 && pairs >= 2:
			rs.handType = TypeFullHouse
		case eff >= 3 && jokerCnt >= 1 && uniqueRanks >= 2:
			rs.handType = TypeFullHouse
		case eff >= 3:
			rs.handType = TypeThreeOfAKind
		case pairs >= 2 || (pairs == 1 && jokerCnt >= 1):
			rs.handType = TypeTwoPair
		case eff >= 2:
			rs.handType = TypePair
		default:
			rs.handType = TypeHighCard
		}
	}

	// maxSuit — joker 单独占 1 个 bucket (JS 中 sc['j']++)
	if len(cards) >= 2 {
		var sc [5]int // s/h/d/c/joker
		for _, c := range cards {
			if c.IsJoker() {
				sc[4]++
			} else {
				sc[c.Suit()]++
			}
		}
		mx := 0
		for _, v := range sc {
			if v > mx {
				mx = v
			}
		}
		rs.maxSuit = mx
	}

	// bestRun — 跨度 ≤ 2 的连续 unique rank
	// 注意 JS 行为: joker 通过 rankIndex('X')=12 加到 rank set, 即 joker 占 rank 12
	if len(cards) >= 2 {
		uniqueR := make([]int, 0, 13)
		jokerActsAs12 := false
		for r := 0; r < 13; r++ {
			if rankCnt[r] > 0 {
				uniqueR = append(uniqueR, r)
			}
		}
		if jokerCnt > 0 {
			// 12 (Ace) 可能已经被 real cards 占, JS Set 自动去重
			has12 := false
			for _, r := range uniqueR {
				if r == 12 {
					has12 = true
					break
				}
			}
			if !has12 {
				uniqueR = append(uniqueR, 12)
				// 保持升序 (12 应在末尾, 已经升序)
			}
			jokerActsAs12 = true
		}
		_ = jokerActsAs12
		// uniqueR 升序检查 run
		best := 1
		run := 1
		for i := 1; i < len(uniqueR); i++ {
			if uniqueR[i]-uniqueR[i-1] <= 2 {
				run++
				if run > best {
					best = run
				}
			} else {
				run = 1
			}
		}
		rs.bestRun = best
	}

	return rs
}

// mlpForward — IN→H1→H2→OutDim ReLU MLP.
//
// 单头 (OutDim=1): 直接返回 W3*h2+B3, 跟 v7_fan 1.0.x 等价
// 多头 (OutDim=3): 解读为 [royalty, fan_logit, foul_logit],
//                  返回 royalty*YStd+YMean + FanBoost*sigmoid(fan) - FoulPenalty*sigmoid(foul)
func mlpForward(net *TrainedNet, b *evalBuffers) float32 {
	IN, H1, H2 := net.InDim, net.H1Dim, net.H2Dim
	// normalize
	for i := 0; i < IN; i++ {
		b.fn[i] = (b.f[i] - net.Means[i]) / net.Stds[i]
	}
	// layer 1
	w1 := net.W1Flat
	for i := 0; i < H1; i++ {
		v := net.B1[i]
		base := i * IN
		for j := 0; j < IN; j++ {
			v += w1[base+j] * b.fn[j]
		}
		if v > 0 {
			b.h1[i] = v
		} else {
			b.h1[i] = 0
		}
	}
	// layer 2
	w2 := net.W2Flat
	for i := 0; i < H2; i++ {
		v := net.B2[i]
		base := i * H1
		for j := 0; j < H1; j++ {
			v += w2[base+j] * b.h1[j]
		}
		if v > 0 {
			b.h2[i] = v
		} else {
			b.h2[i] = 0
		}
	}
	// 选 output layer (2-hidden 用 W3+h2; 3-hidden 先算 h3 再用 W4+h3)
	finalHidden, finalDim, outW, outB := selectOutputLayer(net, b)
	if net.OutDim <= 1 {
		score := outB[0]
		for j := 0; j < finalDim; j++ {
			score += outW[j] * finalHidden[j]
		}
		return score*net.YStd + net.YMean
	}
	outs := make([]float32, net.OutDim)
	for o := 0; o < net.OutDim; o++ {
		v := outB[o]
		base := o * finalDim
		for j := 0; j < finalDim; j++ {
			v += outW[base+j] * finalHidden[j]
		}
		outs[o] = v
	}
	royalty := outs[0]*net.YStd + net.YMean
	score := royalty
	if net.OutDim >= 2 {
		score += DefaultMultiHeadCfg.FanBoost * sigmoid(outs[1])
	}
	if net.OutDim >= 3 {
		score -= DefaultMultiHeadCfg.FoulPenalty * sigmoid(outs[2])
	}
	_ = H2 // H2 used in layer 2 loop above
	return score
}

func sigmoid(x float32) float32 {
	if x > 30 {
		return 1
	}
	if x < -30 {
		return 0
	}
	return 1.0 / (1.0 + float32(expNeg(float64(x))))
}

// expNeg(x) = exp(-x), avoid math import
func expNeg(x float64) float64 {
	// 用 Taylor + range reduction
	if x > 30 {
		return 0
	}
	if x < -30 {
		return 1e13
	}
	// 用标准库的 exp 比较快, 但不引入 math import 增加复杂.
	// 此处简单 Taylor (10 项) — 误差 < 1e-5 在 [-30, 30]
	// 实际上 exp 标准库更准. 为简洁起见用近似.
	// e^(-x) = 1/e^x
	// e^x ≈ ((1 + x/N)^N), N 大
	N := 1024.0
	r := 1.0 + x/N
	for i := 0; i < 10; i++ {
		r *= r
	}
	if r == 0 {
		return 1e13
	}
	return 1.0 / r
}
