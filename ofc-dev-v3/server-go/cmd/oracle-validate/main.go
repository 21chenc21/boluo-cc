// oracle-validate — 跑 testcase 已知正解的 R1 hands, 验证 oracle 给出正确摆法.
// 跑 ~10 个 critical R1 case, K=4 multi-future, 检查 oracle best placement 是否符合期望.
//
// 输出: PASS / FAIL per case, 末尾汇总.
//
// 如果 8/10+ pass → oracle 对, 可以放心 scale up dataset.
// 如果 < 6/10 pass → oracle 有 bug, scale up 前必须 fix.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	multiK = flag.Int("k", 4, "multi-future K for R1 oracle")
	jokers = flag.Int("jokers", 2, "joker count")
	verbose = flag.Bool("v", false, "verbose: show top-5 candidates per case")

	foulCost      = flag.Float64("foul-cost", 20, "")
	fanBonusQQ    = flag.Float64("fan-bonus-qq", 50, "")
	fanBonusKK    = flag.Float64("fan-bonus-kk", 100, "")
	fanBonusAA    = flag.Float64("fan-bonus-aa", 200, "")
	fanBonusTrips = flag.Float64("fan-bonus-trips", 400, "")
)

// expectedFn — 给定 oracle 选的 best placement, 返回 PASS/FAIL + 解释
type validateCase struct {
	name      string
	dealt     string                       // "Ah Ad Kh Kd Qh"
	check     func(top, mid, bot []ofc.Card) (bool, string)
}

// helpers for check fn
func has(cards []ofc.Card, ranks ...uint8) bool {
	cnt := map[uint8]int{}
	for _, c := range cards {
		if !c.IsJoker() {
			cnt[c.Rank()]++
		}
	}
	for _, r := range ranks {
		if cnt[r] == 0 {
			return false
		}
	}
	return true
}
func cntRank(cards []ofc.Card, rank uint8) int {
	c := 0
	for _, card := range cards {
		if !card.IsJoker() && card.Rank() == rank {
			c++
		}
	}
	return c
}
func cntJoker(cards []ofc.Card) int {
	c := 0
	for _, card := range cards {
		if card.IsJoker() {
			c++
		}
	}
	return c
}

// rank 索引: 2=0, ..., T=8, J=9, Q=10, K=11, A=12
var (
	A uint8 = 12
	K uint8 = 11
	Q uint8 = 10
	J uint8 = 9
	T uint8 = 8
)

var cases = []validateCase{
	// case 11: A+joker 应一起上顶 (4d 5h Ah As X)
	{
		name:  "11 [R1]: A+joker 同 top (AA→top + joker, fantasy)",
		dealt: "4d 5h Ah As X",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			ok := cntRank(top, A) == 2 // AA→top
			return ok, fmt.Sprintf("AA on top? %v (cntA top=%d)", ok, cntRank(top, A))
		},
	},
	// case 13: A+joker 同 top (9s 2c X 5h Ac), 单 A + joker → 凑虚 AA
	{
		name:  "13 [R1]: A+joker 同 top (joker+Ac→AA)",
		dealt: "9s 2c X 5h Ac",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			ok := cntRank(top, A) >= 1 && cntJoker(top) >= 1
			return ok, fmt.Sprintf("A+joker on top? %v (A=%d joker=%d)", ok, cntRank(top, A), cntJoker(top))
		},
	},
	// case 14: JsQs 同色高散应底道
	{
		name:  "14 [R1]: JsQs 高同色 → bot (flush 苗)",
		dealt: "9c As Qs Js 7h",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			botS := 0
			for _, c := range bot {
				if !c.IsJoker() && c.Suit() == ofc.SuitS {
					botS++
				}
			}
			ok := botS >= 2
			return ok, fmt.Sprintf("≥2 spades on bot? %v (count=%d)", ok, botS)
		},
	},
	// case 17: TT 应保留底道
	{
		name:  "17 [R1]: TT pair → bot (low pair anchor)",
		dealt: "Td Th 3h 9s Ks",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			ok := cntRank(bot, T) == 2
			return ok, fmt.Sprintf("TT on bot? %v", ok)
		},
	},
	// case 19: 4 张 ♠ 应全底
	{
		name:  "19 [R1]: 4 ♠ → bot (4-flush draw)",
		dealt: "2s 5s 3s Js Ac",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			botS := 0
			for _, c := range bot {
				if !c.IsJoker() && c.Suit() == ofc.SuitS {
					botS++
				}
			}
			ok := botS == 4
			return ok, fmt.Sprintf("4 ♠ on bot? %v (count=%d)", ok, botS)
		},
	},
	// case 51: UR3 hand, AA 锁顶
	{
		name:  "51 [R1]: AA 锁 top (joker+Ac=AA)",
		dealt: "9s 2c X 5h Ac",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			ok := cntRank(top, A) >= 1 && cntJoker(top) >= 1
			return ok, fmt.Sprintf("A+joker on top? %v", ok)
		},
	},
	// case 6: 鬼+KK+AA 双对最优 (AA→top fantasy 是最优)
	{
		name:  "6 [R1]: AA→top + KK→bot (AA fantasy + KK anchor)",
		dealt: "X Kc Kd Ah As",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			topAA := cntRank(top, A) == 2
			botKK := cntRank(bot, K) == 2
			ok := topAA && botKK
			return ok, fmt.Sprintf("AA top? %v, KK bot? %v", topAA, botKK)
		},
	},
	// case 4: 1 鬼放顶+另1鬼配 3♣ 凑 SF
	{
		name:  "4 [R1]: TJQ♣ → bot SF (with joker fill)",
		dealt: "X X Tc Jc Qc",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			// bot has 3+ ♣
			botC := 0
			for _, c := range bot {
				if !c.IsJoker() && c.Suit() == ofc.SuitC {
					botC++
				}
			}
			ok := botC >= 2
			return ok, fmt.Sprintf("≥2 ♣ on bot? %v (count=%d)", ok, botC)
		},
	},
	// case 25: 33 在中保对, 顶无 3
	{
		name:  "25 [R1]: 33 → mid pair (not top)",
		dealt: "3d Js 8s Td 3h",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			midThree := cntRank(mid, 1) // 3 = rank 1
			topThree := cntRank(top, 1)
			ok := midThree == 2 && topThree == 0
			return ok, fmt.Sprintf("33 mid? %v (mid=%d top=%d)", ok, midThree, topThree)
		},
	},
	// case 18: Qd+TT 应同底
	{
		name:  "18 [R1]: TT → bot (Qd kicker bot)",
		dealt: "Qd Tc 4s Td 6d",
		check: func(top, mid, bot []ofc.Card) (bool, string) {
			ok := cntRank(bot, T) == 2
			return ok, fmt.Sprintf("TT on bot? %v", ok)
		},
	},
}

func main() {
	flag.Parse()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanBonusQQ)
	cfg.KKFanBonus = float32(*fanBonusKK)
	cfg.AAFanBonus = float32(*fanBonusAA)
	cfg.TripsFanBonus = float32(*fanBonusTrips)

	fmt.Printf("=== oracle-validate: %d testcase R1 hands, K=%d ===\n", len(cases), *multiK)
	fmt.Printf("knobs: foul=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f\n\n",
		cfg.FoulCost, cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)

	pass := 0
	for i, tc := range cases {
		dealt := parseCards(tc.dealt)
		state := ofc.NewGameState(*jokers)

		// Build "remaining deck" excluding dealt
		used := map[string]bool{}
		for _, c := range dealt {
			used[c.ID()] = true
		}
		fullDeck := ofc.MakeDeck(*jokers)
		var futurePool []ofc.Card
		for _, c := range fullDeck {
			if !used[c.ID()] {
				futurePool = append(futurePool, c)
			}
		}

		// Enumerate R1 candidates
		placements := ofc.GenerateRound1Actions(dealt, state)
		seen := map[string]bool{}
		type cand struct {
			placement []ofc.Row
			score     float32
			top, mid, bot []ofc.Card
		}
		var cands []cand
		for _, p := range placements {
			tmp := state.Clone()
			for k, c := range dealt {
				tmp.PlaceCard(c, p[k])
			}
			key := stateKey(tmp)
			if seen[key] {
				continue
			}
			seen[key] = true
			pCopy := make([]ofc.Row, len(p))
			copy(pCopy, p)

			// K-future oracle
			var sumScore float32
			for k := 0; k < *multiK; k++ {
				tail := sampleTail(futurePool, 12, rng)
				futureRounds := [][]ofc.Card{tail[0:3], tail[3:6], tail[6:9], tail[9:12]}
				sumScore += ofc.OracleSolve(tmp, futureRounds, &cfg)
			}
			avg := sumScore / float32(*multiK)

			topCards, midCards, botCards := splitByPlacement(dealt, pCopy)
			cands = append(cands, cand{pCopy, avg, topCards, midCards, botCards})
		}

		sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })

		best := cands[0]
		ok, explain := tc.check(best.top, best.mid, best.bot)
		marker := "❌ FAIL"
		if ok {
			marker = "✓ PASS"
			pass++
		}
		fmt.Printf("%s  [%2d] %s\n", marker, i+1, tc.name)
		fmt.Printf("       dealt: %s\n", tc.dealt)
		fmt.Printf("       oracle best: top=%s mid=%s bot=%s  score=%.1f\n",
			cardsStr(best.top), cardsStr(best.mid), cardsStr(best.bot), best.score)
		fmt.Printf("       check: %s\n", explain)
		if *verbose {
			fmt.Printf("       top-5 candidates:\n")
			for j := 0; j < 5 && j < len(cands); j++ {
				c := cands[j]
				fmt.Printf("         %d. score=%6.1f  top=%s mid=%s bot=%s\n", j+1, c.score,
					cardsStr(c.top), cardsStr(c.mid), cardsStr(c.bot))
			}
		}
		fmt.Println()
	}

	pct := float64(pass) / float64(len(cases)) * 100
	fmt.Printf("=== Summary: %d/%d PASS (%.1f%%) ===\n", pass, len(cases), pct)
	if pct >= 80 {
		fmt.Println("✓ Oracle output 质量过关, 可放心 scale up dataset")
	} else if pct >= 60 {
		fmt.Println("⚠ Oracle 有部分问题, fix 后再 scale up")
	} else {
		fmt.Println("❌ Oracle 严重错误, 不要 scale up, 需 debug")
	}
}

func parseCards(s string) []ofc.Card {
	parts := strings.Fields(s)
	out := make([]ofc.Card, 0, len(parts))
	jokerIdx := uint8(0)
	for _, p := range parts {
		if p == "X" {
			out = append(out, ofc.MakeJokerWithJID(jokerIdx))
			jokerIdx++
			continue
		}
		c, ok := ofc.ParseCard(p)
		if !ok {
			panic("bad card: " + p)
		}
		out = append(out, c)
	}
	return out
}

func splitByPlacement(dealt []ofc.Card, placement []ofc.Row) (top, mid, bot []ofc.Card) {
	for i, c := range dealt {
		switch placement[i] {
		case ofc.RowTop:
			top = append(top, c)
		case ofc.RowMiddle:
			mid = append(mid, c)
		default:
			bot = append(bot, c)
		}
	}
	return
}

func cardsStr(cards []ofc.Card) string {
	if len(cards) == 0 {
		return "[]"
	}
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = c.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func sampleTail(pool []ofc.Card, n int, rng *rand.Rand) []ofc.Card {
	if n > len(pool) {
		n = len(pool)
	}
	cp := make([]ofc.Card, len(pool))
	copy(cp, pool)
	for i := 0; i < n; i++ {
		j := i + rng.Intn(len(cp)-i)
		cp[i], cp[j] = cp[j], cp[i]
	}
	return cp[:n]
}

func stateKey(gs *ofc.GameState) string {
	tids := cardIDs(gs.Top)
	mids := cardIDs(gs.Middle)
	bids := cardIDs(gs.Bottom)
	sort.Strings(tids)
	sort.Strings(mids)
	sort.Strings(bids)
	return joinIDs(tids) + "|" + joinIDs(mids) + "|" + joinIDs(bids)
}

func cardIDs(cards []ofc.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID()
	}
	return out
}

func joinIDs(ids []string) string {
	return strings.Join(ids, ",")
}
