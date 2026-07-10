package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 64 三线 1500-sim (2026-07-10 弃4s vs 弃Kh 真值钉死)
func TestCase64Lines(t *testing.T) {
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
	build := func(topAdd, midAdd []Card, discard string) *GameState {
		gs := &GameState{NumJokers: 2, Round: 4, UsedCards: map[string]bool{}}
		for _, c := range parse("Xj0", "As") {
			gs.PlaceCard(c, RowTop)
		}
		for _, c := range parse("3c", "6h") {
			gs.PlaceCard(c, RowMiddle)
		}
		for _, c := range parse("Qd", "Td", "8d", "Kd", "4d") {
			gs.PlaceCard(c, RowBottom)
		}
		for _, c := range topAdd {
			gs.PlaceCard(c, RowTop)
		}
		for _, c := range midAdd {
			gs.PlaceCard(c, RowMiddle)
		}
		if d, ok := ParseCard(discard); ok {
			gs.UsedCards[d.ID()] = true
		}
		return gs
	}
	lines := []struct {
		name string
		gs   *GameState
	}{
		{"d6新: Qc顶 Kh中 弃4s", build(parse("Qc"), parse("Kh"), "4s")},
		{"d5旧: Qc顶 4s中 弃Kh", build(parse("Qc"), parse("4s"), "Kh")},
		{"exp:  Kh顶 4s中 弃Qc", build(parse("Kh"), parse("4s"), "Qc")},
	}
	const N = 1500
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(64)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(ln.gs.Clone(), 4)
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
		t.Logf("%-24s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
