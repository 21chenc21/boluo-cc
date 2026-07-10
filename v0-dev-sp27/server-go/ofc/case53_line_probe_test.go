package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 53 两线 1500-sim, 双政策对照 (2026-07-10 用户质疑129a自评)
func TestCase53Lines(t *testing.T) {
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
	base := func() *GameState {
		gs := &GameState{NumJokers: 2, Round: 3, UsedCards: map[string]bool{}}
		for _, c := range parse("7c", "4c") {
			gs.PlaceCard(c, RowMiddle)
		}
		for _, c := range parse("8h", "Jh", "Kh", "3h", "6h") {
			gs.PlaceCard(c, RowBottom)
		}
		return gs
	}
	lines := []struct {
		name           string
		top, mid       []Card
		discard        string
	}{
		{"AI(129a): 头[TdQs] 弃Jd", parse("Td", "Qs"), nil, "Jd"},
		{"exp:      头[Qs] 中[Td] 弃Jd", parse("Qs"), parse("Td"), "Jd"},
	}
	const N = 1500
	for _, ln := range lines {
		gs := base()
		for _, c := range ln.top {
			gs.PlaceCard(c, RowTop)
		}
		for _, c := range ln.mid {
			gs.PlaceCard(c, RowMiddle)
		}
		if d, ok := ParseCard(ln.discard); ok {
			gs.UsedCards[d.ID()] = true
		}
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(53)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(gs.Clone(), 3)
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
		t.Logf("%-28s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
