package main

import (
	"runtime"
	"sync"

	"github.com/boluo/v0-server/ofc"
)

type bruteBest struct {
	roy           int
	top, mid, bot []ofc.Card
}

func combos(n, k int) [][]int {
	var out [][]int
	cur := make([]int, 0, k)
	var gen func(int)
	gen = func(s int) {
		if len(cur) == k {
			out = append(out, append([]int{}, cur...))
			return
		}
		for i := s; i < n; i++ {
			cur = append(cur, i)
			gen(i + 1)
			cur = cur[:len(cur)-1]
		}
	}
	gen(0)
	return out
}

func pick(cs []ofc.Card, idx []int) (in, rest []ofc.Card) {
	m := map[int]bool{}
	for _, i := range idx {
		m[i] = true
		in = append(in, cs[i])
	}
	for i, c := range cs {
		if !m[i] {
			rest = append(rest, c)
		}
	}
	return
}

// bruteForce — 全枚举 top3/mid5/bot5, 只要 non-foul + refan, 取 royalty 最大
func bruteForce(d []ofc.Card) bruteBest {
	tops := combos(len(d), 3)
	mids := combos(len(d)-3, 5)
	bots := combos(len(d)-8, 5)
	var mu sync.Mutex
	best := bruteBest{roy: -1}
	ch := make(chan []int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ti := range ch {
				top, r1 := pick(d, ti)
				for _, mi := range mids {
					mid, r2 := pick(r1, mi)
					for _, bi := range bots {
						bot, _ := pick(r2, bi)
						sc := ofc.ScoreHand(top, mid, bot)
						if sc.Foul || !(sc.TopEval.Type >= ofc.TypeThreeOfAKind || sc.BotEval.Type >= ofc.TypeFourOfAKind) {
							continue
						}
						mu.Lock()
						if sc.Royalties > best.roy {
							best = bruteBest{sc.Royalties, top, mid, bot}
						}
						mu.Unlock()
					}
				}
			}
		}()
	}
	for _, t := range tops {
		ch <- t
	}
	close(ch)
	wg.Wait()
	return best
}
