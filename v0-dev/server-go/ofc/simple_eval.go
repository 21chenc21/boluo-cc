package ofc

// SimpleEval — 启发式评分 (与 JS solver.js simpleEval 完全 parity)
// 用于 expertPlace5 stage 0 候选预筛 + quickRollout 内部决策
// 输出 score (越大越优), foul detected 直接返回 -500
func SimpleEval(gs *GameState) float32 {
	top := gs.Top
	middle := gs.Middle
	bottom := gs.Bottom
	score := float32(0)

	// joker-aware 牌型 / pair rank (与 JS 一致, 鬼牌补强)
	getType := func(cards []Card) int {
		if len(cards) == 0 {
			return TypeHighCard
		}
		var rankCnt [13]int
		jokerCnt := 0
		for _, c := range cards {
			if c.IsJoker() {
				jokerCnt++
				continue
			}
			rankCnt[c.Rank()]++
		}
		mx := 0
		pairs := 0
		uniqueR := 0
		for _, v := range rankCnt {
			if v > 0 {
				uniqueR++
			}
			if v > mx {
				mx = v
			}
			if v >= 2 {
				pairs++
			}
		}
		eff := mx + jokerCnt
		switch {
		case eff >= 4:
			return TypeFourOfAKind
		case eff >= 3 && pairs >= 2:
			return TypeFullHouse
		case eff >= 3 && jokerCnt >= 1 && uniqueR >= 2:
			return TypeFullHouse
		case eff >= 3:
			return TypeThreeOfAKind
		case pairs >= 2 || (pairs == 1 && jokerCnt >= 1):
			return TypeTwoPair
		case eff >= 2:
			return TypePair
		}
		return TypeHighCard
	}
	getPairRank := func(cards []Card) int {
		var rankCnt [13]int
		jokerCnt := 0
		for _, c := range cards {
			if c.IsJoker() {
				jokerCnt++
				continue
			}
			rankCnt[c.Rank()]++
		}
		mx := -1
		for r := 12; r >= 0; r-- {
			if rankCnt[r] >= 2 && r > mx {
				mx = r
			}
		}
		if jokerCnt >= 1 && mx < 0 {
			hi := -1
			for r := 12; r >= 0; r-- {
				if rankCnt[r] > 0 {
					hi = r
					break
				}
			}
			if hi >= 0 {
				mx = hi
			} else {
				mx = 12
			}
		}
		return mx
	}

	botFull := len(bottom) == 5
	midFull := len(middle) == 5
	topFull := len(top) == 3
	var botType, midType, topType int
	if botFull {
		botType = Evaluate5(bottom).Type
	} else {
		botType = getType(bottom)
	}
	if midFull {
		midType = Evaluate5(middle).Type
	} else {
		midType = getType(middle)
	}
	if topFull {
		topType = Evaluate3(top).Type
	} else {
		topType = getType(top)
	}

	// 1. 牌型分
	score += float32(botType) * 22
	score += float32(midType) * 18
	score += float32(topType) * 6
	if botFull {
		score += float32(BottomRoyaltyFromEval(Evaluate5(bottom))) * 3
	}
	if midFull {
		score += float32(MiddleRoyaltyFromEval(Evaluate5(middle))) * 3
	}

	// 2. 排序 (确定犯规 / 潜在风险)
	if topFull && midFull {
		tE := Evaluate3(top)
		mE := Evaluate5(middle)
		if mE.Type < tE.Type {
			return -500
		}
		if mE.Type == tE.Type && tE.Type == TypePair {
			if getPairRank(middle) < getPairRank(top) {
				return -500
			}
		}
	}
	if midFull && botFull {
		if Evaluate5(middle).Value > Evaluate5(bottom).Value {
			return -500
		}
	}

	// 头道 vs 中道
	// Joker flex credit: 任一行含 joker 都能消除大部分 foul-risk.
	//   top 有 joker: 可降级为 high card → 不犯规
	//   mid 有 joker: 可升级 pair/trips → 顶住 top
	// final score.go 评分会自动选 joker 非 foul play, 但 partial state 用 max-pair 评估
	// → 推论 foul → 模型保守. 0.2x 罚分系数保留极轻偏置.
	rowHasJoker := func(cards []Card) bool {
		for _, c := range cards {
			if c.IsJoker() {
				return true
			}
		}
		return false
	}
	topHasJoker := rowHasJoker(top)
	midHasJoker := rowHasJoker(middle)
	botHasJoker := rowHasJoker(bottom)
	topMidFlexFactor := float32(1.0)
	if topHasJoker || midHasJoker {
		topMidFlexFactor = 0.2
	}
	midBotFlexFactor := float32(1.0)
	if midHasJoker || botHasJoker {
		midBotFlexFactor = 0.2
	}
	if len(top) >= 2 && len(middle) >= 1 {
		if topType > midType {
			ms := 5 - len(middle)
			var penalty float32
			if topType >= TypeThreeOfAKind {
				switch {
				case ms >= 3:
					penalty = 60
				case ms >= 2:
					penalty = 100
				case ms >= 1:
					penalty = 200
				default:
					penalty = 500
				}
			} else if topType == TypePair && getPairRank(top) >= 10 && ms >= 3 {
				penalty = 8
			} else {
				switch {
				case ms >= 3:
					penalty = 15
				case ms >= 2:
					penalty = 30
				case ms >= 1:
					penalty = 60
				default:
					penalty = 200
				}
			}
			score -= penalty * topMidFlexFactor
		}
		if topType == midType && topType >= TypePair {
			tpr := getPairRank(top)
			mpr := getPairRank(middle)
			if tpr >= 0 && mpr >= 0 {
				if mpr > tpr {
					score += 8
				} else if mpr < tpr {
					ms := 5 - len(middle)
					var penalty float32
					switch {
					case ms >= 2:
						penalty = 20
					case ms >= 1:
						penalty = 50
					default:
						penalty = 150
					}
					score -= penalty * topMidFlexFactor
				}
			}
		}
	}
	// 中 vs 底 (joker flex credit 同 top-mid: mid joker 可降, bot joker 可升 → 减罚)
	if len(middle) >= 2 && len(bottom) >= 2 {
		if midType > botType {
			bs := 5 - len(bottom)
			var penalty float32
			switch {
			case bs >= 3:
				penalty = 10
			case bs >= 2:
				penalty = 20
			case bs >= 1:
				penalty = 50
			default:
				penalty = 150
			}
			score -= penalty * midBotFlexFactor
		}
		if midType == botType && midType >= TypePair {
			mpr := getPairRank(middle)
			bpr := getPairRank(bottom)
			if mpr >= 0 && bpr >= 0 {
				if bpr > mpr {
					score += 8
				} else if bpr < mpr {
					bs := 5 - len(bottom)
					var penalty float32
					switch {
					case bs >= 2:
						penalty = 15
					case bs >= 1:
						penalty = 35
					default:
						penalty = 100
					}
					score -= penalty * midBotFlexFactor
				}
			}
		}
	}
	// 排序奖励
	if botType > midType && len(bottom) >= 2 && len(middle) >= 2 {
		score += 15
	}
	if midType > topType && len(middle) >= 2 && len(top) >= 1 {
		score += 10
	}

	hasJokerInRow := func(cards []Card) bool {
		for _, c := range cards {
			if c.IsJoker() {
				return true
			}
		}
		return false
	}

	// 小对错位惩罚 (底道)
	if !botFull && botType == TypePair && len(bottom) >= 2 {
		bpr := getPairRank(bottom)
		if !hasJokerInRow(bottom) && bpr >= 0 && bpr < 7 {
			score -= float32(8-bpr) * 8
		}
	}
	// 中道种子对子奖励
	if !midFull && midType == TypePair && len(middle) <= 3 && len(bottom) >= 2 {
		mpr := getPairRank(middle)
		if !hasJokerInRow(middle) && mpr >= 0 && mpr < 10 {
			score += 18
		}
	}

	// 3. 头道
	maxRealRank := func(cards []Card) int {
		mx := -1
		for _, c := range cards {
			if c.IsJoker() {
				if 12 > mx {
					mx = 12
				}
				continue
			}
			if int(c.Rank()) > mx {
				mx = int(c.Rank())
			}
		}
		return mx
	}
	if len(top) > 0 {
		topMax := maxRealRank(top)
		if topType == TypeHighCard {
			if topFull && topMax >= 10 {
				score -= float32(topMax-9) * 8
			} else if !topFull && topMax >= 10 {
				highCnt := 0
				for _, c := range top {
					rk := -1
					if c.IsJoker() {
						rk = 12
					} else {
						rk = int(c.Rank())
					}
					if rk >= 10 {
						highCnt++
					}
				}
				if highCnt >= 2 {
					score += 12
				} else {
					score += 5
				}
			} else if topMax == 9 {
				score -= 10
			}
			if !topFull && topMax >= 6 && topMax <= 9 {
				score -= float32(topMax-4) * 3
			}
		}
		if topType == TypePair {
			tpr := getPairRank(top)
			if tpr >= 0 && tpr < 10 {
				score -= float32(10-tpr) * 3
			}
		}
	}

	// 4. 范特西感知
	topPR := -1
	if len(top) >= 2 {
		topPR = getPairRank(top)
	}
	topTrips := topFull && topType >= TypeThreeOfAKind
	chasingFantasy := topPR >= 10 || topTrips
	totalCards := len(top) + len(middle) + len(bottom)
	if chasingFantasy {
		if totalCards <= 5 {
			switch {
			case topTrips:
				score += 45
			case topPR >= 12:
				score += 35
			case topPR >= 11:
				score += 28
			default:
				score += 18
			}
			if len(middle) >= 1 {
				midMx := maxRealRank(middle)
				if midMx >= 10 {
					score += 5
				}
			}
		} else if topTrips {
			if midType >= TypeThreeOfAKind {
				score += 60
			} else if midType >= TypeTwoPair {
				score += 20
			} else if midType >= TypePair {
				score += 5
			} else if len(middle) >= 3 {
				score -= 40
			}
		} else {
			// 后续轮 QQ+/KK+/AA+ 调中道支撑
			if midType >= TypeTwoPair {
				score += 40
			} else if midType >= TypePair {
				mpr := getPairRank(middle)
				if mpr >= topPR {
					score += 35
				} else if mpr >= 8 {
					score += 20
				} else {
					score += 10
				}
			} else if len(middle) >= 3 {
				score -= 30
			}
		}
		if botType >= TypePair {
			score += 8
		}
	}

	// 5. Draw 潜力 (底/中)
	for rowIdx, row := range [][]Card{bottom, middle} {
		if len(row) < 2 || len(row) >= 5 {
			continue
		}
		w := float32(1.0)
		if rowIdx == 1 { // middle
			w = 0.6
		}
		// 同花 draw (joker 也算 same suit 'j' 单独 bucket)
		var sc [5]int
		for _, c := range row {
			if c.IsJoker() {
				sc[4]++
			} else {
				sc[c.Suit()]++
			}
		}
		mxs := 0
		for _, v := range sc {
			if v > mxs {
				mxs = v
			}
		}
		if mxs >= 4 {
			score += 25 * w
		} else if mxs >= 3 {
			score += 12 * w
		}
		// 顺子 draw
		uniqueR := []int{}
		var rankCnt [13]int
		for _, c := range row {
			if c.IsJoker() {
				continue
			}
			rankCnt[c.Rank()]++
		}
		for r := 0; r < 13; r++ {
			if rankCnt[r] > 0 {
				uniqueR = append(uniqueR, r)
			}
		}
		// joker 占 12 (与 JS rankIndex('X')=12)
		hasJoker := false
		for _, c := range row {
			if c.IsJoker() {
				hasJoker = true
				break
			}
		}
		if hasJoker {
			has12 := false
			for _, r := range uniqueR {
				if r == 12 {
					has12 = true
					break
				}
			}
			if !has12 {
				uniqueR = append(uniqueR, 12)
			}
		}
		bestRun := 1
		run := 1
		for i := 1; i < len(uniqueR); i++ {
			if uniqueR[i]-uniqueR[i-1] <= 2 {
				run++
				if run > bestRun {
					bestRun = run
				}
			} else {
				run = 1
			}
		}
		if bestRun >= 4 {
			score += 15 * w
		} else if bestRun >= 3 && len(row) <= 3 {
			score += 6 * w
		}
		// SF 潜力 (mxs >= 3 && bestRun >= 3 → 找该花色内的 run >= 3)
		if mxs >= 3 && bestRun >= 3 {
			// 找 count >= 3 的 suit
			var fs int = -1
			for s := 0; s < 4; s++ {
				if sc[s] >= 3 {
					fs = s
					break
				}
			}
			if fs >= 0 {
				var fr []int
				var seen [13]bool
				for _, c := range row {
					if c.IsJoker() {
						continue
					}
					if int(c.Suit()) != fs {
						continue
					}
					r := int(c.Rank())
					if !seen[r] {
						seen[r] = true
						fr = append(fr, r)
					}
				}
				// sort asc
				for i := 0; i < len(fr); i++ {
					for j := i + 1; j < len(fr); j++ {
						if fr[i] > fr[j] {
							fr[i], fr[j] = fr[j], fr[i]
						}
					}
				}
				frun := 1
				for i := 1; i < len(fr); i++ {
					if fr[i]-fr[i-1] <= 2 {
						frun++
					}
				}
				if frun >= 3 {
					score += 20 * w
				}
			}
		}
	}

	// 4a. 浪费追范惩罚 (R1)
	if !chasingFantasy && totalCards == 5 {
		highPairsInMidBot := 0
		for _, row := range [][]Card{middle, bottom} {
			var rankCnt [13]int
			for _, c := range row {
				if c.IsJoker() {
					continue
				}
				rankCnt[c.Rank()]++
			}
			for r := 10; r <= 12; r++ {
				if rankCnt[r] >= 2 {
					highPairsInMidBot++
				}
			}
		}
		if highPairsInMidBot >= 2 {
			score -= 35
		}
	}

	// 4b. 鬼上顶倾向 (R1, top 1-2 张, 含 joker, 别处无 high real pair)
	if !chasingFantasy && len(top) >= 1 && len(top) < 3 {
		topJokerCnt := 0
		for _, c := range top {
			if c.IsJoker() {
				topJokerCnt++
			}
		}
		if topJokerCnt >= 1 {
			hasHighPairElsewhere := false
			for _, row := range [][]Card{middle, bottom} {
				var rankCnt [13]int
				for _, c := range row {
					if c.IsJoker() {
						continue
					}
					rankCnt[c.Rank()]++
				}
				for r := 10; r <= 12; r++ {
					if rankCnt[r] >= 2 {
						hasHighPairElsewhere = true
						break
					}
				}
				if hasHighPairElsewhere {
					break
				}
			}
			if !hasHighPairElsewhere {
				score += float32(50 * topJokerCnt)
			}
		}
	}

	// 4c. R1 鬼放底/中 仅靠虚 PAIR 撑分: 重惩罚
	if totalCards == 5 {
		realPair := func(cards []Card) bool {
			var rankCnt [13]int
			for _, c := range cards {
				if c.IsJoker() {
					continue
				}
				rankCnt[c.Rank()]++
			}
			for _, v := range rankCnt {
				if v >= 2 {
					return true
				}
			}
			return false
		}
		botJoker := hasJokerInRow(bottom)
		midJoker := hasJokerInRow(middle)
		topHasJoker := hasJokerInRow(top)
		if botJoker && !realPair(bottom) && !topHasJoker {
			score -= 40
		}
		if midJoker && !realPair(middle) && !topHasJoker {
			score -= 25
		}
	}

	// 5b. 自然 SF draw (3+ 真牌同花在 5-rank 窗口) — 仅 hasAnyJoker 时启用
	hasAnyJoker := hasJokerInRow(top) || hasJokerInRow(middle) || hasJokerInRow(bottom)
	if hasAnyJoker {
		for rowIdx, row := range [][]Card{bottom, middle} {
			if len(row) < 3 {
				continue
			}
			realBySuit := make(map[int][]int)
			for _, c := range row {
				if c.IsJoker() {
					continue
				}
				s := int(c.Suit())
				realBySuit[s] = append(realBySuit[s], int(c.Rank()))
			}
			sfNat := false
			for _, ranks := range realBySuit {
				// dedup + sort asc
				var seen [13]bool
				var sorted []int
				for _, r := range ranks {
					if !seen[r] {
						seen[r] = true
						sorted = append(sorted, r)
					}
				}
				for i := 0; i < len(sorted); i++ {
					for j := i + 1; j < len(sorted); j++ {
						if sorted[i] > sorted[j] {
							sorted[i], sorted[j] = sorted[j], sorted[i]
						}
					}
				}
				if len(sorted) < 3 {
					continue
				}
				for i := 0; i <= len(sorted)-3; i++ {
					if sorted[i+2]-sorted[i] <= 4 {
						sfNat = true
						break
					}
				}
				if sfNat {
					break
				}
			}
			if sfNat {
				if rowIdx == 0 {
					score += 50
				} else {
					score += 30
				}
			}
		}
	}

	// 6. 中道惩罚
	if len(middle) >= 3 && !midFull && midType == TypeHighCard {
		score -= 12
	}

	// 7. 均衡性 (R1)
	if totalCards == 5 {
		if len(middle) == 0 {
			score -= 40
		}
		if len(bottom) == 0 {
			score -= 30
		}
		if len(bottom) == 1 {
			score -= 18
		}
		if len(top) == 0 {
			score += 8
		}
		if len(top) == 1 && len(bottom) >= 2 {
			score += 5
		}
	}

	// 头道灵活性硬规则
	if len(top) == 3 && totalCards <= 7 && topType < TypeThreeOfAKind {
		if topType == TypePair && getPairRank(top) >= 10 {
			// 追范, OK
		} else {
			score -= 200
		}
	}
	if len(top) == 2 && topType < TypePair && totalCards <= 9 {
		score -= 50
	}

	// 行均衡 (满行 + 早期)
	if totalCards >= 5 {
		roundsPlayed := (totalCards-5+1)/2 + 1
		if len(middle) == 5 && roundsPlayed <= 3 {
			need := (3 - len(top)) + (5 - len(bottom))
			can := (5 - roundsPlayed) * 2
			if need > can {
				score -= 100
			} else if need == can {
				score -= 20
			}
		}
		if len(bottom) == 5 && roundsPlayed <= 3 {
			need := (3 - len(top)) + (5 - len(middle))
			can := (5 - roundsPlayed) * 2
			if need > can {
				score -= 100
			} else if need == can {
				score -= 20
			}
		}
	}

	return score
}
