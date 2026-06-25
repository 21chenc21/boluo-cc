// features-v2-smoke — 验证 BuildFeaturesV2 在 case 2 上的关键 feature 值是否合理.
package main

import (
	"fmt"

	"github.com/boluo/v0-server/ofc"
)

type testCase struct {
	name   string
	top    []string
	mid    []string
	bot    []string
	used   []string
	round  int
}

func parseCards(strs []string) []ofc.Card {
	out := []ofc.Card{}
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			panic("bad card: " + s)
		}
		out = append(out, c)
	}
	return out
}

func buildState(tc testCase) *ofc.GameState {
	gs := &ofc.GameState{
		Top:       parseCards(tc.top),
		Middle:    parseCards(tc.mid),
		Bottom:    parseCards(tc.bot),
		UsedCards: map[string]bool{},
		Round:     tc.round,
	}
	for _, s := range tc.used {
		c, _ := ofc.ParseCard(s)
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Top {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Middle {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Bottom {
		gs.UsedCards[c.ID()] = true
	}
	return gs
}

func showKeyFeatures(name string, gs *ofc.GameState) {
	f := ofc.BuildFeaturesV2(gs)
	fmt.Printf("\n=== %s ===\n", name)
	fmt.Printf("State: 头%v 中%v 底%v\n",
		cardsToStr(gs.Top), cardsToStr(gs.Middle), cardsToStr(gs.Bottom))
	fmt.Println("--- Group A (board state) ---")
	fmt.Printf("  topN=%.2f midN=%.2f botN=%.2f  topSlots=%.2f midSlots=%.2f botSlots=%.2f  round=%.2f complete=%.0f\n",
		f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7])
	fmt.Println("--- Group B (hand tier per row) ---")
	fmt.Printf("  top(HC,P<Q,PQ,PK,PA,Trips)=%v\n", f[8:14])
	fmt.Printf("  mid(HC,P,2P,3K,St,Fl,FH,4K,SF)=%v\n", f[14:23])
	fmt.Printf("  bot(HC,P,2P,3K,St,Fl,FH,4K,SF)=%v\n", f[23:32])
	fmt.Println("--- Group C (top fantasy) ---")
	fmt.Printf("  pair_rank_onehot[13]=%v\n", f[32:45])
	fmt.Printf("  has_real_pair=%.0f wild_pair=%.0f real_trips=%.0f\n", f[45], f[46], f[47])
	fmt.Printf("  fantasy_floor_tier=[none,QQ,KK,AA,trips]=%v\n", f[48:53])
	fmt.Printf("  can_upgrade_to_AA=%.0f\n", f[53])
	fmt.Println("--- Group D (joker) ---")
	fmt.Printf("  jokers_top/mid/bot/total/deck=%.2f/%.2f/%.2f/%.2f/%.2f\n",
		f[54], f[55], f[56], f[57], f[58])
	fmt.Printf("  joker_eff_rank top/mid/bot=%.2f/%.2f/%.2f\n", f[59], f[60], f[61])
	fmt.Println("--- Group K (joker completes) ---")
	fmt.Printf("  top_wild_trips=%.0f\n", f[115])
	fmt.Printf("  mid_completes(pair,trips,quad,straight,flush,FH)=%v\n", f[116:122])
	fmt.Printf("  bot_completes(pair,trips,quad,straight,flush,FH)=%v\n", f[122:128])
	fmt.Println("--- Group L (cross-row anti-patterns) ---")
	fmt.Printf("  pairs_split=%.2f  flushgroup_split=%.2f  connectors_split=%.2f  bot_min-mid_max=%+.3f  gap1_orphan=%.2f\n",
		f[128], f[129], f[130], f[131], f[132])
}

func cardsToStr(cards []ofc.Card) string {
	s := "["
	for i, c := range cards {
		if i > 0 {
			s += " "
		}
		s += c.String()
	}
	return s + "]"
}

func main() {
	fmt.Println("=== Case 2 smoke test: dealt X Qc 2d 5h 8s ===")
	fmt.Println()
	fmt.Println("Comparing 3 candidates (post-placement state):")

	// Candidate A (user expected): 头[X Qc] 中[2d 5h] 底[8s]
	candA := buildState(testCase{
		top: []string{"X", "Qc"},
		mid: []string{"2d", "5h"},
		bot: []string{"8s"},
		round: 1,
	})
	showKeyFeatures("Candidate A (X+Qc 顶) — user expected", candA)

	// Candidate B (AI choice): 头[X] 中[2d 5h 8s] 底[Qc]
	candB := buildState(testCase{
		top: []string{"X"},
		mid: []string{"2d", "5h", "8s"},
		bot: []string{"Qc"},
		round: 1,
	})
	showKeyFeatures("Candidate B (X 单顶) — AI choice", candB)

	// Candidate C: 头[Qc] 中[2d 5h] 底[X 8s] (Qc 顶 X 底)
	candC := buildState(testCase{
		top: []string{"Qc"},
		mid: []string{"2d", "5h"},
		bot: []string{"X", "8s"},
		round: 1,
	})
	showKeyFeatures("Candidate C (Qc 单顶, X 底)", candC)

	fmt.Println()
	fmt.Println("=== Case 14 smoke: 9c As Qs Js 7h, expected 头[As] 中[9c 7h] 底[Qs Js] ===")
	cand14a := buildState(testCase{
		top: []string{"As"},
		mid: []string{"9c", "7h"},
		bot: []string{"Qs", "Js"},
		round: 1,
	})
	showKeyFeatures("Case 14 expected", cand14a)

	fmt.Println()
	fmt.Println("=== Case 51 smoke: 0 A used, X + Ac top expected ===")
	cand51 := buildState(testCase{
		top: []string{"X", "Ac"},
		mid: []string{"2c", "5h"},
		bot: []string{"9s"},
		round: 1,
	})
	showKeyFeatures("Case 51 expected (X+Ac 顶, 0A used)", cand51)

	fmt.Println()
	fmt.Println("=== Case 53 smoke: 3 A used → joker eff still A (last A in deck) ===")
	cand53 := buildState(testCase{
		top:   []string{"X", "Ac"},
		mid:   []string{"2c", "5h"},
		bot:   []string{"9s"},
		used:  []string{"Ad", "Ah", "As"},
		round: 1,
	})
	showKeyFeatures("Case 53 (X+Ac 顶, 3A used in deck)", cand53)

	fmt.Println()
	fmt.Println("=== Group L verify: case 12 split connectors test ===")
	// case 12 dealt As 4c 8h X 5h
	// AI choice: 头[As X] 中[5h] 底[4c 8h] — 拆 4-5 (4c bot, 5h mid)
	cand12ai := buildState(testCase{
		top: []string{"As", "X"},
		mid: []string{"5h"},
		bot: []string{"4c", "8h"},
		round: 1,
	})
	showKeyFeatures("Case 12 AI (拆 4-5)", cand12ai)
	// User expected: 头[As X] 中[4c 5h] 底[8h] — 4-5 同行 (中)
	cand12exp := buildState(testCase{
		top: []string{"As", "X"},
		mid: []string{"4c", "5h"},
		bot: []string{"8h"},
		round: 1,
	})
	showKeyFeatures("Case 12 expected (4-5 同行)", cand12exp)

	fmt.Println()
	fmt.Println("=== Group L verify: case 14 split flush test ===")
	// dealt 9c As Qs Js 7h
	// User exp: 头[As] 中[9c 7h] 底[Qs Js] — ♠ split (As 顶 + Qs Js 底) - 但 user 接受 (A 上顶)
	cand14exp := buildState(testCase{
		top: []string{"As"},
		mid: []string{"9c", "7h"},
		bot: []string{"Qs", "Js"},
		round: 1,
	})
	showKeyFeatures("Case 14 expected (As 顶, Qs Js 底, ♠ 3 cards split)", cand14exp)
	// Counter: 全 ♠ 底
	cand14alt := buildState(testCase{
		top: []string{"9c"},
		mid: []string{"7h"},
		bot: []string{"As", "Qs", "Js"},
		round: 1,
	})
	showKeyFeatures("Case 14 alt (♠ 全底, 9c 顶)", cand14alt)

	fmt.Println()
	fmt.Println("=== Group L verify: case 2 kicker order test (高牌应底) ===")
	// AI bug: 头[X] 中[2d 8s] 底[Qc 5h] — 5h (low) 放底, 8s (mid) 放中
	cand2bug := buildState(testCase{
		top: []string{"X"},
		mid: []string{"2d", "8s"},
		bot: []string{"Qc", "5h"},
		round: 1,
	})
	showKeyFeatures("Case 2 BUG (5h 底, 8s 中)", cand2bug)
	// Exp3 fix: 头[X] 中[2d 5h] 底[Qc 8s] — 8s 底
	cand2exp := buildState(testCase{
		top: []string{"X"},
		mid: []string{"2d", "5h"},
		bot: []string{"Qc", "8s"},
		round: 1,
	})
	showKeyFeatures("Case 2 EXP (8s 底, 5h 中)", cand2exp)

	fmt.Println()
	fmt.Println("=== Group L verify: case 33 split pair test (R2) ===")
	// case 33: state 头[Qd] 中[5c 6c] 底[3h 9s], dealt Kh Ks 4d
	// User exp: KK 同底 → no split pair
	cand33exp := buildState(testCase{
		top:   []string{"Qd"},
		mid:   []string{"5c", "6c"},
		bot:   []string{"3h", "9s", "Kh", "Ks"},
		used:  []string{},
		round: 2,
	})
	showKeyFeatures("Case 33 expected (KK 同底)", cand33exp)
	// AI bug: 头[Qd Kh] 底[3h 9s Ks] → 拆 KK
	cand33bug := buildState(testCase{
		top:   []string{"Qd", "Kh"},
		mid:   []string{"5c", "6c"},
		bot:   []string{"3h", "9s", "Ks"},
		round: 2,
	})
	showKeyFeatures("Case 33 BUG (拆 KK 顶+底)", cand33bug)
}
