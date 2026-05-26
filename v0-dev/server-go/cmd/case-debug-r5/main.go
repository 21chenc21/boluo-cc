// case-debug-r5: case 49 (R5) NN value 排序 + features diff
// Case 49 R5: top[X Kh] mid[Qc Qh Jh 9d] bot[3s 4h 5s 6h 7d], dealt 8h Kc 2c
//   AI: 头[2c] 中[Kc] 底[] 弃 8h
//   exp1: 头[8h] 中[Kc] 底[] 弃 2c
//   exp2: 头[8h] 中[2c] 底[] 弃 Kc
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
	state.Round = 5
	for _, s := range []string{"X", "Kh"} {
		c := mustCard(s)
		state.Top = append(state.Top, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"Qc", "Qh", "Jh", "9d"} {
		c := mustCard(s)
		state.Middle = append(state.Middle, c)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range []string{"3s", "4h", "5s", "6h", "7d"} {
		c := mustCard(s)
		state.Bottom = append(state.Bottom, c)
		state.UsedCards[c.ID()] = true
	}
	dealt := []ofc.Card{mustCard("Kc"), mustCard("8h"), mustCard("2c")}

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

	fmt.Printf("Case 49 R5: top[X Kh] mid[Qc Qh Jh 9d] bot[3s 4h 5s 6h 7d], dealt=Kc 8h 2c\n")
	fmt.Printf("Expected exp1: top+8h, mid+Kc → 弃 2c\n")
	fmt.Printf("Expected exp2: top+8h, mid+2c → 弃 Kc\n")
	fmt.Printf("AI 选 (Mac): top+2c, mid+Kc → 弃 8h\n\n")
	fmt.Printf("All %d candidates, sorted by NN value DESC:\n", len(results))
	fmt.Printf("%-4s %-5s %-20s %-30s %-25s %-10s %s\n", "rk", "弃", "top", "mid", "bot", "value", "note")
	for i, r := range results {
		note := ""
		if r.discard == "2c" && contains(r.top, "8h") {
			note = "← exp1"
		} else if r.discard == "Kc" && contains(r.top, "8h") {
			note = "← exp2"
		} else if r.discard == "8h" && contains(r.top, "2c") && contains(r.mid, "Kc") {
			note = "← Mac AI"
		}
		fmt.Printf("%-4d %-5s %-20s %-30s %-25s %-10.4f %s\n",
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
