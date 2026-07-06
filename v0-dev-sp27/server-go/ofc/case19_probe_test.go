package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// #19 EV 拆账: 锁333(教师) vs 留双鬼+Ts底(fuse线). 用户疑 EV 算法有 bug.
func TestCase19Dissect(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg := DefaultRolloutConfig
	t.Logf("rollout knobs: FoulCost=%v FanBonusQQ=%v KK=%v AA=%v Trips=%v",
		cfg.FoulCost, cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
	lines := []struct {
		name string
		gs   *GameState
	}{
		{"19A 教师: 锁333 (XX3c顶) 弃Ts?", buildGS(t, 4, []string{"Xj0", "Xj1", "3c"}, []string{"8c", "8d", "7h", "8h"}, []string{"Td", "Jd", "Tc", "Th"}, "Ts")},
		{"19B fuse: 留双鬼 Ts底(quads) 弃3c", buildGS(t, 4, []string{"Xj0", "Xj1"}, []string{"8c", "8d", "7h", "8h"}, []string{"Td", "Jd", "Tc", "Th", "Ts"}, "3c")},
	}
	const N = 1000
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(19)), Cfg: DefaultRolloutConfig}
		var sum, roySum float64
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
				roySum += float64(r.RawRoyalty)
				fan++
			default:
				sum += float64(r.RawRoyalty)
				roySum += float64(r.RawRoyalty)
			}
		}
		t.Logf("%-30s mean=%7.2f foul=%4.1f%% fan=%5.1f%% 纯royalty均=%5.2f", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N, roySum/N)
	}
}
