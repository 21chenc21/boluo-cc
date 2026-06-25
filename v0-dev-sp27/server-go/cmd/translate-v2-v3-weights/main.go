// translate-v2-v3-weights — V2 (134-d, big-model-v3 用 132-d 截断) → V3 (147-d) 权重迁移.
//
// 用途: V3 NN warm-start from V2 baseline 当 self-play 起点.
// V2-only features (C/F/H/I/K, 共 65 dim) 丢弃; V3-only features (78 dim) 零初始化.
// 共享 features (A/B/D/E/G + L0-L3, 共 73 dim) 权重直接搬运.
//
// 用法:
//   go run ./cmd/translate-v2-v3-weights -in big-models/big-model-v3.json -out v3-train-i147/best.json
//
// 输出 ckpt 形式 = 标准 train ckpt JSON (inDim=147, h1=512, h2=256, h3=128, outDim=4),
// 可直接当 best.json 给 self-play 用.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

type CkptV2V3 struct {
	InDim         int         `json:"inDim"`
	H1Dim         int         `json:"h1Dim"`
	H2Dim         int         `json:"h2Dim"`
	H3Dim         int         `json:"h3Dim"`
	OutDim        int         `json:"outDim"`
	Means         []float32   `json:"means"`
	Stds          []float32   `json:"stds"`
	W1            [][]float32 `json:"w1"`
	B1            []float32   `json:"b1"`
	W2            [][]float32 `json:"w2"`
	B2            []float32   `json:"b2"`
	W3            [][]float32 `json:"w3"`
	B3            []float32   `json:"b3"`
	W4            [][]float32 `json:"w4"`
	B4            []float32   `json:"b4"`
	YStd          float32     `json:"yStd"`
	YMean         float32     `json:"yMean"`
	Round         int         `json:"round"`
	Accuracy      float32     `json:"accuracy"`
	SamplesCount  int         `json:"samplesCount"`
	GamesPlayed   int         `json:"gamesPlayed"`
	Timestamp     string      `json:"timestamp"`
	PolicyVersion string      `json:"policyVersion"`
}

// v2IdxToV3Idx — V2 (132-d big-model-v3) → V3 (147-d) feature idx 映射.
// 返回 -1 表示 V2 dim 在 V3 不存在 (丢弃).
//
// 映射表 (基于 features_v2.go vs features_v3.go 各 group 实际 idx):
//   V2 A (0-7)     → V3 A (0-7)       8 dim
//   V2 B (8-31)    → V3 B (8-31)      24 dim
//   V2 C (32-53)   → 丢弃 (V3 删除 C)
//   V2 D (54-61)   → V3 D (32-39)     8 dim
//   V2 E (62-73)   → V3 E (40-51)     12 dim
//   V2 F (74-85)   → 丢弃
//   V2 G (86-102)  → V3 G (52-68)     17 dim
//   V2 H (103-107) → 丢弃
//   V2 I (108-114) → 丢弃
//   V2 K (115-127) → 丢弃
//   V2 L0-L3 (128-131) → V3 L0-L3 (131-134)  4 dim
//   (V2 L4-L5 132-133 不在 big-model-v3 132-d 内, 不涉及)
//   V3 X/F/Y/Z/U/V/T/C/R5/Q/M/S/N/N2 + L4/L5/LR (78 dim) → 零初始化
//
// 总共 8+24+8+12+17+4 = 73 dim 权重搬运 / V3 147 = 50% 覆盖.
func v2IdxToV3Idx(v2Idx int) int {
	switch {
	case v2Idx >= 0 && v2Idx <= 7: // A
		return v2Idx
	case v2Idx >= 8 && v2Idx <= 31: // B
		return v2Idx
	case v2Idx >= 32 && v2Idx <= 53: // C deleted
		return -1
	case v2Idx >= 54 && v2Idx <= 61: // D: 54-61 → 32-39
		return v2Idx - 22
	case v2Idx >= 62 && v2Idx <= 73: // E: 62-73 → 40-51
		return v2Idx - 22
	case v2Idx >= 74 && v2Idx <= 85: // F deleted
		return -1
	case v2Idx >= 86 && v2Idx <= 102: // G: 86-102 → 52-68
		return v2Idx - 34
	case v2Idx >= 103 && v2Idx <= 107: // H deleted
		return -1
	case v2Idx >= 108 && v2Idx <= 114: // I deleted
		return -1
	case v2Idx >= 115 && v2Idx <= 127: // K deleted
		return -1
	case v2Idx >= 128 && v2Idx <= 131: // L0-L3: 128-131 → 131-134
		return v2Idx + 3
	}
	return -1
}

func main() {
	inPath := flag.String("in", "", "V2 ckpt path (typically big-models/big-model-v3.json, 132-d)")
	outPath := flag.String("out", "", "V3 ckpt output path (147-d)")
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		log.Fatalf("usage: -in <v2 ckpt> -out <v3 ckpt>")
	}

	data, err := os.ReadFile(*inPath)
	if err != nil {
		log.Fatalf("read %s: %v", *inPath, err)
	}
	var src CkptV2V3
	if err := json.Unmarshal(data, &src); err != nil {
		log.Fatalf("parse v2 ckpt: %v", err)
	}

	log.Printf("V2 ckpt: inDim=%d h1=%d h2=%d h3=%d out=%d", src.InDim, src.H1Dim, src.H2Dim, src.H3Dim, src.OutDim)
	if src.InDim < 128 || src.InDim > 134 {
		log.Fatalf("V2 inDim 应该在 128-134 范围 (big-model-v3 是 132), 实际 %d", src.InDim)
	}
	if src.H1Dim != 512 || src.H2Dim != 256 || src.H3Dim != 128 || src.OutDim != 4 {
		log.Fatalf("V2 arch 必须是 512-256-128-4, 实际 %d-%d-%d-%d", src.H1Dim, src.H2Dim, src.H3Dim, src.OutDim)
	}

	// W1 in src: shape [h1Dim][inDim] = [512][132]
	if len(src.W1) != src.H1Dim {
		log.Fatalf("W1 rows mismatch: got %d, want %d", len(src.W1), src.H1Dim)
	}
	if len(src.W1[0]) != src.InDim {
		log.Fatalf("W1 cols mismatch: got %d, want %d", len(src.W1[0]), src.InDim)
	}

	const v3Dim = 147
	dst := CkptV2V3{
		InDim:    v3Dim,
		H1Dim:    src.H1Dim,
		H2Dim:    src.H2Dim,
		H3Dim:    src.H3Dim,
		OutDim:   src.OutDim,
		B1:       append([]float32(nil), src.B1...),
		W2:       cloneMatrix(src.W2),
		B2:       append([]float32(nil), src.B2...),
		W3:       cloneMatrix(src.W3),
		B3:       append([]float32(nil), src.B3...),
		W4:       cloneMatrix(src.W4),
		B4:       append([]float32(nil), src.B4...),
		YStd:     src.YStd,
		YMean:    src.YMean,
		Round:    1,
		Accuracy: 0,

		SamplesCount:  src.SamplesCount,
		GamesPlayed:   -1,
		Timestamp:     time.Now().Format(time.RFC3339),
		PolicyVersion: "v3-translated-from-v2",
	}

	// Means / stds: V3 新增 dim 用 0/1 (identity normalize, 假设 V3 feature 已归一化到 [0,1]).
	dst.Means = make([]float32, v3Dim)
	dst.Stds = make([]float32, v3Dim)
	for i := 0; i < v3Dim; i++ {
		dst.Stds[i] = 1.0 // default 1.0 (无 scaling)
	}

	// W1: 新建 [512][147], 全 0. 对每个 V2 idx 找 V3 idx 复制.
	dst.W1 = make([][]float32, src.H1Dim)
	for h := 0; h < src.H1Dim; h++ {
		dst.W1[h] = make([]float32, v3Dim)
	}

	transferredDims := 0
	v2DimsDropped := []int{}
	for v2i := 0; v2i < src.InDim; v2i++ {
		v3i := v2IdxToV3Idx(v2i)
		if v3i < 0 {
			v2DimsDropped = append(v2DimsDropped, v2i)
			continue
		}
		if v3i >= v3Dim {
			log.Fatalf("v2 %d → v3 %d (out of range %d)", v2i, v3i, v3Dim)
		}
		// 复制 W1 该 dim 的 512 个 weights (跨所有 h1 neurons)
		for h := 0; h < src.H1Dim; h++ {
			dst.W1[h][v3i] = src.W1[h][v2i]
		}
		// 复制 means / stds
		dst.Means[v3i] = src.Means[v2i]
		dst.Stds[v3i] = src.Stds[v2i]
		transferredDims++
	}

	log.Printf("transferred dims: %d / V3 147 (%.0f%% 覆盖)", transferredDims, float32(transferredDims)/float32(v3Dim)*100)
	log.Printf("dropped V2 dims: %d (C/F/H/I/K groups)", len(v2DimsDropped))
	log.Printf("V3 new dims (zero-init): %d", v3Dim-transferredDims)

	// 写出 V3 ckpt
	outData, err := json.MarshalIndent(dst, "", "")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(*outPath, outData, 0644); err != nil {
		log.Fatalf("write %s: %v", *outPath, err)
	}
	fmt.Printf("✓ wrote V3 ckpt: %s (inDim=147, %d/147 dims transferred from V2)\n", *outPath, transferredDims)
}

func cloneMatrix(m [][]float32) [][]float32 {
	out := make([][]float32, len(m))
	for i, row := range m {
		out[i] = append([]float32(nil), row...)
	}
	return out
}
