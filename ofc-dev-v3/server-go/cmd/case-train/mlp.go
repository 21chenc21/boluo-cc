package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// MLP — 90/256/128/3 etc. MLP, 跟 train.go 同 schema 兼容.
type MLP struct {
	InDim  int
	H1     int
	H2     int
	OutDim int
	Means  []float32
	Stds   []float32
	W1     [][]float32 // [H1][InDim]
	B1     []float32   // [H1]
	W2     [][]float32 // [H2][H1]
	B2     []float32   // [H2]
	W3     [][]float32 // [OutDim][H2]
	B3     []float32   // [OutDim]
	YMean  float32
	YStd   float32
	TaskWeights []float32

	// Training scratch buffers
	bufXn       []float32
	bufH1       []float32
	bufH2       []float32
	bufOut      []float32
	bufDOut     []float32
	bufGradH1   []float32
	bufGradH2   []float32
	bufGradW1   [][]float32
	bufGradW2   [][]float32
	bufGradW3   [][]float32
}

type ckptJSON struct {
	InDim      int         `json:"inDim"`
	H1Dim      int         `json:"h1Dim"`
	H2Dim      int         `json:"h2Dim"`
	OutDim     int         `json:"outDim"`
	Means      []float32   `json:"means"`
	Stds       []float32   `json:"stds"`
	W1         [][]float32 `json:"w1"`
	B1         []float32   `json:"b1"`
	W2         [][]float32 `json:"w2"`
	B2         []float32   `json:"b2"`
	W3         [][]float32 `json:"w3"`
	B3         []float32   `json:"b3"`
	YStd       float32     `json:"yStd"`
	YMean      float32     `json:"yMean"`
	Round      int         `json:"round"`
	Accuracy   float32     `json:"accuracy"`
	SamplesCnt int         `json:"samplesCount"`
	GamesCnt   int         `json:"gamesPlayed"`
	Timestamp  string      `json:"timestamp"`
	PolicyVer  string      `json:"policyVersion"`
}

func loadMLPFromCkpt(path string) (*MLP, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ckptJSON
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.InDim == 0 || c.H1Dim == 0 || c.H2Dim == 0 {
		return nil, fmt.Errorf("missing dims: inDim=%d h1=%d h2=%d", c.InDim, c.H1Dim, c.H2Dim)
	}
	outDim := c.OutDim
	if outDim == 0 {
		outDim = 1
	}
	// 自动扩 head 3 (policy) for case-train: 若 outDim < 4, 加一行零初始化
	if outDim < 4 {
		extra := 4 - outDim
		for i := 0; i < extra; i++ {
			c.W3 = append(c.W3, make([]float32, c.H2Dim))
			c.B3 = append(c.B3, 0)
		}
		outDim = 4
	}
	m := &MLP{
		InDim: c.InDim, H1: c.H1Dim, H2: c.H2Dim, OutDim: outDim,
		Means: c.Means, Stds: c.Stds,
		W1: c.W1, B1: c.B1, W2: c.W2, B2: c.B2,
		W3: c.W3, B3: c.B3,
		YMean: c.YMean, YStd: c.YStd,
		TaskWeights: make([]float32, outDim),
	}
	for i := range m.TaskWeights {
		m.TaskWeights[i] = 1.0
	}
	return m, nil
}

func saveMLP(m *MLP, path string, policyVer string, samplesCnt int) error {
	c := ckptJSON{
		InDim: m.InDim, H1Dim: m.H1, H2Dim: m.H2, OutDim: m.OutDim,
		Means: m.Means, Stds: m.Stds,
		W1: m.W1, B1: m.B1, W2: m.W2, B2: m.B2,
		W3: m.W3, B3: m.B3,
		YStd: m.YStd, YMean: m.YMean,
		Round:      999, // case-train round 标记
		Accuracy:   0,
		SamplesCnt: samplesCnt,
		Timestamp:  fmt.Sprintf("case-train-%d", os.Getpid()),
		PolicyVer:  policyVer,
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

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
	m.bufGradW3 = make([][]float32, m.OutDim)
	for i := range m.bufGradW3 {
		m.bufGradW3[i] = make([]float32, m.H2)
	}
}

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
	for o := 0; o < m.OutDim; o++ {
		v := m.B3[o]
		for j := 0; j < m.H2; j++ {
			v += m.W3[o][j] * m.bufH2[j]
		}
		m.bufOut[o] = v
	}
}

// PolicyOnlyMode — 若 true, 跳过 head 0/1/2 训练, 只更新 head 3 (policy) 的梯度.
// 用于 case-train: 保住 value head, 仅通过 policy 影响候选排序.
var PolicyOnlyMode = false

// trainOneWeighted — 单 sample 训, 加 weight 放大 loss/grad. 跟 train.go TrainOne 同
func (m *MLP) trainOneWeighted(s Sample, weight float32, lr float32) float32 {
	m.forwardInto(s.Features)

	var totalLoss float32
	if PolicyOnlyMode {
		m.bufDOut[0] = 0
		if m.OutDim >= 2 {
			m.bufDOut[1] = 0
		}
		if m.OutDim >= 3 {
			m.bufDOut[2] = 0
		}
	} else {
		// head 0 (royalty) MSE, target normalized
		t0 := (s.McScore - m.YMean) / m.YStd
		err0 := m.bufOut[0] - t0
		m.bufDOut[0] = weight * err0
		totalLoss += weight * 0.5 * err0 * err0
	}

	// 其他 head 用 BCE (跟 train.go 同), 但 case-train 给 0/1 默认 (没用 fan/foul/policy 主信号)
	if !PolicyOnlyMode {
		if m.OutDim >= 2 {
			t := s.FanRate
			z := m.bufOut[1]
			sig := sigmoidf(z)
			m.bufDOut[1] = 0.4 * weight * (sig - t)
			totalLoss += 0.4 * weight * bceLoss(z, t)
		}
		if m.OutDim >= 3 {
			t := s.FoulRate
			z := m.bufOut[2]
			sig := sigmoidf(z)
			m.bufDOut[2] = 0.1 * weight * (sig - t)
			totalLoss += 0.1 * weight * bceLoss(z, t)
		}
	}
	if m.OutDim >= 4 {
		t := s.PolicyTarget
		z := m.bufOut[3]
		sig := sigmoidf(z)
		m.bufDOut[3] = 0.3 * weight * (sig - t)
		totalLoss += 0.3 * weight * bceLoss(z, t)
	}

	// Backprop
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

	for i := 0; i < m.H2; i++ {
		gh := m.bufGradH2[i]
		row := m.bufGradW2[i]
		for j := 0; j < m.H1; j++ {
			row[j] = gh * m.bufH1[j]
		}
	}

	for i := 0; i < m.H1; i++ {
		gh := m.bufGradH1[i]
		row := m.bufGradW1[i]
		for j := 0; j < m.InDim; j++ {
			row[j] = gh * m.bufXn[j]
		}
	}

	// SGD — PolicyOnlyMode 只更新 W3[3]/B3[3], 完全冻 W1/W2/B1/B2/W3[0..2]/B3[0..2]
	for o := 0; o < m.OutDim; o++ {
		if PolicyOnlyMode && o != 3 {
			continue
		}
		gw := m.bufGradW3[o]
		w := m.W3[o]
		for j := 0; j < m.H2; j++ {
			w[j] -= lr * gw[j]
		}
		m.B3[o] -= lr * m.bufDOut[o]
	}
	if !PolicyOnlyMode {
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
	}

	return totalLoss
}

func sigmoidf(x float32) float32 {
	if x > 30 {
		return 1
	}
	if x < -30 {
		return 0
	}
	return float32(1.0 / (1.0 + math.Exp(-float64(x))))
}

func bceLoss(z, t float32) float32 {
	if z >= 0 {
		return z - z*t + float32(math.Log(1+math.Exp(-float64(z))))
	}
	return -z*t + float32(math.Log(1+math.Exp(float64(z))))
}
