package ofc

// HandExceeds5 — a 是否严格大于 b (两个 5-card hand value 直接比较)
func HandExceeds5(a, b HandValue) bool {
	if a.Type != b.Type {
		return a.Type > b.Type
	}
	return a.Value > b.Value
}

// TopExceedsMid — top (3-card) 是否大于 mid (5-card)
// top 只有 0/1/3 型. value encoding 不同, 需分类比较
func TopExceedsMid(top, mid HandValue) bool {
	if top.Type != mid.Type {
		return top.Type > mid.Type
	}
	switch top.Type {
	case TypeThreeOfAKind:
		// top: 3e6 + tripRank*15
		// mid: 3e6 + tripRank*15^4 + ...
		topTrip := (top.Value - 3000000) / 15
		midTrip := (mid.Value - 3000000) / 50625
		return topTrip > midTrip
	case TypePair:
		// top: 1e6 + pair*15 + kicker
		// mid: 1e6 + pair*15^4 + k1*15^3 + k2*15^2 + k3*15
		topPair := (top.Value - 1000000) / 15
		topKicker := (top.Value - 1000000) % 15
		midPair := (mid.Value - 1000000) / 50625
		if topPair != midPair {
			return topPair > midPair
		}
		midKicker1 := ((mid.Value - 1000000) % 50625) / 3375
		return topKicker > midKicker1
	case TypeHighCard:
		// top: r0*225 + r1*15 + r2
		// mid: r0*15^4 + ...
		topR0 := top.Value / 225
		midR0 := mid.Value / 50625
		return topR0 > midR0
	}
	return false
}

// IsFoul — 完整 0-joker board 犯规判定 (3+5+5).
// 与 JS isFoul 完全一致, 包括 HIGH_CARD/PAIR 逐张 kicker 比较
func IsFoul(top, middle, bottom []Card) bool {
	if len(top) != 3 || len(middle) != 5 || len(bottom) != 5 {
		return true
	}
	te := Evaluate3(top)
	me := Evaluate5(middle)
	be := Evaluate5(bottom)
	if HandExceeds5(me, be) {
		return true
	}
	return topFoulVsMid(top, te, middle, me)
}

// topFoulVsMid — 3 张 top vs 5 张 middle 是否构成 foul (top > mid)
// 用 raw cards + eval 的 type 信息做 kicker 级比较 (与 JS isFoul 内 PAIR/HIGH_CARD 分支一致)
func topFoulVsMid(top []Card, te HandValue, middle []Card, me HandValue) bool {
	if me.Type < te.Type {
		return true
	}
	if me.Type > te.Type {
		return false
	}
	// same type
	if te.Type == TypeThreeOfAKind {
		// trips: top trip rank vs mid trip rank
		topTrip := topTripRank(top)
		midTrip := midTripRank(middle)
		return midTrip < topTrip
	}
	if te.Type == TypePair {
		topPair, topKickers := topPairAndKickers(top)
		midPair, midKickers := midPairAndKickers(middle)
		if midPair < topPair {
			return true
		}
		if midPair > topPair {
			return false
		}
		// same pair, compare kickers
		for i := 0; i < len(topKickers) && i < len(midKickers); i++ {
			if midKickers[i] < topKickers[i] {
				return true
			}
			if midKickers[i] > topKickers[i] {
				return false
			}
		}
		return false
	}
	// HIGH_CARD: compare top descending ranks vs mid top-3 ranks
	tRanks := sortedRanksDesc(top)
	mRanks := sortedRanksDesc(middle)
	for i := 0; i < len(tRanks) && i < len(mRanks); i++ {
		if mRanks[i] < tRanks[i] {
			return true
		}
		if mRanks[i] > tRanks[i] {
			return false
		}
	}
	return false
}

func sortedRanksDesc(cards []Card) []int {
	out := make([]int, 0, len(cards))
	for _, c := range cards {
		out = append(out, int(c.Rank()))
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i] < out[j] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func topTripRank(top []Card) int {
	var cnt [13]int
	for _, c := range top {
		cnt[c.Rank()]++
	}
	for r := 0; r < 13; r++ {
		if cnt[r] == 3 {
			return r
		}
	}
	return -1
}

func midTripRank(middle []Card) int {
	var cnt [13]int
	for _, c := range middle {
		cnt[c.Rank()]++
	}
	for r := 12; r >= 0; r-- {
		if cnt[r] == 3 {
			return r
		}
	}
	return -1
}

// topPairAndKickers: 返回 pair rank 和 kickers 降序
func topPairAndKickers(top []Card) (int, []int) {
	var cnt [13]int
	for _, c := range top {
		cnt[c.Rank()]++
	}
	pair := -1
	var kickers []int
	for r := 12; r >= 0; r-- {
		if cnt[r] == 2 {
			pair = r
		} else if cnt[r] == 1 {
			kickers = append(kickers, r)
		}
	}
	return pair, kickers
}

// midPairAndKickers: 返回 pair rank (highest) 和 kickers 降序
func midPairAndKickers(middle []Card) (int, []int) {
	var cnt [13]int
	for _, c := range middle {
		cnt[c.Rank()]++
	}
	pair := -1
	var kickers []int
	for r := 12; r >= 0; r-- {
		if cnt[r] == 2 {
			if pair < 0 {
				pair = r
			}
		} else if cnt[r] == 1 {
			kickers = append(kickers, r)
		}
	}
	return pair, kickers
}

// IsFoulJoker — 含 joker 的犯规判定. 用 *Joker eval 函数, 不做 cap 降级.
// 相当于 "joker 总是补成最大", 比真实 cap 路径严格.
// (TODO: 后续实现 cap downgrade chain 与 JS evaluateBoardJoker 完全 parity)
func IsFoulJoker(top, middle, bottom []Card) bool {
	if len(top) != 3 || len(middle) != 5 || len(bottom) != 5 {
		return true
	}
	te := Evaluate3Joker(top)
	me := Evaluate5Joker(middle)
	be := Evaluate5Joker(bottom)
	if HandExceeds5(me, be) {
		return true
	}
	return TopExceedsMid(te, me)
}

// HasJoker — board 中是否含 joker
func HasJoker(cards ...[]Card) bool {
	for _, row := range cards {
		for _, c := range row {
			if c.IsJoker() {
				return true
			}
		}
	}
	return false
}
