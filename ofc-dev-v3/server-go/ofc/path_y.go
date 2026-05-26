package ofc

// Path Y — 候选广化 (anchor-based candidate seeding for R1 stage 1)
//
// Motivation: v7_fan 1.0.0 simpleEval 把 botType*22 权重给得过高, 让"4kind on bottom"
// 类候选碾压 stage1 top-30, 把"AA on top + secured fantasy"类候选挤出筛选窗口.
// stage 3 boost (Path X) 救不回这些被提前筛掉的候选 — 测试已验证 Path X 无效.
//
// Path Y 直接攻 stage 1: 识别 R1 摆完后的 partial state 中的 fantasy/draw anchor,
// 强制保留这些候选进 stage 1 pool, 不论 simpleEval 怎么排.
//
// 检测的 anchor 类型:
//   1. Fantasy chase on top (QQ+ pair / trips / joker+high pair / 双鬼)
//   2. 4-suit-flush draw on bottom (4 张同色或 3+joker)
//
// 不检测的 (留给 simpleEval / quickRollout):
//   - 顺子 draw (3-card 连号) — simpleEval 对此覆盖足够
//   - 顶 高 high card (没 pair) — fantasy chase 概率太低
//   - 中道 SF / 4kind 锚 — 已经是 simpleEval 高分, 不需保护

// IsFantasyAnchorR1 — 检测 partial gs 是否在追范路径上 (R1 摆完后判定)
func IsFantasyAnchorR1(gs *GameState) bool {
	top := gs.Top
	switch len(top) {
	case 0:
		return false // 顶空, 没追范信号
	case 1:
		// 单 joker 上顶 = "等高牌配对进范"路径 (memory 里 J2 / 用户 case 都期望)
		return top[0].IsJoker()
	case 2:
		var jokers, ar, kr, qr int
		for _, c := range top {
			if c.IsJoker() {
				jokers++
				continue
			}
			switch c.Rank() {
			case 12:
				ar++
			case 11:
				kr++
			case 10:
				qr++
			}
		}
		// QQ/KK/AA pair = 已经进范条件 (顶 ≥ QQ pair 触发 fantasy)
		if ar == 2 || kr == 2 || qr == 2 {
			return true
		}
		// 鬼+A/K/Q = 隐含 AA/KK/QQ pair (joker 当对子)
		if jokers == 1 && (ar == 1 || kr == 1 || qr == 1) {
			return true
		}
		// 双鬼 = 隐含 AA pair (虚高对)
		if jokers == 2 {
			return true
		}
		return false
	case 3:
		// 顶 3 张 — 评估是否 trips 或 QQ+ pair
		ev := eval3Maybe(top)
		if ev.Type >= TypeThreeOfAKind {
			return true
		}
		if ev.Type == TypePair {
			pairR := int((ev.Value - 1000000) / 15)
			return pairR >= 10 // QQ+
		}
		return false
	}
	return false
}

// IsFlushDrawAnchorR1 — 底道 4-suit (考虑 joker 当 wild) → flush 大概率
func IsFlushDrawAnchorR1(gs *GameState) bool {
	bot := gs.Bottom
	if len(bot) < 4 {
		return false
	}
	suits := make(map[uint8]int)
	jokers := 0
	for _, c := range bot {
		if c.IsJoker() {
			jokers++
		} else {
			suits[c.Suit()]++
		}
	}
	for _, n := range suits {
		if n+jokers >= 4 {
			return true
		}
	}
	return false
}

// IsAnchorR1 — 综合判定: 任一 anchor 触发即返 true
func IsAnchorR1(gs *GameState) bool {
	return IsFantasyAnchorR1(gs) || IsFlushDrawAnchorR1(gs)
}

// Anchor boost 分级 — fy 已经被 fan_logit / fanRate 广泛覆盖, +5 够补 noise.
// fl 在 r1-debug UR9 实测里仍输冠军 1.4 分 (4♠底 raw 46.17 vs winner 47.56),
// 单一 +5 不够, 提到 +8 才能稳吃 winner.
// 双 anchor 候选自动累加 (fy+fl = +13), 强化 "AA-top + 4♠-bot" 这类高价值路径.
const (
	FantasyAnchorBoost float32 = 5
	FlushAnchorBoost   float32 = 8
)

// AnchorBoost — 候选根据触发的 anchor 类型累加 boost.
// 0 = 没 anchor; 5 = 仅 fantasy; 8 = 仅 flush; 13 = 双 anchor.
func AnchorBoost(gs *GameState) float32 {
	var b float32
	if IsFantasyAnchorR1(gs) {
		b += FantasyAnchorBoost
	}
	if IsFlushDrawAnchorR1(gs) {
		b += FlushAnchorBoost
	}
	return b
}
