package ofc

// R1 ace-joker hardcoded rules.
//
// 用户报告 v7/v0 在 AA+鬼 / A+鬼 模式上常错 (J3 / UR1 / UR2 / UR3).
// 模型选择 "joker+A 上顶组 AAA trips" 而非用户期望的 "AA 上顶留灵活 / A+joker 上顶".
//
// 规则:
//   AAJ (2 真 A + 1+ 鬼): AA 顶, 鬼不上顶 — 保灵活性, 顶留 1 空给未来牌
//   AJ  (1 真 A + 1 鬼): A 与鬼共上顶 — 隐含 AA pair 进 fantasy

// classifyR1AcePattern — 检测 dealt 5 张是否匹配 AAJ / AJ 模式
// 返回:
//   pattern: "AAJ" | "AJ" | ""
//   aceIdxs: 真 A 在 cards 中的下标
//   jokerIdxs: 鬼在 cards 中的下标
func classifyR1AcePattern(cards []Card) (string, []int, []int) {
	if len(cards) != 5 {
		return "", nil, nil
	}
	aceIdxs := []int{}
	jokerIdxs := []int{}
	for i, c := range cards {
		if c.IsJoker() {
			jokerIdxs = append(jokerIdxs, i)
		} else if c.Rank() == 12 { // A
			aceIdxs = append(aceIdxs, i)
		}
	}
	if len(jokerIdxs) >= 1 && len(aceIdxs) == 2 {
		return "AAJ", aceIdxs, jokerIdxs
	}
	if len(jokerIdxs) == 1 && len(aceIdxs) == 1 {
		return "AJ", aceIdxs, jokerIdxs
	}
	return "", nil, nil
}

// filterR1AceJokerPlacements — 当 cards 匹配 AAJ / AJ 时,
// 裁剪 candidates 只保留符合规则的 placement.
//
// 返回 nil 表示 "未匹配模式, 不应过滤" (caller 沿用原 candidates).
// 返回 []Placement{} 长度 0 表示 "匹配但无候选" (异常, 退化为不过滤).
func filterR1AceJokerPlacements(cards []Card, candidates []Placement) []Placement {
	pat, aceIdxs, jokerIdxs := classifyR1AcePattern(cards)
	if pat == "" {
		return nil
	}
	out := make([]Placement, 0, len(candidates)/4)
	for _, p := range candidates {
		ok := false
		switch pat {
		case "AAJ":
			// 两 A 都在顶, 任一鬼不能在顶
			a1Top := p[aceIdxs[0]] == RowTop
			a2Top := p[aceIdxs[1]] == RowTop
			jTop := false
			for _, ji := range jokerIdxs {
				if p[ji] == RowTop {
					jTop = true
					break
				}
			}
			ok = a1Top && a2Top && !jTop
		case "AJ":
			// A 与鬼都在顶
			aTop := p[aceIdxs[0]] == RowTop
			jTop := p[jokerIdxs[0]] == RowTop
			ok = aTop && jTop
		}
		if ok {
			out = append(out, p)
		}
	}
	return out
}
