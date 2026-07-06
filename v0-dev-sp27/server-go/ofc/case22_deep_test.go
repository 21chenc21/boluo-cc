package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// #22 深查 (用户裁: 底浪费鬼 = 真错): 底 KcKd🃏 已有 KKK, AI 双K全下底 → 鬼沦为 kicker 零贡献;
// exp 只需 1 张 K 就凑满四条(鬼当第4张K), 另一张 K 是白赚的材料.
func TestCase22Deep(t *testing.T) {
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
		{"22-AI: KsKh全下底(鬼废)", buildGS(t, 3, []string{"Xj0"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Ks", "Kh"}, "6d")},
		{"22-exp1: Ks头 Kh底(鬼=第4K)", buildGS(t, 3, []string{"Xj0", "Ks"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Kh"}, "6d")},
		{"22-exp3: KsKh中(KK77两对)", buildGS(t, 3, []string{"Xj0"}, []string{"2d", "7s", "7d", "Ks", "Kh"}, []string{"Kc", "Kd", "Xj1"}, "6d")},
		{"22-exp5: 6d头 Ks底 (弃Kh)", buildGS(t, 3, []string{"Xj0", "6d"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Ks"}, "Kh")},
	}
	const N = 1000
	for _, ln := range lines {
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
		te := TrainedEval(ln.gs)
		t.Logf("%-28s mean=%6.2f foul=%4.1f%% fan=%5.1f%% | NN值=%.2f", ln.name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N, te)
	}
}

// 22 featdiff: AI(鬼废线) vs exp1 — 找 NN 倒挂 +9.5 的特征根
func TestCase22FeatDiff(t *testing.T) {
	a := buildGS(t, 3, []string{"Xj0"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Ks", "Kh"}, "6d")
	b := buildGS(t, 3, []string{"Xj0", "Ks"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Kh"}, "6d")
	fa, fb := BuildFeaturesV3(a), BuildFeaturesV3(b)
	for i := range fa {
		d := fb[i] - fa[i]
		if d > 0.15 || d < -0.15 {
			t.Logf("f[%3d] AI=%.3f exp1=%.3f Δ=%+.3f", i, fa[i], fb[i], d)
		}
	}
}
