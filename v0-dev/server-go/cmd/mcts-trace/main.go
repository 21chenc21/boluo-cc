// mcts-trace — 给 case 跑 MCTSSearch 看 top candidates + visit dist
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime/pprof"

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
		c, _ := ofc.ParseCard(s)
		out = append(out, c)
	}
	return out
}

func main() {
	ckpt := flag.String("ckpt", "ckpts-v2-ema/round-001-acc89.json", "")
	cases := flag.String("cases", "cases/all-tests-expanded.json", "")
	caseIdx := flag.Int("case", 3, "")
	sims := flag.Int("sims", 200, "")
	cpuct := flag.Float64("cpuct", 1.5, "")
	seed := flag.Int64("seed", 42, "")
	leafK := flag.Int("leaf-k", 1, "rollouts per leaf (降单次噪声)")
	initN := flag.Int("init-n", 0, "init rollouts per candidate (PUCT 解锁)")
	cpuProf := flag.String("cpuprof", "", "write CPU profile to file")
	flag.Parse()
	ofc.MctsLeafRollouts = *leafK
	ofc.MctsInitRollouts = *initN

	if *cpuProf != "" {
		f, err := os.Create(*cpuProf)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if err := ofc.LoadWeightsFromFile(*ckpt); err != nil {
		panic(err)
	}
	data, err := os.ReadFile(*cases)
	if err != nil {
		panic(fmt.Sprintf("read cases file %s: %v", *cases, err))
	}
	var allCases []map[string]interface{}
	if err := json.Unmarshal(data, &allCases); err != nil {
		n := 200
		if n > len(data) {
			n = len(data)
		}
		panic(fmt.Sprintf("parse cases JSON: %v (first %d chars: %s)", err, n, string(data[:n])))
	}
	if *caseIdx < 1 || *caseIdx > len(allCases) {
		panic(fmt.Sprintf("bad case index %d (len=%d, cases file=%s)", *caseIdx, len(allCases), *cases))
	}
	cb, _ := json.Marshal(allCases[*caseIdx-1])
	var c CaseSpec
	if err := json.Unmarshal(cb, &c); err != nil {
		panic(fmt.Sprintf("decode case: %v", err))
	}

	gs := &ofc.GameState{
		Top:       parseCards(c.State.Top),
		Middle:    parseCards(c.State.Middle),
		Bottom:    parseCards(c.State.Bottom),
		UsedCards: map[string]bool{},
		Round:     c.Round - 1,
	}
	for _, s := range c.State.UsedCards {
		card, _ := ofc.ParseCard(s)
		gs.UsedCards[card.ID()] = true
	}
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
	fmt.Printf("发: %v  state: %v / %v / %v\n\n", c.Dealt, c.State.Top, c.State.Middle, c.State.Bottom)
	for i, e := range c.Expecteds {
		fmt.Printf("  exp%d: 头%v 中%v 底%v\n", i+1, e.Top, e.Middle, e.Bottom)
	}
	fmt.Println()

	rolloutCfg := ofc.DefaultRolloutConfig
	ofc.MctsDebugTrace = true
	cfg := ofc.MCTSConfig{
		Sims:       *sims,
		CPuct:      float32(*cpuct),
		UseValue:   true,
		RolloutCfg: &rolloutCfg,
		Rng:        rand.New(rand.NewSource(*seed)),
	}
	action, _ := ofc.MCTSSearch(gs, dealt, c.Round, cfg)
	gs2 := gs.Clone()
	ofc.ApplyMCTSAction(gs2, dealt, action)
	fmt.Println()
	fmt.Printf("★ 最终摆: 头%v 中%v 底%v\n", cardsToStr(gs2.Top), cardsToStr(gs2.Middle), cardsToStr(gs2.Bottom))
}

func cardsToStr(cs []ofc.Card) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.String())
	}
	return out
}
