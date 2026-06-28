package ofc

import "os"

// 2026-06-28 实验 (用户): OFC_ZERO_USEDCARDS=1 → 特征构建时把 usedCards 重置成"只剩本盘 board".
//   去掉 discards / 对手 / 已发牌 的 deck 感知 (board 牌肯定 used, 保留). 测 deck-awareness 对基本功
//   是帮还是噪声. 影响所有 usedCards 依赖特征: G组(牌堆剩余) / X组(cardsSeen概率) / computeDeckRemaining
//   (maxAchievable/topTripsSeed/W组 的 outs). 仅设 flag 时生效.
var zeroUsedCardsAblation = os.Getenv("OFC_ZERO_USEDCARDS") == "1"

// gsBoardOnlyUsed — 返回 usedCards 只含 board 牌的克隆 (deck 其余视为满).
func gsBoardOnlyUsed(gs *GameState) *GameState {
	g := gs.Clone()
	g.UsedCards = make(map[string]bool)
	for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
		for _, c := range row {
			g.UsedCards[c.ID()] = true
		}
	}
	return g
}
