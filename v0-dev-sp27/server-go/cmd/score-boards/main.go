// score-boards — 读 13张终局板 JSON 数组, 用 ScoreHand 统计 fan/foul/royalty (prod solve_log 实战复盘用)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/boluo/v0-server/ofc"
)

type board struct {
	Top    []string `json:"top"`
	Middle []string `json:"middle"`
	Bottom []string `json:"bottom"`
}

func parse(ss []string) ([]ofc.Card, bool) {
	var out []ofc.Card
	for _, s := range ss {
		c, ok := ofc.ParseCard(s)
		if !ok {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

func main() {
	path := flag.String("boards", "", "boards json")
	flag.Parse()
	data, err := os.ReadFile(*path)
	if err != nil {
		panic(err)
	}
	var bs []board
	if err := json.Unmarshal(data, &bs); err != nil {
		panic(err)
	}
	nFoul, nFan, sumRoy, n := 0, 0, 0, 0
	for _, b := range bs {
		t, ok1 := parse(b.Top)
		m, ok2 := parse(b.Middle)
		bo, ok3 := parse(b.Bottom)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		n++
		sc := ofc.ScoreHand(t, m, bo)
		if sc.Foul {
			nFoul++
		} else if sc.Fantasy {
			nFan++
		}
		sumRoy += sc.Royalties
	}
	fmt.Printf("手数=%d  FOUL=%d (%.1f%%)  范=%d (%.1f%%)  平均royalty=%.2f\n",
		n, nFoul, 100*float64(nFoul)/float64(n), nFan, 100*float64(nFan)/float64(n), float64(sumRoy)/float64(n))
}
