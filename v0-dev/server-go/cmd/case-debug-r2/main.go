// case-debug-r2 case 9
// state: top=[3h] mid=[2s 7d] bot=[4c 6h], dealt=[7h X 2c]
// AI: 弃 2c, 中+7h, 底+X
// exp1: 弃 7h, 顶+X, 中+2c
// exp2: 弃 7h, 中+2c, 底+X
package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

func mustCard(s string) ofc.Card {
	c, ok := ofc.ParseCard(s)
	if !ok {
		log.Fatalf("bad card: %s", s)
	}
	return c
}

func main() {
	ckpt := "v3-train-i147-sp copy/iter-2/round-001-acc84.json"
	if len(os.Args) > 1 {
		ckpt = os.Args[1]
	}
	if err := ofc.LoadWeightsFromFile(ckpt); err != nil {
		log.Fatalf("load %s: %v", ckpt, err)
	}
	fmt.Fprintf(os.Stderr, "loaded: %s\n\n", ckpt)

	state := ofc.NewGameState(2)
	state.Round = 2
	for _, s := range []string{"3h"} {
		c := mustCard(s)
		state.Top = append(state.Top, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"2s", "7d"} {
		c := mustCard(s)
		state.Middle = append(state.Middle, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"4c", "6h"} {
		c := mustCard(s)
		state.Bottom = append(state.Bottom, c)
		state.UsedCards[c.ID()] = true
	}
	dealt := []ofc.Card{mustCard("7h"), mustCard("X"), mustCard("2c")}

	actions := ofc.GenerateRoundNActions(dealt, state)

	type cand struct {
		discard string
		top     string
		mid     string
		bot     string
		te      float32
	}
	results := []cand{}

	for _, a := range actions {
		tmp := state.Clone()
		tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
		tmp.SetDiscard(dealt[a.DiscardIdx])
		for k, c := range a.Kept {
			tmp.PlaceCard(c, a.Placement[k])
		}
		v := ofc.TrainedEval(tmp)
		results = append(results, cand{
			discard: dealt[a.DiscardIdx].String(),
			top:     cardsStr(tmp.Top),
			mid:     cardsStr(tmp.Middle),
			bot:     cardsStr(tmp.Bottom),
			te:      v,
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].te > results[j].te })

	fmt.Printf("Case 9 R2: top[3h] mid[2s 7d] bot[4c 6h], dealt=7h X 2c\n")
	fmt.Printf("AI : 弃 2c, top=[3h] mid=[2s 7d 7h] bot=[4c 6h X]\n")
	fmt.Printf("exp1: 弃 7h, top=[3h X] mid=[2s 7d 2c] bot=[4c 6h]\n")
	fmt.Printf("exp2: 弃 7h, top=[3h] mid=[2s 7d 2c] bot=[4c 6h X]\n\n")
	fmt.Printf("All %d candidates, sorted by NN value DESC:\n", len(results))
	fmt.Printf("%-3s %-5s %-15s %-25s %-25s %-10s %s\n", "rk", "弃", "top", "mid", "bot", "value", "note")

	for i, r := range results {
		note := ""
		switch {
		case r.discard == "2c" && r.mid == "[2s 7d 7h]" && r.bot == "[4c 6h X]":
			note = "← AI"
		case r.discard == "7h" && r.top == "[3h X]" && r.mid == "[2s 7d 2c]":
			note = "← exp1"
		case r.discard == "7h" && r.top == "[3h]" && r.mid == "[2s 7d 2c]" && r.bot == "[4c 6h X]":
			note = "← exp2"
		}
		fmt.Printf("%-3d %-5s %-15s %-25s %-25s %-10.4f %s\n",
			i+1, r.discard, r.top, r.mid, r.bot, r.te, note)
	}
}

func cardsStr(cs []ofc.Card) string {
	if len(cs) == 0 {
		return "[]"
	}
	out := "["
	for i, c := range cs {
		if i > 0 {
			out += " "
		}
		out += c.String()
	}
	return out + "]"
}
