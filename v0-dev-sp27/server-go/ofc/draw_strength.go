package ofc

// 「draw 强度」特征 Z2 组 (sp28, 2026-06-21, dim157-159). 2026-06-28 改纯花.
//
// 背景: "低估顺/花draw"那批 (104/44/11/99). 原版 = 最佳5连窗真牌数(顺) 与 最大同花数(花) 取大.
// 2026-06-28 用户(#104): 顺的 flat-count(distinct rank数)**不分档** — 把 3,4,5(真开口) 和
//   3,5,6(双卡顺垃圾) 都算 3, 抹平好坏, 误导 NN. 而**顺难度分档我们已有方法**: B组
//   rowStraightTightness(1/tier) + pRowStraight(挑牌概率). 所以这里**只留花**(花无分档特征),
//   顺全交给已分档的 B组 + pRowStraight, 去重 + 去误导.
// 花强度 = 最大同花数, 且缺 (5-同花) 张必须 ≤ slots 才完成得了 (#38 slots 修保留). 满行无 draw.

func rowDrawStrength(row []Card, capacity int) int {
	if len(row) >= capacity {
		return 0 // 满行成手已定, 无 draw
	}
	slots := capacity - len(row) // 还能补几张
	var sc [4]int
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++ // 2026-06-29 (#51): 鬼是 wild 当任意花色 → 计入最佳门花draw. 原 bug: skip鬼 漏算 底2梅花+鬼=花draw
		} else {
			sc[int(c.Suit())]++
		}
	}
	bestFl := 0
	for s := 0; s < 4; s++ {
		eff := sc[s] + jokers               // 鬼补到该门
		if eff > bestFl && 5-eff <= slots { // 缺(5-有效同花数)必须 ≤ slots
			bestFl = eff
		}
	}
	return bestFl
}

// fillDrawStrength — 每行 draw 强度 /5 (dim157-159: top/mid/bot). 越高=顺/花 potential 越强.
func fillDrawStrength(f []float32, gs *GameState) {
	f[0] = clampF(float32(rowDrawStrength(gs.Top, 3))/5.0, 0, 1)
	f[1] = clampF(float32(rowDrawStrength(gs.Middle, 5))/5.0, 0, 1)
	f[2] = clampF(float32(rowDrawStrength(gs.Bottom, 5))/5.0, 0, 1)
}
