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
	Top      []string `json:"top"`
	Middle   []string `json:"middle"`
	Bottom   []string `json:"bottom"`
	SuitFree bool     `json:"suitFree,omitempty"` // 2026-06-15: true=按点数匹配(忽略花色), 默认严格(保同花)
}

type TestCase struct {
	Name         string       `json:"name"`
	Round        int          `json:"round"`
	Mode         string       `json:"mode"`         // 2026-06-01: "fantasy" for round=99
	DiscardCount int          `json:"discardCount"` // 2026-06-01: fantasy 需 = len(Dealt) - 13
	Dealt        []string     `json:"dealt"`
	State        StateSpec    `json:"state"`
	Expecteds    []LayoutSpec `json:"expecteds"`
	Warn         bool         `json:"warn"` // 2026-05-20: 非匹配时计警告不计错误
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

// rankKey: 去花色, 仅按点数(+joker)排序匹配. 用于 suitFree exp.
func rankKey(cards []string) string {
	norm := make([]string, len(cards))
	for i, c := range cards {
		nc := normCard(c)
		if nc != "X" && len(nc) >= 2 {
			nc = nc[:len(nc)-1] // 去掉花色字符
		}
		norm[i] = nc
	}
	sort.Strings(norm)
	return strings.Join(norm, ",")
}

// layoutMatch: exp.SuitFree 时按点数, 否则严格(花色敏感, 保同花正确).
func layoutMatch(exp LayoutSpec, aiTop, aiMid, aiBot []string) bool {
	key := sortKey
	if exp.SuitFree {
		key = rankKey
	}
	return key(aiTop) == key(exp.Top) &&
		key(aiMid) == key(exp.Middle) &&
		key(aiBot) == key(exp.Bottom)
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

	// 2026-06-01: fantasy mode (round=99) 走 ExpertPlaceFantasy, dealt 14-17 张, discardCount = len-13
	if c.Round == 99 || c.Mode == "fantasy" {
		dc := c.DiscardCount
		if dc == 0 {
			dc = len(dealt) - 13
		}
		r := ofc.ExpertPlaceFantasy(dealt, dc)
		if r != nil {
			// fantasy 直接出完整 layout, 不区分 added (state 必为空)
			aiTop = cardsToStr(r.Layout.Top)
			aiMid = cardsToStr(r.Layout.Middle)
			aiBot = cardsToStr(r.Layout.Bottom)
			if len(r.Layout.Discards) > 0 {
				discard = r.Layout.Discards[0].String()
			}
		}
		// match: fantasy 的 expected 是 FULL layout (不是 added)
		for _, exp := range c.Expecteds {
			if layoutMatch(exp, aiTop, aiMid, aiBot) {
				return true, aiTop, aiMid, aiBot, discard
			}
		}
		return false, aiTop, aiMid, aiBot, discard
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
		if layoutMatch(exp, aiTop, aiMid, aiBot) {
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
	r1Mult := flag.Float64("r1-mult", 1.0, "RolloutConfig.R1Mult (1.0=full 30 candidates, 0.17≈top-5)")
	prerankW := flag.Float64("prerank-w", 0.0, "MctsPrerankW (0=纯 rollout, 1=纯 NN, 0.5=blend)")
	stageMin := flag.Int("stage-min", 0, "stage1/2/3 candidate min (0=default 5/3/2; 2=top-2, 3=top-3)")
	topkSample := flag.Int("topk-sample", 0, "pureMLP R1 top-K sample (0=top-1 deterministic, 2=top-2 随机)")
	topkSampleRN := flag.Int("topk-sample-rn", 0, "pureMLP R2-R5 top-K sample (0=top-1)")
	jokers := flag.Int("jokers", 2, "deck jokers")
	workers := flag.Int("workers", 0, "parallel workers (0=NumCPU)")
	seed := flag.Int64("seed", 42, "rng seed")
	featdiff := flag.Bool("featdiff", false, "失败 case dump AI首选 vs 期望[0] 的 BuildFeaturesV3 特征 diff")
	labelprobe := flag.Bool("labelprobe", false, "失败 case 直接量 AI摆 vs exp摆 的平均 rollout label(EV), 自动分类 缺reward vs over-value特征压. 配 -featdiff")
	flag.Parse()
	labelProbeEnabled = *labelprobe

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
	ofc.MctsPrerankW = float32(*prerankW)
	if *stageMin > 0 {
		ofc.MctsStage1Min = *stageMin
		ofc.MctsStage2Min = *stageMin
		ofc.MctsStage3Min = *stageMin
	}
	if *topkSample > 1 {
		ofc.MctsTopKSample = *topkSample
		ofc.MctsDisabled = true
		fmt.Printf("[bench-cases] R1 top-K sample = %d, DISABLE_MCTS auto-on\n", *topkSample)
	}
	if *topkSampleRN > 1 {
		ofc.MctsTopKSampleRN = *topkSampleRN
		ofc.MctsDisabled = true
		fmt.Printf("[bench-cases] R2-R5 top-K sample = %d\n", *topkSampleRN)
	}
	if os.Getenv("DISABLE_MCTS") != "" {
		ofc.MctsDisabled = true
		fmt.Println("[bench-cases] DISABLE_MCTS set; pure MLP mode (prerank top-1, no rollout)")
	}
	if os.Getenv("DISABLE_SOFT_RULES") != "" {
		ofc.SoftRulesDisabled = true
		fmt.Println("[bench-cases] DISABLE_SOFT_RULES=1: 裸跑 (跳过全部软 bonus/penalty, 纯 value head)")
	}
	if os.Getenv("DISABLE_HARD_RULES") != "" {
		ofc.HardRulesDisabled = true
		fmt.Println("[bench-cases] DISABLE_HARD_RULES=1: 跳过候选 filter")
	}
	if os.Getenv("OFC_DEBUG_TRACE") != "" {
		ofc.MctsDebugTrace = true
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
	cfg.R1Mult = float32(*r1Mult)

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

	if *featdiff {
		fmt.Println("\n========== 特征 DIFF (失败 case: AI首选 vs 期望[0]) ==========")
		fdrng := rand.New(rand.NewSource(*seed))
		for _, r := range results {
			if r.passed || r.c.Warn {
				continue
			}
			featDiffCase(r.c, *jokers, &cfg, fdrng)
		}
	}
}

func featGroup(i int) string {
	switch {
	case i < 8:
		return "BoardState"
	case i < 32:
		return "HandTiers"
	case i < 40:
		return "JokerState"
	case i < 52:
		return "SuitDist"
	case i < 69:
		return "DeckAware"
	case i < 90:
		return "Prob(draw)"
	case i < 94:
		return "FantasyGran"
	case i < 97:
		return "ExpRoyalty"
	case i < 107:
		return "G97-106"
	case i < 112:
		return "PairToTrips"
	case i < 116:
		return "TopFantasyLock"
	case i < 119:
		return "MaxAchiev"
	case i < 147:
		return "G119-146"
	case i < 150:
		return "O-行序"
	default:
		return "W-支撑夹缝"
	}
}

var labelProbeEnabled bool

// labelProbe — 直接量两个 post-state 的平均 rollout label(EV). NN 学的是 label, 这是金标准:
//   标签偏 exp 但 NN 选 AI → over-value 特征压住区分特征(找 feature cap, 治得了);
//   标签偏 AI → reward 不够/case 判断本身问题(调 reward 或重审 case). 见 reference_diagnosis_method.
func labelProbe(aiState, expState *ofc.GameState, round int, cfg *ofc.RolloutConfig) {
	// 2026-06-29 fix: 用 gen 的 fan/foul 校准 (DefaultRolloutConfig 是 AA=80/FoulCost=6, gen 是 AA=100/FoulCost=3)
	//   → 量的 EV 才跟 NN 真训练的 label 一致. 否则绝对值偏.
	pcfg := *cfg
	pcfg.FoulCost = 3
	pcfg.QQFanBonus = 10
	pcfg.KKFanBonus = 30
	pcfg.AAFanBonus = 100
	pcfg.TripsFanBonus = 140
	probe := func(st *ofc.GameState) float64 {
		var sum float64
		const N = 500
		for i := 0; i < N; i++ {
			er := &ofc.ExpertRollout{Rng: rand.New(rand.NewSource(int64(i))), Cfg: pcfg}
			sum += float64(er.QuickRollout(st.Clone(), round))
		}
		return sum / N
	}
	lAI, lExp := probe(aiState), probe(expState)
	cls := "📊标签偏 exp → NN 该学会, 是 over-value 特征压住区分特征 (找 feature cap)"
	if lExp <= lAI {
		cls = "📊标签偏 AI → reward 不够 / case 判断本身问题 (调 reward 或重审 case)"
	}
	fmt.Printf("  >> LABEL PROBE: exp=%.2f  AI=%.2f  Δ(exp-AI)=%+.2f\n     %s\n", lExp, lAI, lExp-lAI, cls)
}

// featDiffCase: 重建 AI首选 post-state 和 期望[0] post-state, dump BuildFeaturesV3 diff.
// 判断"缺特征 vs 缺权重": 强信号组(Δ>0.3)=特征捕捉到了→是权重问题; 全弱(<0.1)=特征缺.
func featDiffCase(c TestCase, jokers int, cfg *ofc.RolloutConfig, rng *rand.Rand) {
	build := func() *ofc.GameState {
		st := ofc.NewGameState(jokers)
		st.Round = c.Round
		for _, cs := range c.State.Top {
			if card, ok := ofc.ParseCard(cs); ok {
				st.PlaceCard(card, ofc.RowTop)
			}
		}
		for _, cs := range c.State.Middle {
			if card, ok := ofc.ParseCard(cs); ok {
				st.PlaceCard(card, ofc.RowMiddle)
			}
		}
		for _, cs := range c.State.Bottom {
			if card, ok := ofc.ParseCard(cs); ok {
				st.PlaceCard(card, ofc.RowBottom)
			}
		}
		for _, cid := range c.State.UsedCards {
			st.UsedCards[cid] = true
		}
		return st
	}
	dealt := make([]ofc.Card, 0, len(c.Dealt))
	for _, ds := range c.Dealt {
		if card, ok := ofc.ParseCard(ds); ok {
			dealt = append(dealt, card)
		}
	}
	aiState := build()
	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
	if c.Round == 1 || len(dealt) == 5 {
		er.ExpertPlace5(aiState, dealt)
	} else {
		er.ExpertPlace3(aiState, dealt)
	}
	if len(c.Expecteds) == 0 {
		return
	}
	exp := c.Expecteds[0]
	expState := build()
	placeStr := func(ss []string, row ofc.Row) {
		for _, s := range ss {
			if card, ok := ofc.ParseCard(s); ok {
				expState.PlaceCard(card, row)
			}
		}
	}
	placeStr(exp.Top, ofc.RowTop)
	placeStr(exp.Middle, ofc.RowMiddle)
	placeStr(exp.Bottom, ofc.RowBottom)
	if c.Round > 1 {
		placed := map[string]bool{}
		for _, grp := range [][]string{exp.Top, exp.Middle, exp.Bottom} {
			for _, s := range grp {
				if card, ok := ofc.ParseCard(s); ok {
					placed[card.ID()] = true
				}
			}
		}
		for _, d := range dealt {
			if !placed[d.ID()] {
				expState.SetDiscard(d)
				expState.UsedCards[d.ID()] = true
				break
			}
		}
	}
	fa := ofc.BuildFeaturesV3(aiState)
	fe := ofc.BuildFeaturesV3(expState)
	teAI := ofc.TrainedEval(aiState)
	teExp := ofc.TrainedEval(expState)
	gap := teAI - teExp
	wAI := ofc.WastedTotal(aiState)
	wExp := ofc.WastedTotal(expState)
	tag := "边际/噪声"
	if gap >= 5 {
		tag = "⚠️NN真错(强偏好错摆)"
	} else if gap >= 2 {
		tag = "中等偏差"
	}
	wtag := ""
	if wAI > wExp {
		wtag = " ✓死牌特征助"
	} else if wAI < wExp {
		wtag = " ✗死牌特征反"
	}
	fmt.Printf("\n===== %s | te gap=%+.2f [%s] | 死牌 AI=%d exp=%d%s =====\n", caseTag(c.Name), gap, tag, wAI, wExp, wtag)
	if labelProbeEnabled {
		labelProbe(aiState, expState, c.Round, cfg)
	}
	n := 0
	grpMax := map[string]float64{}
	for i := range fa {
		d := float64(fe[i] - fa[i])
		ad := d
		if ad < 0 {
			ad = -ad
		}
		if ad > 0.01 {
			g := featGroup(i)
			fmt.Printf("  f[%3d] %-13s AI=%.3f exp=%.3f Δ=%+.3f\n", i, g, fa[i], fe[i], d)
			if ad > grpMax[g] {
				grpMax[g] = ad
			}
			n++
		}
	}
	fmt.Printf("  >> %d维diff; 强信号(Δ>0.3)组: ", n)
	any := false
	for g, m := range grpMax {
		if m > 0.3 {
			fmt.Printf("%s(%.2f) ", g, m)
			any = true
		}
	}
	if !any {
		fmt.Print("无 → ⚠️疑似特征缺/弱")
	}
	fmt.Println()
}
