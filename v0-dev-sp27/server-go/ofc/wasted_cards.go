package ofc

// 「死牌 / 放牌效率」特征 Z 组 (sp28, 2026-06-21, dim154-156).
//
// 背景 (用户 2026-06-21 失败case注释主线): value-head 低估"顺/draw 发育", 实为
// "没把低牌放在能发育的行 / 用死低牌污染了强行". 关键概念是 **放牌效率**, 不是"数 outs"
// (数 outs 被槽数污染: 4-5 比 2-4-5 outs 多只因少一张牌, 非更好).
//
// 定义: **未满行**里一张真牌若 ① 不成对(行内同 rank ≥2) ② 不在顺draw(含自己的某5连窗内真牌≥3)
//   ③ 不在花draw(行内同花 ≥3), 就是"死牌"—— 占了发育槽却不贡献 (早期尤其浪费).
// 满行跳过 (成手已定型, 无"该不该放"的决策).
//
// 实测 (AI首选 vs 期望): 64 AI中[3-6-K]全死 vs exp中[3-4-6]顺draw 0死 (Δ3);
//   75 AI底[TT+死2] vs exp底干净[TT] (Δ3). 平不反: 76/11/35/44 全 Δ0. → 干净不反向.

// wastedInRow — 未满行内死牌数. 满行返回 0.
// isTop: 顶行的 Q/K/A 单张是范种子(种 QQ+/AA), 不算死牌 (修 99 Ks 顶种子被误伤).
func wastedInRow(row []Card, capacity int, isTop bool) int {
	if len(row) >= capacity {
		return 0
	}
	var rc [13]int
	var sc [4]int
	for _, c := range row {
		if !c.IsJoker() {
			rc[c.Rank()]++
			sc[c.Suit()]++
		}
	}
	w := 0
	for _, c := range row {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		s := int(c.Suit())
		if rc[r] >= 2 { // 成对/三条
			continue
		}
		if isTop && r >= int(RankQ) { // 顶上 Q/K/A 单张 = 范种子, 不算死牌
			continue
		}
		if sc[s] >= 3 { // 花draw
			continue
		}
		// 顺draw: 含 r 的某 5-连窗内真牌 ≥3
		straight := false
		for hi := r; hi <= r+4 && hi <= 12; hi++ {
			lo := hi - 4
			if lo < 0 {
				continue
			}
			cnt := 0
			for k := lo; k <= hi; k++ {
				if rc[k] >= 1 {
					cnt++
				}
			}
			if cnt >= 3 {
				straight = true
				break
			}
		}
		if !straight && r <= 3 { // 轮子 A-2-3-4-5
			cnt := 0
			for _, k := range []int{12, 0, 1, 2, 3} {
				if rc[k] >= 1 {
					cnt++
				}
			}
			if cnt >= 3 {
				straight = true
			}
		}
		if straight {
			continue
		}
		w++
	}
	return w
}

// straightDrawTier — 一组无对的 distinct rank 作为顺draw的"紧密度档位"(细化权重, 非二元).
//   档位越高越松/越差: tier = 缺口数 + 1 = span - (count-1) + 1, span = max-min.
//   只在能塞进一个 5-连窗(span<=4)且 count>=2 时有效; 否则返回 0 (非单一顺draw shape).
//   A 可当低(A-2-3-4-5): 含A时取 A高/A低 中更紧(span更小)的.
//   例(用户 spec): 2张 45→1 46→2 47→3 48→4 | 3张 456→1 467→2 468→3 | 4张 4567→1 4578→2.
func straightDrawTier(ranks []int) int {
	seen := map[int]bool{}
	var rs []int
	for _, r := range ranks {
		if !seen[r] {
			seen[r] = true
			rs = append(rs, r)
		}
	}
	if len(rs) < 2 {
		return 0
	}
	tierFor := func(vals []int) int {
		mn, mx := vals[0], vals[0]
		for _, v := range vals {
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
		}
		span := mx - mn
		if span > 4 {
			return 0 // 塞不进5连窗, 非单一顺draw
		}
		return span - (len(vals) - 1) + 1
	}
	best := tierFor(rs)
	if seen[int(RankA)] { // A 当低再试
		lo := make([]int, len(rs))
		for i, r := range rs {
			if r == int(RankA) {
				lo[i] = -1
			} else {
				lo[i] = r
			}
		}
		if t := tierFor(lo); t > 0 && (best == 0 || t < best) {
			best = t
		}
	}
	return best
}

// rowStraightTightness — 行内最紧顺draw的质量 = 1/tier (tier 见 straightDrawTier; 越紧值越高).
//   滑10个顺窗(含A低A2345)找真牌最紧(tier最小)的一组; 鬼可填gap(每鬼减1缺口). 无顺draw→0.
//   连张/紧凑 tier1→1.0, 卡顺 2档→0.5 / 3档→0.33 / 4档→0.25. (B: 细化"是顺就满分"→按紧密度给分.)
func rowStraightTightness(row []Card) float32 {
	var present [13]bool
	jokers := 0
	for _, c := range row {
		if c.IsJoker() {
			jokers++
		} else {
			present[c.Rank()] = true
		}
	}
	windows := [10][5]int{
		{12, 0, 1, 2, 3}, {0, 1, 2, 3, 4}, {1, 2, 3, 4, 5}, {2, 3, 4, 5, 6}, {3, 4, 5, 6, 7},
		{4, 5, 6, 7, 8}, {5, 6, 7, 8, 9}, {6, 7, 8, 9, 10}, {7, 8, 9, 10, 11}, {8, 9, 10, 11, 12},
	}
	bestCnt, bestGaps := 0, 0
	for _, w := range windows {
		first, last, cnt := -1, -1, 0
		for pos, r := range w {
			if present[r] {
				if first < 0 {
					first = pos
				}
				last = pos
				cnt++
			}
		}
		if cnt >= 2 {
			gaps := (last - first) - (cnt - 1) - jokers // 内部空位, 鬼填
			if gaps < 0 {
				gaps = 0
			}
			// 先取真牌最多的窗(最承诺那条顺), 同张数再取最紧 — 避免取了更紧的小子集
			if cnt > bestCnt || (cnt == bestCnt && gaps < bestGaps) {
				bestCnt, bestGaps = cnt, gaps
			}
		}
	}
	if bestCnt == 0 {
		return 0
	}
	tier := bestGaps + 1
	// 4 真张(差1成顺)时看"单边性": 数有几个顺窗能装下全4张. 2窗=开口(tier1), 1窗=单边/卡顺(tier2).
	//   治 A-2-3-4 / J-Q-K-A 这种边缘连张(只一头能接)= 卡顺 2档 (用户 2026-06-25).
	if bestCnt == 4 {
		nWin := 0
		for _, w := range windows {
			cnt := 0
			for _, r := range w {
				if present[r] {
					cnt++
				}
			}
			if cnt == 4 {
				nWin++
			}
		}
		if openTier := 3 - nWin; openTier > tier {
			tier = openTier
		}
	}
	return 1.0 / float32(tier)
}

// fillStraightTightness — B 组(2026-06-25 用户): 中/底 顺draw紧密度质量 (2 dim). 顶不可能成顺, 略.
func fillStraightTightness(f []float32, gs *GameState) {
	f[0] = rowStraightTightness(gs.Middle)
	f[1] = rowStraightTightness(gs.Bottom)
}

// WastedTotal — 全盘死牌总数 (诊断/featdiff 用).
func WastedTotal(gs *GameState) int {
	return wastedInRow(gs.Top, 3, true) + wastedInRow(gs.Middle, 5, false) + wastedInRow(gs.Bottom, 5, false)
}

// fillWastedCards — 每行死牌数 /3 (dim154-156: top/mid/bot). 越低越好 (放牌越有效率).
func fillWastedCards(f []float32, gs *GameState) {
	f[0] = clampF(float32(wastedInRow(gs.Top, 3, true))/3.0, 0, 1)
	f[1] = clampF(float32(wastedInRow(gs.Middle, 5, false))/3.0, 0, 1)
	f[2] = clampF(float32(wastedInRow(gs.Bottom, 5, false))/3.0, 0, 1)
}
