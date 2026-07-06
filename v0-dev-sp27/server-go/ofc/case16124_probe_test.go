package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 2026-07-06 #16/#124 解剖: 用户指出存在 100% 进范摆法, 验 AI 线 vs 保底线.
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestCase16124Dissect -v
func TestCase16124Dissect(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	type line struct {
		name string
		gs   *GameState
		rnd  int
	}
	// 16 基础: 头[🃏 Qc] 中[9s 2h] 底[Th Tc Ts], R3发 Ah As Ac
	// 124 基础: 头[🃏 Ah] 中[2s 🃏 2h] 底[8s Jh 9h 9s], R4发 Js Ts 8h
	lines := []line{
		{"16A AI: Ac头 As中 弃Ah", buildGS(t, 3,
			[]string{"Xj0", "Qc", "Ac"}, []string{"9s", "2h", "As"}, []string{"Th", "Tc", "Ts"}, "Ah"), 3},
		{"16B exp1: AhAc中 弃As (顶留🃏Q)", buildGS(t, 3,
			[]string{"Xj0", "Qc"}, []string{"9s", "2h", "Ah", "Ac"}, []string{"Th", "Tc", "Ts"}, "As"), 3},
		{"124A AI: Js头 8h底 弃Ts", buildGS(t, 4,
			[]string{"Xj0", "Ah", "Js"}, []string{"2s", "Xj1", "2h"}, []string{"8s", "Jh", "9h", "9s", "8h"}, "Ts"), 4},
		{"124B exp1: Ts中 Js底 弃8h", buildGS(t, 4,
			[]string{"Xj0", "Ah"}, []string{"2s", "Xj1", "2h", "Ts"}, []string{"8s", "Jh", "9h", "9s", "Js"}, "8h"), 4},
	}
	const N = 1000
	for _, ln := range lines {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(16)), Cfg: DefaultRolloutConfig}
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
		t.Logf("%-32s mean=%6.2f  foul=%4.1f%%  fan=%5.1f%%", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
	}
}
