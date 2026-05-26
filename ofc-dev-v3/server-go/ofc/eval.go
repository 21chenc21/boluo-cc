package ofc

// HandType: 与 game.js HAND_TYPE 对齐
const (
	TypeHighCard      = 0
	TypePair          = 1
	TypeTwoPair       = 2
	TypeThreeOfAKind  = 3
	TypeStraight      = 4
	TypeFlush         = 5
	TypeFullHouse     = 6
	TypeFourOfAKind   = 7
	TypeStraightFlush = 8
	TypeRoyalFlush    = 9
)

// HandValue: { Type, Value }, Value 用于同型大小比较
// Value 编码: type*1e6 + 5 个 kicker × 15^(4..0)
type HandValue struct {
	Type  int
	Value int64
}

// makeValue: type 主权重 + kicker 用 15 进制衰减 (与 game.js makeValue 一致)
func makeValue(t int, kickers ...int) int64 {
	v := int64(t) * 1000000
	mul := int64(1)
	for k := 4; k >= 0; k-- {
		if k < len(kickers) {
			v += int64(kickers[k]) * mul
		}
		mul *= 15
	}
	return v
}

// Evaluate3 — 顶道 3 张. 含 joker 自动 dispatch 到 Evaluate3Joker.
// 编码与 JS evaluate3 一致 (不走 makeValue):
//   high: r0*225 + r1*15 + r2
//   pair: 1e6 + pair*15 + kicker
//   trips: 3e6 + trip*15
func Evaluate3(cards []Card) HandValue {
	if len(cards) != 3 {
		return HandValue{Type: -1, Value: 0}
	}
	for _, c := range cards {
		if c.IsJoker() {
			return Evaluate3Joker(cards)
		}
	}
	var rankCnt [13]int
	for _, c := range cards {
		rankCnt[c.Rank()]++
	}
	maxCnt := 0
	for _, c := range rankCnt {
		if c > maxCnt {
			maxCnt = c
		}
	}
	if maxCnt == 3 {
		var rank int
		for r := 0; r < 13; r++ {
			if rankCnt[r] == 3 {
				rank = r
				break
			}
		}
		return HandValue{Type: TypeThreeOfAKind, Value: int64(TypeThreeOfAKind)*1000000 + int64(rank)*15}
	}
	if maxCnt == 2 {
		pair := -1
		kicker := -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 2 {
				pair = r
			} else if rankCnt[r] == 1 && r > kicker {
				kicker = r
			}
		}
		return HandValue{Type: TypePair, Value: int64(TypePair)*1000000 + int64(pair)*15 + int64(kicker)}
	}
	// high card: top 3 ranks descending
	var ranks [3]int
	idx := 0
	for r := 12; r >= 0; r-- {
		if rankCnt[r] == 1 {
			ranks[idx] = r
			idx++
			if idx == 3 {
				break
			}
		}
	}
	return HandValue{Type: TypeHighCard, Value: int64(ranks[0])*225 + int64(ranks[1])*15 + int64(ranks[2])}
}

// Evaluate5 — 中/底道 5 张. 含 joker 自动 dispatch 到 Evaluate5Joker.
func Evaluate5(cards []Card) HandValue {
	if len(cards) != 5 {
		return HandValue{Type: -1, Value: 0}
	}
	for _, c := range cards {
		if c.IsJoker() {
			return Evaluate5Joker(cards)
		}
	}
	var rankCnt [13]int
	var suitCnt [4]int
	for _, c := range cards {
		rankCnt[c.Rank()]++
		suitCnt[c.Suit()]++
	}

	// max count + group counts (同型 game.js 的 groups)
	maxCnt, secondCnt := 0, 0
	for _, c := range rankCnt {
		if c > maxCnt {
			secondCnt = maxCnt
			maxCnt = c
		} else if c > secondCnt {
			secondCnt = c
		}
	}
	pairs := 0
	for _, c := range rankCnt {
		if c >= 2 {
			pairs++
		}
	}

	// flush
	flush := false
	for _, sc := range suitCnt {
		if sc == 5 {
			flush = true
			break
		}
	}

	// straight: unique ranks, 跨度 4 (含 wheel A2345)
	uniques := 0
	var ranks []int
	for r := 12; r >= 0; r-- {
		if rankCnt[r] > 0 {
			ranks = append(ranks, r)
			uniques++
		}
	}
	straight := false
	straightHigh := -1
	if uniques == 5 {
		// ranks 已降序
		if ranks[0]-ranks[4] == 4 {
			straight = true
			straightHigh = ranks[0]
		}
		// wheel: A,5,4,3,2 → ranks=[12,3,2,1,0]
		if ranks[0] == 12 && ranks[1] == 3 && ranks[2] == 2 && ranks[3] == 1 && ranks[4] == 0 {
			straight = true
			straightHigh = 3 // 5-high straight
		}
	}

	// === 分类 ===
	if flush && straight {
		if uniques == 5 && ranks[0] == 12 && ranks[4] == 8 {
			return HandValue{Type: TypeRoyalFlush, Value: makeValue(TypeRoyalFlush, 12)}
		}
		return HandValue{Type: TypeStraightFlush, Value: makeValue(TypeStraightFlush, straightHigh)}
	}
	if maxCnt == 4 {
		// four of a kind: quad rank + kicker
		quad := -1
		kicker := -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 4 {
				quad = r
			} else if rankCnt[r] == 1 {
				if r > kicker {
					kicker = r
				}
			}
		}
		return HandValue{Type: TypeFourOfAKind, Value: makeValue(TypeFourOfAKind, quad, kicker)}
	}
	if maxCnt == 3 && secondCnt == 2 {
		// full house: trip rank + pair rank
		trip, pair := -1, -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 3 {
				trip = r
			} else if rankCnt[r] == 2 {
				pair = r
			}
		}
		return HandValue{Type: TypeFullHouse, Value: makeValue(TypeFullHouse, trip, pair)}
	}
	if flush {
		// 5 highest ranks
		k := []int{ranks[0], ranks[1], ranks[2], ranks[3], ranks[4]}
		return HandValue{Type: TypeFlush, Value: makeValue(TypeFlush, k[0], k[1], k[2], k[3], k[4])}
	}
	if straight {
		return HandValue{Type: TypeStraight, Value: makeValue(TypeStraight, straightHigh)}
	}
	if maxCnt == 3 {
		// trips + 2 kickers
		trip := -1
		k1, k2 := -1, -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 3 {
				trip = r
			} else if rankCnt[r] == 1 {
				if k1 < 0 {
					k1 = r
				} else if k2 < 0 {
					k2 = r
				}
			}
		}
		return HandValue{Type: TypeThreeOfAKind, Value: makeValue(TypeThreeOfAKind, trip, k1, k2)}
	}
	if maxCnt == 2 && secondCnt == 2 {
		// two pair: hi pair, lo pair, kicker
		var pairs []int
		var kicker int
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 2 {
				pairs = append(pairs, r)
			} else if rankCnt[r] == 1 {
				if r > kicker {
					kicker = r
				}
			}
		}
		// pairs already descending
		if len(pairs) >= 2 {
			return HandValue{Type: TypeTwoPair, Value: makeValue(TypeTwoPair, pairs[0], pairs[1], kicker)}
		}
	}
	if maxCnt == 2 {
		// pair + 3 kickers
		pair := -1
		var k []int
		for r := 12; r >= 0; r-- {
			if rankCnt[r] == 2 {
				pair = r
			} else if rankCnt[r] == 1 {
				k = append(k, r)
			}
		}
		k1, k2, k3 := -1, -1, -1
		if len(k) > 0 {
			k1 = k[0]
		}
		if len(k) > 1 {
			k2 = k[1]
		}
		if len(k) > 2 {
			k3 = k[2]
		}
		return HandValue{Type: TypePair, Value: makeValue(TypePair, pair, k1, k2, k3)}
	}
	// high card
	return HandValue{Type: TypeHighCard, Value: makeValue(TypeHighCard, ranks[0], ranks[1], ranks[2], ranks[3], ranks[4])}
}
