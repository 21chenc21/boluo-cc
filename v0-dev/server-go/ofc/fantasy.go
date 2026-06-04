package ofc

// === Fantasyland (FL) — Phase 1: reFan 快速通道 ===
// 完整文档: 见 ../../solver.js expertPlaceFantasy 系列
// 此文件仅 port reFan 路径 (锚枚举 → 布局穷举 → 选最高 royalty),
// 真正的 beam search (_expertPlaceFantasyImpl) 留 Phase 2.

import (
	"sort"
)

// FantasyAnchor — 一组确定占据某行的牌 (top trips / bot quads / bot SF / mid SF / 高对 / 葫芦)
type FantasyAnchor struct {
	Type  string // 'top-trips' / 'bot-quads' / 'bot-sf' / 'mid-quads' / 'mid-sf' / 'top-pair-X' / 'mid-fh-X-Y' / 'bot-fh-X-Y'
	Row   string // 'top' / 'bot' / 'mid' / 'bot5' / 'mid4' / 'top2'
	Cards []Card
}

// FantasyLayout — 完整 13-card 布局 + 弃牌
type FantasyLayout struct {
	Top      []Card
	Middle   []Card
	Bottom   []Card
	Discards []Card
}

// FantasyResult — Layout + Royalty + (用于 tie-break 的 sc)
type FantasyResult struct {
	Layout  FantasyLayout
	Royalty int
	Sc      ScoreResult // 用于 tie-break (top/mid/bot eval value)
}

// rankForSort — 与 JS isJoker(c)?13:rankIndex(c.rank) 一致
func rankForSort(c Card) int {
	if c.IsJoker() {
		return 13
	}
	return int(c.Rank())
}

// eval5Maybe — 自动选 joker / 非 joker 版本
func eval5Maybe(cards []Card) HandValue {
	for _, c := range cards {
		if c.IsJoker() {
			return Evaluate5Joker(cards)
		}
	}
	return Evaluate5(cards)
}

func eval3Maybe(cards []Card) HandValue {
	for _, c := range cards {
		if c.IsJoker() {
			return Evaluate3Joker(cards)
		}
	}
	return Evaluate3(cards)
}

// scTieBreak — 已知都非 foul, 比较 (royalty, topVal, midVal, botVal) 谁更大. >0 = a 更好
func scTieBreak(a, b *FantasyResult) int {
	if a.Royalty != b.Royalty {
		return a.Royalty - b.Royalty
	}
	if a.Sc.TopEval.Value != b.Sc.TopEval.Value {
		if a.Sc.TopEval.Value > b.Sc.TopEval.Value {
			return 1
		}
		return -1
	}
	if a.Sc.MidEval.Value != b.Sc.MidEval.Value {
		if a.Sc.MidEval.Value > b.Sc.MidEval.Value {
			return 1
		}
		return -1
	}
	if a.Sc.BotEval.Value > b.Sc.BotEval.Value {
		return 1
	}
	if a.Sc.BotEval.Value < b.Sc.BotEval.Value {
		return -1
	}
	return 0
}

func cardSetExclude(all []Card, exclude []Card) []Card {
	excl := make(map[Card]int, len(exclude))
	for _, c := range exclude {
		excl[c]++
	}
	out := make([]Card, 0, len(all))
	for _, c := range all {
		if excl[c] > 0 {
			excl[c]--
			continue
		}
		out = append(out, c)
	}
	return out
}

// ============================================================
// Phase 1: 锚枚举
// ============================================================

// FindReFanAnchors — 反向枚举所有 re-fantasy 锚 (top trips / bot quads / bot SF)
// 与 JS _findReFanAnchors 完全一致 (枚举顺序也保持)
func FindReFanAnchors(dealt []Card) []FantasyAnchor {
	var real, jokers []Card
	for _, c := range dealt {
		if c.IsJoker() {
			jokers = append(jokers, c)
		} else {
			real = append(real, c)
		}
	}
	J := len(jokers)
	// byRank: 只含真牌, 保留出现顺序 (与 JS 对象 key 顺序一致, 影响枚举次序但 sort 兜底)
	byRank := make(map[uint8][]Card)
	rankOrder := make([]uint8, 0)
	for _, c := range real {
		r := c.Rank()
		if _, ok := byRank[r]; !ok {
			rankOrder = append(rankOrder, r)
		}
		byRank[r] = append(byRank[r], c)
	}
	bySuit := make(map[uint8][]Card)
	suitOrder := make([]uint8, 0)
	for _, c := range real {
		s := c.Suit()
		if _, ok := bySuit[s]; !ok {
			suitOrder = append(suitOrder, s)
		}
		bySuit[s] = append(bySuit[s], c)
	}

	anchors := make([]FantasyAnchor, 0)
	// (1) 顶 trips
	// 2026-06-01 修: 原 `cs[:3-min(3-len(cs), 0)]` 是 bug — len(cs)=2 时 cs[:3] 越界读到零值 Card "2s",
	// 致 AAA/QQQ/TTT anchor 用 2s 代替 joker, fantasy re-fan 全失败. 改成跟 bot-quads 同模式.
	for _, r := range rankOrder {
		cs := byRank[r]
		if len(cs)+J >= 3 {
			realUsed := cs
			if len(cs) >= 3 {
				realUsed = cs[:3]
			}
			jokersNeeded := 3 - len(realUsed)
			ach := FantasyAnchor{Type: "top-trips", Row: "top"}
			ach.Cards = append(ach.Cards, realUsed...)
			ach.Cards = append(ach.Cards, jokers[:jokersNeeded]...)
			anchors = append(anchors, ach)
		}
	}
	// 双鬼可形成虚 trips of A
	if len(real) == 0 && J >= 3 {
		ach := FantasyAnchor{Type: "top-trips", Row: "top"}
		ach.Cards = append(ach.Cards, jokers[:3]...)
		anchors = append(anchors, ach)
	}
	// (2) 底 4-of-a-kind
	for _, r := range rankOrder {
		cs := byRank[r]
		if len(cs)+J >= 4 {
			realUsed := cs
			if len(cs) >= 4 {
				realUsed = cs[:4]
			}
			jokersNeeded := 4 - len(realUsed)
			ach := FantasyAnchor{Type: "bot-quads", Row: "bot"}
			ach.Cards = append(ach.Cards, realUsed...)
			ach.Cards = append(ach.Cards, jokers[:jokersNeeded]...)
			anchors = append(anchors, ach)
		}
	}
	// (3) 底 SF: 每个 suit, 每个 5-rank window
	for _, suit := range suitOrder {
		suitMap := make(map[int]Card)
		for _, c := range bySuit[suit] {
			suitMap[int(c.Rank())] = c
		}
		tryWindow := func(winRanks []int, typ string) {
			have := make([]Card, 0, 5)
			for _, rk := range winRanks {
				if c, ok := suitMap[rk]; ok {
					have = append(have, c)
				}
			}
			need := 5 - len(have)
			if need >= 0 && need <= J {
				ach := FantasyAnchor{Type: typ, Row: "bot"}
				ach.Cards = append(ach.Cards, have...)
				ach.Cards = append(ach.Cards, jokers[:need]...)
				anchors = append(anchors, ach)
			}
		}
		for rmin := 0; rmin <= 8; rmin++ {
			tryWindow([]int{rmin, rmin + 1, rmin + 2, rmin + 3, rmin + 4}, "bot-sf")
		}
		// wheel A-2-3-4-5
		tryWindow([]int{12, 0, 1, 2, 3}, "bot-sf-wheel")
	}
	return anchors
}

// FindNonRefanAnchors — 非 re-fan 高价值锚 (mid 4-kind / mid SF / 顶 AA-QQ / 葫芦)
func FindNonRefanAnchors(dealt []Card) []FantasyAnchor {
	var real, jokers []Card
	for _, c := range dealt {
		if c.IsJoker() {
			jokers = append(jokers, c)
		} else {
			real = append(real, c)
		}
	}
	J := len(jokers)
	byRank := make(map[uint8][]Card)
	rankOrder := make([]uint8, 0)
	for _, c := range real {
		r := c.Rank()
		if _, ok := byRank[r]; !ok {
			rankOrder = append(rankOrder, r)
		}
		byRank[r] = append(byRank[r], c)
	}
	bySuit := make(map[uint8][]Card)
	suitOrder := make([]uint8, 0)
	for _, c := range real {
		s := c.Suit()
		if _, ok := bySuit[s]; !ok {
			suitOrder = append(suitOrder, s)
		}
		bySuit[s] = append(bySuit[s], c)
	}
	anchors := make([]FantasyAnchor, 0)
	// 中道 4-kind
	for _, r := range rankOrder {
		cs := byRank[r]
		if len(cs)+J >= 4 {
			realUsed := cs
			if len(cs) >= 4 {
				realUsed = cs[:4]
			}
			jU := jokers[:max(0, 4-len(realUsed))]
			ach := FantasyAnchor{Type: "mid-quads", Row: "mid4"}
			ach.Cards = append(ach.Cards, realUsed...)
			ach.Cards = append(ach.Cards, jU...)
			anchors = append(anchors, ach)
		}
	}
	// 中道 SF
	for _, suit := range suitOrder {
		suitMap := make(map[int]Card)
		for _, c := range bySuit[suit] {
			suitMap[int(c.Rank())] = c
		}
		tryWindow := func(winRanks []int, typ string) {
			have := make([]Card, 0, 5)
			for _, rk := range winRanks {
				if c, ok := suitMap[rk]; ok {
					have = append(have, c)
				}
			}
			need := 5 - len(have)
			if need >= 0 && need <= J {
				ach := FantasyAnchor{Type: typ, Row: "mid"}
				ach.Cards = append(ach.Cards, have...)
				ach.Cards = append(ach.Cards, jokers[:need]...)
				anchors = append(anchors, ach)
			}
		}
		for rmin := 0; rmin <= 8; rmin++ {
			tryWindow([]int{rmin, rmin + 1, rmin + 2, rmin + 3, rmin + 4}, "mid-sf")
		}
		tryWindow([]int{12, 0, 1, 2, 3}, "mid-sf-wheel")
	}
	// 纯同花 anchor (mid-flush / bot-flush) — 2026-06-03 补.
	// 之前漏了非 SF 的纯 flush: 它不 re-fan (re-fan 锚跳过), 也不在 quads/SF/top-pair/FH 列表里 →
	// 两花局 (e.g. 无鬼 16 张, 5 方块 + 5 梅花 = 12 royalty) 被 QQ-top (9) 盖过. 见用户范手 case.
	for _, suit := range suitOrder {
		cs := bySuit[suit]
		if len(cs)+J < 5 {
			continue
		}
		hi := append([]Card{}, cs...)
		sort.SliceStable(hi, func(i, j int) bool { return hi[i].Rank() > hi[j].Rank() })
		realTake := min(5, len(hi))
		need := 5 - realTake
		if need > J {
			continue
		}
		flushCards := append([]Card{}, hi[:realTake]...)
		flushCards = append(flushCards, jokers[:need]...)
		anchors = append(anchors, FantasyAnchor{Type: "mid-flush", Row: "mid", Cards: append([]Card{}, flushCards...)})
		anchors = append(anchors, FantasyAnchor{Type: "bot-flush", Row: "bot5", Cards: append([]Card{}, flushCards...)})
	}
	// 顶 AA/KK/QQ — 注意按 ['A','K','Q'] 顺序 (rank 12, 11, 10)
	for _, r := range []uint8{12, 11, 10} {
		if cs, ok := byRank[r]; ok && len(cs)+J >= 2 {
			realUsed := cs
			if len(cs) >= 2 {
				realUsed = cs[:2]
			}
			jU := jokers[:max(0, 2-len(realUsed))]
			rankCh := rankToString(r)
			ach := FantasyAnchor{Type: "top-pair-" + rankCh, Row: "top2"}
			ach.Cards = append(ach.Cards, realUsed...)
			ach.Cards = append(ach.Cards, jU...)
			anchors = append(anchors, ach)
		}
	}
	// 葫芦 anchor (mid + bot5 各一)
	tripCands := make([]uint8, 0)
	pairCands := make([]uint8, 0)
	for _, r := range rankOrder {
		if len(byRank[r])+J >= 3 {
			tripCands = append(tripCands, r)
		}
		if len(byRank[r]) >= 2 {
			pairCands = append(pairCands, r)
		}
	}
	for _, tr := range tripCands {
		trReal := byRank[tr]
		trJ := max(0, 3-len(trReal))
		trCards := make([]Card, 0, 3)
		realTake := 3 - trJ
		if realTake > len(trReal) {
			realTake = len(trReal)
		}
		trCards = append(trCards, trReal[:realTake]...)
		trCards = append(trCards, jokers[:trJ]...)
		remJ := J - trJ
		for _, pr := range pairCands {
			if pr == tr {
				continue
			}
			prReal := byRank[pr]
			prJ := max(0, 2-len(prReal))
			if prJ > remJ {
				continue
			}
			prCards := make([]Card, 0, 2)
			prRealTake := 2 - prJ
			if prRealTake > len(prReal) {
				prRealTake = len(prReal)
			}
			prCards = append(prCards, prReal[:prRealTake]...)
			prCards = append(prCards, jokers[trJ:trJ+prJ]...)
			fh := append(append([]Card{}, trCards...), prCards...)
			anchors = append(anchors, FantasyAnchor{
				Type: "mid-fh-" + rankToString(tr) + "-" + rankToString(pr),
				Row:  "mid", Cards: fh,
			})
			anchors = append(anchors, FantasyAnchor{
				Type: "bot-fh-" + rankToString(tr) + "-" + rankToString(pr),
				Row:  "bot5", Cards: fh,
			})
		}
	}
	return anchors
}

func rankToString(r uint8) string {
	const ranks = "23456789TJQKA"
	if int(r) < len(ranks) {
		return string(ranks[r])
	}
	return "X"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ============================================================
// Enum helpers (与 JS 行为一致, 复杂度同; 保持 "选 best by (royalty, top, mid, bot)" 语义)
// ============================================================

// enumTopBot — 给定 mid 5 (固定), 穷举 top + bot
//
//	JS _enumTopBot, line 1056
func enumTopBot(midCards, remaining []Card, discardCount int) *FantasyResult {
	N := len(remaining)
	if N != 3+5+discardCount {
		return nil
	}
	midEval := eval5Maybe(midCards)
	var best *FantasyResult
	for i := 0; i < N-2; i++ {
		for j := i + 1; j < N-1; j++ {
			for k := j + 1; k < N; k++ {
				top := []Card{remaining[i], remaining[j], remaining[k]}
				topEval := eval3Maybe(top)
				if HandExceeds5(topEval, midEval) {
					continue
				}
				rest := make([]Card, 0, N-3)
				for x := 0; x < N; x++ {
					if x != i && x != j && x != k {
						rest = append(rest, remaining[x])
					}
				}
				M := len(rest)
				for a := 0; a < M-4; a++ {
					for b := a + 1; b < M-3; b++ {
						for c := b + 1; c < M-2; c++ {
							for d := c + 1; d < M-1; d++ {
								for e := d + 1; e < M; e++ {
									bot := []Card{rest[a], rest[b], rest[c], rest[d], rest[e]}
									botEval := eval5Maybe(bot)
									if botEval.Value < midEval.Value {
										continue
									}
									sc := ScoreHand(top, midCards, bot)
									if sc.Foul {
										continue
									}
									discards := make([]Card, 0, discardCount)
									for x := 0; x < M; x++ {
										if x != a && x != b && x != c && x != d && x != e {
											discards = append(discards, rest[x])
										}
									}
									cand := &FantasyResult{
										Layout:  FantasyLayout{Top: append([]Card{}, top...), Middle: midCards, Bottom: bot, Discards: discards},
										Royalty: sc.Royalties, Sc: sc,
									}
									if best == nil || scTieBreak(cand, best) > 0 {
										best = cand
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return best
}

// enumTopMid — 给定 bot 5, 穷举 top 3 + mid 5 + discards.
// returnFlag: 仅控制返回 (跟 JS 区别, Go 总返回 *FantasyResult, 调用方按需解构)
// 含 reFan 剪枝: top.type < 3 && bot.type < 7 → skip
func enumTopMid(botCards, remaining []Card, discardCount int) *FantasyResult {
	N := len(remaining)
	if N != 3+5+discardCount {
		return nil
	}
	botEval := eval5Maybe(botCards)
	var best *FantasyResult
	for i := 0; i < N-2; i++ {
		for j := i + 1; j < N-1; j++ {
			for k := j + 1; k < N; k++ {
				top := []Card{remaining[i], remaining[j], remaining[k]}
				topEval := eval3Maybe(top)
				_ = topEval
				rest := make([]Card, 0, N-3)
				for x := 0; x < N; x++ {
					if x != i && x != j && x != k {
						rest = append(rest, remaining[x])
					}
				}
				M := len(rest)
				for a := 0; a < M-4; a++ {
					for b := a + 1; b < M-3; b++ {
						for c := b + 1; c < M-2; c++ {
							for d := c + 1; d < M-1; d++ {
								for e := d + 1; e < M; e++ {
									mid := []Card{rest[a], rest[b], rest[c], rest[d], rest[e]}
									midEval := eval5Maybe(mid)
									if midEval.Value > botEval.Value {
										continue
									}
									if HandExceeds5(topEval, midEval) {
										continue
									}
									sc := ScoreHand(top, mid, botCards)
									if sc.Foul {
										continue
									}
									if sc.TopEval.Type < 3 && sc.BotEval.Type < 7 {
										continue
									}
									discards := make([]Card, 0, discardCount)
									for x := 0; x < M; x++ {
										if x != a && x != b && x != c && x != d && x != e {
											discards = append(discards, rest[x])
										}
									}
									cand := &FantasyResult{
										Layout:  FantasyLayout{Top: append([]Card{}, top...), Middle: mid, Bottom: botCards, Discards: discards},
										Royalty: sc.Royalties, Sc: sc,
									}
									if best == nil || scTieBreak(cand, best) > 0 {
										best = cand
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return best
}

// enumMidBot — 给定 top 3, 穷举 mid 5 + bot 5 + discards
// 顶 trips 锚专用, 含强剪枝 (mid 必须 > top trips, bot 必须 > mid)
func enumMidBot(topCards, remaining []Card, discardCount int) *FantasyResult {
	N := len(remaining)
	if N != 5+5+discardCount {
		return nil
	}
	topEval := eval3Maybe(topCards)
	var best *FantasyResult
	for i := 0; i < N-4; i++ {
		for j := i + 1; j < N-3; j++ {
			for k := j + 1; k < N-2; k++ {
				for l := k + 1; l < N-1; l++ {
					for m := l + 1; m < N; m++ {
						mid := []Card{remaining[i], remaining[j], remaining[k], remaining[l], remaining[m]}
						midEval := eval5Maybe(mid)
						if midEval.Type < topEval.Type {
							continue
						}
						if midEval.Type == topEval.Type && midEval.Value <= topEval.Value {
							continue
						}
						rest := make([]Card, 0, N-5)
						for x := 0; x < N; x++ {
							if x != i && x != j && x != k && x != l && x != m {
								rest = append(rest, remaining[x])
							}
						}
						M := len(rest)
						for a := 0; a < M-4; a++ {
							for b := a + 1; b < M-3; b++ {
								for c := b + 1; c < M-2; c++ {
									for d := c + 1; d < M-1; d++ {
										for e := d + 1; e < M; e++ {
											bot := []Card{rest[a], rest[b], rest[c], rest[d], rest[e]}
											botEval := eval5Maybe(bot)
											// 2026-06-01 修: <= 改 <, OFC 允许 bot==mid (e.g. 两 same-value flush). 老代码错砍 tie.
											if botEval.Value < midEval.Value {
												continue
											}
											sc := ScoreHand(topCards, mid, bot)
											if sc.Foul {
												continue
											}
											if sc.TopEval.Type < 3 && sc.BotEval.Type < 7 {
												continue
											}
											discards := make([]Card, 0, discardCount)
											for x := 0; x < M; x++ {
												if x != a && x != b && x != c && x != d && x != e {
													discards = append(discards, rest[x])
												}
											}
											cand := &FantasyResult{
												Layout:  FantasyLayout{Top: topCards, Middle: mid, Bottom: bot, Discards: discards},
												Royalty: sc.Royalties, Sc: sc,
											}
											if best == nil || scTieBreak(cand, best) > 0 {
												best = cand
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return best
}

// enumTopMidNoRefan — 给定 bot 5, 不要求 reFan (Phase 2 / 非 reFan anchor 用)
func enumTopMidNoRefan(botCards, remaining []Card, discardCount int) *FantasyResult {
	N := len(remaining)
	if N != 3+5+discardCount {
		return nil
	}
	botEval := eval5Maybe(botCards)
	var best *FantasyResult
	for i := 0; i < N-2; i++ {
		for j := i + 1; j < N-1; j++ {
			for k := j + 1; k < N; k++ {
				top := []Card{remaining[i], remaining[j], remaining[k]}
				topEval := eval3Maybe(top)
				rest := make([]Card, 0, N-3)
				for x := 0; x < N; x++ {
					if x != i && x != j && x != k {
						rest = append(rest, remaining[x])
					}
				}
				M := len(rest)
				for a := 0; a < M-4; a++ {
					for b := a + 1; b < M-3; b++ {
						for c := b + 1; c < M-2; c++ {
							for d := c + 1; d < M-1; d++ {
								for e := d + 1; e < M; e++ {
									mid := []Card{rest[a], rest[b], rest[c], rest[d], rest[e]}
									midEval := eval5Maybe(mid)
									if midEval.Value > botEval.Value {
										continue
									}
									if HandExceeds5(topEval, midEval) {
										continue
									}
									sc := ScoreHand(top, mid, botCards)
									if sc.Foul {
										continue
									}
									discards := make([]Card, 0, discardCount)
									for x := 0; x < M; x++ {
										if x != a && x != b && x != c && x != d && x != e {
											discards = append(discards, rest[x])
										}
									}
									cand := &FantasyResult{
										Layout:  FantasyLayout{Top: append([]Card{}, top...), Middle: mid, Bottom: botCards, Discards: discards},
										Royalty: sc.Royalties, Sc: sc,
									}
									if best == nil || scTieBreak(cand, best) > 0 {
										best = cand
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return best
}

// enumMidBotForTopPair — 顶 2 张 + kicker 已固定 (高对场景), 穷举 mid + bot, 不要求 mid type >= trips
func enumMidBotForTopPair(topCards, remaining []Card, discardCount int) *FantasyResult {
	N := len(remaining)
	if N != 5+5+discardCount {
		return nil
	}
	topEval := eval3Maybe(topCards)
	var best *FantasyResult
	for i := 0; i < N-4; i++ {
		for j := i + 1; j < N-3; j++ {
			for k := j + 1; k < N-2; k++ {
				for l := k + 1; l < N-1; l++ {
					for m := l + 1; m < N; m++ {
						mid := []Card{remaining[i], remaining[j], remaining[k], remaining[l], remaining[m]}
						midEval := eval5Maybe(mid)
						if HandExceeds5(topEval, midEval) {
							continue
						}
						rest := make([]Card, 0, N-5)
						for x := 0; x < N; x++ {
							if x != i && x != j && x != k && x != l && x != m {
								rest = append(rest, remaining[x])
							}
						}
						M := len(rest)
						for a := 0; a < M-4; a++ {
							for b := a + 1; b < M-3; b++ {
								for c := b + 1; c < M-2; c++ {
									for d := c + 1; d < M-1; d++ {
										for e := d + 1; e < M; e++ {
											bot := []Card{rest[a], rest[b], rest[c], rest[d], rest[e]}
											botEval := eval5Maybe(bot)
											if botEval.Value < midEval.Value {
												continue
											}
											sc := ScoreHand(topCards, mid, bot)
											if sc.Foul {
												continue
											}
											discards := make([]Card, 0, discardCount)
											for x := 0; x < M; x++ {
												if x != a && x != b && x != c && x != d && x != e {
													discards = append(discards, rest[x])
												}
											}
											cand := &FantasyResult{
												Layout:  FantasyLayout{Top: topCards, Middle: mid, Bottom: bot, Discards: discards},
												Royalty: sc.Royalties, Sc: sc,
											}
											if best == nil || scTieBreak(cand, best) > 0 {
												best = cand
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return best
}

// ============================================================
// Anchor → Layout 构造
// ============================================================

// BuildLayoutFromAnchor — 处理 reFan 锚 (top-trips / bot-quads / bot-sf)
func BuildLayoutFromAnchor(anchor FantasyAnchor, dealt []Card, discardCount int) *FantasyLayout {
	remaining := cardSetExclude(dealt, anchor.Cards)
	if anchor.Row == "bot" {
		if len(anchor.Cards) == 5 {
			r := enumTopMid(anchor.Cards, remaining, discardCount)
			if r != nil {
				return &r.Layout
			}
			return nil
		}
		if len(anchor.Cards) == 4 {
			// Bot 4-kind: 试 3 个候选 kicker (最高/最低/中间)
			sorted := append([]Card{}, remaining...)
			sort.SliceStable(sorted, func(i, j int) bool {
				return rankForSort(sorted[i]) > rankForSort(sorted[j])
			})
			seen := make(map[Card]bool)
			cands := make([]Card, 0, 3)
			for _, idx := range []int{0, len(sorted) - 1, len(sorted) / 2} {
				if idx < 0 || idx >= len(sorted) {
					continue
				}
				c := sorted[idx]
				if !seen[c] {
					seen[c] = true
					cands = append(cands, c)
				}
			}
			var best *FantasyResult
			for _, kicker := range cands {
				bot := append(append([]Card{}, anchor.Cards...), kicker)
				restAfter := cardSetExclude(remaining, []Card{kicker})
				r := enumTopMid(bot, restAfter, discardCount)
				if r != nil && (best == nil || scTieBreak(r, best) > 0) {
					best = r
				}
			}
			if best != nil {
				return &best.Layout
			}
			return nil
		}
		return nil
	}
	if anchor.Row == "top" {
		// top-trips (3 张固定) → 由 BuildTopTripsLayout 接管 (含 method A + beam fallback)
		// 简单 enumMidBot (含 reFan 剪枝) 也作为兜底, 但 BuildTopTripsLayout 优于
		layout := BuildTopTripsLayout(anchor.Cards, dealt, discardCount)
		if layout != nil {
			return layout
		}
		r := enumMidBot(anchor.Cards, remaining, discardCount)
		if r != nil {
			return &r.Layout
		}
		return nil
	}
	return nil
}

// BuildLayoutFromNonRefanAnchor — 处理非 reFan 锚 (mid 5 / mid4 / bot5 / top2)
func BuildLayoutFromNonRefanAnchor(anchor FantasyAnchor, dealt []Card, discardCount int) *FantasyLayout {
	remaining := cardSetExclude(dealt, anchor.Cards)
	switch anchor.Row {
	case "mid":
		r := enumTopBot(anchor.Cards, remaining, discardCount)
		if r != nil {
			return &r.Layout
		}
		return nil
	case "bot5":
		r := enumTopMidNoRefan(anchor.Cards, remaining, discardCount)
		if r != nil {
			return &r.Layout
		}
		return nil
	case "mid4":
		// 4-kind on mid: 试 3 个 kicker
		sorted := append([]Card{}, remaining...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return rankForSort(sorted[i]) > rankForSort(sorted[j])
		})
		seen := make(map[Card]bool)
		cands := make([]Card, 0, 3)
		for _, idx := range []int{0, len(sorted) / 2, len(sorted) - 1} {
			if idx < 0 || idx >= len(sorted) {
				continue
			}
			c := sorted[idx]
			if !seen[c] {
				seen[c] = true
				cands = append(cands, c)
			}
		}
		var best *FantasyResult
		for _, kicker := range cands {
			mid := append(append([]Card{}, anchor.Cards...), kicker)
			restAfter := cardSetExclude(remaining, []Card{kicker})
			r := enumTopBot(mid, restAfter, discardCount)
			if r != nil && (best == nil || scTieBreak(r, best) > 0) {
				best = r
			}
		}
		if best != nil {
			return &best.Layout
		}
		return nil
	case "top2":
		// 4 候选 kicker (最低 2 + 最高 2), 然后 enumMidBotForTopPair
		sorted := append([]Card{}, remaining...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return rankForSort(sorted[i]) < rankForSort(sorted[j])
		})
		_N := len(sorted)
		seen := make(map[Card]bool)
		cands := make([]Card, 0, 4)
		tryAdd := func(idx int) {
			if idx < 0 || idx >= _N {
				return
			}
			c := sorted[idx]
			if !seen[c] {
				seen[c] = true
				cands = append(cands, c)
			}
		}
		tryAdd(0)
		tryAdd(1)
		tryAdd(_N - 2)
		tryAdd(_N - 1)
		var best *FantasyResult
		for _, kicker := range cands {
			top := append(append([]Card{}, anchor.Cards...), kicker)
			restAfter := cardSetExclude(remaining, []Card{kicker})
			r := enumMidBotForTopPair(top, restAfter, discardCount)
			if r != nil && (best == nil || scTieBreak(r, best) > 0) {
				best = r
			}
		}
		if best != nil {
			return &best.Layout
		}
		return nil
	}
	return nil
}

// ============================================================
// 顶 trips 锚专用 (方案 A 高 rank mid trips 候选 + 方案 B beam 兜底)
// ============================================================

func BuildTopTripsLayout(topCards []Card, dealt []Card, discardCount int) *FantasyLayout {
	remaining := cardSetExclude(dealt, topCards)
	if len(remaining) != 10+discardCount {
		return nil
	}
	var real, jokers []Card
	for _, c := range remaining {
		if c.IsJoker() {
			jokers = append(jokers, c)
		} else {
			real = append(real, c)
		}
	}
	J := len(jokers)
	// topRankIdx: 优先 real top card; 全 joker 时按 12 (A) 算
	topRankIdx := 12
	for _, c := range topCards {
		if !c.IsJoker() {
			topRankIdx = int(c.Rank())
			break
		}
	}
	byRank := make(map[uint8][]Card)
	rankOrder := make([]uint8, 0)
	for _, c := range real {
		r := c.Rank()
		if _, ok := byRank[r]; !ok {
			rankOrder = append(rankOrder, r)
		}
		byRank[r] = append(byRank[r], c)
	}
	var best *FantasyResult
	// 方案 A: 高 rank mid trips 候选
	for _, r := range rankOrder {
		if int(r) <= topRankIdx {
			continue
		}
		cs := byRank[r]
		if len(cs)+J >= 3 {
			realUsed := cs
			if len(cs) >= 3 {
				realUsed = cs[:3]
			}
			jUsed := jokers[:max(0, 3-len(realUsed))]
			midCore := append(append([]Card{}, realUsed...), jUsed...)
			r2 := enumBotForTopMidTrips(topCards, remaining, midCore, discardCount)
			if r2 != nil && (best == nil || r2.Royalty > best.Royalty) {
				best = r2
			}
		}
	}
	// 方案 B: beam 兜底
	beam := distributeMidBotByBeam(topCards, remaining, discardCount, 15)
	if beam != nil {
		sc := ScoreHand(beam.Top, beam.Middle, beam.Bottom)
		if !sc.Foul {
			// 触发再范条件: top.type >= 3 OR bot.type >= 7
			trig := sc.TopEval.Type >= 3 || sc.BotEval.Type >= 7
			if trig && (best == nil || sc.Royalties > best.Royalty) {
				best = &FantasyResult{Layout: *beam, Royalty: sc.Royalties, Sc: sc}
			}
		}
	}
	if best != nil {
		return &best.Layout
	}
	return nil
}

// enumBotForTopMidTrips — 给定 top + mid trips core, 穷举 bot 5-subset (C(N,5)=252), trig=true required
func enumBotForTopMidTrips(topCards, remaining, midCore []Card, discardCount int) *FantasyResult {
	rest10 := cardSetExclude(remaining, midCore)
	if len(rest10) != 7+discardCount {
		return nil
	}
	N := len(rest10)
	combos := make([][]int, 0, 252)
	var gen func(start int, current []int)
	gen = func(start int, current []int) {
		if len(current) == 5 {
			cp := make([]int, 5)
			copy(cp, current)
			combos = append(combos, cp)
			return
		}
		for i := start; i < N; i++ {
			gen(i+1, append(current, i))
		}
	}
	gen(0, nil)
	var best *FantasyResult
	for _, botIdxs := range combos {
		bot := make([]Card, 5)
		botSet := make(map[int]bool, 5)
		for i, idx := range botIdxs {
			bot[i] = rest10[idx]
			botSet[idx] = true
		}
		restCards := make([]Card, 0, N-5)
		for i := 0; i < N; i++ {
			if !botSet[i] {
				restCards = append(restCards, rest10[i])
			}
		}
		// 排序 desc: top 2 → mid 补足
		sort.SliceStable(restCards, func(i, j int) bool {
			return rankForSort(restCards[i]) > rankForSort(restCards[j])
		})
		midKickers := restCards[:2]
		discards := restCards[2:]
		if len(discards) != discardCount {
			continue
		}
		fullMid := append(append([]Card{}, midCore...), midKickers...)
		sc := ScoreHand(topCards, fullMid, bot)
		if sc.Foul {
			continue
		}
		// 触发再范
		trig := sc.TopEval.Type >= 3 || sc.BotEval.Type >= 7
		if !trig {
			continue
		}
		if best == nil || sc.Royalties > best.Royalty {
			best = &FantasyResult{
				Layout:  FantasyLayout{Top: topCards, Middle: fullMid, Bottom: bot, Discards: append([]Card{}, discards...)},
				Royalty: sc.Royalties, Sc: sc,
			}
		}
	}
	return best
}

// ============================================================
// Beam search (用作 BuildTopTripsLayout 的 fallback, 也是 Phase 2 主体的雏形)
// ============================================================

type beamItem struct {
	mid, bot, discards []Card
	score              float64
}

func distributeMidBotByBeam(top []Card, remaining []Card, discardCount, beamWidth int) *FantasyLayout {
	beam := []*beamItem{{}}
	for _, card := range remaining {
		next := make([]*beamItem, 0, len(beam)*3)
		for _, item := range beam {
			if len(item.mid) < 5 {
				cp := *item
				cp.mid = append(append([]Card{}, item.mid...), card)
				next = append(next, &cp)
			}
			if len(item.bot) < 5 {
				cp := *item
				cp.bot = append(append([]Card{}, item.bot...), card)
				next = append(next, &cp)
			}
			if len(item.discards) < discardCount {
				cp := *item
				cp.discards = append(append([]Card{}, item.discards...), card)
				next = append(next, &cp)
			}
		}
		if len(next) == 0 {
			return nil
		}
		for _, n := range next {
			n.score = partialRoyaltyScore(n)
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].score > next[j].score })
		if len(next) > beamWidth {
			next = next[:beamWidth]
		}
		beam = next
	}
	var best *FantasyLayout
	var bestRoy = -1
	for _, b := range beam {
		if len(b.mid) != 5 || len(b.bot) != 5 || len(b.discards) != discardCount {
			continue
		}
		sc := ScoreHand(top, b.mid, b.bot)
		if sc.Foul {
			continue
		}
		if best == nil || sc.Royalties > bestRoy {
			best = &FantasyLayout{Top: top, Middle: append([]Card{}, b.mid...), Bottom: append([]Card{}, b.bot...), Discards: append([]Card{}, b.discards...)}
			bestRoy = sc.Royalties
		}
	}
	return best
}

// partialRoyaltyScore — beam 启发评分 (简单 royalty 潜力)
func partialRoyaltyScore(s *beamItem) float64 {
	score := 0.0
	evalRow := func(row []Card, weight float64) {
		if len(row) == 0 {
			return
		}
		ranks := make(map[uint8]int)
		suits := make(map[uint8]int)
		jokers := 0
		for _, c := range row {
			if c.IsJoker() {
				jokers++
				continue
			}
			ranks[c.Rank()]++
			suits[c.Suit()]++
		}
		maxSameRank := 0
		for _, v := range ranks {
			if v > maxSameRank {
				maxSameRank = v
			}
		}
		maxSameRank += jokers
		switch {
		case maxSameRank >= 4:
			score += 20 * weight
		case maxSameRank >= 3:
			score += 10 * weight
		case maxSameRank >= 2:
			score += 3 * weight
		}
		pairCnt := 0
		for _, v := range ranks {
			if v >= 2 {
				pairCnt++
			}
		}
		if pairCnt >= 2 {
			score += 5 * weight
		}
		maxSuit := 0
		for _, v := range suits {
			if v > maxSuit {
				maxSuit = v
			}
		}
		maxSuit += jokers
		switch {
		case maxSuit >= 5:
			score += 12 * weight
		case maxSuit >= 4:
			score += 5 * weight
		case maxSuit >= 3:
			score += 1.5 * weight
		}
		// 顺子潜力
		seen := make(map[int]bool)
		for _, c := range row {
			if !c.IsJoker() {
				seen[int(c.Rank())] = true
			}
		}
		ri := make([]int, 0, len(seen))
		for k := range seen {
			ri = append(ri, k)
		}
		sort.Ints(ri)
		bestRun, run := 1, 1
		for i := 1; i < len(ri); i++ {
			if ri[i]-ri[i-1] <= 2 {
				run++
				if run > bestRun {
					bestRun = run
				}
			} else {
				run = 1
			}
		}
		effRun := bestRun + jokers
		switch {
		case effRun >= 5:
			score += 10 * weight
		case effRun >= 4:
			score += 4 * weight
		}
	}
	evalRow(s.bot, 1.5)
	evalRow(s.mid, 1.0)
	return score
}

// ============================================================
// 主决策: DirectReFanSearch (Phase 1 入口)
// ============================================================

// DirectReFanSearch — 枚举锚 → 构 layout → 选最高 royalty
// 如果有 reFan 解, 返回 reFan 锚下最高 royalty 的; 否则返回 nonReFan 高价值锚最高 royalty 的
// 都没解返回 nil (留 Phase 2 的 beam search 兜底)
func DirectReFanSearch(dealt []Card, discardCount int) *FantasyResult {
	anchors := FindReFanAnchors(dealt)
	// 排锚: bot anchors first, 内部按 rank desc
	sort.SliceStable(anchors, func(i, j int) bool {
		a, b := anchors[i], anchors[j]
		aRank, bRank := 12, 12
		for _, c := range a.Cards {
			if !c.IsJoker() {
				aRank = int(c.Rank())
				break
			}
		}
		for _, c := range b.Cards {
			if !c.IsJoker() {
				bRank = int(c.Rank())
				break
			}
		}
		aType := 1
		if a.Row == "bot" {
			aType = 0
		}
		bType := 1
		if b.Row == "bot" {
			bType = 0
		}
		if aType != bType {
			return aType < bType
		}
		return bRank < aRank
	})
	cands := make([]*FantasyResult, 0)
	for _, anchor := range anchors {
		layout := BuildLayoutFromAnchor(anchor, dealt, discardCount)
		if layout == nil {
			continue
		}
		sc := ScoreHand(layout.Top, layout.Middle, layout.Bottom)
		if sc.Foul {
			continue
		}
		// 必须 reFan: top trips OR bot 4-kind+
		if sc.TopEval.Type < 3 && sc.BotEval.Type < 7 {
			continue
		}
		cands = append(cands, &FantasyResult{Layout: *layout, Royalty: sc.Royalties, Sc: sc})
	}
	if len(cands) > 0 {
		sort.SliceStable(cands, func(i, j int) bool {
			return scTieBreak(cands[i], cands[j]) > 0
		})
		return cands[0]
	}
	// Phase 2: 非 reFan 高价值锚
	nrAnchors := FindNonRefanAnchors(dealt)
	nrCands := make([]*FantasyResult, 0)
	for _, anchor := range nrAnchors {
		layout := BuildLayoutFromNonRefanAnchor(anchor, dealt, discardCount)
		if layout == nil {
			continue
		}
		sc := ScoreHand(layout.Top, layout.Middle, layout.Bottom)
		if sc.Foul {
			continue
		}
		nrCands = append(nrCands, &FantasyResult{Layout: *layout, Royalty: sc.Royalties, Sc: sc})
	}
	if len(nrCands) > 0 {
		sort.SliceStable(nrCands, func(i, j int) bool {
			return scTieBreak(nrCands[i], nrCands[j]) > 0
		})
		return nrCands[0]
	}
	return nil
}

// ExpertPlaceFantasy — 完整 FL 入口 (Phase 1 + Phase 2):
//   1. DirectReFanSearch (reFan/nonReFan 锚直枚举) — 覆盖 ~94% case
//   2. ExpertPlaceFantasyBeam — beam search + trainedEval scoring (兜底 + 复杂 case)
//   3. AntiFoulFallback — beam 全 foul 时安全摆法
//
// dealt = 13 + discardCount cards.
func ExpertPlaceFantasy(dealt []Card, discardCount int) *FantasyResult {
	if len(dealt)-discardCount != 13 {
		return nil
	}
	if r := DirectReFanSearch(dealt, discardCount); r != nil {
		return r
	}
	if r := ExpertPlaceFantasyBeam(dealt, discardCount, 10); r != nil {
		return r
	}
	if l := AntiFoulFallback(dealt, discardCount); l != nil {
		sc := ScoreHand(l.Top, l.Middle, l.Bottom)
		return &FantasyResult{Layout: *l, Royalty: sc.Royalties, Sc: sc}
	}
	return nil
}

// ============================================================
// Phase 2: beam search + trainedEval scoring (与 JS _expertPlaceFantasyImpl 一致)
// ============================================================

type beamFLItem struct {
	top, mid, bot, discards []Card
	score                   float64
}

// ExpertPlaceFantasyBeam — beam search 主体, 把 dealt 分配到 top/mid/bot/discards.
// 与 JS _expertPlaceFantasyImpl 行为一致: 高 rank 先入, 鬼牌最后; 评分用 trainedEval;
// 提前 foul 检测 -1e6; 完整后选 max-score non-foul.
//
// epsilon 在 JS 默认 0 (deterministic), 我们也不实现随机分支 (生产不需要).
func ExpertPlaceFantasyBeam(dealt []Card, discardCount, beamWidth int) *FantasyResult {
	N := len(dealt)
	if N-discardCount != 13 {
		return nil
	}

	// Sort: 高 rank 先, 鬼牌最后 (与 JS 一致, joker 看完真牌再决定)
	sorted := append([]Card{}, dealt...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, aj := sorted[i], sorted[j]
		if ai.IsJoker() && !aj.IsJoker() {
			return false
		}
		if !ai.IsJoker() && aj.IsJoker() {
			return true
		}
		if ai.IsJoker() && aj.IsJoker() {
			return false
		}
		return ai.Rank() > aj.Rank()
	})

	beam := []*beamFLItem{{}}
	for depth := 0; depth < N; depth++ {
		card := sorted[depth]
		next := make([]*beamFLItem, 0, len(beam)*4)
		for _, item := range beam {
			if len(item.top) < 3 {
				cp := cloneBeamFLItem(item)
				cp.top = append(cp.top, card)
				next = append(next, cp)
			}
			if len(item.mid) < 5 {
				cp := cloneBeamFLItem(item)
				cp.mid = append(cp.mid, card)
				next = append(next, cp)
			}
			if len(item.bot) < 5 {
				cp := cloneBeamFLItem(item)
				cp.bot = append(cp.bot, card)
				next = append(next, cp)
			}
			if len(item.discards) < discardCount {
				cp := cloneBeamFLItem(item)
				cp.discards = append(cp.discards, card)
				next = append(next, cp)
			}
		}
		if len(next) == 0 {
			return nil
		}

		// 评分 (trainedEval + 提前 foul 惩罚)
		// 复用 GameState 仅作 TrainedEval 接口, 但避免 PlaceCard 开销 — 直接构 GameState
		for _, n := range next {
			score := scoreBeamItem(n)
			n.score = score
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].score > next[j].score })
		if len(next) > beamWidth {
			next = next[:beamWidth]
		}
		beam = next
	}

	// 选最佳完整 non-foul
	var completed []*beamFLItem
	for _, b := range beam {
		if len(b.top) == 3 && len(b.mid) == 5 && len(b.bot) == 5 {
			completed = append(completed, b)
		}
	}
	if len(completed) == 0 {
		return nil
	}
	// 硬过滤 foul
	var nonFoul []*beamFLItem
	for _, b := range completed {
		sc := ScoreHand(b.top, b.mid, b.bot)
		if !sc.Foul {
			nonFoul = append(nonFoul, b)
		}
	}
	if len(nonFoul) == 0 {
		// beam 全 foul → 调用方走 AntiFoulFallback
		return nil
	}
	sort.SliceStable(nonFoul, func(i, j int) bool { return nonFoul[i].score > nonFoul[j].score })
	chosen := nonFoul[0]
	sc := ScoreHand(chosen.top, chosen.mid, chosen.bot)
	return &FantasyResult{
		Layout: FantasyLayout{
			Top:      append([]Card{}, chosen.top...),
			Middle:   append([]Card{}, chosen.mid...),
			Bottom:   append([]Card{}, chosen.bot...),
			Discards: append([]Card{}, chosen.discards...),
		},
		Royalty: sc.Royalties,
		Sc:      sc,
	}
}

func cloneBeamFLItem(s *beamFLItem) *beamFLItem {
	cp := &beamFLItem{
		top:      append([]Card{}, s.top...),
		mid:      append([]Card{}, s.mid...),
		bot:      append([]Card{}, s.bot...),
		discards: append([]Card{}, s.discards...),
		score:    s.score,
	}
	return cp
}

// scoreBeamItem — trainedEval(state) + foul 提前检测惩罚 -1e6
//
// 与 JS:
//   if (n.top.length === 3 && n.mid.length === 5) {
//       if (handExceeds5(top, mid)) n.score -= 1e6
//   }
//   if (n.mid.length === 5 && n.bot.length === 5) {
//       if (mid.value > bot.value) n.score -= 1e6
//   }
func scoreBeamItem(n *beamFLItem) float64 {
	gs := &GameState{
		Top: n.top, Middle: n.mid, Bottom: n.bot,
	}
	score := float64(TrainedEval(gs))
	if len(n.top) == 3 && len(n.mid) == 5 {
		tE := eval3Maybe(n.top)
		mE := eval5Maybe(n.mid)
		if HandExceeds5(tE, mE) {
			score -= 1e6
		}
	}
	if len(n.mid) == 5 && len(n.bot) == 5 {
		mE := eval5Maybe(n.mid)
		bE := eval5Maybe(n.bot)
		if mE.Value > bE.Value {
			score -= 1e6
		}
	}
	return score
}

// ============================================================
// AntiFoulFallback — beam 全 foul 时安全摆法 (与 JS _antiFoulFallback 一致)
// ============================================================

// AntiFoulFallback — 4 种策略尝试找一个 non-foul 摆法
func AntiFoulFallback(dealt []Card, discardCount int) *FantasyLayout {
	asc := append([]Card{}, dealt...)
	sort.SliceStable(asc, func(i, j int) bool {
		return rankForSort(asc[i]) < rankForSort(asc[j])
	})
	tryLayout := func(top, mid, bot, disc []Card) *FantasyLayout {
		if len(top) != 3 || len(mid) != 5 || len(bot) != 5 {
			return nil
		}
		sc := ScoreHand(top, mid, bot)
		if sc.Foul {
			return nil
		}
		return &FantasyLayout{
			Top: append([]Card{}, top...), Middle: append([]Card{}, mid...),
			Bottom: append([]Card{}, bot...), Discards: append([]Card{}, disc...),
		}
	}

	// 策略 1: 弃最低 (升序), 顶 3 低 / 中 5 / 底 5 高
	if len(asc) >= 13+discardCount {
		disc := asc[:discardCount]
		rest := asc[discardCount:]
		if r := tryLayout(rest[:3], rest[3:8], rest[8:13], disc); r != nil {
			return r
		}
	}
	// 策略 2: 弃中段 (top 用最低 3, 弃 [3:3+dc], 剩 mid+bot)
	if len(asc) >= 13+discardCount {
		top := asc[:3]
		disc := asc[3 : 3+discardCount]
		remaining := asc[3+discardCount:]
		if len(remaining) >= 10 {
			if r := tryLayout(top, remaining[:5], remaining[5:10], disc); r != nil {
				return r
			}
		}
	}
	// 策略 3: 弃最高
	if len(asc) >= 13+discardCount {
		disc := asc[len(asc)-discardCount:]
		rest := asc[:len(asc)-discardCount]
		if r := tryLayout(rest[:3], rest[3:8], rest[8:13], disc); r != nil {
			return r
		}
	}
	// 策略 4: 多次 shuffle, 取升序 — 这里去掉 RNG (测试稳定), 改为枚举一些 deterministic 排列
	// 2026-06-01 修: shift 上限 20 超过 len(asc) (fantasy dealt 通常 14-17), `asc[shift:]` 越界 panic.
	// 改成 shift < len(asc).
	for shift := 1; shift < len(asc); shift++ {
		rotated := append([]Card{}, asc[shift:]...)
		rotated = append(rotated, asc[:shift]...)
		sort.SliceStable(rotated, func(i, j int) bool {
			// 复杂排序: 按 (rank+shift) % 13 来打乱; 仍按 rank
			return rankForSort(rotated[i])*7+shift < rankForSort(rotated[j])*7+shift
		})
		if len(rotated) < 13+discardCount {
			continue
		}
		disc := rotated[:discardCount]
		rest := append([]Card{}, rotated[discardCount:]...)
		sort.SliceStable(rest, func(i, j int) bool {
			return rankForSort(rest[i]) < rankForSort(rest[j])
		})
		if r := tryLayout(rest[:3], rest[3:8], rest[8:13], disc); r != nil {
			return r
		}
	}
	return nil
}
