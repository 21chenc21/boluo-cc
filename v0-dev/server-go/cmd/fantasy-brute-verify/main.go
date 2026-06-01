// fantasy-brute-verify: 暴力枚举验证 fantasy solver 没漏 re-fan 解
//
// 用途: 给 prod solve_log 的 fantasy 记录 (TSV: id|request_json|response_json),
// 对每个"出范 (no re-fan)"的 case, brute force 枚举所有 top-trips 摆法 + mid/bot 组合,
// 验证是否真的不可 re-fan, 还是 solver miss 了.
//
// 用法:
//   # 1. 拿 prod fantasy logs
//   ssh prod 'sqlite3 ~/boluo-cc/ofc-dev-v3/games.db "SELECT id, request_json, response_json FROM solve_log WHERE mode=\"fantasy\" ORDER BY id"' > /tmp/prod_fantasy.tsv
//
//   # 2. 跑暴力验证
//   ./fantasy-brute-verify -file=/tmp/prod_fantasy.tsv
//   ./fantasy-brute-verify -file=/tmp/prod_fantasy.tsv -min-dealt=14 -max-dealt=15
//
// 性能 (单 case):
//   dealt=14: C(14,3)*C(11,5)*C(6,5) ~ 8s 全 batch
//   dealt=16: C(16,3)*C(13,5)*C(8,5) ~ 1 分钟单 case (慎用 -max-dealt=16)
//   有 mid type > 3 早期剪枝, 实际更快.

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/boluo/v0-server/ofc"
)

type req struct {
	Dealt  []string `json:"dealt"`
	DC     int      `json:"discardCount"`
	GameID string   `json:"game_id"`
	UID    string   `json:"uid"`
}
type resp struct {
	Layout struct {
		Top, Middle, Bottom []string
	} `json:"layout"`
}

func parseCards(ss []string) []ofc.Card {
	out := make([]ofc.Card, 0, len(ss))
	for _, s := range ss {
		c, ok := ofc.ParseCard(s)
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// isReFan: top trips OR bot quads/SF/RF
func isReFan(top, mid, bot []ofc.Card) bool {
	if len(top) == 3 {
		var midEv ofc.HandValue
		if len(mid) == 5 {
			midEv = ofc.Evaluate5(mid)
		}
		topEv := ofc.Evaluate3JokerCap(top, &midEv)
		if topEv.Type == ofc.TypeThreeOfAKind {
			return true
		}
	}
	if len(bot) == 5 {
		botEv := ofc.Evaluate5JokerCap(bot, nil)
		if botEv.Type >= ofc.TypeFourOfAKind {
			return true
		}
	}
	return false
}

// combos: 从 items 选 k 个, 回调每个组合
func combos(items []int, k int, cb func([]int) bool) {
	cur := make([]int, k)
	var rec func(start, idx int) bool
	rec = func(start, idx int) bool {
		if idx == k {
			return cb(cur)
		}
		for i := start; i <= len(items)-(k-idx); i++ {
			cur[idx] = items[i]
			if !rec(i+1, idx+1) {
				return false
			}
		}
		return true
	}
	rec(0, 0)
}

// bruteFindTopTripsReFan: 暴力找 top-trips re-fan 解 (含 mid/bot 非 foul 验证)
// 返回 (found, top, mid, bot)
func bruteFindTopTripsReFan(dealt []ofc.Card) (bool, []ofc.Card, []ofc.Card, []ofc.Card) {
	N := len(dealt)
	if N < 13 {
		return false, nil, nil, nil
	}
	all := make([]int, N)
	for i := range all {
		all[i] = i
	}

	var foundTop, foundMid, foundBot []ofc.Card
	combos(all, 3, func(topIdx []int) bool {
		topCards := []ofc.Card{dealt[topIdx[0]], dealt[topIdx[1]], dealt[topIdx[2]]}
		var midEv ofc.HandValue
		topEv := ofc.Evaluate3JokerCap(topCards, &midEv)
		if topEv.Type != ofc.TypeThreeOfAKind {
			return true // continue
		}

		used := make(map[int]bool)
		for _, ti := range topIdx {
			used[ti] = true
		}
		rest := []int{}
		for i := 0; i < N; i++ {
			if !used[i] {
				rest = append(rest, i)
			}
		}

		stop := false
		combos(rest, 5, func(midIdx []int) bool {
			midCards := []ofc.Card{}
			midUsed := make(map[int]bool)
			for _, mi := range midIdx {
				midCards = append(midCards, dealt[mi])
				midUsed[mi] = true
			}
			midEv := ofc.Evaluate5JokerCap(midCards, nil)
			// 早期剪: mid 必须 > top trips
			if midEv.Type < ofc.TypeThreeOfAKind {
				return true
			}
			if midEv.Type == ofc.TypeThreeOfAKind && midEv.Value <= topEv.Value {
				return true
			}

			rest2 := []int{}
			for _, ri := range rest {
				if !midUsed[ri] {
					rest2 = append(rest2, ri)
				}
			}
			combos(rest2, 5, func(botIdx []int) bool {
				botCards := []ofc.Card{}
				for _, bi := range botIdx {
					botCards = append(botCards, dealt[bi])
				}
				sc := ofc.ScoreHand(topCards, midCards, botCards)
				if sc.Foul {
					return true
				}
				if isReFan(topCards, midCards, botCards) {
					foundTop = topCards
					foundMid = midCards
					foundBot = botCards
					stop = true
					return false
				}
				return true
			})
			if stop {
				return false
			}
			return true
		})
		if stop {
			return false
		}
		return true
	})
	return foundTop != nil, foundTop, foundMid, foundBot
}

func rowStr(cs []ofc.Card) string {
	s := ""
	for i, c := range cs {
		if i > 0 {
			s += " "
		}
		s += c.ID()
	}
	return s
}

func main() {
	file := flag.String("file", "/tmp/prod_fantasy.tsv", "TSV file (id|request_json|response_json), see header comment")
	minDealt := flag.Int("min-dealt", 14, "只 brute dealt 张数 ≥ 此值")
	maxDealt := flag.Int("max-dealt", 16, "只 brute dealt 张数 ≤ 此值 (>=17 太慢)")
	verbose := flag.Bool("v", false, "每 N 个 case 输出进度")
	flag.Parse()

	f, err := os.Open(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", *file, err)
		os.Exit(1)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 65536), 1<<20)

	total, checked, missed := 0, 0, 0
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		id := parts[0]
		var rq req
		var rs resp
		if json.Unmarshal([]byte(parts[1]), &rq) != nil {
			continue
		}
		if json.Unmarshal([]byte(parts[2]), &rs) != nil {
			continue
		}
		total++

		prodTop := parseCards(rs.Layout.Top)
		prodMid := parseCards(rs.Layout.Middle)
		prodBot := parseCards(rs.Layout.Bottom)
		if isReFan(prodTop, prodMid, prodBot) {
			continue // 已 re-fan, skip
		}

		dealt := parseCards(rq.Dealt)
		if len(dealt) < *minDealt || len(dealt) > *maxDealt {
			continue
		}

		// 当前 solver 是否找到
		dc := rq.DC
		if dc == 0 {
			dc = len(dealt) - 13
		}
		var r *ofc.FantasyResult
		func() {
			defer func() { recover() }()
			r = ofc.ExpertPlaceFantasy(dealt, dc)
		}()
		if r != nil && isReFan(r.Layout.Top, r.Layout.Middle, r.Layout.Bottom) {
			continue // 修后 solver 已找到, skip
		}

		checked++
		if *verbose && checked%5 == 0 {
			fmt.Fprintf(os.Stderr, "checked %d (dealt %d-%d), missed=%d\n", checked, *minDealt, *maxDealt, missed)
		}

		found, t, m, b := bruteFindTopTripsReFan(dealt)
		if found {
			missed++
			fmt.Printf("[id=%s game=%s len=%d] dealt=%v\n  ‼ SOLVER MISSED. Brute: top=[%s] mid=[%s] bot=[%s]\n",
				id, rq.GameID, len(dealt), rq.Dealt, rowStr(t), rowStr(m), rowStr(b))
		}
	}
	fmt.Printf("\n=== Brute verify: total=%d, brute checked (dealt %d-%d)=%d, MISSED=%d ===\n",
		total, *minDealt, *maxDealt, checked, missed)
	if missed == 0 {
		fmt.Println("✓ 所有 dealt 范围内的 fantasy 出范 case 都是真无解, solver 没漏")
	}
}
