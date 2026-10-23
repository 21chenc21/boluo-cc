package ofc

import (
	"os"
	"testing"
)

// 2026-07-04 #23 诊断 harness (临时): 量化 f129(弃牌rank)/f146(拆connector) 对 AI/exp 两摆法 net 分的贡献.
// 跑法: OFC_PROBE_CKPT=../../v3-train-i169-sp39-gcp/iter-1/round-001-acc93.json go test ./ofc -run TestCase23FeatureProbe -v
func TestCase23FeatureProbe(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT=ckpt路径")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load ckpt: %v", err)
	}
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(top, mid, bot []string, discard string) *GameState {
		g := &GameState{NumJokers: 2, Round: 2}
		g.Top, g.Middle, g.Bottom = mk(top...), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		if discard != "" {
			d, _ := ParseCard(discard)
			g.LastDiscard, g.HasLastDiscard = d, true
			g.UsedCards[d.ID()] = true
		}
		return g
	}
	// #23 post-states: AI top-1 = Kh→底 3h→中 弃Ks; exp = KsKh→底 弃3h
	ai := build([]string{"Ac", "As"}, []string{"3h"}, []string{"Qh", "Qc", "6h", "Kh"}, "Ks")
	exp := build([]string{"Ac", "As"}, []string{}, []string{"Qh", "Qc", "6h", "Ks", "Kh"}, "3h")

	evalMasked := func(gs *GameState, zeroDims ...int) float32 {
		net := defaultTrainedNet
		b := newEvalBuffers(net)
		f := BuildFeatures(gs, net.InDim)
		copy(b.f, f)
		for _, d := range zeroDims {
			if d < len(b.f) {
				b.f[d] = 0
			}
		}
		return mlpForward(net, b)
	}

	sAI, sEXP := evalMasked(ai), evalMasked(exp)
	t.Logf("baseline:        AI=%.3f  exp=%.3f  gap(AI-exp)=%+.3f", sAI, sEXP, sAI-sEXP)
	for _, probe := range []struct {
		name string
		dims []int
	}{
		{"zero f129(弃rank)", []int{129}},
		{"zero f146(拆conn)", []int{146}},
		{"zero f129+f146", []int{129, 146}},
		{"zero f145+f146(N2全)", []int{145, 146}},
		{"zero f129+145+146(弃牌全)", []int{129, 145, 146}},
	} {
		a, e := evalMasked(ai, probe.dims...), evalMasked(exp, probe.dims...)
		t.Logf("%-24s AI=%.3f  exp=%.3f  gap=%+.3f  (Δgap %+.3f)", probe.name, a, e, a-e, (a-e)-(sAI-sEXP))
	}
}
