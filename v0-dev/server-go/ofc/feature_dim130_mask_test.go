package ofc

import "testing"

// 2026-06-03: dim130 (N_disc "弃了 Q/K/A" 标志) 固化清零 — 净负特征.
// 见 features_v3.go BuildFeaturesV3 末尾注释 + 实战 9 (ypk-178127178-8 R4).

func TestFeatureV3_Dim130_AlwaysZero_DiscardQ(t *testing.T) {
	// 弃了 Qd (高牌) — 修前 dim130=1.0, 固化后必为 0
	gs := NewGameState(2)
	gs.Round = 4
	for _, s := range []string{"8h", "X", "3h", "7h", "2h"} {
		gs.Middle = append(gs.Middle, mustParse(s))
	}
	for _, s := range []string{"7c", "Qc", "3c", "Kc", "6c"} {
		gs.Bottom = append(gs.Bottom, mustParse(s))
	}
	gs.Top = append(gs.Top, mustParse("Jd"))
	gs.SetDiscard(mustParse("Qd"))
	f := BuildFeaturesV3(gs)
	if f[130] != 0 {
		t.Fatalf("dim130 应固化清零 (弃 Qd), got %v", f[130])
	}
}

func TestFeatureV3_Dim130_AlwaysZero_DiscardJoker(t *testing.T) {
	// 弃鬼也会触发 dim130=1 (fillDiscard joker 分支), 固化后必为 0
	gs := NewGameState(2)
	gs.Round = 4
	for _, s := range []string{"8h", "9h", "3h", "7h", "2h"} {
		gs.Middle = append(gs.Middle, mustParse(s))
	}
	for _, s := range []string{"7c", "Qc", "3c", "Kc", "6c"} {
		gs.Bottom = append(gs.Bottom, mustParse(s))
	}
	gs.Top = append(gs.Top, mustParse("Jd"))
	gs.SetDiscard(mustParse("X"))
	f := BuildFeaturesV3(gs)
	if f[130] != 0 {
		t.Fatalf("dim130 应固化清零 (弃鬼), got %v", f[130])
	}
}

func TestFeatureV3_Dim129_Preserved(t *testing.T) {
	// dim129 (弃牌 rank 连续值) 不该被清零 (只固化 dim130)
	gs := NewGameState(2)
	gs.Round = 4
	for _, s := range []string{"8h", "9h", "3h", "7h", "2h"} {
		gs.Middle = append(gs.Middle, mustParse(s))
	}
	for _, s := range []string{"7c", "Qc", "3c", "Kc", "6c"} {
		gs.Bottom = append(gs.Bottom, mustParse(s))
	}
	gs.Top = append(gs.Top, mustParse("Jd"))
	gs.SetDiscard(mustParse("Qd")) // rank Q → dim129 = 10/12 ≈ 0.833
	f := BuildFeaturesV3(gs)
	if f[129] == 0 {
		t.Fatalf("dim129 (弃牌 rank) 不应被清零, got 0")
	}
}
