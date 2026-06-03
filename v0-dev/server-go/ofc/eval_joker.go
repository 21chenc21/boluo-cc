package ofc

// Evaluate5Joker — 5 张牌, 含 0..5 张鬼牌. 鬼牌补成最强可能 (无 cap).
func Evaluate5Joker(cards []Card) HandValue {
	return Evaluate5JokerCap(cards, nil)
}

// Evaluate5JokerCap — 带 cap 的 5 张评估, 鬼牌降级到 ≤ cap (避免犯规)
// cap = nil 表示无限制
func Evaluate5JokerCap(cards []Card, cap *HandValue) HandValue {
	if len(cards) != 5 {
		return HandValue{Type: -1, Value: 0}
	}
	var real []Card
	k := 0
	for _, c := range cards {
		if c.IsJoker() {
			k++
		} else {
			real = append(real, c)
		}
	}
	if k == 0 {
		r := Evaluate5(cards)
		if cap != nil && HandExceeds5(r, *cap) {
			// 无鬼牌不能降级, 标记 overCap (用 Type=-1 表达 "犯规候选")
			return HandValue{Type: -2, Value: r.Value}
		}
		return r
	}
	if k == 5 && cap == nil {
		return HandValue{Type: TypeRoyalFlush, Value: makeValue(TypeRoyalFlush, 12)}
	}
	return analyticalEval5(real, k, cap)
}

// Evaluate3Joker — 3 张, 含鬼 (无 cap)
func Evaluate3Joker(cards []Card) HandValue {
	return Evaluate3JokerCap(cards, nil)
}

// Evaluate3JokerCap — 带 cap 的 3 张评估, 鬼牌降级到 ≤ cap
func Evaluate3JokerCap(cards []Card, cap *HandValue) HandValue {
	if len(cards) != 3 {
		return HandValue{Type: -1, Value: 0}
	}
	var real []Card
	k := 0
	for _, c := range cards {
		if c.IsJoker() {
			k++
		} else {
			real = append(real, c)
		}
	}
	if k == 0 {
		r := Evaluate3(cards)
		if cap != nil && TopExceedsMid(r, *cap) {
			return HandValue{Type: -2, Value: r.Value}
		}
		return r
	}
	return analyticalEval3(real, k, cap)
}

// analyticalEval3 — k≥1 鬼牌, 3 张顶道分析式评估, 可选 cap
// 编码: high=r0*225+r1*15+r2, pair=1e6+pair*15+kicker, trips=3e6+trip*15
func analyticalEval3(real []Card, k int, cap *HandValue) HandValue {
	var rankCnt [13]int
	for _, c := range real {
		rankCnt[c.Rank()]++
	}
	var realRanks []int
	for r := 12; r >= 0; r-- {
		if rankCnt[r] > 0 {
			for i := 0; i < rankCnt[r]; i++ {
				realRanks = append(realRanks, r)
			}
		}
	}
	allDistinct := true
	{
		var seen [13]bool
		for _, r := range realRanks {
			if seen[r] {
				allDistinct = false
				break
			}
			seen[r] = true
		}
	}

	bestType := -1
	bestValue := int64(-1)
	update := func(t int, v int64) {
		// cap check: 候选 (t, v) 必须 ≤ cap
		if cap != nil {
			cand := HandValue{Type: t, Value: v}
			if TopExceedsMid(cand, *cap) {
				return
			}
		}
		if t > bestType || (t == bestType && v > bestValue) {
			bestType = t
			bestValue = v
		}
	}

	// 三条 (type 3)
	for tr := 12; tr >= 0; tr-- {
		got := rankCnt[tr]
		need := 3 - got
		if need < 0 || need > k {
			continue
		}
		// 其余 reals 必须无 (否则多余卡)
		if len(real)-got > 0 {
			continue
		}
		update(TypeThreeOfAKind, int64(TypeThreeOfAKind)*1000000+int64(tr)*15)
	}

	// 对子 (type 1)
	for pr := 12; pr >= 0; pr-- {
		gotP := rankCnt[pr]
		if gotP > 2 {
			continue
		}
		need := 2 - gotP
		if need > k {
			continue
		}
		rOther := len(real) - gotP
		if rOther+(k-need) != 1 {
			continue
		}
		var kicker int
		if rOther == 1 {
			// 找 ≠ pr 的 real rank
			for _, rk := range realRanks {
				if rk != pr {
					kicker = rk
					break
				}
			}
		} else {
			if pr == 12 {
				kicker = 11
			} else {
				kicker = 12
			}
		}
		update(TypePair, int64(TypePair)*1000000+int64(pr)*15+int64(kicker))
	}

	// 高牌 (fallback)
	if bestType < 0 && allDistinct {
		ranks := append([]int(nil), realRanks...)
		for r := 12; r >= 0 && len(ranks) < 3; r-- {
			if rankCnt[r] == 0 {
				ranks = append(ranks, r)
			}
		}
		// sort desc
		for i := 0; i < len(ranks); i++ {
			for j := i + 1; j < len(ranks); j++ {
				if ranks[i] < ranks[j] {
					ranks[i], ranks[j] = ranks[j], ranks[i]
				}
			}
		}
		if len(ranks) >= 3 {
			update(TypeHighCard, int64(ranks[0])*225+int64(ranks[1])*15+int64(ranks[2]))
		}
	}

	if bestType < 0 {
		// cap 排除了所有候选 → overCap (foul)
		if cap != nil {
			return HandValue{Type: -2, Value: 0}
		}
		// 无 cap 时全鬼 3 张默认 AAA
		return HandValue{Type: TypeThreeOfAKind, Value: int64(TypeThreeOfAKind)*1000000 + 12*15}
	}
	return HandValue{Type: bestType, Value: bestValue}
}

// analyticalEval5 — k≥1 鬼牌, 5 张分析式评估, 可选 cap (≤ cap 的最大候选)
func analyticalEval5(real []Card, k int, cap *HandValue) HandValue {
	var rankCnt [13]int
	var suitCnt [4]int
	for _, c := range real {
		rankCnt[c.Rank()]++
		suitCnt[c.Suit()]++
	}
	var realRanks []int
	var realSuits []int
	for _, c := range real {
		realRanks = append(realRanks, int(c.Rank()))
		realSuits = append(realSuits, int(c.Suit()))
	}
	var realRankSet [13]bool
	for _, r := range realRanks {
		realRankSet[r] = true
	}
	allDistinct := true
	{
		var seen [13]bool
		for _, r := range realRanks {
			if seen[r] {
				allDistinct = false
				break
			}
			seen[r] = true
		}
	}

	bestType := -1
	bestValue := int64(-1)
	update := func(t int, v int64) {
		if cap != nil {
			cand := HandValue{Type: t, Value: v}
			if HandExceeds5(cand, *cap) {
				return
			}
		}
		if t > bestType || (t == bestType && v > bestValue) {
			bestType = t
			bestValue = v
		}
	}

	// === Royal Flush (type 9) ===
	for s := 0; s < 4; s++ {
		// real 是否全在 suit s 且 rank ≥ 8
		ok := true
		for i, sui := range realSuits {
			if sui != s {
				ok = false
				break
			}
			if realRanks[i] < 8 {
				ok = false
				break
			}
		}
		if !ok || !allDistinct {
			continue
		}
		if 5-len(real) <= k {
			update(TypeRoyalFlush, makeValue(TypeRoyalFlush, 12))
			break
		}
	}

	// === Straight Flush (type 8) ===
	for s := 0; s < 4; s++ {
		ok := true
		for _, sui := range realSuits {
			if sui != s {
				ok = false
				break
			}
		}
		if !ok || !allDistinct {
			continue
		}
		for high := 4; high <= 12; high++ {
			// sfRanks = high-4..high
			allIn := true
			for _, r := range realRanks {
				if r < high-4 || r > high {
					allIn = false
					break
				}
			}
			if !allIn {
				continue
			}
			if 5-len(real) > k {
				continue
			}
			update(TypeStraightFlush, makeValue(TypeStraightFlush, high))
		}
		// Wheel (A2345)
		wheelRanks := map[int]bool{0: true, 1: true, 2: true, 3: true, 12: true}
		allIn := true
		for _, r := range realRanks {
			if !wheelRanks[r] {
				allIn = false
				break
			}
		}
		if allIn && 5-len(real) <= k {
			update(TypeStraightFlush, makeValue(TypeStraightFlush, 3))
		}
	}

	// === Four of a Kind (type 7) ===
	for r := 12; r >= 0; r-- {
		got := rankCnt[r]
		need := 4 - got
		if need < 0 || need > k {
			continue
		}
		remainingK := k - need
		realNotR := len(real) - got
		if realNotR+remainingK != 1 {
			continue
		}
		var kicker int
		if realNotR == 1 {
			for _, rk := range realRanks {
				if rk != r {
					kicker = rk
					break
				}
			}
		} else {
			if r == 12 {
				kicker = 11
			} else {
				kicker = 12
			}
		}
		update(TypeFourOfAKind, makeValue(TypeFourOfAKind, r, kicker))
	}

	// === Full House (type 6) ===
	for tr := 12; tr >= 0; tr-- {
		for pr := 12; pr >= 0; pr-- {
			if tr == pr {
				continue
			}
			gotT := rankCnt[tr]
			gotP := rankCnt[pr]
			if gotT > 3 || gotP > 2 {
				continue
			}
			needT := 3 - gotT
			needP := 2 - gotP
			if needT+needP > k {
				continue
			}
			if len(real)-gotT-gotP > 0 {
				continue
			}
			update(TypeFullHouse, makeValue(TypeFullHouse, tr, pr))
		}
	}

	// === Flush (type 5) ===
	for s := 0; s < 4; s++ {
		ok := true
		for _, sui := range realSuits {
			if sui != s {
				ok = false
				break
			}
		}
		if !ok || !allDistinct {
			continue
		}
		nFill := 5 - len(real)
		if nFill > k {
			continue
		}
		// 2026-06-03 fix: 枚举 joker 补位的所有 distinct unused-rank 组合 (不止贪心最高).
		// 旧代码只试 joker=最高未用 rank, 当它超 cap (e.g. joker=A → A-high flush > cap K-high flush)
		// 就整个 flush 候选被 update() 拒绝 → 错降级成 pair, 丢中道 flush royalty.
		// 改成枚举所有补位, update() 会保留 ≤ cap 的最高 flush (joker=K → K8732 ≤ K-high club flush).
		// (ypk-178127178-8 R4: 中道 joker 红桃 flush vs 底道 K-high 梅花 flush)
		emit := func(fill []int) {
			ranks := append([]int(nil), realRanks...)
			ranks = append(ranks, fill...)
			for i := 0; i < len(ranks); i++ {
				for j := i + 1; j < len(ranks); j++ {
					if ranks[i] < ranks[j] {
						ranks[i], ranks[j] = ranks[j], ranks[i]
					}
				}
			}
			update(TypeFlush, makeValue(TypeFlush, ranks[0], ranks[1], ranks[2], ranks[3], ranks[4]))
		}
		if nFill == 0 {
			emit(nil)
		} else {
			unused := []int{}
			for r := 12; r >= 0; r-- {
				if !realRankSet[r] {
					unused = append(unused, r)
				}
			}
			comb := make([]int, nFill)
			var rec func(start, idx int)
			rec = func(start, idx int) {
				if idx == nFill {
					emit(comb)
					return
				}
				for i := start; i < len(unused); i++ {
					comb[idx] = unused[i]
					rec(i+1, idx+1)
				}
			}
			rec(0, 0)
		}
	}

	// === Straight (type 4) ===
	if allDistinct {
		for high := 4; high <= 12; high++ {
			allIn := true
			for _, r := range realRanks {
				if r < high-4 || r > high {
					allIn = false
					break
				}
			}
			if !allIn {
				continue
			}
			if 5-len(real) > k {
				continue
			}
			update(TypeStraight, makeValue(TypeStraight, high))
		}
		// Wheel
		wheelRanks := map[int]bool{0: true, 1: true, 2: true, 3: true, 12: true}
		allIn := true
		for _, r := range realRanks {
			if !wheelRanks[r] {
				allIn = false
				break
			}
		}
		if allIn && 5-len(real) <= k {
			update(TypeStraight, makeValue(TypeStraight, 3))
		}
	}

	// === Three of a Kind (type 3) ===
	for tr := 12; tr >= 0; tr-- {
		gotT := rankCnt[tr]
		needT := 3 - gotT
		if needT < 0 || needT > k {
			continue
		}
		remainingK := k - needT
		realOther := len(real) - gotT
		if realOther+remainingK != 2 {
			continue
		}
		var otherRs []int
		for _, rk := range realRanks {
			if rk != tr {
				otherRs = append(otherRs, rk)
			}
		}
		// other reals must be distinct
		distinct := true
		{
			var seen [13]bool
			for _, r := range otherRs {
				if seen[r] {
					distinct = false
					break
				}
				seen[r] = true
			}
		}
		if !distinct {
			continue
		}
		kickers := append([]int(nil), otherRs...)
		for r := 12; r >= 0 && len(kickers) < 2; r-- {
			if r == tr {
				continue
			}
			has := false
			for _, kk := range kickers {
				if kk == r {
					has = true
					break
				}
			}
			if has {
				continue
			}
			kickers = append(kickers, r)
		}
		if len(kickers) != 2 {
			continue
		}
		// sort desc
		if kickers[0] < kickers[1] {
			kickers[0], kickers[1] = kickers[1], kickers[0]
		}
		update(TypeThreeOfAKind, makeValue(TypeThreeOfAKind, tr, kickers[0], kickers[1]))
	}

	// === Two Pair (type 2) ===
	for pr1 := 12; pr1 >= 0; pr1-- {
		for pr2 := pr1 - 1; pr2 >= 0; pr2-- {
			g1 := rankCnt[pr1]
			g2 := rankCnt[pr2]
			if g1 > 2 || g2 > 2 {
				continue
			}
			need := (2 - g1) + (2 - g2)
			if need > k {
				continue
			}
			rOther := len(real) - g1 - g2
			if rOther+(k-need) != 1 {
				continue
			}
			kicker := -1
			if rOther == 1 {
				for _, rk := range realRanks {
					if rk != pr1 && rk != pr2 {
						kicker = rk
						break
					}
				}
			} else {
				for r := 12; r >= 0; r-- {
					if r != pr1 && r != pr2 {
						kicker = r
						break
					}
				}
			}
			update(TypeTwoPair, makeValue(TypeTwoPair, pr1, pr2, kicker))
		}
	}

	// === Pair (type 1) ===
	for pr := 12; pr >= 0; pr-- {
		gP := rankCnt[pr]
		if gP > 2 {
			continue
		}
		need := 2 - gP
		if need > k {
			continue
		}
		rOther := len(real) - gP
		if rOther+(k-need) != 3 {
			continue
		}
		var otherRs []int
		for _, rk := range realRanks {
			if rk != pr {
				otherRs = append(otherRs, rk)
			}
		}
		distinct := true
		{
			var seen [13]bool
			for _, r := range otherRs {
				if seen[r] {
					distinct = false
					break
				}
				seen[r] = true
			}
		}
		if !distinct {
			continue
		}
		kickers := append([]int(nil), otherRs...)
		for r := 12; r >= 0 && len(kickers) < 3; r-- {
			if r == pr {
				continue
			}
			has := false
			for _, kk := range kickers {
				if kk == r {
					has = true
					break
				}
			}
			if has {
				continue
			}
			kickers = append(kickers, r)
		}
		if len(kickers) != 3 {
			continue
		}
		// sort desc (3 elements, simple)
		for i := 0; i < 3; i++ {
			for j := i + 1; j < 3; j++ {
				if kickers[i] < kickers[j] {
					kickers[i], kickers[j] = kickers[j], kickers[i]
				}
			}
		}
		update(TypePair, makeValue(TypePair, pr, kickers[0], kickers[1], kickers[2]))
	}

	// === High Card (fallback, only if no other type matched) ===
	if bestType < 0 && allDistinct {
		ranks := append([]int(nil), realRanks...)
		for r := 12; r >= 0 && len(ranks) < 5; r-- {
			if !realRankSet[r] {
				ranks = append(ranks, r)
			}
		}
		for i := 0; i < len(ranks); i++ {
			for j := i + 1; j < len(ranks); j++ {
				if ranks[i] < ranks[j] {
					ranks[i], ranks[j] = ranks[j], ranks[i]
				}
			}
		}
		if len(ranks) >= 5 {
			update(TypeHighCard, makeValue(TypeHighCard, ranks[0], ranks[1], ranks[2], ranks[3], ranks[4]))
		}
	}

	if bestType < 0 {
		if cap != nil {
			return HandValue{Type: -2, Value: 0}
		}
		// 极端兜底, 不应到
		return HandValue{Type: TypeHighCard, Value: makeValue(TypeHighCard, 12, 11, 10, 9, 8)}
	}
	return HandValue{Type: bestType, Value: bestValue}
}
