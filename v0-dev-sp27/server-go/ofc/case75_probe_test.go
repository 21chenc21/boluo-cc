package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 2026-07-05 #75 人机分歧解剖: TT中+245底(老师) vs TT底(用户/exp) vs 2c头(折中), 各1000 rollouts 拆帐.
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestCase75Dissect -v
func TestCase75Dissect(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(top, mid, bot []string) *GameState {
		g := &GameState{NumJokers: 2, Round: 1}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		return g
	}
	lines := map[string]*GameState{
		"75A 老师: TT中+245底":  build(nil, []string{"Td", "Ts"}, []string{"2c", "5h", "4h"}),
		"75B exp:  245中+TT底":  build(nil, []string{"2c", "5h", "4h"}, []string{"Td", "Ts"}),
		"75C: 2c头 TT中 45底":   build([]string{"2c"}, []string{"Td", "Ts"}, []string{"5h", "4h"}),
		"75D: 2c头 45中 TT底":   build([]string{"2c"}, []string{"5h", "4h"}, []string{"Td", "Ts"}),
		"s1A 老师: 鬼3h中 KK7底": build(nil, []string{"Xj0", "3h"}, []string{"Kc", "Kd", "7s"}),
		"s1B exp:  鬼顶 3h7s中 KK底": build([]string{"Xj0"}, []string{"3h", "7s"}, []string{"Kc", "Kd"}),
		"s1C: 鬼顶 7s中 KK3h底": build([]string{"Xj0"}, []string{"7s"}, []string{"Kc", "Kd", "3h"}),
	}
	const N = 1000
	for name, gs := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(75)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(gs.Clone(), 1)
			r := er.LastResult
			switch {
			case r.IsFoul:
				sum += -float64(er.Cfg.FoulCost)
				foul++
			case r.IsFantasy:
				sum += float64(r.RawRoyalty + r.FanBonus)
				fan++
			default:
				sum += float64(r.RawRoyalty)
			}
		}
		t.Logf("%-22s mean=%6.2f  foul=%4.1f%%  fan=%4.1f%%", name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
