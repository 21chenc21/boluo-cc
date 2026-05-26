// case20-debug — 调试 case 20 R1: dealt 3c Td 8s 7h 4d
// AI 选 头[] 中[8s 4d] 底[3c Td 7h] (拆 7-8♠ 连张), 期望 7-8-T 同底
package main

import (
	"fmt"
	"os"

	"github.com/boluo/v0-server/ofc"
)

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

func buildState(top, mid, bot []string) *ofc.GameState {
	gs := &ofc.GameState{
		Top:       parseCards(top),
		Middle:    parseCards(mid),
		Bottom:    parseCards(bot),
		UsedCards: map[string]bool{},
		Round:     1,
	}
	for _, c := range gs.Top {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Middle {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Bottom {
		gs.UsedCards[c.ID()] = true
	}
	return gs
}

type candidate struct {
	name string
	top  []string
	mid  []string
	bot  []string
}

func main() {
	ckptPath := "ckpts-v2-ema/round-001-acc89.json"
	if len(os.Args) > 1 {
		ckptPath = os.Args[1]
	}
	if err := ofc.LoadWeightsFromFile(ckptPath); err != nil {
		panic(err)
	}
	fmt.Printf("=== Case 20 R1 debug: dealt 3c Td 8s 7h 4d, ckpt=%s ===\n\n", ckptPath)
	fmt.Println("AI 选: 头[] 中[8s 4d] 底[3c Td 7h]")
	fmt.Println("期望: 7-8-T 同底 (e.g. 头[] 中[3c 4d] 底[Td 8s 7h])")
	fmt.Println()

	cands := []candidate{
		// AI choice + variants of the same shape
		{"AI: 头[] 中[8s 4d] 底[3c Td 7h]", []string{}, []string{"8s", "4d"}, []string{"3c", "Td", "7h"}},
		// User expecteds
		{"exp1: 头[] 中[3c 4d] 底[Td 8s 7h]", []string{}, []string{"3c", "4d"}, []string{"Td", "8s", "7h"}},
		{"exp2: 头[Td] 中[3c 4d] 底[8s 7h]", []string{"Td"}, []string{"3c", "4d"}, []string{"8s", "7h"}},
		// Other key alternatives
		{"alt: 头[] 中[8s 7h] 底[3c Td 4d]", []string{}, []string{"8s", "7h"}, []string{"3c", "Td", "4d"}},
		{"alt: 头[] 中[Td 8s] 底[3c 7h 4d]", []string{}, []string{"Td", "8s"}, []string{"3c", "7h", "4d"}},
		{"alt: 头[] 中[Td 7h] 底[3c 8s 4d]", []string{}, []string{"Td", "7h"}, []string{"3c", "8s", "4d"}},
		{"alt: 头[Td] 中[8s 7h] 底[3c 4d]", []string{"Td"}, []string{"8s", "7h"}, []string{"3c", "4d"}},
		{"alt: 头[3c] 中[8s 4d] 底[Td 7h]", []string{"3c"}, []string{"8s", "4d"}, []string{"Td", "7h"}},
		{"alt: 头[3c] 中[Td 4d] 底[8s 7h]", []string{"3c"}, []string{"Td", "4d"}, []string{"8s", "7h"}},
		{"alt: 头[3c] 中[Td 8s] 底[7h 4d]", []string{"3c"}, []string{"Td", "8s"}, []string{"7h", "4d"}},
		{"alt: 头[3c] 中[7h 4d] 底[Td 8s]", []string{"3c"}, []string{"7h", "4d"}, []string{"Td", "8s"}},
		{"alt: 头[3c] 中[3c 4d] 底[Td 8s 7h]", []string{}, []string{}, []string{}}, // placeholder; skip
	}

	fmt.Printf("%-45s %10s %10s %10s\n", "candidate", "value", "fanProb", "foulProb")
	for _, c := range cands {
		if len(c.top)+len(c.mid)+len(c.bot) != 5 {
			continue
		}
		gs := buildState(c.top, c.mid, c.bot)
		v, fan, foul, _, _ := ofc.TrainedEvalFull(gs)
		fmt.Printf("%-45s %10.4f %10.4f %10.4f\n", c.name, v, fan, foul)
	}

	fmt.Println()
	fmt.Println("note: value = 纯 head 0 royalty 预测 (denormalized); 比较时越高越优")

	// === rollout mean 实验 ===
	fmt.Println()
	fmt.Println("=== Rollout mean (sims sweep) ===")
	fmt.Println("用 ExpertRollout.QuickRollout 跑 N 次, 平均得到 rollout EV.")
	fmt.Println("若 sims 收敛但 AI choice 仍 > exp1 → 是 rollout policy bias")
	fmt.Println("若 sims 收敛后 exp1 > AI → 是 sims 不够 (variance dominated)")
	fmt.Println()

	er := ofc.NewExpertRollout()
	er.Cfg = ofc.DefaultRolloutConfig

	rolloutCands := []candidate{
		{"AI: 头[] 中[8s 4d] 底[3c Td 7h]", []string{}, []string{"8s", "4d"}, []string{"3c", "Td", "7h"}},
		{"exp1: 头[] 中[3c 4d] 底[Td 8s 7h]", []string{}, []string{"3c", "4d"}, []string{"Td", "8s", "7h"}},
		{"exp2: 头[Td] 中[3c 4d] 底[8s 7h]", []string{"Td"}, []string{"3c", "4d"}, []string{"8s", "7h"}},
	}

	sims := []int{30, 100, 300, 1000, 3000}
	fmt.Printf("%-45s", "candidate")
	for _, n := range sims {
		fmt.Printf(" sims=%-6d", n)
	}
	fmt.Println()

	for _, c := range rolloutCands {
		gs := buildState(c.top, c.mid, c.bot)
		fmt.Printf("%-45s", c.name)
		for _, n := range sims {
			var total float32
			for s := 0; s < n; s++ {
				total += er.QuickRollout(gs, 1)
			}
			mean := total / float32(n)
			fmt.Printf(" %10.3f", mean)
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Println("note: rollout 用 ExpertRollout.QuickRollout, 用 default 加载的 ckpt 当 rollout policy")
}
