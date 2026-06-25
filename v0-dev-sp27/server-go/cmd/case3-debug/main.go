// case3-debug — 给定 ckpt + dealt cards, 枚举几个关键候选, 打 TrainedEvalFull 分数
// 用于诊断 MLP 为什么这么选
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

func cardsToStr(cards []ofc.Card) string {
	s := "["
	for i, c := range cards {
		if i > 0 {
			s += " "
		}
		s += c.String()
	}
	return s + "]"
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
	fmt.Printf("=== Case 3 R1 debug: dealt X Ac Ad 3h 7s, ckpt=%s ===\n\n", ckptPath)

	cands := []candidate{
		{"AI: 头[AA] 中[3h 7s] 底[X]", []string{"Ac", "Ad"}, []string{"3h", "7s"}, []string{"X"}},
		{"alt: 头[AA 7s] 中[3h] 底[X]", []string{"Ac", "Ad", "7s"}, []string{"3h"}, []string{"X"}},
		{"alt: 头[AA X] 中[3h 7s] 底[]", []string{"Ac", "Ad", "X"}, []string{"3h", "7s"}, []string{}},
		{"alt: 头[AA] 中[3h] 底[7s X]", []string{"Ac", "Ad"}, []string{"3h"}, []string{"7s", "X"}},
		{"alt: 头[AA] 中[X 3h 7s] 底[]", []string{"Ac", "Ad"}, []string{"X", "3h", "7s"}, []string{}},
		{"alt: 头[AA] 中[X 7s] 底[3h]", []string{"Ac", "Ad"}, []string{"X", "7s"}, []string{"3h"}},
		{"alt: 头[AA] 中[X 3h] 底[7s]", []string{"Ac", "Ad"}, []string{"X", "3h"}, []string{"7s"}},
		{"alt: 头[AA] 中[3h 7s] 底[X] (= AI)", []string{"Ac", "Ad"}, []string{"3h", "7s"}, []string{"X"}},
		{"alt: 头[X] 中[3h 7s] 底[Ac Ad]", []string{"X"}, []string{"3h", "7s"}, []string{"Ac", "Ad"}},
		{"alt: 头[X Ac] 中[3h 7s] 底[Ad]", []string{"X", "Ac"}, []string{"3h", "7s"}, []string{"Ad"}},
	}

	fmt.Printf("%-45s %10s %10s %10s %12s\n", "candidate", "value", "fanProb", "foulProb", "policyLogit")
	for _, c := range cands {
		gs := buildState(c.top, c.mid, c.bot)
		v, fan, foul, pol, has := ofc.TrainedEvalFull(gs)
		polStr := "-"
		if has {
			polStr = fmt.Sprintf("%.4f", pol)
		}
		fmt.Printf("%-45s %10.4f %10.4f %10.4f %12s\n", c.name, v, fan, foul, polStr)
	}
	fmt.Println()
	fmt.Println("note: value 含 fanBonus - foulCost (combined royalty), 越高越好 (越值得选)")
	fmt.Println("note: fanProb/foulProb 是 sigmoid 后概率, policyLogit 是 raw logit (POLICY_BOOST=30 才用)")
}
