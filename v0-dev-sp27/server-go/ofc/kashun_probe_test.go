package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 卡顺案 (112999374-4) 三线 1500-sim
func TestKashunLines(t *testing.T) {
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
		return gs
	}
	lines := []struct {
		name string
		gs   *GameState
	}{
		{"AI: 底[89JQ卡顺] 中[3s]", build(nil, parse("3s"), parse("9d", "Qc", "8s", "Jc"))},
		{"B:  底[QJ9] 中[8s 3s]", build(nil, parse("8s", "3s"), parse("Qc", "Jc", "9d"))},
		{"C:  底[QJ] 中[9d 8s 3s]", build(nil, parse("9d", "8s", "3s"), parse("Qc", "Jc"))},
	}
	const N = 1500
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(999)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-24s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
