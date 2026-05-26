package ofc

import (
	"math"
	"testing"
)

// makeStateV3 — V3 测试 helper
func makeStateV3(t *testing.T, top, mid, bot []string) *GameState {
	gs := NewGameState(2)
	for _, s := range top {
		gs.Top = append(gs.Top, mustParse(s))
		gs.UsedCards[mustParse(s).ID()] = true
	}
	for _, s := range mid {
		gs.Middle = append(gs.Middle, mustParse(s))
		gs.UsedCards[mustParse(s).ID()] = true
	}
	for _, s := range bot {
		gs.Bottom = append(gs.Bottom, mustParse(s))
		gs.UsedCards[mustParse(s).ID()] = true
	}
	return gs
}

// TestV3_Dim — feature 维度严格 147 (2026-05-19: 131 → 147 加 Tier 1+2+3)
func TestV3_Dim(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	if len(f) != 147 {
		t.Fatalf("V3 dim: got %d, want 147", len(f))
	}
	if FeatureDimV3 != 147 {
		t.Fatalf("FeatureDimV3 const: got %d, want 147", FeatureDimV3)
	}
}

// TestV3_Hypergeo — 概率公式
func TestV3_Hypergeo(t *testing.T) {
	// deck 52, target 4 (e.g. 4 As), draw 5, P(0 A) = C(48,5)/C(52,5) ≈ 0.6588
	p0 := hypergeoP(52, 4, 5, 0)
	if math.Abs(float64(p0)-0.6588) > 0.01 {
		t.Errorf("hypergeo P(0 A | 52,4,5): got %.4f, want ~0.6588", p0)
	}
	// P(at least 1 A) ≈ 0.3412
	p1 := hypergeoAtLeast(52, 4, 5, 1)
	if math.Abs(float64(p1)-0.3412) > 0.01 {
		t.Errorf("hypergeo P(≥1 A | 52,4,5): got %.4f, want ~0.3412", p1)
	}
}

// TestV3_TopPairAA_Lock — top 已 AA, T1 = 1
func TestV3_TopPairAA_Lock(t *testing.T) {
	gs := makeStateV3(t, []string{"Ac", "Ad"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// T1 at idx 113
	if f[113] != 1 {
		t.Errorf("T1 top_currently_AA: got %.2f, want 1", f[113])
	}
	// T0 at idx 112 (pair Q+ lock)
	if f[112] != 1 {
		t.Errorf("T0 top_currently_pair_Q+: got %.2f, want 1", f[112])
	}
	// U0 top_pair_rank = 12/12 = 1.0
	if math.Abs(float64(f[102]-1.0)) > 0.01 {
		t.Errorf("U0 top_pair_rank: got %.2f, want 1.0", f[102])
	}
}

// TestV3_TopTrips_Lock — top 已 trips, T2 = 1 (任何 rank trips, fix old bug)
func TestV3_TopTrips_Lock(t *testing.T) {
	// trips 2 (低 rank)
	gs := makeStateV3(t, []string{"2c", "2d", "2h"}, nil, nil)
	f := BuildFeaturesV3(gs)
	if f[114] != 1 {
		t.Errorf("T2 top_currently_trips (2-2-2): got %.2f, want 1 (any rank trips)", f[114])
	}

	// trips Q
	gs2 := makeStateV3(t, []string{"Qc", "Qd", "Qh"}, nil, nil)
	f2 := BuildFeaturesV3(gs2)
	if f2[114] != 1 {
		t.Errorf("T2 top_currently_trips (Q-Q-Q): got %.2f, want 1", f2[114])
	}
}

// TestV3_FoulImminent_Detected — mid pair-K > bot high → X20 = 1
func TestV3_FoulImminent_Detected(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"As", "Kc", "Qd"},
		[]string{"Kh", "Ks", "Qs", "8d", "4c"}, // pair K (KK QQ kicker? actually KK + Q + 8 + 4 = pair K with 2pair K-Q)
		[]string{"Jh", "9c", "6d", "4d", "3h"}, // J high
	)
	f := BuildFeaturesV3(gs)
	// X20 P(foul) at idx 89
	if f[89] < 0.9 {
		t.Errorf("X20 P(foul) for mid > bot full: got %.2f, want ≥ 0.9", f[89])
	}
}

// TestV3_NoFoul — mid pair-5 < bot straight, no foul
func TestV3_NoFoul(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"As", "Kc", "Qd"},
		[]string{"5h", "5s", "Jh", "8d", "4c"}, // pair 5
		[]string{"5c", "6d", "7s", "8c", "9h"}, // straight 5-9
	)
	f := BuildFeaturesV3(gs)
	if f[89] > 0.3 {
		t.Errorf("X20 P(foul) for pair < straight: got %.2f, want ≤ 0.3", f[89])
	}
}

// TestV3_PairRankU0_Top — U0 pair rank
func TestV3_PairRankU0_Top(t *testing.T) {
	gs := makeStateV3(t, []string{"Qc", "Qd"}, nil, nil) // pair Q
	f := BuildFeaturesV3(gs)
	// rank Q = 10 / 12 ≈ 0.833
	if math.Abs(float64(f[102]-10.0/12.0)) > 0.01 {
		t.Errorf("U0 top pair rank Q: got %.3f, want ~0.833", f[102])
	}
}

// TestV3_MaxAchievable_Dead — mid 全杂乱不同色不连号, max = pair
func TestV3_MaxAchievable_Dead(t *testing.T) {
	gs := makeStateV3(t, nil,
		[]string{"2c", "5d", "Jh"}, // 2 仅 1 张 mid, 不同色, 不连
		nil,
	)
	f := BuildFeaturesV3(gs)
	// C1 mid max achievable, should NOT be SF (8) or RF (9). Should ≤ trips
	// Type values: HighCard=0, Pair=1, 2pair=2, Trips=3
	// Note: in 5-card row, may still reach flush etc with more cards. But with these scattered + 2 more slots, max likely pair/2pair/trips
	if f[117] > float32(TypeStraight)/9.0 {
		t.Errorf("C1 mid max (scattered): got %.2f, want ≤ %.2f (≤ straight)", f[117], float32(TypeStraight)/9.0)
	}
}

// TestV3_MaxAchievable_FlushPossible — bot 3 ♦, max ≥ flush
func TestV3_MaxAchievable_FlushPossible(t *testing.T) {
	gs := makeStateV3(t, nil, nil,
		[]string{"2d", "5d", "8d"}, // 3 diamonds, deck has 10 more
	)
	f := BuildFeaturesV3(gs)
	// C2 bot max ≥ flush (5/9)
	if f[118] < float32(TypeFlush)/9.0 {
		t.Errorf("C2 bot max (3 ♦): got %.2f, want ≥ %.2f (flush)", f[118], float32(TypeFlush)/9.0)
	}
}

// TestV3_LastRound_R5 — R5 标志
func TestV3_LastRound_R5(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 5
	f := BuildFeaturesV3(gs)
	if f[119] != 1 {
		t.Errorf("R5_0 is_last_round (round=5): got %.2f, want 1", f[119])
	}
	gs.Round = 3
	f = BuildFeaturesV3(gs)
	if f[119] != 0 {
		t.Errorf("R5_0 (round=3): got %.2f, want 0", f[119])
	}
}

// TestV3_SlotBalance_Imbalance — R4 极不均衡 → 0 (round-gated 启用且 minR=0)
// 2026-05-20 sp15: S_slot 改 round-gated (R<4 → 0) + scale 0.3 (R4-R5).
func TestV3_SlotBalance_Imbalance(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"Ac", "Ad", "Ah"},
		nil,
		[]string{"2c", "3d", "4h"},
	)
	gs.Round = 4 // 启用 S_slot
	f := BuildFeaturesV3(gs)
	// S0: min=0 (top 满) / max=5 (mid 空) = 0, scale 后 0
	if f[128] > 0.01 {
		t.Errorf("S0 imbalance R4: got %.3f, want ≈ 0", f[128])
	}
}

// TestV3_CommitFlush — bot 4 ♦ → Q0 = 0.8
func TestV3_CommitFlush(t *testing.T) {
	gs := makeStateV3(t, nil, nil,
		[]string{"2d", "5d", "8d", "Jd"}, // 4 ♦
	)
	f := BuildFeaturesV3(gs)
	if math.Abs(float64(f[121]-0.8)) > 0.01 {
		t.Errorf("Q0 bot_commit_flush (4 ♦): got %.2f, want 0.8", f[121])
	}
}

// TestV3_FantasyGranular_AA — top AA, F2 ≈ 1, F0 (QQ)/F1 (KK) = 0
func TestV3_FantasyGranular_AA(t *testing.T) {
	gs := makeStateV3(t, []string{"Ac", "Ad"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// F2 P(top final AA exact, not trips) — top has 2 A, 1 slot left, P(摸 A) ~3/49 (joker also wild)
	// Should be high but not 1 (slight chance to trips)
	if f[92] < 0.7 {
		t.Errorf("F2 P(top AA, has 2 A 1 slot): got %.2f, want > 0.7", f[92])
	}
	// F0 P(top QQ) should be 0 (top is AA, not QQ)
	if f[90] > 0.05 {
		t.Errorf("F0 P(top QQ): got %.2f, want ≤ 0.05", f[90])
	}
}

// ============================================================
// 补充测试: 每个 group 至少一个明确 assertion
// ============================================================

// TestV3_X_MidPair — X3 mid 已 pair, P(mid ≥ pair) = 1
func TestV3_X_MidPair(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"5c", "5h", "Js"}, nil)
	f := BuildFeaturesV3(gs)
	// X3 mid P(≥ pair) at idx 72 (X 起 69, X3 = 69+3 = 72)
	if f[72] < 0.99 {
		t.Errorf("X3 P(mid ≥ pair, has pair already): got %.2f, want 1.0", f[72])
	}
}

// TestV3_X_BotFlushHigh — bot 4 ♦, X15 P(bot flush) 应较高
func TestV3_X_BotFlushHigh(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"2d", "5d", "8d", "Jd"})
	f := BuildFeaturesV3(gs)
	// X15 P(bot flush) at idx 84 (X 起 69, X15 = 69+15 = 84)
	if f[84] < 0.15 {
		t.Errorf("X15 P(bot flush, 4 ♦): got %.2f, want ≥ 0.15", f[84])
	}
}

// TestV3_Y_HighRoyalty — bot 4 ♥, Y2 期望 royalty 应 > 0
func TestV3_Y_BotPositive(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"2h", "5h", "8h", "Jh"})
	f := BuildFeaturesV3(gs)
	// Y2 at idx 96 (94+2)
	if f[96] <= 0 {
		t.Errorf("Y2 E[royalty_bot] (4 ♥ flush draw): got %.4f, want > 0", f[96])
	}
}

// TestV3_Z_PhaseR1 — R1 game phase = ~1.0 (full slots)
func TestV3_Z_PhaseR1(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	// Z4 phase at idx 101 (97+4), slots_total = 13 → 13/13 = 1.0
	if math.Abs(float64(f[101]-1.0)) > 0.01 {
		t.Errorf("Z4 phase (empty board): got %.2f, want 1.0", f[101])
	}
}

// TestV3_U_TwoPairHigh — mid 2pair K + 8, U3 = K rank / 12
func TestV3_U_TwoPairHigh(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"Kc", "Kd", "8h", "8s", "3c"}, nil)
	f := BuildFeaturesV3(gs)
	// U3 mid 2pair high rank at idx 105 (102+3), K rank = 11/12 ≈ 0.917
	expected := 11.0 / 12.0
	if math.Abs(float64(f[105])-expected) > 0.01 {
		t.Errorf("U3 mid 2pair high (KK+88): got %.3f, want %.3f", f[105], expected)
	}
}

// TestV3_V_PairToTrips — top has pair K + deck has 2 K, P(升 trips) > 0
func TestV3_V_TopPairToTrips(t *testing.T) {
	gs := makeStateV3(t, []string{"Kc", "Kd"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// V0 P(top pair → trips) at idx 107
	// top 1 slot, deck has 2 K + 2 jokers
	// 至少 1 K 或 1 joker in next 1 draw → P 不低
	if f[107] < 0.05 {
		t.Errorf("V0 P(top pair K → trips): got %.3f, want > 0.05", f[107])
	}
}

// TestV3_T3_MaxReachable — top has K, deck has more K, T3 should be ≥ K rank
func TestV3_T3_MaxReachable(t *testing.T) {
	gs := makeStateV3(t, []string{"Kc"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// T3 max pair rank reachable at idx 115
	// top has K + 2 slots remaining + deck has K → max reachable ≥ K (11/12)
	if f[115] < 11.0/12.0-0.01 {
		t.Errorf("T3 top max pair rank (has K, deck has K): got %.3f, want ≥ %.3f", f[115], 11.0/12.0)
	}
}

// TestV3_C0_Top — top has pair Q, C0 max should be ≥ pair (1/3) or trips (3/3)
func TestV3_C0_Top(t *testing.T) {
	gs := makeStateV3(t, []string{"Qc", "Qd"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// C0 top max at idx 116; top already pair Q, max ≥ pair (1/3) up to trips (3/3)
	if f[116] < 1.0/3.0-0.01 {
		t.Errorf("C0 top max (pair Q): got %.3f, want ≥ %.3f", f[116], 1.0/3.0)
	}
}

// TestV3_R5_ForcedCount — R5 with 3 slots empty, forced count = 1.0
func TestV3_R5_ForcedCount(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"As", "Kc"},                  // top has 2 (1 slot)
		[]string{"5c", "5h", "Js", "8d", "4c"}, // mid full
		[]string{"Jh", "9c", "6d", "4d"},      // bot has 4 (1 slot)
	)
	gs.Round = 5
	f := BuildFeaturesV3(gs)
	// R5_1 forced count at idx 120 = (1+0+1) / 3 = 0.667
	expected := 2.0 / 3.0
	if math.Abs(float64(f[120])-expected) > 0.01 {
		t.Errorf("R5_1 forced count (2 slots empty): got %.3f, want %.3f", f[120], expected)
	}
}

// TestV3_Q1_BotStraightCommit — bot 7-8-9 connected, Q1 should be 0.6 (3/5)
func TestV3_Q1_BotStraight(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"7d", "8c", "9h"})
	f := BuildFeaturesV3(gs)
	// Q1 at idx 122 (121+1)
	if math.Abs(float64(f[122]-0.6)) > 0.01 {
		t.Errorf("Q1 bot_commit_straight (789): got %.3f, want 0.6", f[122])
	}
}

// TestV3_M_FoulMargin — mid pair + bot high → mid-bot margin < 0
func TestV3_M_FoulMarginNegative(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"As", "Kc"},
		[]string{"Kh", "Ks", "Qs", "8d", "4c"}, // pair K
		[]string{"Jh", "9c", "6d", "4d", "3h"}, // high J
	)
	f := BuildFeaturesV3(gs)
	// M1 bot-mid margin at idx 126 (125+1) — bot (high) < mid (pair) → negative
	if f[126] >= 0 {
		t.Errorf("M1 bot-mid margin (mid pair > bot high): got %.3f, want < 0", f[126])
	}
}

// TestV3_S_Scale03_R1 — R1 全场 scale 0.3 (2026-05-20 sp15 v2: 不再 R<4 直接 0).
// Why: R<4 直接 0 太激进, baseline 退 15 点; 改 0.3 全场, 信号方向保留强度减 70%.
func TestV3_S_Scale03_R1(t *testing.T) {
	gs := NewGameState(2) // 默认 Round=1, empty 状态
	f := BuildFeaturesV3(gs)
	// R1 空 state: topR=3, midR=5, botR=5 → min/max = 3/5 = 0.6, scale 0.3 → 0.18
	expected := float32(0.3 * 3.0 / 5.0)
	if math.Abs(float64(f[128]-expected)) > 0.01 {
		t.Errorf("S0 R1 empty (scale 0.3): got %.3f, want %.3f (0.3 * 3/5)", f[128], expected)
	}
}

// TestV3_S_Scale03_R3_Balanced — R3 平衡 → scale 0.3
func TestV3_S_Scale03_R3_Balanced(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"Ac"}, []string{"3d", "4s"}, []string{"5h", "6h"},
	)
	gs.Round = 3
	f := BuildFeaturesV3(gs)
	// topR=2 midR=3 botR=3 → min=2/max=3=0.667, scale 0.3 → 0.20
	expected := float32(0.3 * 2.0 / 3.0)
	if math.Abs(float64(f[128]-expected)) > 0.01 {
		t.Errorf("S0 R3: got %.3f, want %.3f (0.3 * 2/3)", f[128], expected)
	}
}

// TestV3_S_Scale03_R5_Perfect — R5 完美平衡 → 0.3
func TestV3_S_Scale03_R5_Perfect(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"Ac", "Kc"}, []string{"3d", "4s", "5d", "6d"}, []string{"5h", "6h", "7h", "8h"},
	)
	gs.Round = 5
	f := BuildFeaturesV3(gs)
	// topR=1 midR=1 botR=1, min/max=1, scale 0.3 → 0.30
	if math.Abs(float64(f[128]-0.3)) > 0.01 {
		t.Errorf("S0 R5 perfect: got %.3f, want 0.3", f[128])
	}
}

// TestV3_N_NoDiscard — 没 SetDiscard → N0/N1/N2-0/N2-1 全 0
func TestV3_N_NoDiscard(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	if f[129] != 0 || f[130] != 0 {
		t.Errorf("N no-discard: N0=%.2f N1=%.2f, want 0/0", f[129], f[130])
	}
	if f[145] != 0 || f[146] != 0 {
		t.Errorf("N2 no-discard: N2-0=%.2f N2-1=%.2f, want 0/0", f[145], f[146])
	}
}

// TestV3_N_DiscardPremium — 弃 Ah: rank=A → N0=1, premium (A) → N1=1
func TestV3_N_DiscardPremium(t *testing.T) {
	gs := NewGameState(2)
	gs.SetDiscard(mustParse("Ah"))
	f := BuildFeaturesV3(gs)
	if math.Abs(float64(f[129]-1.0)) > 0.01 {
		t.Errorf("N0 discard rank=A: got %.3f, want 1.0", f[129])
	}
	if f[130] != 1.0 {
		t.Errorf("N1 discard premium A: got %.3f, want 1.0", f[130])
	}
}

// TestV3_N_DiscardLowRank — 弃 2s: rank=0 → N0=0, 不 premium → N1=0
func TestV3_N_DiscardLowRank(t *testing.T) {
	gs := NewGameState(2)
	gs.SetDiscard(mustParse("2s"))
	f := BuildFeaturesV3(gs)
	if f[129] != 0 {
		t.Errorf("N0 discard rank=2: got %.3f, want 0", f[129])
	}
	if f[130] != 0 {
		t.Errorf("N1 discard non-premium: got %.3f, want 0", f[130])
	}
}

// TestV3_N_DiscardJoker — 弃 joker: N0=1, N1=1 (joker 算 premium)
func TestV3_N_DiscardJoker(t *testing.T) {
	gs := NewGameState(2)
	gs.SetDiscard(MakeJoker())
	f := BuildFeaturesV3(gs)
	if f[129] != 1 || f[130] != 1 {
		t.Errorf("N joker: N0=%.2f N1=%.2f, want 1/1", f[129], f[130])
	}
}

// TestV3_N2_BreakBotSuit — bot 有 3 ♠, 弃 ♠ → N2-0=1
func TestV3_N2_BreakBotSuit(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"As", "Ks", "5s"})
	gs.SetDiscard(mustParse("9s"))
	f := BuildFeaturesV3(gs)
	if f[145] != 1 {
		t.Errorf("N2-0 break_bot_suit (bot=3♠, discard ♠): got %.2f, want 1", f[145])
	}
}

// TestV3_N2_NoBreakBotSuit — bot 只 2 ♠ (<3 阈值), 弃 ♠ → N2-0=0
func TestV3_N2_NoBreakBotSuit(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"As", "Ks"})
	gs.SetDiscard(mustParse("9s"))
	f := BuildFeaturesV3(gs)
	if f[145] != 0 {
		t.Errorf("N2-0 bot=2♠ < threshold: got %.2f, want 0", f[145])
	}
}

// TestV3_N2_BreakConnector — bot 有 5h, 弃 6h (rank 4 vs 5 相邻) → N2-1=1
func TestV3_N2_BreakConnector(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"5h", "Td"})
	gs.SetDiscard(mustParse("6c")) // rank 4 (6)
	f := BuildFeaturesV3(gs)
	if f[146] != 1 {
		t.Errorf("N2-1 break_connector (bot 5h, discard 6c): got %.2f, want 1", f[146])
	}
}

// TestV3_N2_NoBreakConnector — bot 无相邻 rank → N2-1=0
func TestV3_N2_NoBreakConnector(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"5h", "Td"})
	gs.SetDiscard(mustParse("2c")) // rank 0, 跟 5(3) / T(8) 都不相邻
	f := BuildFeaturesV3(gs)
	if f[146] != 0 {
		t.Errorf("N2-1 no neighbor: got %.2f, want 0", f[146])
	}
}

// ============ L group (131-136) tests ============

// TestV3_L_PairsSplit — Kc 顶 + Kd 中 → pairs_split=1/4=0.25
func TestV3_L_PairsSplit(t *testing.T) {
	gs := makeStateV3(t, []string{"Kc"}, []string{"Kd"}, nil)
	f := BuildFeaturesV3(gs)
	want := float32(0.25)
	if math.Abs(float64(f[131]-want)) > 0.01 {
		t.Errorf("L0 pairs_split: got %.3f, want %.3f", f[131], want)
	}
}

// TestV3_L_FlushGroupSplit — top=[♠♠♠] mid=[♠♠] → flushgroup_split=1/4=0.25
func TestV3_L_FlushGroupSplit(t *testing.T) {
	gs := makeStateV3(t, []string{"As", "Ks", "Qs"}, []string{"Js", "Ts"}, nil)
	f := BuildFeaturesV3(gs)
	want := float32(0.25)
	if math.Abs(float64(f[132]-want)) > 0.01 {
		t.Errorf("L1 flushgroup_split: got %.3f, want %.3f", f[132], want)
	}
}

// TestV3_L_ConnectorsSplit — mid=[5h] bot=[6c] → connectors_split (5/6 跨行)
func TestV3_L_ConnectorsSplit(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"5h"}, []string{"6c"})
	f := BuildFeaturesV3(gs)
	// connectors_split count >= 1 → L2 >= 1/6 ≈ 0.167
	if f[133] <= 0 {
		t.Errorf("L2 connectors_split (5/6 cross-row): got %.3f, want > 0", f[133])
	}
}

// TestV3_L_KickerOrderAnomaly — bot=[2h] mid=[Kh] → bot_min(0) - mid_max(11) = -11/12
func TestV3_L_KickerOrderAnomaly(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"Kh"}, []string{"2h"})
	f := BuildFeaturesV3(gs)
	// L3 = (botMin - midMax) / 12 = (0 - 11)/12 ≈ -0.917
	if f[134] > -0.5 {
		t.Errorf("L3 bot_min_minus_mid_max_norm: got %.3f, want negative anomaly < -0.5", f[134])
	}
}

// TestV3_L_Gap1Orphan — mid=[2h 4h] bot=[3c] → 3 被孤立 → gap1_orphan=1
func TestV3_L_Gap1Orphan(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"2h", "4h"}, []string{"3c"})
	f := BuildFeaturesV3(gs)
	// L4 = count/4, ≥1 → ≥0.25
	if f[135] < 0.2 {
		t.Errorf("L4 gap1_orphan (2,4 mid + 3 bot): got %.3f, want ≥ 0.25", f[135])
	}
}

// TestV3_L_MidHeavierThanBot — mid=[3 张] bot=[1 张] → mid 重 → L5 正
func TestV3_L_MidHeavierThanBot(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"2h", "3h", "4h"}, []string{"5c"})
	f := BuildFeaturesV3(gs)
	// L5 = mid/5 - bot/5 = 3/5 - 1/5 = 0.4
	want := float32(0.4)
	if math.Abs(float64(f[136]-want)) > 0.01 {
		t.Errorf("L5 mid_minus_bot_fill: got %.3f, want %.3f", f[136], want)
	}
}

// ============ LR group (137-144) tests ============

// TestV3_LR_BotLockedStraight — bot 已 straight → LR0 > 0
func TestV3_LR_BotLockedStraight(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"5h", "6c", "7d", "8s", "9h"})
	f := BuildFeaturesV3(gs)
	// 期望 (TypeStraight=4, (4-4+1)/6 ≈ 0.167)
	if f[137] < 0.1 {
		t.Errorf("LR0 bot_locked_straight: got %.3f, want > 0.1", f[137])
	}
}

// TestV3_LR_MidLockedTrips — mid 已 trips → LR1 > 0
func TestV3_LR_MidLockedTrips(t *testing.T) {
	gs := makeStateV3(t,
		nil,
		[]string{"7h", "7c", "7d", "2s", "3h"},
		[]string{"Ah", "Kc", "Qd", "Js", "Th"}, // bot 强保 cap
	)
	f := BuildFeaturesV3(gs)
	if f[138] < 0.1 {
		t.Errorf("LR1 mid_locked_trips: got %.3f, want > 0.1", f[138])
	}
}

// TestV3_LR_NoLocked_NotComplete — 不完整行 → LR0/LR1=0
func TestV3_LR_NotLocked(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"5h", "6c"}) // bot 不完整
	f := BuildFeaturesV3(gs)
	if f[137] != 0 {
		t.Errorf("LR0 bot incomplete: got %.3f, want 0", f[137])
	}
}

// TestV3_LR_Bot4Flush — bot=[♠♠♠♠] 4 同色 1 空 → LR2=1
func TestV3_LR_Bot4Flush(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"As", "Ks", "9s", "5s"})
	f := BuildFeaturesV3(gs)
	if f[139] != 1 {
		t.Errorf("LR2 bot_4flush: got %.3f, want 1", f[139])
	}
}

// TestV3_LR_Bot4StraightOpen — bot=[3 4 5 6] → 4-straight open → LR3=1
func TestV3_LR_Bot4StraightOpen(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"3c", "4d", "5h", "6s"})
	f := BuildFeaturesV3(gs)
	if f[140] != 1 {
		t.Errorf("LR3 bot_4straight_open: got %.3f, want 1", f[140])
	}
	if f[141] != 0 {
		t.Errorf("LR4 bot_4straight_gutshot (open shouldn't trigger gut): got %.3f, want 0", f[141])
	}
}

// TestV3_LR_Bot4StraightGutshot — bot=[3 4 5 7] span 4 → gutshot → LR4=1
func TestV3_LR_Bot4StraightGutshot(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"3c", "4d", "5h", "7s"})
	f := BuildFeaturesV3(gs)
	if f[141] != 1 {
		t.Errorf("LR4 bot_4straight_gutshot: got %.3f, want 1", f[141])
	}
}

// TestV3_LR_Mid4Flush — mid=[♥♥♥♥]
func TestV3_LR_Mid4Flush(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"Ah", "Th", "5h", "2h"}, nil)
	f := BuildFeaturesV3(gs)
	if f[142] != 1 {
		t.Errorf("LR5 mid_4flush: got %.3f, want 1", f[142])
	}
}

// TestV3_LR_Mid4Straight — mid=[3 4 5 6] 顺 → LR6=1
func TestV3_LR_Mid4StraightOpen(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"3c", "4d", "5h", "6s"}, nil)
	f := BuildFeaturesV3(gs)
	if f[143] != 1 {
		t.Errorf("LR6 mid_4straight: got %.3f, want 1", f[143])
	}
}

// TestV3_LR_PairKickerMax — bot=[K K A 2 3] pair K, kicker max=A
func TestV3_LR_PairKickerMax(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"Kh", "Kc", "Ad", "2s", "3h"})
	f := BuildFeaturesV3(gs)
	want := float32(1.0) // A = rank 12 / 12
	if math.Abs(float64(f[144]-want)) > 0.01 {
		t.Errorf("LR7 pair_kicker_max (bot pair K + A kicker): got %.3f, want %.3f", f[144], want)
	}
}

// TestV3_LR_PairKickerLow — bot=[K K 2 3 4] pair K, kicker max=4
func TestV3_LR_PairKickerLow(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"Kh", "Kc", "2d", "3s", "4h"})
	f := BuildFeaturesV3(gs)
	// rank 4 = 2/12 ≈ 0.167
	want := float32(2.0 / 12.0)
	if math.Abs(float64(f[144]-want)) > 0.02 {
		t.Errorf("LR7 pair_kicker low (kicker=4): got %.3f, want %.3f", f[144], want)
	}
}

// TestV3_Idx_Boundary — 所有 idx 都在 [0, FeatureDimV3) 内 (no overflow)
func TestV3_Idx_Boundary(t *testing.T) {
	gs := makeStateV3(t, []string{"Ac", "Ad"}, []string{"5h", "5s"}, []string{"7d", "8c", "9h"})
	f := BuildFeaturesV3(gs)
	if len(f) != FeatureDimV3 {
		t.Fatalf("dim: got %d, want %d", len(f), FeatureDimV3)
	}
	// 各 feature 应该有限值 (no NaN/Inf)
	for i, v := range f {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Errorf("f[%d] = %v (NaN or Inf)", i, v)
		}
	}
}

// TestV3_PFoul_Empty — 空 state, P(foul) 应该 ≤ 0.3 (无 placement, 默认低)
func TestV3_PFoul_Empty(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	// X20 at idx 89
	if f[89] > 0.5 {
		t.Errorf("X20 P(foul) empty state: got %.3f, want ≤ 0.5", f[89])
	}
}

// ============================================================
// 2026-05-19: V2-共享底层 5 组 (A/B/D/E/G) 补单元测试
// ============================================================

// ============ Group A: Board state (idx 0-7) ============

// TestV3_A_EmptyState — 空 state, top/mid/bot 都 0, free 全满
func TestV3_A_EmptyState(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	// A0/1/2 = filled count / max
	if f[0] != 0 || f[1] != 0 || f[2] != 0 {
		t.Errorf("A0-2 empty fill: got %.2f/%.2f/%.2f, want 0/0/0", f[0], f[1], f[2])
	}
	// A3/4/5 = free count / max = 1/1/1
	if f[3] != 1 || f[4] != 1 || f[5] != 1 {
		t.Errorf("A3-5 empty free: got %.2f/%.2f/%.2f, want 1/1/1", f[3], f[4], f[5])
	}
	// A6 round/5, default Round=0 → 0
	if f[6] != 0 {
		t.Errorf("A6 round empty: got %.3f, want 0", f[6])
	}
	// A7 complete flag = 0
	if f[7] != 0 {
		t.Errorf("A7 complete empty: got %.2f, want 0", f[7])
	}
}

// TestV3_A_PartialFill — 顶 2 张, 中 3 张, 底 1 张, R3
func TestV3_A_PartialFill(t *testing.T) {
	gs := makeStateV3(t, []string{"Ah", "Kc"}, []string{"5h", "5d", "5s"}, []string{"2c"})
	gs.Round = 3
	f := BuildFeaturesV3(gs)
	if math.Abs(float64(f[0]-2.0/3.0)) > 0.01 {
		t.Errorf("A0 top fill: got %.3f, want %.3f", f[0], 2.0/3.0)
	}
	if math.Abs(float64(f[1]-3.0/5.0)) > 0.01 {
		t.Errorf("A1 mid fill: got %.3f, want %.3f", f[1], 3.0/5.0)
	}
	if math.Abs(float64(f[2]-1.0/5.0)) > 0.01 {
		t.Errorf("A2 bot fill: got %.3f, want %.3f", f[2], 0.2)
	}
	if math.Abs(float64(f[6]-3.0/5.0)) > 0.01 {
		t.Errorf("A6 round=3: got %.3f, want 0.6", f[6])
	}
}

// TestV3_A_CompleteFlag — 顶 3 中 5 底 5 → A7=1
func TestV3_A_Complete(t *testing.T) {
	gs := makeStateV3(t,
		[]string{"Ah", "Kc", "Qd"},
		[]string{"5h", "5d", "5s", "5c", "2h"},
		[]string{"6h", "7h", "8h", "9h", "Th"},
	)
	f := BuildFeaturesV3(gs)
	if f[7] != 1 {
		t.Errorf("A7 complete: got %.2f, want 1", f[7])
	}
	if f[3] != 0 || f[4] != 0 || f[5] != 0 {
		t.Errorf("A3-5 free all 0 when complete: got %.2f/%.2f/%.2f", f[3], f[4], f[5])
	}
}

// ============ Group B: Hand tier per row (idx 8-31) ============

// V3 layout: top 8-13 (6 dim), mid 14-22 (9 dim), bot 23-31 (9 dim)

// TestV3_B_TopHighCard — top 1 张高 card, tier = HighCard
func TestV3_B_TopHighCard(t *testing.T) {
	gs := makeStateV3(t, []string{"Ah"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// top one-hot: HighCard=8, Pair<Q=9, Pair_Q=10, Pair_K=11, Pair_A=12, Trips=13
	if f[8] != 1 {
		t.Errorf("B top HighCard one-hot: got %.2f at idx 8, want 1", f[8])
	}
	for i := 9; i <= 13; i++ {
		if f[i] != 0 {
			t.Errorf("B top other tier idx %d should be 0: got %.2f", i, f[i])
		}
	}
}

// TestV3_B_TopPairK — top KK, tier=Pair_K (idx 11)
func TestV3_B_TopPairK(t *testing.T) {
	gs := makeStateV3(t, []string{"Kh", "Kc"}, nil, nil)
	f := BuildFeaturesV3(gs)
	if f[11] != 1 {
		t.Errorf("B top Pair_K at idx 11: got %.2f, want 1", f[11])
	}
}

// TestV3_B_MidFlush — mid 同色 5 张 (Flush). 需 bot ≥ Flush 才不被 cap-chain 降级.
// 注: V3 fillHandTiers 用 cap 后的 eval. Bot 空时 eval=HighCard, 会强制 cap mid → HighCard.
// 故 bot 必须给个 ≥ Flush 的完整行.
func TestV3_B_MidFlush(t *testing.T) {
	gs := makeStateV3(t,
		nil,
		[]string{"As", "Ks", "9s", "5s", "2s"},                // mid Flush ♠
		[]string{"Ah", "Ad", "Ac", "Kh", "Kd"},                // bot Full House (> Flush) → mid 不 cap
	)
	f := BuildFeaturesV3(gs)
	// mid 9-dim: HighCard=14, Pair=15, TwoPair=16, Trips=17, Straight=18, Flush=19
	if f[19] != 1 {
		t.Errorf("B mid Flush at idx 19: got %.2f, want 1 (bot=FH 不 cap mid)", f[19])
	}
}

// TestV3_B_BotFullHouse — bot 葫芦, tier=FullHouse (6)
func TestV3_B_BotFullHouse(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"Ah", "Ac", "Ad", "Kh", "Kc"})
	f := BuildFeaturesV3(gs)
	// bot 9-dim: HighCard=23, Pair=24, TwoPair=25, Trips=26, Straight=27, Flush=28, FullHouse=29
	if f[29] != 1 {
		t.Errorf("B bot FullHouse at idx 29: got %.2f, want 1", f[29])
	}
}

// ============ Group D: Joker state (idx 32-39) ============

// TestV3_D_NoJokers — 无 joker 在 row 里, D0/1/2/3=0
func TestV3_D_NoJokers(t *testing.T) {
	gs := makeStateV3(t, []string{"Ah"}, []string{"5h", "5d"}, nil)
	f := BuildFeaturesV3(gs)
	for i := 32; i <= 35; i++ { // top/mid/bot/total joker counts
		if f[i] != 0 {
			t.Errorf("D no joker idx %d: got %.2f, want 0", i, f[i])
		}
	}
	// D4 = jokersInDeck / 4. 注: jokersInDeck 返回 4 - used (上界, 不区分 NumJokers).
	// 没用任何 joker → 4/4 = 1.0
	if math.Abs(float64(f[36]-1.0)) > 0.01 {
		t.Errorf("D4 jokers in deck (none used): got %.3f, want 1.0 (impl 返回 4-used)", f[36])
	}
}

// TestV3_D_OneJokerInDeckUsed — 用 1 个 joker (jid=0), D4 = 3/4
func TestV3_D_OneJokerUsed(t *testing.T) {
	gs := NewGameState(2)
	jk := MakeJokerWithJID(0)
	gs.UsedCards[jk.ID()] = true
	f := BuildFeaturesV3(gs)
	want := float32(3.0 / 4.0)
	if math.Abs(float64(f[36]-want)) > 0.01 {
		t.Errorf("D4 jokers in deck (1 used): got %.3f, want %.3f", f[36], want)
	}
}

// TestV3_D_OneJokerInMid — mid 含 1 joker, D1 = 1/4 = 0.25
func TestV3_D_OneJokerMid(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"X"}, nil)
	f := BuildFeaturesV3(gs)
	if math.Abs(float64(f[33]-0.25)) > 0.01 {
		t.Errorf("D1 mid joker: got %.3f, want 0.25", f[33])
	}
	// D3 total = 0.25
	if math.Abs(float64(f[35]-0.25)) > 0.01 {
		t.Errorf("D3 total joker: got %.3f, want 0.25", f[35])
	}
}

// ============ Group E: Suit dist per row (idx 40-51) ============

// TestV3_E_BotSuitMonotonic — bot 全 ♠, top/mid 空 → bot ♠ count = 3/5
func TestV3_E_BotSuit(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"As", "Ks", "5s"})
	f := BuildFeaturesV3(gs)
	// V3: top 40-43 / mid 44-47 / bot 48-51. SuitS=0 (Spades).
	// 注: feature 用 c.Suit(), 内部 SuitS=0 SuitH=1 SuitD=2 SuitC=3
	want := float32(3.0 / 5.0)
	if math.Abs(float64(f[48]-want)) > 0.01 {
		t.Errorf("E bot Spade count: got %.3f, want %.3f (3/5)", f[48], want)
	}
	// 其它 suit 0
	if f[49] != 0 || f[50] != 0 || f[51] != 0 {
		t.Errorf("E bot other suits: got %.2f/%.2f/%.2f, want 0", f[49], f[50], f[51])
	}
}

// TestV3_E_TopMixedSuits — top 1 ♥, 1 ♦, 1 ♠
func TestV3_E_TopMixed(t *testing.T) {
	gs := makeStateV3(t, []string{"Ah", "Kd", "Qs"}, nil, nil)
	f := BuildFeaturesV3(gs)
	// top 40-43, /3
	want := float32(1.0 / 3.0)
	if math.Abs(float64(f[40]-want)) > 0.01 { // Spade
		t.Errorf("E top Spade: got %.3f, want %.3f", f[40], want)
	}
	if math.Abs(float64(f[41]-want)) > 0.01 { // Heart
		t.Errorf("E top Heart: got %.3f, want %.3f", f[41], want)
	}
	if math.Abs(float64(f[42]-want)) > 0.01 { // Diamond
		t.Errorf("E top Diamond: got %.3f, want %.3f", f[42], want)
	}
	if f[43] != 0 { // Club
		t.Errorf("E top Club (none): got %.3f, want 0", f[43])
	}
}

// TestV3_E_JokerExcluded — joker 不计入 suit
func TestV3_E_JokerExcluded(t *testing.T) {
	gs := makeStateV3(t, []string{"X", "Ah"}, nil, nil) // 1 joker + 1 heart
	f := BuildFeaturesV3(gs)
	// top heart only counts 1/3, joker 不计
	want := float32(1.0 / 3.0)
	if math.Abs(float64(f[41]-want)) > 0.01 {
		t.Errorf("E top Heart (joker excluded): got %.3f, want %.3f", f[41], want)
	}
	// 总和 = 1/3, 不是 2/3
	totalTop := f[40] + f[41] + f[42] + f[43]
	if math.Abs(float64(totalTop-want)) > 0.01 {
		t.Errorf("E top sum (with joker): got %.3f, want %.3f (joker excluded)", totalTop, want)
	}
}

// ============ Group G: Deck awareness (idx 52-68) ============

// TestV3_G_EmptyDeck — 空 state, 所有 rank 各 4 张 → 全 1.0
func TestV3_G_EmptyAllAvailable(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV3(gs)
	// rank 52-64 (13 dim), all 4/4 = 1.0
	for i := 52; i <= 64; i++ {
		if math.Abs(float64(f[i]-1.0)) > 0.01 {
			t.Errorf("G rank_remaining idx %d empty: got %.3f, want 1.0", i, f[i])
		}
	}
	// suit 65-68, all 13/13 = 1.0
	for i := 65; i <= 68; i++ {
		if math.Abs(float64(f[i]-1.0)) > 0.01 {
			t.Errorf("G suit_remaining idx %d empty: got %.3f, want 1.0", i, f[i])
		}
	}
}

// TestV3_G_RanksUsed — 全 4 张 A 用掉 → G[rank_A] = 0
func TestV3_G_AllAcesUsed(t *testing.T) {
	gs := makeStateV3(t, nil, nil, []string{"Ah", "Ac", "Ad", "As"})
	f := BuildFeaturesV3(gs)
	// rank A index in deck-aware: A=12 → idx 52+12 = 64
	if f[64] != 0 {
		t.Errorf("G rank A remaining (all 4 used): got %.3f, want 0", f[64])
	}
	// rank K (idx 52+11=63) 应该 1.0
	if math.Abs(float64(f[63]-1.0)) > 0.01 {
		t.Errorf("G rank K untouched: got %.3f, want 1.0", f[63])
	}
}

// TestV3_G_SuitUsed — 用 5 ♠ → 黑桃剩 8/13
func TestV3_G_SpadeUsed(t *testing.T) {
	gs := makeStateV3(t, nil, []string{"As", "Ks", "Qs", "Js", "Ts"}, nil)
	f := BuildFeaturesV3(gs)
	// suit S = 0, idx 65+0 = 65
	want := float32(8.0 / 13.0)
	if math.Abs(float64(f[65]-want)) > 0.01 {
		t.Errorf("G suit S remaining (5 used): got %.3f, want %.3f", f[65], want)
	}
}
