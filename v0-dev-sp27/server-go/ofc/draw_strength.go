package ofc

// 「draw 强度」特征 Z2 组 (sp28, 2026-06-21, dim157-159).
//
// 背景: "低估顺/花draw"那批 (104/44/11/99) 验证为**权重**问题 —— 顺/花潜力其实被概率特征
// f[69:90] 编码了, 但 value-head 给的权重不够 (概率值小, 弱draw 才 0.057). 借鉴 W-rescale 思路:
// 给一个**更显著的 count-based draw 强度**信号 (非小概率), 让 value-head 更难忽略.
// 位置加权(底>中>顶)验证对齐 104/44/11/99 (4/5). 这里给 per-row 强度, 由 NN 学位置权重
// (board-state f[0:8] 有各行牌数, NN 可去 card-count 混淆).
//
// 强度 = 最佳5连窗内真牌数(顺potential) 与 最大同花数(花potential) 取大. 满行(成手定型)给当前成手不再算draw.

func rowDrawStrength(row []Card, capacity int) int {
	if len(row) >= capacity {
		return 0 // 满行成手已定, 无 draw
	}
	var rc [13]int
	var sc [4]int
	for _, c := range row {
		if !c.IsJoker() {
			rc[c.Rank()]++
			sc[int(c.Suit())]++
		}
	}
	bestStr := 0
	for hi := 4; hi <= 12; hi++ {
		lo := hi - 4
		cnt := 0
		for k := lo; k <= hi; k++ {
			if rc[k] >= 1 {
				cnt++
			}
		}
		if cnt > bestStr {
			bestStr = cnt
		}
	}
	cnt := 0 // 轮子 A-2-3-4-5
	for _, k := range []int{12, 0, 1, 2, 3} {
		if rc[k] >= 1 {
			cnt++
		}
	}
	if cnt > bestStr {
		bestStr = cnt
	}
	bestFl := 0
	for s := 0; s < 4; s++ {
		if sc[s] > bestFl {
			bestFl = sc[s]
		}
	}
	if bestFl > bestStr {
		return bestFl
	}
	return bestStr
}

// fillDrawStrength — 每行 draw 强度 /5 (dim157-159: top/mid/bot). 越高=顺/花 potential 越强.
func fillDrawStrength(f []float32, gs *GameState) {
	f[0] = clampF(float32(rowDrawStrength(gs.Top, 3))/5.0, 0, 1)
	f[1] = clampF(float32(rowDrawStrength(gs.Middle, 5))/5.0, 0, 1)
	f[2] = clampF(float32(rowDrawStrength(gs.Bottom, 5))/5.0, 0, 1)
}
