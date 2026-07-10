package ofc

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
)

// F10 杂牌R1 扫描探针 (2026-07-09 用户"构建一些看看 主要看有没有小牌放底"):
// 生成结构约束的杂牌手 (无对/无3+连张/同花≤2/含J+大张/含≤4低张/无鬼),
// 喂裸NN摆R1, 统计"低张(≤4)进底道"率. 跑法:
// OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestJunkR1Scan -v
func TestJunkR1Scan(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	MctsDisabled = true
	defer func() { MctsDisabled = false }()

	rng := rand.New(rand.NewSource(710))
	deck := MakeDeck(0) // 无鬼 52 张

	isJunk := func(cs []Card) bool {
		var rc [13]int
		var sc [4]int
		hasBig, hasLow := false, false
		for _, c := range cs {
			rc[c.Rank()]++
			sc[c.Suit()]++
			if c.Rank() >= 9 { // J+
				hasBig = true
			}
			if c.Rank() <= 2 { // 2/3/4
				hasLow = true
			}
		}
		for _, n := range rc {
			if n >= 2 {
				return false // 有对
			}
		}
		for _, n := range sc {
			if n >= 3 {
				return false // 3+ 同花
			}
		}
		run := 0
		for r := 0; r < 13; r++ { // 3+ 连张
			if rc[r] > 0 {
				run++
				if run >= 3 {
					return false
				}
			} else {
				run = 0
			}
		}
		return hasBig && hasLow
	}

	const N = 300
	scanned, lowBottom := 0, 0
	var examples []string
	for scanned < N {
		rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
		hand := append([]Card(nil), deck[:5]...)
		if !isJunk(hand) {
			continue
		}
		scanned++
		gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(int64(scanned))), Cfg: DefaultRolloutConfig}
		er.Cfg.PureMLP = true
		er.ExpertPlace5(gs, hand)
		bad := false
		for _, c := range gs.Bottom {
			if c.Rank() <= 2 {
				bad = true
			}
		}
		if bad {
			lowBottom++
			if len(examples) < 12 {
				examples = append(examples, fmt.Sprintf("发%v → 顶%v 中%v 底%v", hand, gs.Top, gs.Middle, gs.Bottom))
			}
		}
	}
	t.Logf("杂牌R1 扫描: %d 手, 低张(≤4)进底 %d 手 (%.1f%%)", scanned, lowBottom, 100*float64(lowBottom)/float64(scanned))
	for _, e := range examples {
		t.Logf("  ✗ %s", e)
	}
}
