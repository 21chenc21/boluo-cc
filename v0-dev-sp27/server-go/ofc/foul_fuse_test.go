package ofc

import (
	"math/rand"
	"os"
	"strings"
	"testing"
)

// 2026-07-06 保险丝#3: NN top-1 高foul(f89>0.7) → 无视margin强制搜索, 对比集=top-1+安全替代(f89<0.3).
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestFoulFuseCase42 -v
func TestFoulFuseCase42(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	oldMargin := ServeSearchMargin
	ServeSearchMargin = 2.5
	defer func() { ServeSearchMargin = oldMargin }()

	run := func() (*GameState, []string) {
		gs := buildGS(t, 4,
			[]string{"Kc", "Ks"},
			[]string{"3d", "4s", "4c"},
			[]string{"Ts", "Tc", "9h", "Th"}, "")
		dealt := mkCards(t, "Kh", "Qs", "Xj0")
		var audit []string
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(42)), Cfg: DefaultRolloutConfig}
		er.SearchAudit = &audit
		er.ExpertPlace3(gs, dealt)
		return gs, audit
	}
	gs, audit := run()

	// 顶不能是 KKK (真foul 91.6%, exp=Qs头KKQ 或 鬼中留头)
	kTop := 0
	for _, c := range gs.Top {
		if !c.IsJoker() && c.Rank() == RankK {
			kTop++
		}
	}
	t.Logf("摆法: 头%v 中%v 底%v", gs.Top, gs.Middle, gs.Bottom)
	t.Logf("audit: %v", audit)
	if kTop >= 3 {
		t.Errorf("保险丝#3 没拦住 KKK 顶 (真foul 91.6%%)")
	}
	// audit 里应有 foulfuse 触发痕迹 (NN top-1 是 KKK 线时)
	fuseSeen := false
	for _, l := range audit {
		if strings.Contains(l, "foulfuse") {
			fuseSeen = true
		}
	}
	if !fuseSeen && kTop >= 3 {
		t.Errorf("KKK 被选且无 foulfuse 审计 — 保险丝没触发")
	}
}

// serveFoulFuseStates 选集: top-1 + 最高排名安全线(f89<0.3); 无安全线回退 top-K
func TestServeFoulFuseStates(t *testing.T) {
	// s0 = 42A (f89=0.939 高foul), s1 = 42A同型 (高foul), s2 = 42B (f89=0 安全)
	s0 := buildGS(t, 4, []string{"Kc", "Ks", "Kh"}, []string{"3d", "4s", "4c", "Xj0"}, []string{"Ts", "Tc", "9h", "Th"}, "Qs")
	s1 := buildGS(t, 4, []string{"Kc", "Ks", "Kh"}, []string{"3d", "4s", "4c", "Qs"}, []string{"Ts", "Tc", "9h", "Th"}, "Xj0")
	s2 := buildGS(t, 4, []string{"Kc", "Ks", "Qs"}, []string{"3d", "4s", "4c", "Xj0"}, []string{"Ts", "Tc", "9h", "Th"}, "Kh")
	if f := BuildFeaturesV3(s0)[89]; f < 0.7 {
		t.Fatalf("前置: s0 f89=%.3f 应>0.7", f)
	}
	if f := BuildFeaturesV3(s2)[89]; f > 0.3 {
		t.Fatalf("前置: s2 f89=%.3f 应<0.3", f)
	}
	out := serveFoulFuseStates([]*GameState{s0, s1, s2}, 3)
	if len(out) < 2 || out[0] != s0 || out[1] != s2 {
		t.Errorf("选集应 [s0, s2(安全线)], 得 %d 条, out[1]==s2? %v", len(out), len(out) > 1 && out[1] == s2)
	}
	// 全危局 → 回退 top-K
	out2 := serveFoulFuseStates([]*GameState{s0, s1}, 3)
	if len(out2) != 2 {
		t.Errorf("无安全线应回退 top-K, 得 %d 条", len(out2))
	}
}
