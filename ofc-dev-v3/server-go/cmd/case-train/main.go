// case-train — 给现成 ckpt 注入 hard-case 监督样本, 短时间 finetune 修打地鼠 case.
//
// vs 自对弈训练:
//   自对弈 8h: silver-label rollout 平均, 间接信号
//   case-train 5min: 直接监督 (expected 摆法 = 200 分, wrong 摆法 = 0 分)
//
// 用法:
//   case-train -ckpt round-004.json -cases cases/hard.json -out fine.json
//
// cases.json 格式见 cases/hard.json.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

type CaseSpec struct {
	Name      string       `json:"name"`
	Round     int          `json:"round"` // 1-5
	Dealt     []string     `json:"dealt"`
	State     StateSpec    `json:"state"`
	Expected  *LayoutSpec  `json:"expected,omitempty"`  // 单 expected (legacy, 兼容)
	Expecteds []LayoutSpec `json:"expecteds,omitempty"` // 多 expected (multi-solution)
	Wrongs    []LayoutSpec `json:"wrongs,omitempty"`    // 已知错误摆法 (negative samples)
	LabelValue float32     `json:"labelValue,omitempty"`
	WrongLabel float32     `json:"wrongLabel,omitempty"`
	Weight     float32     `json:"weight,omitempty"`
}

type StateSpec struct {
	Top       []string `json:"top"`
	Middle    []string `json:"middle"`
	Bottom    []string `json:"bottom"`
	UsedCards []string `json:"usedCards"` // 桌面已可见 (含自己已摆 + 对手 visible)
}

type LayoutSpec struct {
	Top    []string `json:"top"`
	Middle []string `json:"middle"`
	Bottom []string `json:"bottom"`
}

// Sample (复用 train.go schema)
type Sample struct {
	Features     []float32 `json:"features"`
	McScore      float32   `json:"mcScore"`
	FanRate      float32   `json:"fanRate,omitempty"`
	FoulRate     float32   `json:"foulRate,omitempty"`
	PolicyTarget float32   `json:"policyTarget,omitempty"`
	Round        int       `json:"round,omitempty"`
}

var (
	ckptIn       = flag.String("ckpt", "", "starting ckpt (required)")
	casesPath    = flag.String("cases", "", "cases JSON file (required)")
	outPath      = flag.String("out", "case-fine.json", "output ckpt path")
	epochs       = flag.Int("epochs", 50, "training epochs")
	lr           = flag.Float64("lr", 0.001, "learning rate")
	caseWeight   = flag.Float64("case-weight", 5.0, "case sample weight multiplier")
	mixDataset   = flag.String("mix-dataset", "", "(optional) mix in baseline samples from this oracle-dataset dir")
	mixCap       = flag.Int("mix-cap", 5000, "max baseline samples to mix in")
	policyVer    = flag.String("policy", "case-fine", "policy version label")
	policyOnly   = flag.Bool("policy-only", false, "只训 head 3 (policy), 保住 head 0/1/2 (推荐, 不破坏 value)")
)

func main() {
	flag.Parse()
	if *ckptIn == "" || *casesPath == "" {
		fmt.Fprintln(os.Stderr, "usage: case-train -ckpt X.json -cases Y.json -out Z.json [flags]")
		os.Exit(1)
	}

	if *policyOnly {
		PolicyOnlyMode = true
		log.Print("[case-train] policy-only mode: 只训 head 3, 不动 head 0/1/2")
	}

	// 1. 加载 ckpt
	mlp, err := loadMLPFromCkpt(*ckptIn)
	if err != nil {
		log.Fatalf("load ckpt: %v", err)
	}
	log.Printf("[case-train] loaded ckpt: inDim=%d h1=%d h2=%d outDim=%d", mlp.InDim, mlp.H1, mlp.H2, mlp.OutDim)

	// 2. 加载 cases
	cases, err := loadCases(*casesPath)
	if err != nil {
		log.Fatalf("load cases: %v", err)
	}
	log.Printf("[case-train] loaded %d cases", len(cases))

	// 3. 生成 sample
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	samples := []weightedSample{}
	cfg := ofc.DefaultRolloutConfig
	for _, c := range cases {
		caseSamples, err := generateSamplesForCase(c, mlp.InDim, &cfg)
		if err != nil {
			log.Printf("  [warn] case %q skipped: %v", c.Name, err)
			continue
		}
		samples = append(samples, caseSamples...)
		nExp := 0
		if c.Expected != nil {
			nExp++
		}
		nExp += len(c.Expecteds)
		log.Printf("  case %q: %d samples (%d expected + %d wrongs)", c.Name, len(caseSamples), nExp, len(caseSamples)-nExp)
	}
	log.Printf("[case-train] total case samples: %d", len(samples))

	// 4. (可选) mix in baseline 样本
	if *mixDataset != "" {
		baselines, err := loadBaselineSamples(*mixDataset, *mixCap, rng)
		if err != nil {
			log.Printf("  [warn] mix-dataset load failed: %v", err)
		} else {
			for _, s := range baselines {
				samples = append(samples, weightedSample{Sample: s, weight: 1.0})
			}
			log.Printf("[case-train] mixed in %d baseline samples (cap %d)", len(baselines), *mixCap)
		}
	}

	if len(samples) == 0 {
		log.Fatal("no samples generated")
	}

	// 5. SGD finetune
	log.Printf("[case-train] training %d epochs LR=%.5f, case-weight=%.1f", *epochs, *lr, *caseWeight)
	mlp.allocTrainBufs()
	for ep := 0; ep < *epochs; ep++ {
		// shuffle samples
		rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
		var lossSum float32
		for _, ws := range samples {
			loss := mlp.trainOneWeighted(ws.Sample, ws.weight*float32(*caseWeight), float32(*lr))
			lossSum += loss
		}
		if ep%10 == 0 || ep == *epochs-1 {
			log.Printf("  epoch %d: avg loss = %.4f", ep, lossSum/float32(len(samples)))
		}
	}

	// 6. 保存
	if err := saveMLP(mlp, *outPath, *policyVer, len(samples)); err != nil {
		log.Fatalf("save: %v", err)
	}
	log.Printf("[case-train] saved fine-ckpt to %s", *outPath)
}

type weightedSample struct {
	Sample
	weight float32
}

func generateSamplesForCase(c CaseSpec, inDim int, cfg *ofc.RolloutConfig) ([]weightedSample, error) {
	if len(c.Dealt) == 0 {
		return nil, fmt.Errorf("empty dealt")
	}

	// 默认值
	labelValue := c.LabelValue
	if labelValue == 0 {
		labelValue = 200
	}
	wrongLabel := c.WrongLabel
	weight := c.Weight
	if weight == 0 {
		weight = 1.0
	}

	// 重建 state
	state, err := buildStateFromSpec(c.State)
	if err != nil {
		return nil, fmt.Errorf("buildState: %w", err)
	}
	state.Round = c.Round

	dealt, err := parseCardList(c.Dealt)
	if err != nil {
		return nil, fmt.Errorf("dealt: %w", err)
	}

	// 收集所有 expected layouts (多 solution 支持)
	allExpecteds := []LayoutSpec{}
	if c.Expected != nil {
		allExpecteds = append(allExpecteds, *c.Expected)
	}
	allExpecteds = append(allExpecteds, c.Expecteds...)
	if len(allExpecteds) == 0 {
		return nil, fmt.Errorf("no expected layout (need 'expected' or 'expecteds')")
	}

	out := []weightedSample{}

	// 每个 expected 都生成正样本 (multi-solution 时 MLP 学到都是高分)
	for _, exp := range allExpecteds {
		expectedPost, err := applyLayoutToState(state, dealt, exp)
		if err != nil {
			log.Printf("  [warn] expected layout error for %s: %v", c.Name, err)
			continue
		}
		out = append(out, weightedSample{
			Sample: Sample{
				Features:     ofc.BuildFeatures(expectedPost, inDim),
				McScore:      labelValue,
				FanRate:      0,
				FoulRate:     0,
				PolicyTarget: 1.0,
				Round:        c.Round,
			},
			weight: weight,
		})
	}

	// wrong samples (negative, 标 wrongLabel)
	for _, w := range c.Wrongs {
		wrongPost, err := applyLayoutToState(state, dealt, w)
		if err != nil {
			log.Printf("  [warn] wrong layout error for %s: %v", c.Name, err)
			continue
		}
		out = append(out, weightedSample{
			Sample: Sample{
				Features:     ofc.BuildFeatures(wrongPost, inDim),
				McScore:      wrongLabel,
				FanRate:      0,
				FoulRate:     0,
				PolicyTarget: 0,
				Round:        c.Round,
			},
			weight: weight,
		})
	}
	return out, nil
}

func buildStateFromSpec(spec StateSpec) (*ofc.GameState, error) {
	gs := &ofc.GameState{
		Top:       []ofc.Card{},
		Middle:    []ofc.Card{},
		Bottom:    []ofc.Card{},
		UsedCards: map[string]bool{},
	}
	if cs, err := parseCardList(spec.Top); err == nil {
		gs.Top = cs
	} else {
		return nil, err
	}
	if cs, err := parseCardList(spec.Middle); err == nil {
		gs.Middle = cs
	} else {
		return nil, err
	}
	if cs, err := parseCardList(spec.Bottom); err == nil {
		gs.Bottom = cs
	} else {
		return nil, err
	}
	for _, s := range spec.UsedCards {
		c, ok := ofc.ParseCard(s)
		if !ok {
			return nil, fmt.Errorf("invalid usedCard %q", s)
		}
		gs.UsedCards[c.ID()] = true
	}
	// 加 placed cards 到 UsedCards
	for _, c := range gs.Top {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Middle {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Bottom {
		gs.UsedCards[c.ID()] = true
	}
	return gs, nil
}

func parseCardList(strs []string) ([]ofc.Card, error) {
	out := make([]ofc.Card, 0, len(strs))
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			return nil, fmt.Errorf("invalid card %q", s)
		}
		out = append(out, c)
	}
	return out, nil
}

// applyLayoutToState — 把 dealt cards 按 layout 摆到 state, 返回 post-state
func applyLayoutToState(state *ofc.GameState, dealt []ofc.Card, layout LayoutSpec) (*ofc.GameState, error) {
	post := state.Clone()
	pool := append([]ofc.Card(nil), dealt...)

	tryTake := func(s string) (ofc.Card, bool) {
		for i, c := range pool {
			if cardToString(c) == s {
				pool = append(pool[:i], pool[i+1:]...)
				return c, true
			}
		}
		return ofc.Card(0), false
	}

	for _, s := range layout.Top {
		c, ok := tryTake(s)
		if !ok {
			return nil, fmt.Errorf("top card %q not in dealt", s)
		}
		post.PlaceCard(c, ofc.RowTop)
	}
	for _, s := range layout.Middle {
		c, ok := tryTake(s)
		if !ok {
			return nil, fmt.Errorf("middle card %q not in dealt", s)
		}
		post.PlaceCard(c, ofc.RowMiddle)
	}
	for _, s := range layout.Bottom {
		c, ok := tryTake(s)
		if !ok {
			return nil, fmt.Errorf("bottom card %q not in dealt", s)
		}
		post.PlaceCard(c, ofc.RowBottom)
	}
	// 剩下 pool 都进 usedCards (discarded). R2-R5 应该恰好 1 张; R1 应该 0 张.
	for _, c := range pool {
		post.UsedCards[c.ID()] = true
	}
	if len(pool) == 1 {
		post.SetDiscard(pool[0]) // V3 N/N2 features (R2-R5 case data)
	}
	return post, nil
}

func cardToString(c ofc.Card) string {
	if c.IsJoker() {
		return "X"
	}
	return c.String()
}

func loadCases(path string) ([]CaseSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []CaseSpec
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func loadBaselineSamples(dir string, cap int, rng *rand.Rand) ([]Sample, error) {
	var shards []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl.gz") {
			shards = append(shards, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(shards) == 0 {
		return nil, fmt.Errorf("no shards under %s", dir)
	}

	// 简单 reservoir sample
	out := []Sample{}
	seen := 0
	for _, p := range shards {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			continue
		}
		dec := json.NewDecoder(gz)
		for dec.More() {
			var s Sample
			if err := dec.Decode(&s); err != nil {
				break
			}
			seen++
			if len(out) < cap {
				out = append(out, s)
			} else {
				j := rng.Intn(seen)
				if j < cap {
					out[j] = s
				}
			}
		}
		gz.Close()
		f.Close()
	}
	return out, nil
}
