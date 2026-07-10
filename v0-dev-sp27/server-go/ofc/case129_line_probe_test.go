package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 129 半课线 vs exp线 1500-sim (2026-07-10)
func TestCase129Lines(t *testing.T) {
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
	used := []string{"2d", "4h", "3d", "9d", "Tc"}
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
		{"新(半课): 头[X] 中[4d8h] 底[TsTh]", build(parse("Xj0"), parse("4d", "8h"), parse("Ts", "Th"))},
		{"exp:      头[4dX] 中[8h] 底[TsTh]", build(parse("4d", "Xj0"), parse("8h"), parse("Ts", "Th"))},
	}
	const N = 1500
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(129)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-36s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
