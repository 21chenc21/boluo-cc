// case-mcts-trace — 给单个 case + ckpt, 跑 ExpertPlace5/3 一次, 打印各 stage top 候选.
//
// 用法:
//   ./case-mcts-trace -ckpt ckpts-v2-ema/round-001-acc89.json -cases cases/all-tests-expanded.json -case 20
//
// 输出:
//   === Case 20 [R1] ===
//   发: ...
//   === R1 Prerank top 5 === ...
//   === R1 Stage 1 (sims=30) top 5 === ...
//   === R1 Stage 2 (sims=60) === ...
//   === R1 Stage 3 (sims=150) === ...
//   ★ 最终选: ...
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/boluo/v0-server/ofc"
)

type CaseSpec struct {
	Name      string       `json:"name"`
	Round     int          `json:"round"`
	Dealt     []string     `json:"dealt"`
	State     StateSpec    `json:"state"`
	Expecteds []LayoutSpec `json:"expecteds,omitempty"`
}

type StateSpec struct {
	Top       []string `json:"top"`
	Middle    []string `json:"middle"`
	Bottom    []string `json:"bottom"`
	UsedCards []string `json:"usedCards"`
}

type LayoutSpec struct {
	Top    []string `json:"top"`
	Middle []string `json:"middle"`
	Bottom []string `json:"bottom"`
}

func parseCards(strs []string) []ofc.Card {
	out := []ofc.Card{}
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			panic("bad card: " + s)
		}
		out = append(out, c)
	}
	return out
}

func main() {
	ckptPath := flag.String("ckpt", "ckpts-v2-ema/round-001-acc89.json", "ckpt path")
	casesPath := flag.String("cases", "cases/all-tests-expanded.json", "cases JSON path")
	caseIdx := flag.Int("case", 20, "case number (1-based)")
	seed := flag.Int64("seed", 42, "RNG seed (deterministic)")
	simsMult := flag.Float64("sims-mult", 1.0, "MCTS_SIMS_MULT")
	prerankW := flag.Float64("prerank-w", 0, "MCTS_PRERANK_W")
	noHardRules := flag.Bool("no-hard-rules", false, "DISABLE_HARD_RULES")
	flag.Parse()

	if err := ofc.LoadWeightsFromFile(*ckptPath); err != nil {
		panic(err)
	}

	data, err := os.ReadFile(*casesPath)
	if err != nil {
		panic(err)
	}
	var cases []CaseSpec
	if err := json.Unmarshal(data, &cases); err != nil {
		panic(err)
	}
	if *caseIdx < 1 || *caseIdx > len(cases) {
		fmt.Fprintf(os.Stderr, "case %d 越界 (1-%d)\n", *caseIdx, len(cases))
		os.Exit(1)
	}
	c := cases[*caseIdx-1]

	// 构建 state
	gs := &ofc.GameState{
		Top:       parseCards(c.State.Top),
		Middle:    parseCards(c.State.Middle),
		Bottom:    parseCards(c.State.Bottom),
		UsedCards: map[string]bool{},
		Round:     c.Round - 1, // dealt 是这 round, 当前 state.Round = round-1
	}
	for _, s := range c.State.UsedCards {
		card, _ := ofc.ParseCard(s)
		gs.UsedCards[card.ID()] = true
	}
	// state 里的牌也在 used
	for _, card := range gs.Top {
		gs.UsedCards[card.ID()] = true
	}
	for _, card := range gs.Middle {
		gs.UsedCards[card.ID()] = true
	}
	for _, card := range gs.Bottom {
		gs.UsedCards[card.ID()] = true
	}

	dealt := parseCards(c.Dealt)

	fmt.Printf("=== Case %d [R%d] %s ===\n", *caseIdx, c.Round, c.Name)
	fmt.Printf("ckpt: %s, seed: %d, sims_mult: %.1f, prerank_w: %.2f\n", *ckptPath, *seed, *simsMult, *prerankW)
	fmt.Printf("发: %s\n", c.Dealt)
	if c.Round > 1 {
		fmt.Printf("state: 头%v 中%v 底%v\n", c.State.Top, c.State.Middle, c.State.Bottom)
	}
	if len(c.Expecteds) > 0 {
		fmt.Println("expected (符合即 pass):")
		for i, e := range c.Expecteds {
			fmt.Printf("  exp%d: 头%v 中%v 底%v\n", i+1, e.Top, e.Middle, e.Bottom)
		}
	}
	fmt.Println()

	// 设置 env-like flags
	ofc.MctsSimsMult = float32(*simsMult)
	ofc.MctsPrerankW = float32(*prerankW)
	ofc.MctsDebugTrace = true
	ofc.HardRulesDisabled = *noHardRules

	rng := rand.New(rand.NewSource(*seed))
	er := &ofc.ExpertRollout{
		Rng: rng,
		Cfg: ofc.DefaultRolloutConfig,
	}

	startT := time.Now()
	if c.Round == 1 {
		er.ExpertPlace5(gs, dealt)
	} else {
		er.ExpertPlace3(gs, dealt)
	}
	elapsed := time.Since(startT)

	fmt.Println()
	fmt.Printf("★ 最终摆: 头%v 中%v 底%v  (耗时 %v)\n",
		cardsToStrSlice(gs.Top), cardsToStrSlice(gs.Middle), cardsToStrSlice(gs.Bottom), elapsed)
}

func cardsToStrSlice(cards []ofc.Card) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.String())
	}
	return out
}
