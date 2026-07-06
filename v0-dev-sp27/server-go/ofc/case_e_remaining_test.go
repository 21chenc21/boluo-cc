package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// E 剩余 5 案账本听证 (1000-sim): 账本站 exp → 可配保险丝; 打平/反对 → 只能数据.
func TestERemainLedger(t *testing.T) {
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
		rnd  int
	}{
		{"23-AI: Kh头 3h中 弃Ks", buildGS(t, 2, []string{"Ac", "As", "Kh"}, []string{"3h"}, []string{"Qh", "Qc", "6h"}, "Ks"), 2},
		{"23-exp: KsKh底 弃3h", buildGS(t, 2, []string{"Ac", "As"}, []string{}, []string{"Qh", "Qc", "6h", "Ks", "Kh"}, "3h"), 2},
		{"67-AI: 8c中 Jc底 弃5s", buildGS(t, 2, []string{"Kh"}, []string{"3s", "2c", "8c"}, []string{"7c", "Qd", "Jc"}, "5s"), 2},
		{"67-exp1: 5s中 Jc底 弃8c", buildGS(t, 2, []string{"Kh"}, []string{"3s", "2c", "5s"}, []string{"7c", "Qd", "Jc"}, "8c"), 2},
		{"104-AI: 6h中 4c底 弃7s", buildGS(t, 2, []string{}, []string{"3s", "5s", "6h"}, []string{"9h", "Qd", "Qh", "4c"}, "7s"), 2},
		{"104-exp2: 6h头 7s中 弃4c", buildGS(t, 2, []string{"6h"}, []string{"3s", "5s", "7s"}, []string{"9h", "Qd", "Qh"}, "4c"), 2},
		{"110-AI: 9c头 6c中 弃7h", buildGS(t, 4, []string{"Xj0", "9c"}, []string{"6h", "6d", "4c", "6c"}, []string{"9d", "8h", "8c", "8d", "8s"}, "7h"), 4},
		{"110-exp1: 6c7h中 弃9c", buildGS(t, 4, []string{"Xj0"}, []string{"6h", "6d", "4c", "6c", "7h"}, []string{"9d", "8h", "8c", "8d", "8s"}, "9c"), 4},
		{"75-AI: 2c底", buildGS(t, 1, []string{}, []string{"5h", "4h"}, []string{"Td", "Ts", "2c"}, ""), 1},
		{"75-exp: 2c中", buildGS(t, 1, []string{}, []string{"2c", "5h", "4h"}, []string{"Td", "Ts"}, ""), 1},
	}
	const N = 1000
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(7)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-26s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
