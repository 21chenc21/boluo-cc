package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// #82 EV拆账 (用户理论: 头放T, 底成型后TJ成对分高): 头[Jd] 中[666+7] 底[5cQc4c8c], 发[2d 5d Ts]
func TestCase82Dissect(t *testing.T) {
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
		{"82-AI: 头2d5d(双填) 弃Ts", buildGS(t, 4, []string{"Jd", "2d", "5d"}, []string{"6c", "6h", "7d", "6s"}, []string{"5c", "Qc", "4c", "8c"}, "Ts")},
		{"82-exp1: 头2d 中5d 弃Ts", buildGS(t, 4, []string{"Jd", "2d"}, []string{"6c", "6h", "7d", "6s", "5d"}, []string{"5c", "Qc", "4c", "8c"}, "Ts")},
		{"82-exp4: 头Ts 中5d 弃2d", buildGS(t, 4, []string{"Jd", "Ts"}, []string{"6c", "6h", "7d", "6s", "5d"}, []string{"5c", "Qc", "4c", "8c"}, "2d")},
		{"82-exp2: 头Ts 中2d 弃5d", buildGS(t, 4, []string{"Jd", "Ts"}, []string{"6c", "6h", "7d", "6s", "2d"}, []string{"5c", "Qc", "4c", "8c"}, "5d")},
	}
	const N = 1000
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(82)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-26s mean=%6.2f foul=%4.1f%% fan=%4.1f%% | NN值=%.2f", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N, TrainedEval(ln.gs))
	}
}
