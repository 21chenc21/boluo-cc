// bench-cases — 独立 testcase bench 工具, 复用 alphazero-train 的 8 核并行 benchTestcases logic.
// 直接 in-process 跑 ExpertPlace5/3, 不依赖 HTTP server / Node.js.
//
// 用法:
//   ./bench-cases -ckpt big-model-v1.json
//   ./bench-cases -ckpt round-001-acc89.json -cases cases/all-tests-expanded.json -bench-sims-mult 2 -workers 0
//
// 时间: ~1-2 min for 63 cases (vs run-cases.sh 5-10 min)
//
// 输出格式跟 test-cases.js 一致 (每 case ✓/✗ + 结果汇总).

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boluo/v0-server/ofc"
)

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

type TestCase struct {
	Name      string       `json:"name"`
	Round     int          `json:"round"`
	Dealt     []string     `json:"dealt"`
	State     StateSpec    `json:"state"`
	Expecteds []LayoutSpec `json:"expecteds"`
	Warn      bool         `json:"warn"` // 2026-05-20: 非匹配时计警告不计错误 (NN 选不在 expecteds 但不是大错)
}

func loadCases(path string) ([]TestCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []TestCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func normCard(c string) string {
	if c == "X" || strings.HasPrefix(c, "X") {
		return "X"
	}
	return c
}

func sortKey(cards []string) string {
	norm := make([]string, len(cards))
	for i, c := range cards {
		norm[i] = normCard(c)
	}
	sort.Strings(norm)
	return strings.Join(norm, ",")
}

func diffCards(before, after []ofc.Card) []ofc.Card {
	beforeSet := make(map[string]int)
	for _, c := range before {
		beforeSet[c.ID()]++
	}
	out := make([]ofc.Card, 0)
	for _, c := range after {
		if beforeSet[c.ID()] > 0 {
			beforeSet[c.ID()]--
		} else {
			out = append(out, c)
		}
	}
	return out
}

func cardsToStr(cs []ofc.Card) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}

func fmtCard(c string) string {
	if c == "X" || strings.HasPrefix(c, "X") {
		return "🃏"
	}
	return c
}

func fmtRow(cards []string) string {
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = fmtCard(c)
	}
	return strings.Join(parts, " ")
}

func caseTag(name string) string {
	// 兼容 "9 [R2]: ..." → "9 [R2]:"  跟  "case 6: ..." → "case 6:"
	for _, sep := range []string{": ", " : "} {
		if idx := strings.Index(name, sep); idx >= 0 {
			return name[:idx+1]
		}
	}
	return name
}

func runOneCase(c TestCase, jokers int, cfg *ofc.RolloutConfig, rng *rand.Rand) (passed bool, aiTop, aiMid, aiBot []string, discard string) {
	state := ofc.NewGameState(jokers)
	state.Round = c.Round
	for _, cs := range c.State.Top {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowTop)
	}
	for _, cs := range c.State.Middle {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowMiddle)
	}
	for _, cs := range c.State.Bottom {
		card, ok := ofc.ParseCard(cs)
		if !ok {
			continue
		}
		state.PlaceCard(card, ofc.RowBottom)
	}
	for _, cid := range c.State.UsedCards {
		state.UsedCards[cid] = true
	}

	dealt := make([]ofc.Card, 0, len(c.Dealt))
	for _, ds := range c.Dealt {
		card, ok := ofc.ParseCard(ds)
		if !ok {
			continue
		}
		dealt = append(dealt, card)
	}

	beforeTop := append([]ofc.Card(nil), state.Top...)
	beforeMid := append([]ofc.Card(nil), state.Middle...)
	beforeBot := append([]ofc.Card(nil), state.Bottom...)
	beforeUsed := make(map[string]bool, len(state.UsedCards))
	for k, v := range state.UsedCards {
		beforeUsed[k] = v
	}

	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
	if c.Round == 1 || len(dealt) == 5 {
		er.ExpertPlace5(state, dealt)
	} else {
		er.ExpertPlace3(state, dealt)
	}

	addedTop := diffCards(beforeTop, state.Top)
	addedMid := diffCards(beforeMid, state.Middle)
	addedBot := diffCards(beforeBot, state.Bottom)

	aiTop = cardsToStr(addedTop)
	aiMid = cardsToStr(addedMid)
	aiBot = cardsToStr(addedBot)

	// discard for R2+: 是 dealt 里没被 placement 用的那张
	if c.Round > 1 {
		placed := make(map[string]bool)
		for _, c := range addedTop {
			placed[c.ID()] = true
		}
		for _, c := range addedMid {
			placed[c.ID()] = true
		}
		for _, c := range addedBot {
			placed[c.ID()] = true
		}
		for _, dc := range dealt {
			if !placed[dc.ID()] {
				discard = dc.String()
				break
			}
		}
	}

	for _, exp := range c.Expecteds {
		if sortKey(aiTop) == sortKey(exp.Top) &&
			sortKey(aiMid) == sortKey(exp.Middle) &&
			sortKey(aiBot) == sortKey(exp.Bottom) {
			return true, aiTop, aiMid, aiBot, discard
		}
	}
	return false, aiTop, aiMid, aiBot, discard
}

type caseResult struct {
	idx     int
	c       TestCase
	passed  bool
	aiTop   []string
	aiMid   []string
	aiBot   []string
	discard string
}

func main() {
	ckpt := flag.String("ckpt", "", "ckpt JSON path")
	casesFile := flag.String("cases", "cases/all-tests-expanded.json", "testcase JSON path")
	benchSimsMult := flag.Float64("bench-sims-mult", 2.0, "MCTS_SIMS_MULT (default 2 = run-cases.sh 等价)")
	jokers := flag.Int("jokers", 2, "deck jokers")
	workers := flag.Int("workers", 0, "parallel workers (0=NumCPU)")
	seed := flag.Int64("seed", 42, "rng seed")
	flag.Parse()

	if *ckpt == "" {
		log.Fatal("usage: -ckpt CKPT.json")
	}
	if *workers <= 0 {
		*workers = runtime.NumCPU()
	}

	startT := time.Now()

	// Load ckpt
	if err := ofc.LoadWeightsFromFile(*ckpt); err != nil {
		log.Fatalf("load ckpt: %v", err)
	}
	ofc.MctsSimsMult = float32(*benchSimsMult)
	if os.Getenv("DISABLE_MCTS") != "" {
		ofc.MctsDisabled = true
		fmt.Println("[bench-cases] DISABLE_MCTS set; pure MLP mode (prerank top-1, no rollout)")
	}

	// Load cases
	cases, err := loadCases(*casesFile)
	if err != nil {
		log.Fatalf("load cases: %v", err)
	}

	fmt.Printf("[bench-cases] ckpt=%s, cases=%d, workers=%d, MctsSimsMult=%.1f\n",
		*ckpt, len(cases), *workers, *benchSimsMult)

	// Pre-build cfg from defaults
	cfg := ofc.DefaultRolloutConfig

	// 并行跑所有 case
	results := make([]caseResult, len(cases))
	jobs := make(chan int, len(cases))
	for i := range cases {
		jobs <- i
	}
	close(jobs)

	rng := rand.New(rand.NewSource(*seed))
	var done atomic.Int32
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		workerSeed := rng.Int63() ^ int64(uint64(w)*0x9E3779B97F4A7C15)
		go func(seed int64) {
			defer wg.Done()
			workerRng := rand.New(rand.NewSource(seed))
			for idx := range jobs {
				passed, aiTop, aiMid, aiBot, disc := runOneCase(cases[idx], *jokers, &cfg, workerRng)
				results[idx] = caseResult{
					idx:     idx,
					c:       cases[idx],
					passed:  passed,
					aiTop:   aiTop,
					aiMid:   aiMid,
					aiBot:   aiBot,
					discard: disc,
				}
				done.Add(1)
			}
		}(workerSeed)
	}
	wg.Wait()

	// Print results in case order
	passed, warned, failed := 0, 0, 0
	for _, r := range results {
		c := r.c
		mark := "✓"
		if !r.passed {
			if c.Warn {
				mark = "⚠"
				warned++
			} else {
				mark = "✗"
				failed++
			}
		} else {
			passed++
		}
		if c.Round == 1 {
			fmt.Printf("%s %s\n", mark, caseTag(c.Name))
		} else {
			init := fmt.Sprintf("头[%s] 中[%s] 底[%s]",
				fmtRow(c.State.Top), fmtRow(c.State.Middle), fmtRow(c.State.Bottom))
			fmt.Printf("%s %s %s\n", mark, caseTag(c.Name), init)
		}
		aiLine := fmt.Sprintf("  AI: 头[%s] 中[%s] 底[%s]",
			fmtRow(r.aiTop), fmtRow(r.aiMid), fmtRow(r.aiBot))
		if r.discard != "" {
			aiLine += " 弃 " + fmtCard(r.discard)
		}
		fmt.Println(aiLine)
		for i, exp := range c.Expecteds {
			fmt.Printf("  exp%d: 头[%s] 中[%s] 底[%s]\n",
				i+1, fmtRow(exp.Top), fmtRow(exp.Middle), fmtRow(exp.Bottom))
		}
	}

	elapsed := time.Since(startT).Seconds()
	if warned > 0 {
		fmt.Printf("\n=== 结果: %d通过 / %d警告 / %d失败 / %d总计 (%.1fs) ===\n", passed, warned, failed, len(cases), elapsed)
	} else {
		fmt.Printf("\n=== 结果: %d通过 / %d失败 / %d总计 (%.1fs) ===\n", passed, failed, len(cases), elapsed)
	}
}
