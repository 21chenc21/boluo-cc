package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// prod-0709 R1 [7c 5d Jh 2d Ks] (对手已见 QQ/TT/3d): 2d放哪 — 底(AI) vs 中 vs 顶.
// 沙盘铁律: R1 裁决 1000+ sims. 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestProd0709R1 -v
func TestProd0709R1(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	parse := func(ss ...string) []Card {
		var r []Card
		for _, x := range ss {
			c, _ := ParseCard(x)
			r = append(r, c)
		}
		return r
	}
	used := []string{"Th", "3d", "Td", "Qd", "Qh"}
	build := func(top, mid, bot []Card) *GameState {
		gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
		for _, c := range top {
			gs.PlaceCard(c, RowTop)
		}
		for _, c := range mid {
			gs.PlaceCard(c, RowMiddle)
		}
		for _, c := range bot {
			gs.PlaceCard(c, RowBottom)
		}
		for _, u := range used {
			gs.UsedCards[u] = true
		}
		return gs
	}
	lines := []struct {
		name string
		gs   *GameState
	}{
		{"AI: 2d底  底[KsJh2d] 中[7c5d]", build(nil, parse("7c", "5d"), parse("Ks", "Jh", "2d"))},
		{"B:  2d中  底[KsJh] 中[7c5d2d]", build(nil, parse("7c", "5d", "2d"), parse("Ks", "Jh"))},
		{"C:  2d顶  底[KsJh] 中[7c5d]", build(parse("2d"), parse("7c", "5d"), parse("Ks", "Jh"))},
	}
	const N = 1500
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(709)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(ln.gs.Clone(), 1)
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
		t.Logf("%-32s mean=%6.2f  foul=%4.1f%%  fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
