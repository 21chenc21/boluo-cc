// case-debug-r1: R1 候选完整 score 分解 (TrainedEval + penalty/bonus)
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
	state.Round = 1
	caseN := os.Getenv("CASE")
	var dealt []ofc.Card
	switch caseN {
	case "11":
		dealt = []ofc.Card{mustCard("4d"), mustCard("5h"), mustCard("Ah"), mustCard("As"), mustCard("X")}
	case "12":
		dealt = []ofc.Card{mustCard("X"), mustCard("4c"), mustCard("5h"), mustCard("As"), mustCard("8h")}
	case "13":
		dealt = []ofc.Card{mustCard("X"), mustCard("2c"), mustCard("5h"), mustCard("9s"), mustCard("Ac")}
	case "24":
		dealt = []ofc.Card{mustCard("3h"), mustCard("2d"), mustCard("Ks"), mustCard("As"), mustCard("X")}
	default: // case 3
		dealt = []ofc.Card{mustCard("X"), mustCard("Ac"), mustCard("Ad"), mustCard("3h"), mustCard("7s")}
	}
	fmt.Fprintf(os.Stderr, "case=%s dealt=", caseN)
	for _, c := range dealt {
		fmt.Fprintf(os.Stderr, "%s ", c.String())
	}
	fmt.Fprintln(os.Stderr)

	actions := ofc.GenerateRound1Actions(dealt, state)

	type cand struct {
		top      string
		mid      string
		bot      string
		teVal    float32
		connSp   float32
		r1Four   float32
		r1Incoh  float32
		r1TopN   float32
		r1JOWA   float32
		foulImm  float32
		bonSame  float32
		bonJokerA float32
		bonSingleA float32
		bonFlushBot float32
		netPen   float32
		score    float32
	}
	results := []cand{}

	for _, p := range actions {
		tmp := state.Clone()
		for i, c := range dealt {
			tmp.PlaceCard(c, p[i])
		}
		teVal := ofc.TrainedEval(tmp)
		connSp := ofc.ConnectorSplitPenalty(p, dealt)
		r1Four := ofc.R1FourInRowPenalty(p, dealt)
		r1Incoh := ofc.R1IncoherentRowPenalty(p, dealt)
		r1TopN := ofc.R1TopNonAKXPenalty(p, dealt, state)
		r1JOWA := ofc.R1JokerOnTopWithAAPenalty(p, dealt)
		foulImm := ofc.FoulImminentPenalty(tmp)
		bSame := ofc.R1SameSuitInRowBonus(p, dealt)
		bJokerA := ofc.R1JokerWithAOnTopBonus(p, dealt)
		bSingleA := ofc.R1SingleAOnTopBonus(p, dealt)
		bFlushBot := ofc.R1FlushGroupOnBotBonus(p, dealt)
		penalty := connSp + r1Four + r1Incoh + r1TopN + r1JOWA + foulImm - bSame - bJokerA - bSingleA - bFlushBot
		score := teVal - penalty
		results = append(results, cand{
			top: cardsStr(tmp.Top), mid: cardsStr(tmp.Middle), bot: cardsStr(tmp.Bottom),
			teVal: teVal, connSp: connSp, r1Four: r1Four, r1Incoh: r1Incoh, r1TopN: r1TopN,
			r1JOWA: r1JOWA, foulImm: foulImm, bonSame: bSame, bonJokerA: bJokerA,
			bonSingleA: bSingleA, bonFlushBot: bFlushBot, netPen: penalty, score: score,
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	fmt.Printf("Case 3 R1: dealt=[X Ac Ad 3h 7s]\n")
	fmt.Printf("AI : top=[Ac Ad] mid=[] bot=[X 3h 7s]\n")
	fmt.Printf("exp1: top=[Ac Ad] mid=[3h 7s] bot=[X]\n")
	fmt.Printf("exp2: top=[Ac Ad] mid=[X 3h] bot=[7s]\n")
	fmt.Printf("exp3: top=[Ac Ad] mid=[3h] bot=[7s X]\n\n")
	fmt.Printf("Top 10 候选 — score = TrainedEval - 净 penalty\n\n")
	fmt.Printf("%-3s %-15s %-15s %-15s %-8s %-8s %-8s\n", "rk", "top", "mid", "bot", "TE", "净pen", "score")

	for i, r := range results {
		if i >= 10 {
			break
		}
		note := matchNote(r)
		fmt.Printf("%-3d %-15s %-15s %-15s %-8.3f %-+8.3f %-8.3f %s\n",
			i+1, r.top, r.mid, r.bot, r.teVal, r.netPen, r.score, note)
	}

	// 详细 penalty 分解给 top 4
	fmt.Printf("\n详细 penalty 分解 (top 4):\n")
	fmt.Printf("%-3s %-15s %-15s %-15s %-7s %-7s %-7s %-7s %-7s %-7s %-7s %-7s %-7s %-7s\n",
		"rk", "top", "mid", "bot", "conSp", "4-row", "incoh", "topN", "JokA", "foul", "bsame", "bJokA", "bsngA", "bflBot")
	for i, r := range results {
		if i >= 4 {
			break
		}
		fmt.Printf("%-3d %-15s %-15s %-15s %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f %-7.2f\n",
			i+1, r.top, r.mid, r.bot, r.connSp, r.r1Four, r.r1Incoh, r.r1TopN, r.r1JOWA, r.foulImm,
			r.bonSame, r.bonJokerA, r.bonSingleA, r.bonFlushBot)
	}
}

func matchNote(c struct {
	top, mid, bot string
	teVal, connSp, r1Four, r1Incoh, r1TopN, r1JOWA, foulImm, bonSame, bonJokerA, bonSingleA, bonFlushBot, netPen, score float32
}) string {
	if c.top != "[Ac Ad]" {
		return ""
	}
	switch {
	case c.mid == "[]":
		return "← AI"
	case (c.mid == "[3h 7s]" || c.mid == "[7s 3h]") && c.bot == "[X]":
		return "← exp1"
	case (c.mid == "[X 3h]" || c.mid == "[3h X]") && c.bot == "[7s]":
		return "← exp2"
	case c.mid == "[3h]":
		return "← exp3"
	}
	return ""
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
