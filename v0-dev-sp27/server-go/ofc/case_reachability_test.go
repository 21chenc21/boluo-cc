package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 可达性测试 (用户设计): 把案发板反推成 R1/R2 发牌序列喂给 E,
// 看 E 自己的摆法会不会走进案发现场. 摆不进 = 该案在 E 治下不存在.
// 跑法: OFC_PROBE_CKPT=<E> go test ./ofc -run TestCaseReachability -v
func TestCaseReachability(t *testing.T) {
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
	rowOf := func(top, mid, bot []Card) map[string]int {
		m := map[string]int{}
		for _, c := range top {
			m[c.ID()] = 0
		}
		for _, c := range mid {
			m[c.ID()] = 1
		}
		for _, c := range bot {
			m[c.ID()] = 2
		}
		return m
	}
	// E 摆 R1 后, 每张牌是否落在案发行
	matchRows := func(gs *GameState, want map[string]int) bool {
		for i, row := range [][]Card{gs.Top, gs.Middle, gs.Bottom} {
			for _, c := range row {
				if w, ok := want[c.ID()]; !ok || w != i {
					return false
				}
			}
		}
		return true
	}
	newER := func() *ExpertRollout {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(1)), Cfg: DefaultRolloutConfig}
		er.Cfg.PureMLP = true
		return er
	}

	// ===== R2 案 (板 = R1 手, 直接测) =====
	r2cases := []struct {
		name          string
		top, mid, bot []string
	}{
		{"23", []string{"Ac", "As"}, []string{}, []string{"Qh", "Qc", "6h"}},
		{"67", []string{"Kh"}, []string{"3s", "2c"}, []string{"7c", "Qd"}},
		{"104", []string{}, []string{"3s", "5s"}, []string{"9h", "Qd", "Qh"}},
	}
	for _, c := range r2cases {
		want := rowOf(parse(c.top), parse(c.mid), parse(c.bot))
		hand := append(append(parse(c.top), parse(c.mid)...), parse(c.bot)...)
		gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
		newER().ExpertPlace5(gs, hand)
		hit := matchRows(gs, want)
		t.Logf("案%s(R2): E的R1摆法=%v | 案发板可达=%v", c.name, gs, hit)
	}

	// ===== 16 (R3, 7 placed): 枚举 C(7,5) R1 拆分, R2=剩2+垫牌 =====
	reach16 := 0
	tried16 := 0
	{
		top, mid, bot := parse([]string{"Xj0", "Qc"}), parse([]string{"9s", "2h"}), parse([]string{"Th", "Tc", "Ts"})
		all := append(append(append([]Card{}, top...), mid...), bot...)
		want := rowOf(top, mid, bot)
		junks := parse([]string{"2d", "3c", "4d"})
		n := len(all)
		var idx [5]int
		var rec func(start, k int)
		rec = func(start, k int) {
			if k == 5 {
				inR1 := map[int]bool{}
				for _, i := range idx {
					inR1[i] = true
				}
				var r1 []Card
				var r2 []Card
				for i := 0; i < n; i++ {
					if inR1[i] {
						r1 = append(r1, all[i])
					} else {
						r2 = append(r2, all[i])
					}
				}
				for _, junk := range junks {
					tried16++
					gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
					er := newER()
					er.ExpertPlace5(gs, r1)
					if !matchRows(gs, want) {
						continue
					}
					gs.Round = 2
					er.ExpertPlace3(gs, []Card{r2[0], r2[1], junk})
					if matchRows(gs, want) && len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 7 {
						reach16++
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
		t.Logf("案16(R3): %d/%d 条重建路径可达 (E 会不会自己摆进 头[鬼Qc] 中[9s2h] 底[TTT])", reach16, tried16)
	}

	// ===== 105 (R4, 9 placed): 头[AsAc] 中[5h5c4h] 底[🃏QdKhJs] =====
	reach105 := 0
	tried105 := 0
	{
		top, mid, bot := parse([]string{"As", "Ac"}), parse([]string{"5h", "5c", "4h"}), parse([]string{"Xj0", "Qd", "Kh", "Js"})
		all := append(append(append([]Card{}, top...), mid...), bot...)
		want := rowOf(top, mid, bot)
		junk1 := parse([]string{"2d"})[0]
		junk2 := parse([]string{"3c"})[0]
		n := len(all)
		var i5 [5]int
		var rec5 func(start, k int)
		rec5 = func(start, k int) {
			if k == 5 {
				in1 := map[int]bool{}
				for _, i := range i5 {
					in1[i] = true
				}
				var r1 []Card
				var rest []Card
				for i := 0; i < n; i++ {
					if in1[i] {
						r1 = append(r1, all[i])
					} else {
						rest = append(rest, all[i])
					}
				}
				for a := 0; a < 4; a++ {
					for b := a + 1; b < 4; b++ {
						tried105++
						gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
						er := newER()
						er.ExpertPlace5(gs, r1)
						if !matchRows(gs, want) {
							continue
						}
						gs.Round = 2
						er.ExpertPlace3(gs, []Card{rest[a], rest[b], junk1})
						if !matchRows(gs, want) {
							continue
						}
						var r3 []Card
						for i := 0; i < 4; i++ {
							if i != a && i != b {
								r3 = append(r3, rest[i])
							}
						}
						gs.Round = 3
						er.ExpertPlace3(gs, []Card{r3[0], r3[1], junk2})
						if matchRows(gs, want) && len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 9 {
							reach105++
						}
					}
				}
				return
			}
			for i := start; i < n; i++ {
				i5[k] = i
				rec5(i+1, k+1)
			}
		}
		rec5(0, 0)
		t.Logf("案105(R4): %d/%d 条重建路径可达", reach105, tried105)
	}

	// ===== 22 (R3, 7 placed): 头[🃏] 中[2d7s7d] 底[KcKd🃏] =====
	reach22 := 0
	tried22 := 0
	{
		top, mid, bot := parse([]string{"Xj0"}), parse([]string{"2d", "7s", "7d"}), parse([]string{"Kc", "Kd", "Xj1"})
		all := append(append(append([]Card{}, top...), mid...), bot...)
		want := rowOf(top, mid, bot)
		junks := parse([]string{"3c", "4d", "5s"})
		n := len(all)
		var idx [5]int
		var rec func(start, k int)
		rec = func(start, k int) {
			if k == 5 {
				inR1 := map[int]bool{}
				for _, i := range idx {
					inR1[i] = true
				}
				var r1 []Card
				var r2 []Card
				for i := 0; i < n; i++ {
					if inR1[i] {
						r1 = append(r1, all[i])
					} else {
						r2 = append(r2, all[i])
					}
				}
				for _, junk := range junks {
					tried22++
					gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
					er := newER()
					er.ExpertPlace5(gs, r1)
					if !matchRows(gs, want) {
						continue
					}
					gs.Round = 2
					er.ExpertPlace3(gs, []Card{r2[0], r2[1], junk})
					if matchRows(gs, want) && len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 7 {
						reach22++
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
		t.Logf("案22(R3): %d/%d 条重建路径可达", reach22, tried22)
	}

	// ===== 110 (R4, 9 placed): 枚举 R1 C(9,5) × R2 C(4,2), 垫牌×2 =====
	reach110 := 0
	tried110 := 0
	{
		top, mid, bot := parse([]string{"Xj0"}), parse([]string{"6h", "6d", "4c"}), parse([]string{"9d", "8h", "8c", "8d", "8s"})
		all := append(append(append([]Card{}, top...), mid...), bot...)
		want := rowOf(top, mid, bot)
		junk1 := parse([]string{"2d"})[0]
		junk2 := parse([]string{"3c"})[0]
		n := len(all)
		var i5 [5]int
		var rec5 func(start, k int)
		rec5 = func(start, k int) {
			if k == 5 {
				in1 := map[int]bool{}
				for _, i := range i5 {
					in1[i] = true
				}
				var r1 []Card
				var rest []Card
				for i := 0; i < n; i++ {
					if in1[i] {
						r1 = append(r1, all[i])
					} else {
						rest = append(rest, all[i])
					}
				}
				// R2 取 rest 中 2 张
				for a := 0; a < 4; a++ {
					for b := a + 1; b < 4; b++ {
						tried110++
						gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
						er := newER()
						er.ExpertPlace5(gs, r1)
						if !matchRows(gs, want) {
							continue
						}
						gs.Round = 2
						er.ExpertPlace3(gs, []Card{rest[a], rest[b], junk1})
						if !matchRows(gs, want) {
							continue
						}
						var r3 []Card
						for i := 0; i < 4; i++ {
							if i != a && i != b {
								r3 = append(r3, rest[i])
							}
						}
						gs.Round = 3
						er.ExpertPlace3(gs, []Card{r3[0], r3[1], junk2})
						if matchRows(gs, want) && len(gs.Top)+len(gs.Middle)+len(gs.Bottom) == 9 {
							reach110++
						}
					}
				}
				return
			}
			for i := start; i < n; i++ {
				i5[k] = i
				rec5(i+1, k+1)
			}
		}
		rec5(0, 0)
		t.Logf("案110(R4): %d/%d 条重建路径可达", reach110, tried110)
	}
}
