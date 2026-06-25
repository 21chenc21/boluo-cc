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
	ckpt := "v3-train-i147-sp-from-acc85/iter-1/round-002-acc89.json"
	if len(os.Args) > 1 {
		ckpt = os.Args[1]
	}
	if err := ofc.LoadWeightsFromFile(ckpt); err != nil {
		log.Fatalf("load %s: %v", ckpt, err)
	}
	fmt.Fprintf(os.Stderr, "loaded: %s\n\n", ckpt)

	// Case 58 [R3]: 头[🃏] 中[3c 4c 5c] 底[2s 7d 9d]
	state := ofc.NewGameState(2)
	state.Round = 3
	for _, s := range []string{"X"} {
		c := mustCard(s)
		state.Top = append(state.Top, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"3c", "4c", "5c"} {
		c := mustCard(s)
		state.Middle = append(state.Middle, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"2s", "7d", "9d"} {
		c := mustCard(s)
		state.Bottom = append(state.Bottom, c)
		state.UsedCards[c.ID()] = true
	}
	dealt := []ofc.Card{mustCard("8h"), mustCard("Ah"), mustCard("2h")}

	actions := ofc.GenerateRoundNActions(dealt, state)

	type cand struct {
		discard string
		top     string
		mid     string
		bot     string
		value   float32
	}
	results := []cand{}

	for _, a := range actions {
		tmp := state.Clone()
		tmp.UsedCards[dealt[a.DiscardIdx].ID()] = true
		tmp.SetDiscard(dealt[a.DiscardIdx])
		for k, c := range a.Kept {
			tmp.PlaceCard(c, a.Placement[k])
		}

		// Get NN value via TrainedEval (handles V3 dispatch correctly)
		v := ofc.TrainedEval(tmp)

		results = append(results, cand{
			discard: dealt[a.DiscardIdx].String(),
			top:     cardsStr(tmp.Top),
			mid:     cardsStr(tmp.Middle),
			bot:     cardsStr(tmp.Bottom),
			value:   v,
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].value > results[j].value })

	fmt.Printf("Case 58 R3: head[X] mid[3c 4c 5c] bot[2s 7d 9d], dealt=8h Ah 2h\n")
	fmt.Printf("Expected: head[Ah]  mid[] bot[2h], 弃 8h\n\n")
	fmt.Printf("All %d candidates, sorted by NN value DESC:\n", len(results))
	fmt.Printf("%-4s %-5s %-15s %-25s %-25s %-10s %-6s\n",
		"rk", "弃", "top", "mid", "bot", "value", "note")

	expectedTop := "[Ah X]"
	expectedDis := "8h"
	for i, r := range results {
		note := ""
		if r.discard == expectedDis && r.top == expectedTop {
			note = "← exp1"
		}
		fmt.Printf("%-4d %-5s %-15s %-25s %-25s %-10.4f %s\n",
			i+1, r.discard, r.top, r.mid, r.bot, r.value, note)
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
