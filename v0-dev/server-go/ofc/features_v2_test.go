package ofc

import (
	"math"
	"testing"
)

// makeStateV2 — V2 测试 helper (跟 V3 helper 一致, 复制省得 cross-file 耦合).
func makeStateV2(t *testing.T, top, mid, bot []string) *GameState {
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

// TestV2_Dim — V2 dim 严格 134
func TestV2_Dim(t *testing.T) {
	gs := NewGameState(2)
	f := BuildFeaturesV2(gs)
	if len(f) != 134 {
		t.Fatalf("V2 dim: got %d, want 134", len(f))
	}
	if FeatureDimV2 != 134 {
		t.Fatalf("FeatureDimV2 const: got %d, want 134", FeatureDimV2)
	}
}

// TestV2_Idx_Boundary — 所有 idx 有限 (no NaN/Inf)
func TestV2_Idx_Boundary(t *testing.T) {
	gs := makeStateV2(t, []string{"Ac", "Ad"}, []string{"5h", "5s"}, []string{"7d", "8c", "9h"})
	f := BuildFeaturesV2(gs)
	for i, v := range f {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Errorf("V2 f[%d] = %v (NaN or Inf)", i, v)
		}
	}
}

// ============ Group A: Board state (idx 0-7) ============
// 跟 V3 共享 fillBoardState 逻辑, 但 V2 layout 验证 idx 位置.

func TestV2_A_PartialAndRound(t *testing.T) {
	gs := makeStateV2(t, []string{"Ah"}, []string{"5h", "5d"}, nil)
	gs.Round = 3
	f := BuildFeaturesV2(gs)
	if math.Abs(float64(f[0]-1.0/3.0)) > 0.01 {
		t.Errorf("A0 top 1/3: got %.3f", f[0])
	}
	if math.Abs(float64(f[1]-2.0/5.0)) > 0.01 {
		t.Errorf("A1 mid 2/5: got %.3f", f[1])
	}
	if math.Abs(float64(f[6]-0.6)) > 0.01 {
		t.Errorf("A6 round=3/5: got %.3f", f[6])
	}
}

// ============ Group B: Hand tier (idx 8-31) ============
// V2 layout 跟 V3 一致 (top 8-13 / mid 14-22 / bot 23-31).

func TestV2_B_TopPairA(t *testing.T) {
	gs := makeStateV2(t, []string{"Ah", "Ac"}, nil, nil)
	f := BuildFeaturesV2(gs)
	// top one-hot at idx 8+ : HighCard=8, <Q=9, Q=10, K=11, A=12, Trips=13
	if f[12] != 1 {
		t.Errorf("B top Pair_A: got %.2f at idx 12, want 1", f[12])
	}
}

// ============ Group C: Top fantasy progress (idx 32-53) — V2 独有 ============
// C 22 dim:
//   32-44: top_pair_rank_onehot[13] (real pair only)
//   45: top_has_real_pair
//   46: top_has_wild_pair (joker + ≥1 real)
//   47: top_has_real_trips
//   48-52: top_fantasy_floor_tier_onehot[5] = [none, QQ, KK, AA, trips]
//   53: top_can_upgrade_to_AA

// TestV2_C_TopRealAA — top AA → onehot[A]=1, has_real_pair=1, floor=AA tier (idx 51)
func TestV2_C_TopRealAA(t *testing.T) {
	gs := makeStateV2(t, []string{"Ah", "Ac"}, nil, nil)
	f := BuildFeaturesV2(gs)
	// onehot[RankA] = idx 32 + 12 = 44
	if f[44] != 1 {
		t.Errorf("C onehot A: got %.2f at idx 44, want 1", f[44])
	}
	// has_real_pair
	if f[45] != 1 {
		t.Errorf("C has_real_pair: got %.2f", f[45])
	}
	// has_wild_pair = 0 (no joker)
	if f[46] != 0 {
		t.Errorf("C has_wild_pair (no joker): got %.2f, want 0", f[46])
	}
	// floor: AA tier = index 51 (32+16+3)
	if f[51] != 1 {
		t.Errorf("C floor AA at idx 51: got %.2f, want 1", f[51])
	}
}

// TestV2_C_TopWildPairK — X + K → wild_pair=1, floor=KK
func TestV2_C_TopWildPairK(t *testing.T) {
	gs := makeStateV2(t, []string{"X", "Kh"}, nil, nil)
	f := BuildFeaturesV2(gs)
	if f[46] != 1 {
		t.Errorf("C wild_pair (X+K): got %.2f, want 1", f[46])
	}
	// floor: KK tier = idx 32+16+2 = 50
	if f[50] != 1 {
		t.Errorf("C floor KK (X+K): got %.2f at idx 50, want 1", f[50])
	}
}

// TestV2_C_TopTrips — top trips → floor=trips tier (idx 52)
func TestV2_C_TopTrips(t *testing.T) {
	gs := makeStateV2(t, []string{"5h", "5c", "5d"}, nil, nil)
	f := BuildFeaturesV2(gs)
	if f[47] != 1 {
		t.Errorf("C has_real_trips: got %.2f, want 1", f[47])
	}
	// floor trips = idx 32+16+4 = 52
	if f[52] != 1 {
		t.Errorf("C floor trips: got %.2f at idx 52, want 1", f[52])
	}
}

// TestV2_C_TopFloorNone — top 高 card (无 pair / 无 joker) → floor=none idx 48
func TestV2_C_TopFloorNone(t *testing.T) {
	gs := makeStateV2(t, []string{"Ah"}, nil, nil)
	f := BuildFeaturesV2(gs)
	// floor none = idx 32+16 = 48
	if f[48] != 1 {
		t.Errorf("C floor none: got %.2f at idx 48, want 1", f[48])
	}
}

// ============ Group D: Joker state (idx 54-61) — V2 layout ============

func TestV2_D_OneJokerMid(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"X"}, nil)
	f := BuildFeaturesV2(gs)
	// V2: idx 54 top joker, 55 mid, 56 bot, 57 total, 58 inDeck, 59-61 effRank
	// 注: 实现 fillJokerState f[0]=jt/4 f[1]=jm/4 f[2]=jb/4 f[3]=total/4 f[4]=inDeck/4
	// 所以 V2 absolute: 54 jt, 55 jm, 56 jb, 57 total, 58 inDeck, 59-61 effRank
	if math.Abs(float64(f[55]-0.25)) > 0.01 {
		t.Errorf("D mid joker: got %.3f at idx 55, want 0.25 (1/4)", f[55])
	}
	if math.Abs(float64(f[57]-0.25)) > 0.01 {
		t.Errorf("D total joker: got %.3f at idx 57, want 0.25", f[57])
	}
}

// ============ Group E: Suit dist (idx 62-73) — V2 layout ============

func TestV2_E_BotAllSpade(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"As", "Ks", "5s"})
	f := BuildFeaturesV2(gs)
	// V2: top 62-65, mid 66-69, bot 70-73. SuitS=0 → bot ♠ at idx 70.
	want := float32(3.0 / 5.0)
	if math.Abs(float64(f[70]-want)) > 0.01 {
		t.Errorf("E bot Spade: got %.3f at idx 70, want %.3f", f[70], want)
	}
}

// ============ Group F: Straight draw (idx 74-85) — V2 独有 ============
// 12 dim:
//   74-76: consecutiveRunMax top/mid/bot (normalized /3 or /5)
//   77/78: hasFourCardOE mid/bot
//   79/80: straightOuts mid/bot / 8
//   81-83: highCount top/mid/bot
//   84/85: has3ConsecHigh mid/bot

func TestV2_F_BotConsecutiveRun(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"5h", "6c", "7d"}) // 3 连续
	f := BuildFeaturesV2(gs)
	// idx 76 = bot run / 5 = 3/5
	want := float32(3.0 / 5.0)
	if math.Abs(float64(f[76]-want)) > 0.01 {
		t.Errorf("F bot run 3 consec: got %.3f at idx 76, want %.3f", f[76], want)
	}
}

func TestV2_F_Mid4CardOE(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"5h", "6c", "7d", "8s"}, nil) // 4 连张 OE
	f := BuildFeaturesV2(gs)
	// idx 77 = mid_has_4card_OE
	if f[77] != 1 {
		t.Errorf("F mid 4-card OE: got %.2f at idx 77, want 1", f[77])
	}
}

func TestV2_F_BotHighCount(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"Ah", "Kc", "Qd", "5h", "2s"}) // 3 high cards (≥T)
	f := BuildFeaturesV2(gs)
	// idx 83 = bot_high_count / 5. high=rank ≥ T (idx 8). A=12, K=11, Q=10 → 3 张
	want := float32(3.0 / 5.0)
	if math.Abs(float64(f[83]-want)) > 0.01 {
		t.Errorf("F bot high count: got %.3f at idx 83, want %.3f", f[83], want)
	}
}

// ============ Group G: Deck aware (idx 86-102) — V2 layout ============

func TestV2_G_RankAllUsed(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"Ah", "Ac", "Ad", "As"})
	f := BuildFeaturesV2(gs)
	// V2: idx 86-98 rank_remaining[13], 99-102 suit_remaining[4]
	// rank A = 12 → idx 86+12 = 98, all 4 used → 0
	if f[98] != 0 {
		t.Errorf("G rank A all used at idx 98: got %.3f, want 0", f[98])
	}
	// rank K (idx 97) 应 1.0
	if math.Abs(float64(f[97]-1.0)) > 0.01 {
		t.Errorf("G rank K untouched at idx 97: got %.3f, want 1.0", f[97])
	}
}

// ============ Group H: Foul risk (idx 103-107) — V2 独有 ============
// 5 dim:
//   103: foul_currently_inevitable (top > mid type 直接 1)
//   104-106: top/mid/bot type / 9
//   107: min margin (min(mid-top, bot-mid)) / 9 clamp [-1,1]

func TestV2_H_FoulInevitable(t *testing.T) {
	// top trips > mid pair → 立 foul
	gs := makeStateV2(t,
		[]string{"Ah", "Ac", "Ad"},     // top trips A
		[]string{"5h", "5c", "2d", "3s", "4h"}, // mid pair 5
		nil,
	)
	f := BuildFeaturesV2(gs)
	if f[103] != 1 {
		t.Errorf("H foul_inevitable (top trips > mid pair): got %.2f, want 1", f[103])
	}
}

// TestV2_H_NotFoul — 完整 non-fouling 摆: top pair5 < mid trips7 < bot quads K.
// 注: 这里 top/mid 不完整时 cap-chain 会把高 type 强降 (foul 保护), 难造干净非 foul, 故用完整 state.
func TestV2_H_NotFoul(t *testing.T) {
	gs := makeStateV2(t,
		[]string{"5h", "5c"},                              // top pair 5
		[]string{"7h", "7c", "7d", "2s", "3h"},            // mid trips 7
		[]string{"Kh", "Kc", "Kd", "Ks", "2c"},            // bot quads K
	)
	f := BuildFeaturesV2(gs)
	if f[103] != 0 {
		t.Errorf("H foul_inevitable (pair5 < trips7 < quadsK): got %.2f, want 0", f[103])
	}
}

func TestV2_H_TierNormalized(t *testing.T) {
	gs := makeStateV2(t,
		nil,
		nil,
		[]string{"Ah", "Ac", "Ad", "As", "2h"}, // bot quads = Type 7
	)
	f := BuildFeaturesV2(gs)
	// idx 106 = bot type / 9 = 7/9 ≈ 0.778
	want := float32(7.0 / 9.0)
	if math.Abs(float64(f[106]-want)) > 0.02 {
		t.Errorf("H bot tier (quads=7): got %.3f at idx 106, want %.3f", f[106], want)
	}
}

// ============ Group I: Pair preservation (idx 108-114) — V2 独有 ============
// 7 dim:
//   108: mid_max_pair_rank / 12
//   109: bot_max_pair_rank / 12
//   110: mid_has_real_pair
//   111: bot_has_real_pair
//   112: mid_has_real_trips
//   113: bot_has_real_trips
//   114: bot_has_flush_potential (3+ same suit)

func TestV2_I_BotPairKing(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"Kh", "Kc"})
	f := BuildFeaturesV2(gs)
	// idx 109 = bot max pair rank / 12. K = rank 11 → 11/12
	want := float32(11.0 / 12.0)
	if math.Abs(float64(f[109]-want)) > 0.01 {
		t.Errorf("I bot pair K rank: got %.3f at idx 109, want %.3f", f[109], want)
	}
	if f[111] != 1 {
		t.Errorf("I bot_has_real_pair: got %.2f, want 1", f[111])
	}
}

func TestV2_I_BotFlushPotential(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"As", "Ks", "5s"})
	f := BuildFeaturesV2(gs)
	// idx 114 = bot flush potential (3+ same suit)
	if f[114] != 1 {
		t.Errorf("I bot flush potential (3♠): got %.2f, want 1", f[114])
	}
}

func TestV2_I_NoTrips(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"5h", "5c"}, nil) // mid pair only, not trips
	f := BuildFeaturesV2(gs)
	if f[112] != 0 {
		t.Errorf("I mid_has_real_trips (only pair): got %.2f, want 0", f[112])
	}
}

// ============ Group K: Joker completes (idx 115-127) — V2 独有 ============
// 13 dim:
//   115: top_has_wild_trips
//   116-121: mid pair/trips/quad/straight/flush/FH
//   122-127: bot pair/trips/quad/straight/flush/FH

func TestV2_K_TopWildTrips(t *testing.T) {
	gs := makeStateV2(t, []string{"Ah", "Ac", "X"}, nil, nil) // 2 A + joker = wild trips
	f := BuildFeaturesV2(gs)
	if f[115] != 1 {
		t.Errorf("K top_wild_trips (AA + X): got %.2f, want 1", f[115])
	}
}

func TestV2_K_MidWildFlush(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"Ah", "Kh", "Qh", "Jh", "X"}, nil) // 4 ♥ + joker
	f := BuildFeaturesV2(gs)
	// mid flush at idx 116+4 = 120
	if f[120] != 1 {
		t.Errorf("K mid_wild_flush: got %.2f at idx 120, want 1", f[120])
	}
}

func TestV2_K_BotPairOnly(t *testing.T) {
	gs := makeStateV2(t, nil, nil, []string{"5h", "5c"}) // pair only
	f := BuildFeaturesV2(gs)
	// bot pair at idx 122
	if f[122] != 1 {
		t.Errorf("K bot pair: got %.2f at idx 122, want 1", f[122])
	}
	// bot trips at idx 123 应该 0
	if f[123] != 0 {
		t.Errorf("K bot trips (only pair): got %.2f at idx 123, want 0", f[123])
	}
}

// ============ Group L: Cross-row anti-pattern (idx 128-133) — V2 独有 ============
// 6 dim (跟 V3 L 共享 helper, 但 V2 idx 128-133, V3 idx 131-136)
//   128: pairs_split
//   129: flushgroup_split
//   130: connectors_split
//   131: bot_min_minus_mid_max_norm
//   132: gap1_orphan
//   133: mid_minus_bot_fill_ratio

func TestV2_L_PairsSplit(t *testing.T) {
	gs := makeStateV2(t, []string{"Kc"}, []string{"Kd"}, nil)
	f := BuildFeaturesV2(gs)
	want := float32(0.25)
	if math.Abs(float64(f[128]-want)) > 0.01 {
		t.Errorf("V2 L0 pairs_split: got %.3f at idx 128, want %.3f", f[128], want)
	}
}

func TestV2_L_FlushgroupSplit(t *testing.T) {
	gs := makeStateV2(t, []string{"As", "Ks", "Qs"}, []string{"Js", "Ts"}, nil)
	f := BuildFeaturesV2(gs)
	want := float32(0.25)
	if math.Abs(float64(f[129]-want)) > 0.01 {
		t.Errorf("V2 L1 flushgroup_split: got %.3f at idx 129, want %.3f", f[129], want)
	}
}

func TestV2_L_KickerAnomaly(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"Kh"}, []string{"2h"})
	f := BuildFeaturesV2(gs)
	// idx 131 = (bot_min - mid_max)/12 = (0-11)/12 ≈ -0.917
	if f[131] > -0.5 {
		t.Errorf("V2 L3 kicker order anomaly: got %.3f at idx 131, want < -0.5", f[131])
	}
}

func TestV2_L_MidHeavier(t *testing.T) {
	gs := makeStateV2(t, nil, []string{"2h", "3h", "4h"}, []string{"5c"})
	f := BuildFeaturesV2(gs)
	// idx 133 = mid/5 - bot/5 = 3/5 - 1/5 = 0.4
	want := float32(0.4)
	if math.Abs(float64(f[133]-want)) > 0.01 {
		t.Errorf("V2 L5 mid_minus_bot_fill: got %.3f at idx 133, want %.3f", f[133], want)
	}
}
