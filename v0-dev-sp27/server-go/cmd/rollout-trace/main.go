// stdin: { state, currentRound, seed } → stdout: trace + final score
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/boluo/v0-server/ofc"
)

type request struct {
	State struct {
		Top    []string `json:"top"`
		Middle []string `json:"middle"`
		Bottom []string `json:"bottom"`
		Used   []string `json:"used"`
		Round  int      `json:"round"`
	} `json:"state"`
	CurrentRound int    `json:"currentRound"`
	Seed         uint32 `json:"seed"`
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		gs := ofc.NewGameState(0)
		gs.Round = req.State.Round
		for _, s := range req.State.Top {
			c, _ := ofc.ParseCard(s)
			gs.PlaceCard(c, ofc.RowTop)
		}
		for _, s := range req.State.Middle {
			c, _ := ofc.ParseCard(s)
			gs.PlaceCard(c, ofc.RowMiddle)
		}
		for _, s := range req.State.Bottom {
			c, _ := ofc.ParseCard(s)
			gs.PlaceCard(c, ofc.RowBottom)
		}
		for _, cid := range req.State.Used {
			gs.UsedCards[cid] = true
		}
		lcg := ofc.NewLCG(req.Seed)
		// 打印 deck order (filter 后, shuffle 前) 看是否与 JS 一致
		gsClone := gs.Clone()
		preDeck := gsClone.GetRemainingDeck()
		preStr := make([]string, len(preDeck))
		for i, c := range preDeck {
			preStr[i] = c.String()
		}
		fmt.Fprintf(os.Stdout, "deck-pre-shuffle (len=%d): %v\n", len(preDeck), preStr[:10])
		// 不消耗 lcg, 后面 QuickRollout 会再 shuffle
		er := &ofc.ExpertRollout{
			Rng: lcg, Cfg: ofc.DefaultRolloutConfig,
			Verbose: func(format string, args ...interface{}) {
				fmt.Fprintf(os.Stdout, format, args...)
			},
		}
		score := er.QuickRollout(gs, req.CurrentRound)
		fmt.Printf("FINAL=%.2f\n", score)
	}
}
