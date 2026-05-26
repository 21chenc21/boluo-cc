// init-mlp-v2 — 随机初始化 inDim=128 / H1=256 / H2=128 / outDim=4 的 MLP, 保存 ckpt.
// 用于 features V2 训练起点 (不依赖 R004 weights).
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"os"
)

type ckptJSON struct {
	InDim       int           `json:"inDim"`
	H1Dim       int           `json:"h1Dim"`
	H2Dim       int           `json:"h2Dim"`
	OutDim      int           `json:"outDim"`
	Means       []float32     `json:"means"`
	Stds        []float32     `json:"stds"`
	W1          [][]float32   `json:"w1"`
	B1          []float32     `json:"b1"`
	W2          [][]float32   `json:"w2"`
	B2          []float32     `json:"b2"`
	W3          [][]float32   `json:"w3"`
	B3          []float32     `json:"b3"`
	YMean       float32       `json:"yMean"`
	YStd        float32       `json:"yStd"`
	Round       int           `json:"round"`
	SamplesCnt  int           `json:"samplesCnt"`
	Timestamp   string        `json:"timestamp"`
	PolicyVer   string        `json:"policyVer"`
}

var (
	outPath = flag.String("out", "ckpts/v2-init.json", "output ckpt path")
	inDim   = flag.Int("indim", 128, "input dim (128 for v2 features)")
	h1Dim   = flag.Int("h1", 256, "hidden 1 dim")
	h2Dim   = flag.Int("h2", 128, "hidden 2 dim")
	outDim  = flag.Int("outdim", 4, "output heads (4 = value/fan/foul/policy)")
	seed    = flag.Int64("seed", 42, "random seed")
)

// Xavier init: stddev = sqrt(2 / fan_in)
func xavierInit(rng *rand.Rand, fanIn int) float32 {
	stddev := float32(1.0 / float32(fanIn))
	if stddev > 0.5 {
		stddev = 0.5
	}
	return float32(rng.NormFloat64()) * stddev
}

func main() {
	flag.Parse()
	rng := rand.New(rand.NewSource(*seed))

	// W1: H1 x InDim
	w1 := make([][]float32, *h1Dim)
	for i := range w1 {
		w1[i] = make([]float32, *inDim)
		for j := range w1[i] {
			w1[i][j] = xavierInit(rng, *inDim)
		}
	}
	b1 := make([]float32, *h1Dim)

	// W2: H2 x H1
	w2 := make([][]float32, *h2Dim)
	for i := range w2 {
		w2[i] = make([]float32, *h1Dim)
		for j := range w2[i] {
			w2[i][j] = xavierInit(rng, *h1Dim)
		}
	}
	b2 := make([]float32, *h2Dim)

	// W3: OutDim x H2
	w3 := make([][]float32, *outDim)
	for i := range w3 {
		w3[i] = make([]float32, *h2Dim)
		for j := range w3[i] {
			w3[i][j] = xavierInit(rng, *h2Dim)
		}
	}
	b3 := make([]float32, *outDim)

	// Means/Stds: 标准化 features. 用 default 0/1 (训练时再 update).
	means := make([]float32, *inDim)
	stds := make([]float32, *inDim)
	for i := range stds {
		stds[i] = 1.0
	}

	ckpt := ckptJSON{
		InDim: *inDim, H1Dim: *h1Dim, H2Dim: *h2Dim, OutDim: *outDim,
		Means: means, Stds: stds,
		W1: w1, B1: b1, W2: w2, B2: b2, W3: w3, B3: b3,
		// YMean/YStd default for OFC royalty distribution (rough: range 0-400, mean ~80, std ~100)
		YMean: 80, YStd: 100,
		Round: 0, SamplesCnt: 0,
		Timestamp: "init-v2",
		PolicyVer: "v2-init",
	}

	data, err := json.MarshalIndent(ckpt, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		log.Fatalf("write: %v", err)
	}
	log.Printf("✓ Fresh 128-input MLP saved to %s (inDim=%d h1=%d h2=%d outDim=%d, seed=%d)",
		*outPath, *inDim, *h1Dim, *h2Dim, *outDim, *seed)
}
