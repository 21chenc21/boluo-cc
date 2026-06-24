package ofc

import "testing"

// 2026-06-23 (用户, ypk-19398986-5 R2): 底仅2张(刚成对) 且 底对rank < 本轮放进中道的真牌rank
//   且中道是"卡顺"(顺面非连张) → 罚 -2. 中[6c 4d 8c]=4-6-8 内部gap=卡顺, 底[7s 7d]对7, 8>7 → -2.
func TestMidDrawFace_ThinBot_Gutshot_Penalize(t *testing.T) {
	gs := st([]string{}, []string{"6c", "4d", "8c"}, []string{"7s", "7d"})
	dealt := parseHand("Tc", "8c", "7d") // 8c 进了中道, rank8 > 底对7
	if got := RnMidDrawFaceGated(dealt, gs); got != -2 {
		t.Fatalf("底2张对7 + 中道放8c(8>7) + 中卡顺4-6-8 应罚-2, got %v", got)
	}
}

// thin-bot 触发但中道是"连张顺面"(6-7-8 连续) → 不是卡顺, 仅 return 0 (不奖不罚).
func TestMidDrawFace_ThinBot_RunNotGutshot_Zero(t *testing.T) {
	gs := st([]string{}, []string{"6c", "7d", "8c"}, []string{"3s", "3d"})
	dealt := parseHand("Tc", "8c", "9d") // 8c 进中道 rank8 > 底对3, 但中6-7-8连张
	if got := RnMidDrawFaceGated(dealt, gs); got != 0 {
		t.Fatalf("中6-7-8连张(非卡顺) thin-bot 应 return 0(不罚), got %v", got)
	}
}

// 底对 >= 放进中道的牌 → 不触发守护, 照奖 +2.
func TestMidDrawFace_ThinBot_BotPairHigher_Keep(t *testing.T) {
	gs := st([]string{}, []string{"6c", "4d", "8c"}, []string{"9s", "9d"})
	dealt := parseHand("Tc", "8c", "7d") // 8c 进中道 rank8 < 底对9
	if got := RnMidDrawFaceGated(dealt, gs); got != 2 {
		t.Fatalf("底对9 >= 中道8 应照奖2, got %v", got)
	}
}

// 底道 3 张(非2张) → 守护不触发, 照奖.
func TestMidDrawFace_ThinBot_BotThreeCards_Keep(t *testing.T) {
	gs := st([]string{}, []string{"6c", "4d", "8c"}, []string{"7s", "7d", "2h"})
	dealt := parseHand("Tc", "8c", "7d")
	if got := RnMidDrawFaceGated(dealt, gs); got != 2 {
		t.Fatalf("底3张 守护不触发 应照奖2, got %v", got)
	}
}

// 底道 2 张但未成对 → 无"底对"可比, 守护不触发, 照奖.
func TestMidDrawFace_ThinBot_BotNoPair_Keep(t *testing.T) {
	gs := st([]string{}, []string{"6c", "4d", "8c"}, []string{"7s", "9d"})
	dealt := parseHand("Tc", "8c", "7d")
	if got := RnMidDrawFaceGated(dealt, gs); got != 2 {
		t.Fatalf("底2张无对 守护不触发 应照奖2, got %v", got)
	}
}
