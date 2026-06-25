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

// RnSingleAOnTopBonus tests 已删 (2026-06-13): 规则退休 (case 29 太子自学 / case 46 放宽 / 帮不到手2).

