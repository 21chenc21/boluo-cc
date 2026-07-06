package ofc

import (
	"math/rand"
	"os"
	"strings"
	"testing"
)

// 保险丝#4 行为: #16 — NN(iter-2)选 Ac头As中(24%foul/40%范), 场上有保底线(顶留🃏Q, 100%范).
// fuse 该触发搜索并翻到保底族摆法.
// 跑法: OFC_FUSE4_CKPT=<在16上选错的ckpt> go test ./ofc -run TestFanFuseCase16 -v
func TestFanFuseCase16(t *testing.T) {
	ckpt := os.Getenv("OFC_FUSE4_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_FUSE4_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	oldMargin := ServeSearchMargin
	ServeSearchMargin = 2.5
	defer func() { ServeSearchMargin = oldMargin }()

	gs := buildGS(t, 3,
		[]string{"Xj0", "Qc"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"}, "")
	dealt := mkCards(t, "Ah", "As", "Ac")
	var audit []string
	er := &ExpertRollout{Rng: rand.New(rand.NewSource(16)), Cfg: DefaultRolloutConfig}
	er.SearchAudit = &audit
	er.ExpertPlace3(gs, dealt)
	t.Logf("摆法: 头%v 中%v 底%v", gs.Top, gs.Middle, gs.Bottom)
	t.Logf("audit: %v", audit)

	// 坏摆 = A 上顶 + 单A 塞中 (AI线): 顶含A 且 中恰好多1张A
	topA, midA := 0, 0
	for _, c := range gs.Top {
		if !c.IsJoker() && c.Rank() == RankA {
			topA++
		}
	}
	for _, c := range gs.Middle {
		if c.Rank() == RankA {
			midA++
		}
	}
	if topA >= 1 && midA == 1 {
		t.Errorf("保险丝#4 没拦住 AI线 (A顶+单A中, 24%%foul): 需 fanfloor 审计=%v", audit)
	}
	fuseSeen := false
	for _, l := range audit {
		if strings.Contains(l, "fanfloor") {
			fuseSeen = true
		}
	}
	t.Logf("fanfloor 触发: %v", fuseSeen)
}

// fanFloorCandidate + serveFanFuseStates 确定性单测 (#16 双线)
func TestFanFloorCandidate(t *testing.T) {
	// 16B 保底线: 顶留🃏Q(未满), AA中 — 该是 fanFloor
	b := buildGS(t, 3, []string{"Xj0", "Qc"}, []string{"9s", "2h", "Ah", "Ac"}, []string{"Th", "Tc", "Ts"}, "As")
	// 16A AI线: 顶🃏QcAc 满 + As 塞中 — 顶也有鬼+大牌, 但 f89 是否放行看 foul 链
	a := buildGS(t, 3, []string{"Xj0", "Qc", "Ac"}, []string{"9s", "2h", "As"}, []string{"Th", "Tc", "Ts"}, "Ah")
	// 无鬼顶: 不是 fanFloor
	c := buildGS(t, 3, []string{"Kc", "Qc"}, []string{"9s", "2h", "Ah", "Ac"}, []string{"Th", "Tc", "Ts"}, "As")
	if !fanFloorCandidate(b) {
		t.Errorf("16B 保底线应命中 fanFloor (f89=%.3f)", BuildFeaturesV3(b)[89])
	}
	// v2 (2026-07-06): 16A 鬼一牌两用不可兼得 (承诺AA后中道无A必两对) → 必须被拒
	if fanFloorCandidate(a) {
		t.Errorf("16A 假保底(承诺范后24%%foul)不该命中 fanFloor")
	}
	if fanFloorCandidate(c) {
		t.Errorf("无鬼顶不该命中 fanFloor")
	}
	// 选集: top-1=a(AI线), 候选含 b → 对比集应包含 b
	out := serveFanFuseStates([]*GameState{a, c, b}, 3)
	found := false
	for _, st := range out[1:] {
		if st == b {
			found = true
		}
	}
	if !found {
		t.Errorf("对比集应含保底线 b, 得 %d 条", len(out))
	}
}

// v3 条件承诺 (#22): 22B(顶🃏Ks留位+中77对+f89=0)=免费卷该命中; 22A(顶满KKK)该拒
func TestFanFloorV3Case22(t *testing.T) {
	b := buildGS(t, 3, []string{"Xj0", "Ks"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1", "Kh"}, "6d")
	a := buildGS(t, 3, []string{"Xj0", "Ks", "Kh"}, []string{"2d", "7s", "7d"}, []string{"Kc", "Kd", "Xj1"}, "6d")
	if !fanFloorCandidate(b) {
		t.Errorf("22B 免费卷该命中 (f89=%.3f)", BuildFeaturesV3(b)[89])
	}
	if fanFloorCandidate(a) {
		t.Errorf("22A (真foul 27%%) 不该命中")
	}
}
