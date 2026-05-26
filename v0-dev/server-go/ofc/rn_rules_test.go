package ofc

import "testing"

// makeRNAction — helper for rnRule tests
func makeRNAction(discardIdx int, kept []Card, placement Placement) *RoundNAction {
	return &RoundNAction{
		DiscardIdx: discardIdx,
		Kept:       kept,
		Placement:  placement,
	}
}

// TestRnRuleTopMustAllowFantasy_R2_NoFantasy_Filter — R2 摆完 top 3 张但不能 fantasy → filter (return false)
// case 44/50 之前的硬过滤行为, 但只对 R2-R3 生效.
func TestRnRuleTopMustAllowFantasy_R2_NoFantasy_Filter(t *testing.T) {
	gs := NewGameState(0)
	gs.Round = 2
	gs.Top = []Card{mustParse("3h"), mustParse("4s")} // top 已有 2 张低牌
	dealt := []Card{mustParse("7h"), mustParse("8h"), mustParse("Kc")}
	// Action: 弃 Kc, 7h → top (现 top 3 张 = 3h 4s 7h, 没 fantasy 可达), 8h → mid
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	if rnRuleTopMustAllowFantasy(action, dealt, gs) {
		t.Errorf("R2 top=[3h 4s 7h] no fantasy: rule should filter, got pass")
	}
}

// TestRnRuleTopMustAllowFantasy_R4_NoFantasy_Skip — R4 round-gated skip, 即使 top 不能 fantasy 也 pass
// case 44 修复: R4 让 NN 自由决定 (例: 放弃 top fantasy 走 mid flush-draw 避 foul)
func TestRnRuleTopMustAllowFantasy_R4_NoFantasy_Skip(t *testing.T) {
	gs := NewGameState(0)
	gs.Round = 4
	gs.Top = []Card{mustParse("Ah"), mustParse("Kc")}
	dealt := []Card{mustParse("2s"), mustParse("9h"), mustParse("2d")}
	// Action: 弃 2s, 2d → top (top = AKQ变A-K-2 完, A high 无 fantasy), 9h → bot
	action := makeRNAction(0, []Card{dealt[2], dealt[1]}, Placement{RowTop, RowBottom})
	if !rnRuleTopMustAllowFantasy(action, dealt, gs) {
		t.Errorf("R4 round-gated: rule must skip filtering (R>=4), got blocked")
	}
}

// TestRnRuleTopMustAllowFantasy_R5_NoFantasy_Skip — R5 也 skip (case 50)
func TestRnRuleTopMustAllowFantasy_R5_NoFantasy_Skip(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 5
	gs.Top = []Card{mustParse("X"), mustParse("2c")} // joker + 2c (state)
	dealt := []Card{mustParse("As"), mustParse("8s"), mustParse("7h")}
	// Action: 弃 As, 7h → top (现 top = X 2c 7h, joker as 7 = 77 pair, 不是 QQ+ fantasy), 8s → bot
	action := makeRNAction(0, []Card{dealt[2], dealt[1]}, Placement{RowTop, RowBottom})
	if !rnRuleTopMustAllowFantasy(action, dealt, gs) {
		t.Errorf("R5 round-gated: rule must skip filtering (R>=4), got blocked")
	}
}

// TestRnRuleTopMustAllowFantasy_R3_Fantasy_Pass — R3 top 可达 fantasy → pass (joker + K)
func TestRnRuleTopMustAllowFantasy_R3_Fantasy_Pass(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 3
	gs.Top = []Card{mustParse("X")} // joker on top
	dealt := []Card{mustParse("Kh"), mustParse("3d"), mustParse("4c")}
	// Action: 弃 4c, Kh → top (top = X + Kh + ?, joker wild → KK pair = fantasy 可达), 3d → mid
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	if !rnRuleTopMustAllowFantasy(action, dealt, gs) {
		t.Errorf("R3 top=[X Kh] (KK fantasy possible): rule should pass, got filter")
	}
}

// applyRNAction — 模拟 expert_place.go ExpertPlace3 line 319-324, 构 postState
// 2026-05-22 重构: 软规则函数改吃 postState, 测试要自己 build.
func applyRNAction(state *GameState, action *RoundNAction, dealt []Card) *GameState {
	post := state.Clone()
	post.UsedCards[dealt[action.DiscardIdx].ID()] = true
	post.SetDiscard(dealt[action.DiscardIdx])
	for k, c := range action.Kept {
		post.PlaceCard(c, action.Placement[k])
	}
	return post
}

// ============ RnJokerWithHighOnTopBonus tests ============

// TestRnJokerWithHigh_Case32 — case 32: state.Top=[Kc], action 放 joker → top=[Kc, joker] = KK fantasy → +10
func TestRnJokerWithHigh_Case32(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("Kc")}
	dealt := []Card{mustParse("X"), mustParse("X"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokerWithHighOnTopBonus(action, post, FoulImminentPenalty(post)); got != 10 {
		t.Errorf("case 32 K+joker fantasy: got %v, want 10", got)
	}
}

// TestRnJokerWithHigh_Case36 — case 36: state.Top=[joker], action 放 As → top=[joker, As] = AA fantasy → +10
func TestRnJokerWithHigh_Case36(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("X")}
	dealt := []Card{mustParse("4d"), mustParse("As"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[1], dealt[0]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokerWithHighOnTopBonus(action, post, FoulImminentPenalty(post)); got != 10 {
		t.Errorf("case 36 joker+A fantasy: got %v, want 10", got)
	}
}

// TestRnJokerWithHigh_NoTopPlace — action 没在 top 摆任何牌 → 0 (即使 state.Top 已有 joker+A)
func TestRnJokerWithHigh_NoTopPlace(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 3
	gs.Top = []Card{mustParse("X"), mustParse("As")} // 已 locked
	dealt := []Card{mustParse("4d"), mustParse("5c"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowMiddle, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokerWithHighOnTopBonus(action, post, FoulImminentPenalty(post)); got != 0 {
		t.Errorf("no top placement: got %v, want 0 (避免无差别加分)", got)
	}
}

// TestRnJokerWithHigh_NoHigh_NoBonus — joker 上 top 但 partner 是低牌 → 0
func TestRnJokerWithHigh_NoHigh_NoBonus(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("3c")} // 低牌
	dealt := []Card{mustParse("X"), mustParse("5d"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnJokerWithHighOnTopBonus(action, post, FoulImminentPenalty(post)); got != 0 {
		t.Errorf("joker + 3c (low): got %v, want 0", got)
	}
}

// ============ RnSingleAOnTopBonus tests ============

// TestRnSingleA_Case29 — case 29: state.Top=[Kh] (no joker), action 放 Ac → top=[Kh, Ac] → +10
func TestRnSingleA_Case29(t *testing.T) {
	gs := NewGameState(0)
	gs.Round = 2
	gs.Top = []Card{mustParse("Kh")}
	dealt := []Card{mustParse("2s"), mustParse("7d"), mustParse("Ac")}
	// Action: 弃 7d, Ac → top, 2s → mid
	action := makeRNAction(1, []Card{dealt[2], dealt[0]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnSingleAOnTopBonus(action, post, FoulImminentPenalty(post)); got != 10 {
		t.Errorf("case 29 A on top (no joker): got %v, want 10", got)
	}
}

// TestRnSingleA_StateHasJoker — state.Top 已有 joker → 0 (走 JokerWithHigh, 不重复)
func TestRnSingleA_StateHasJoker(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 2
	gs.Top = []Card{mustParse("X")} // 已有 joker
	dealt := []Card{mustParse("4d"), mustParse("As"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[1], dealt[0]}, Placement{RowTop, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnSingleAOnTopBonus(action, post, FoulImminentPenalty(post)); got != 0 {
		t.Errorf("state has joker on top: got %v, want 0 (走 JokerWithHigh)", got)
	}
}

// TestRnSingleA_NoTop — A 不上 top → 0
func TestRnSingleA_NoTop(t *testing.T) {
	gs := NewGameState(0)
	gs.Round = 2
	gs.Top = []Card{mustParse("Kh")}
	dealt := []Card{mustParse("2s"), mustParse("7d"), mustParse("Ac")}
	// Action: 弃 7d, Ac → bot, 2s → mid
	action := makeRNAction(1, []Card{dealt[2], dealt[0]}, Placement{RowBottom, RowMiddle})
	post := applyRNAction(gs, action, dealt)
	if got := RnSingleAOnTopBonus(action, post, FoulImminentPenalty(post)); got != 0 {
		t.Errorf("A not on top: got %v, want 0", got)
	}
}

// ============ RnTopCapBlockedFantasyPenalty tests ============

// TestRnTopCapBlocked_Case50_PairLow — case 50 R5: mid full KK, top [X 2c], action 把 As 摆 top
// post top [X 2c As] cap-aware = pair-2 (joker 被 KK cap, 凑不到 AA) → 浪费 → +5 penalty
func TestRnTopCapBlocked_Case50_PairLow(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 5
	gs.Top = []Card{mustParse("X"), mustParse("2c")}
	gs.Middle = []Card{mustParse("Kh"), mustParse("Kd"), mustParse("Qs"), mustParse("Jh"), mustParse("Th")}
	gs.Bottom = []Card{mustParse("3s"), mustParse("4s"), mustParse("5s"), mustParse("6h")}
	dealt := []Card{mustParse("As"), mustParse("7h"), mustParse("8s")}
	// Action: 弃 8s, As → top, 7h → bot
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnTopCapBlockedFantasyPenalty(action, post); got != 5 {
		t.Errorf("case 50 cap-blocked pair-2: got %v, want 5", got)
	}
}

// TestRnTopCapBlocked_Case50_PairHigh — case 50 R5: 同 state, 改 action: 弃 8s, 7h → top, As → bot
// post top [X 2c 7h] cap-aware = pair-7 (≥ Rank6, +2 royalty) → 不浪费 → 0
func TestRnTopCapBlocked_Case50_PairHigh(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 5
	gs.Top = []Card{mustParse("X"), mustParse("2c")}
	gs.Middle = []Card{mustParse("Kh"), mustParse("Kd"), mustParse("Qs"), mustParse("Jh"), mustParse("Th")}
	gs.Bottom = []Card{mustParse("3s"), mustParse("4s"), mustParse("5s"), mustParse("6h")}
	dealt := []Card{mustParse("As"), mustParse("7h"), mustParse("8s")}
	// Action: 弃 8s, 7h → top, As → bot
	action := makeRNAction(2, []Card{dealt[1], dealt[0]}, Placement{RowTop, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnTopCapBlockedFantasyPenalty(action, post); got != 0 {
		t.Errorf("case 50 cap pair-7 (royalty +2): got %v, want 0", got)
	}
}

// TestRnTopCapBlocked_NoTopPlace — action 不放 top → 0 (R3 state, top 1 张, action mid+bot)
func TestRnTopCapBlocked_NoTopPlace(t *testing.T) {
	gs := NewGameState(2)
	gs.Round = 3
	gs.Top = []Card{mustParse("Kc")}
	dealt := []Card{mustParse("3h"), mustParse("5d"), mustParse("8h")}
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowMiddle, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnTopCapBlockedFantasyPenalty(action, post); got != 0 {
		t.Errorf("no top placement: got %v, want 0", got)
	}
}

// TestRnTopCapBlocked_NoJokerOnTop — post top 不含 joker → 0 (无 wild 浪费问题)
func TestRnTopCapBlocked_NoJokerOnTop(t *testing.T) {
	gs := NewGameState(0)
	gs.Round = 5
	gs.Top = []Card{mustParse("7h"), mustParse("7s")}
	gs.Middle = []Card{mustParse("Kh"), mustParse("Kd"), mustParse("Qs"), mustParse("Jh"), mustParse("Th")}
	gs.Bottom = []Card{mustParse("3s"), mustParse("4s"), mustParse("5s"), mustParse("6h")}
	dealt := []Card{mustParse("Ad"), mustParse("8c"), mustParse("9s")}
	// Action: 弃 9s, Ad → top, 8c → bot
	action := makeRNAction(2, []Card{dealt[0], dealt[1]}, Placement{RowTop, RowBottom})
	post := applyRNAction(gs, action, dealt)
	if got := RnTopCapBlockedFantasyPenalty(action, post); got != 0 {
		t.Errorf("no joker on top: got %v, want 0", got)
	}
}
