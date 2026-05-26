// place-debug: dump 给定 state+dealt 下所有候选的 teScore (Go 内部排序前)
// stdin: 一行 JSON 同 place-bench
// stdout: 每候选一行: discardCardID | top | mid | bot | teScore
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

type request struct {
	State struct {
		Top    []string `json:"top"`
		Middle []string `json:"middle"`
		Bottom []string `json:"bottom"`
		Used   []string `json:"used"`
	} `json:"state"`
	Dealt []string `json:"dealt"`
	Round int      `json:"round"`
}

// 复制 Go 端的 expertPlace3 评分逻辑, 但只 dump 不选
func score3(state *ofc.GameState, cards []ofc.Card) {
	actions := ofc.GenerateRoundNActions(cards, state)
	type cand struct {
		discardCard ofc.Card
		gs          *ofc.GameState
		teScore     float32
		topAdded    []string
		midAdded    []string
		botAdded    []string
	}
	type rawCand struct {
		discardIdx int
		kept       []ofc.Card
		placement  []ofc.Row
		gs         *ofc.GameState
		s          float32
	}
	dumps := []rawCand{}
	for i := range actions {
		a := &actions[i]
		gs := state.Clone()
		gs.UsedCards[cards[a.DiscardIdx].ID()] = true
		for k, c := range a.Kept {
			gs.PlaceCard(c, a.Placement[k])
		}
		s := ofc.TrainedEval(gs)

		for k, card := range a.Kept {
			row := a.Placement[k]
			var rowBefore []ofc.Card
			switch row {
			case ofc.RowTop:
				rowBefore = state.Top
			case ofc.RowMiddle:
				rowBefore = state.Middle
			default:
				rowBefore = state.Bottom
			}
			ri := -1
			if !card.IsJoker() {
				ri = int(card.Rank())
			}
			rankMatch := false
			if !card.IsJoker() {
				for _, c2 := range rowBefore {
					if !c2.IsJoker() && c2.Rank() == card.Rank() {
						rankMatch = true
						break
					}
				}
			}
			if rankMatch {
				s += float32(20 + ri*2)
				var rrc [13]int
				for _, c2 := range rowBefore {
					if !c2.IsJoker() {
						rrc[c2.Rank()]++
					}
				}
				rrc[card.Rank()]++
				pairs := 0
				for _, v := range rrc {
					if v >= 2 {
						pairs++
					}
				}
				if pairs >= 2 {
					s += 15
				}
			} else if !card.IsJoker() {
				used := 0
				for _, c2 := range state.Top {
					if !c2.IsJoker() && c2.Rank() == card.Rank() {
						used++
					}
				}
				for _, c2 := range state.Middle {
					if !c2.IsJoker() && c2.Rank() == card.Rank() {
						used++
					}
				}
				for _, c2 := range state.Bottom {
					if !c2.IsJoker() && c2.Rank() == card.Rank() {
						used++
					}
				}
				s += float32(3-used) * 2
			}
			if ri >= 12 {
				s += 5
			} else if ri >= 11 {
				s += 4
			} else if ri >= 10 {
				s += 3
			}
			// 同步注释 (跟 expert_place.go 保持一致)
			// if row == ofc.RowTop && ri >= 10 && len(state.Top) <= 1 {
			// 	s += 20
			// 	if ri >= 12 {
			// 		s += 10
			// 	} else if ri >= 11 {
			// 		s += 5
			// 	}
			// 	rankMatchHere := false
			// 	for _, c2 := range state.Top {
			// 		if !c2.IsJoker() && c2.Rank() == card.Rank() {
			// 			rankMatchHere = true
			// 			break
			// 		}
			// 	}
			// 	if rankMatchHere {
			// 		s += 40
			// 	}
			// }
		}
		dc := cards[a.DiscardIdx]
		if dc.IsJoker() {
			s -= 1e6
		} else {
			dri := int(dc.Rank())
			if dri >= 12 {
				s -= 8
			} else if dri >= 11 {
				s -= 6
			} else if dri >= 10 {
				s -= 4
			}
		}
		// 头道追范保护 / 鬼独顶 / 广义鬼上顶 等条件分支 (复制即可, 此 case 用不到)
		dumps = append(dumps, rawCand{
			discardIdx: a.DiscardIdx,
			kept:       a.Kept,
			placement:  a.Placement,
			gs:         gs,
			s:          s,
		})
	}
	sort.SliceStable(dumps, func(i, j int) bool { return dumps[i].s > dumps[j].s })
	for i, d := range dumps {
		dCard := cards[d.discardIdx]
		var keptStrs []string
		for k, c := range d.kept {
			keptStrs = append(keptStrs, fmt.Sprintf("%s→%s", c.String(), d.placement[k]))
		}
		fmt.Printf("#%2d  s=%7.2f  d=%s  %v\n", i, d.s, dCard.String(), keptStrs)
		if i > 25 {
			fmt.Println("  ... (truncated)")
			break
		}
	}
}

func main() {
	weights := flag.String("weights", "", "path to weights JSON")
	flag.Parse()
	if *weights != "" {
		if err := ofc.LoadWeightsFromFile(*weights); err != nil {
			log.Fatalf("load weights: %v", err)
		}
		fmt.Fprintf(os.Stderr, "[place-debug] loaded weights from %s\n", *weights)
	}
	dec := json.NewDecoder(os.Stdin)
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			break
		}
		gs := ofc.NewGameState(0)
		gs.Round = req.Round
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
		for _, s := range req.State.Used {
			c, ok := ofc.ParseCard(s)
			if !ok {
				log.Fatalf("bad used card: %s", s)
			}
			gs.UsedCards[c.ID()] = true
		}
		var dealt []ofc.Card
		for _, s := range req.Dealt {
			c, _ := ofc.ParseCard(s)
			dealt = append(dealt, c)
		}
		fmt.Printf("=== state: top=%v mid=%v bot=%v used=%v dealt=%v round=%d ===\n",
			req.State.Top, req.State.Middle, req.State.Bottom, req.State.Used, req.Dealt, req.Round)
		score3(gs, dealt)
	}
}
