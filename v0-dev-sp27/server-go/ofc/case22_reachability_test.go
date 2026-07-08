package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 案22 可达性 (2026-07-08 fuse4d2 新伤过堂): 头[🃏] 中[2d 7s 7d] 底[Kc Kd 🃏] R3 案发板.
// 枚举 C(7,5) R1 拆分 + R2 剩2+垫牌, 看候选权重会不会亲手摆进案发现场.
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestCase22Reachability -v
func TestCase22Reachability(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	MctsDisabled = true
	defer func() { MctsDisabled = false }()

	parse := func(ss []string) []Card {
		var r []Card
		for _, x := range ss {
			c, _ := ParseCard(x)
			r = append(r, c)
		}
		return r
	}
	// 鬼身份互换容忍: joker 一律归一成 "X"
	norm := func(c Card) string {
		id := c.ID()
		if c.IsJoker() {
			return "X"
		}
		return id
	}
	rowOf := func(top, mid, bot []Card) map[string]int {
		m := map[string]int{}
		for _, c := range top {
			m[norm(c)] = 0
		}
		for _, c := range mid {
			m[norm(c)] = 1
		}
		for _, c := range bot {
			m[norm(c)] = 2
		}
		return m
	}
	matchRows := func(gs *GameState, want map[string]int) bool {
		for i, row := range [][]Card{gs.Top, gs.Middle, gs.Bottom} {
			for _, c := range row {
				if w, ok := want[norm(c)]; !ok || w != i {
					return false
				}
			}
		}
		return true
	}
	// 案发板: 鬼上顶要求 want 里 "X" 只能一行 — 但案里两鬼分处顶/底.
	// 处理: matchRows 对鬼只查"是否在鬼允许的行集合" (顶或底).
	jokerRows := map[int]bool{0: true, 2: true}
	matchRowsJ := func(gs *GameState, want map[string]int) bool {
		jTop, jBot := 0, 0
		for i, row := range [][]Card{gs.Top, gs.Middle, gs.Bottom} {
			for _, c := range row {
				if c.IsJoker() {
					if !jokerRows[i] {
						return false
					}
					if i == 0 {
						jTop++
					} else {
						jBot++
					}
					continue
				}
				if w, ok := want[c.ID()]; !ok || w != i {
					return false
				}
			}
		}
		return jTop == 1 && jBot == 1 // 案发: 顶1鬼 底1鬼
	}
	_ = matchRows

	top := parse([]string{"Xj0"})
	mid := parse([]string{"2d", "7s", "7d"})
	bot := parse([]string{"Kc", "Kd", "Xj1"})
	all := append(append(append([]Card{}, top...), mid...), bot...)
	want := rowOf(top, mid, bot)
	junks := parse([]string{"3c", "4h", "5s"})

	reach, tried := 0, 0
	n := len(all)
	var idx [5]int
	var rec func(start, k int)
	rec = func(start, k int) {
		if k == 5 {
			inR1 := map[int]bool{}
			for _, i := range idx {
				inR1[i] = true
			}
			var r1, r2 []Card
			for i := 0; i < n; i++ {
				if inR1[i] {
					r1 = append(r1, all[i])
				} else {
					r2 = append(r2, all[i])
				}
			}
			for _, junk := range junks {
				tried++
				gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
				er := &ExpertRollout{Rng: rand.New(rand.NewSource(1)), Cfg: DefaultRolloutConfig}
				er.Cfg.PureMLP = true
				er.ExpertPlace5(gs, r1)
				if !matchRowsJ(gs, want) {
					continue
				}
				gs.Round = 2
				er.ExpertPlace3(gs, []Card{r2[0], r2[1], junk})
				if matchRowsJ(gs, want) && len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 7 {
					reach++
				}
			}
			return
		}
		for i := start; i < n; i++ {
			idx[k] = i
			rec(i+1, k+1)
		}
	}
	rec(0, 0)
	t.Logf("案22(R3): %d/%d 条重建路径可达 (头[🃏] 中[2d7s7d] 底[KcKd🃏])", reach, tried)
}
