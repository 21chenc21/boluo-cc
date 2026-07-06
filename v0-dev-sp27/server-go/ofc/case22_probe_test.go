package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// #22: AI 头[KsKh]配🃏成KKK(顶满,floor=KK锁死) vs exp 头[Ks]留slot(可降级逃生). 量 f89 + 真实foul.
func TestCase22Probe(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	lines := []struct {
		name string
		gs   *GameState
	}{
		{"22A AI: 头🃏KsKh(满) 弃6d", buildGS(t, 3, []string{"Xj0", "Ks", "Kh"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1"}, "6d")},
		{"22B exp1: 头🃏Ks 底Kh 弃6d", buildGS(t, 3, []string{"Xj0", "Ks"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Kh"}, "6d")},
	}
	const N = 1000
	for _, ln := range lines {
		f := BuildFeaturesV3(ln.gs)
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(22)), Cfg: DefaultRolloutConfig}
		var sum float64
		foul, fan := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(ln.gs.Clone(), 3)
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
		t.Logf("%-26s f89=%.3f | 真实: mean=%.2f foul=%.1f%% fan=%.1f%%", ln.name, f[89], sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
