package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 130 三线 1500-sim (2026-07-10 TT44保守线判决用)
func TestCase130Lines(t *testing.T) {
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
	used := []string{"Th", "2d", "Td", "Qc", "Ks"}
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
		{"exp:  中[4s4h] 底[TcTs7s]", build(nil, parse("4s", "4h"), parse("Tc", "Ts", "7s"))},
		{"AI现: 中[7s] 底[4sTcTs4h]", build(nil, parse("7s"), parse("4s", "Tc", "Ts", "4h"))},
		{"原病: 中[TcTs7s] 底[4s4h]", build(nil, parse("Tc", "Ts", "7s"), parse("4s", "4h"))},
	}
	const N = 1500
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(130)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-26s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
