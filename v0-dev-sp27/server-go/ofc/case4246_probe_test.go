package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 2026-07-06 #42/#46 解剖: 用户论点 — 42=大概率foul理论不该+, 46=鬼可降级理论foul≈0只亏A范.
// 各线 1000 rollouts 实测 mean/foul%/fan% (沙盘铁律), 对照 f89 的估值.
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestCase4246Dissect -v
func TestCase4246Dissect(t *testing.T) {
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
	build := func(round int, top, mid, bot []string, discard string) *GameState {
		g := &GameState{NumJokers: 2, Round: round}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		if discard != "" {
			c, _ := ParseCard(discard)
			g.UsedCards[c.ID()] = true
			g.SetDiscard(c)
		}
		return g
	}
	type line struct {
		name string
		gs   *GameState
		rnd  int
	}
	// 42 基础: 头[Kc Ks] 中[3d 4s 4c] 底[Ts Tc 9h Th], R4发 Kh Qs X
	// 46 基础: 头[X 2c] 中[4s 8c Th] 底[7h 7d 7c Qc], R4发 2s 8s 6c
	lines := []line{
		{"42A AI:  Kh头KKK 鬼中 弃Qs", build(4,
			[]string{"Kc", "Ks", "Kh"}, []string{"3d", "4s", "4c", "Xj0"}, []string{"Ts", "Tc", "9h", "Th"}, "Qs"), 4},
		{"42B exp: Qs头KKQ 鬼中 弃Kh", build(4,
			[]string{"Kc", "Ks", "Qs"}, []string{"3d", "4s", "4c", "Xj0"}, []string{"Ts", "Tc", "9h", "Th"}, "Kh"), 4},
		{"46A AI:  2s头(鬼22) 8s中 弃6c", build(4,
			[]string{"Xj0", "2c", "2s"}, []string{"4s", "8c", "Th", "8s"}, []string{"7h", "7d", "7c", "Qc"}, "6c"), 4},
		{"46B exp: 8s中 6c底 顶留鬼2c", build(4,
			[]string{"Xj0", "2c"}, []string{"4s", "8c", "Th", "8s"}, []string{"7h", "7d", "7c", "Qc", "6c"}, "2s"), 4},
	}
	const N = 1000
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(46)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(ln.gs.Clone(), ln.rnd)
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
		t.Logf("%-28s mean=%6.2f  foul=%4.1f%%  fan=%4.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
	// f89 对照: 各线 BuildFeaturesV3 的 foul 概率维
	for _, ln := range lines {
		ft := BuildFeaturesV3(ln.gs)
		t.Logf("%-28s f89(foul特征)=%.3f", ln.name, ft[89])
	}
}
