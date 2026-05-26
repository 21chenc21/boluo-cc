// Place-bench: 用 LCG seed 做确定性 expertPlace5/3 调用
// stdin: 每行 JSON `{ "round": N, "state": {top,middle,bottom,used,round},
//                     "dealt":[...], "seed": N, "level": "low/medium/high" }`
// stdout: 每行 JSON `{ "top":[...], "middle":[...], "bottom":[...], "discards":[...] }`
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/boluo/v0-server/ofc"
)

type request struct {
	Round int `json:"round"`
	State struct {
		Top    []string `json:"top"`
		Middle []string `json:"middle"`
		Bottom []string `json:"bottom"`
		Used   []string `json:"used"`
	} `json:"state"`
	Dealt []string `json:"dealt"`
	Seed  uint32   `json:"seed"`
	Level string   `json:"level"`
}

type response struct {
	Top      []string `json:"top"`
	Middle   []string `json:"middle"`
	Bottom   []string `json:"bottom"`
	Discards []string `json:"discards"`
}

func parseCards(strs []string) []ofc.Card {
	out := make([]ofc.Card, 0, len(strs))
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			fmt.Fprintf(os.Stderr, "bad card: %s\n", s)
			os.Exit(1)
		}
		out = append(out, c)
	}
	return out
}

func cardStrs(cards []ofc.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.String()
	}
	return out
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			fmt.Fprintf(os.Stderr, "bad json: %v\n", err)
			continue
		}
		// 构 state
		gs := ofc.NewGameState(0)
		gs.Round = req.Round
		for _, c := range parseCards(req.State.Top) {
			gs.PlaceCard(c, ofc.RowTop)
		}
		for _, c := range parseCards(req.State.Middle) {
			gs.PlaceCard(c, ofc.RowMiddle)
		}
		for _, c := range parseCards(req.State.Bottom) {
			gs.PlaceCard(c, ofc.RowBottom)
		}
		for _, cid := range req.State.Used {
			gs.UsedCards[cid] = true
		}
		dealt := parseCards(req.Dealt)

		// LCG with seed
		lcg := ofc.NewLCG(req.Seed)
		cfg := ofc.DefaultRolloutConfig
		switch req.Level {
		case "low":
			cfg.R1Mult = 0.25
		case "medium":
			cfg.R1Mult = 0.5
		default:
			cfg.R1Mult = 1.0
		}
		er := &ofc.ExpertRollout{Rng: lcg, Cfg: cfg}

		// 记 before
		beforeTop := append([]ofc.Card(nil), gs.Top...)
		beforeMid := append([]ofc.Card(nil), gs.Middle...)
		beforeBot := append([]ofc.Card(nil), gs.Bottom...)

		if req.Round == 1 || len(dealt) == 5 {
			er.ExpertPlace5(gs, dealt)
		} else {
			er.ExpertPlace3(gs, dealt)
		}

		// diff
		addedTop := diffCards(beforeTop, gs.Top)
		addedMid := diffCards(beforeMid, gs.Middle)
		addedBot := diffCards(beforeBot, gs.Bottom)
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
		var discards []string
		for _, c := range dealt {
			if !placed[c.ID()] {
				discards = append(discards, c.String())
			}
		}
		resp := response{
			Top: cardStrs(addedTop), Middle: cardStrs(addedMid), Bottom: cardStrs(addedBot),
			Discards: discards,
		}
		out, _ := json.Marshal(resp)
		fmt.Println(string(out))
	}
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
