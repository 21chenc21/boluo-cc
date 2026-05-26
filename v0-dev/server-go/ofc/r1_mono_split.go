package ofc

// R1 monotonic-split tie-break: 摆完后 mid+bot 都是 high-card 时,
// "中道低牌应是 dealt 中最低的 K 张" — 不允许 mid 有低牌但 bot 有更低的低牌.
//
// 用户实战观察 (UR17 dealt 2,3,4,J + Ac):
//   ✓ 23 中 4J 底, 234 中 J 底 — mid 取连续最低
//   ✗ 24 中 3J 底, 34 中 2J 底 — 跳号, 人不会这么摆
//
// EV 差距 1-2 分 (gap straight 损失), 在 rollout noise 内, 但人感很强.
// 此 tie-break 仅在 mid AND bot 都是 high-card (无 pair/flush/straight royalty)
// 时生效, 不会干扰 33 中 / 同花追 / 高对追范 类正常摆法.
//
// 输出 inversions 数 (mid 中的低牌 > bot 中的低牌的次数), 0 = 完美顺序.

const monoSplitLowThr = 6 // rank 6 = card 8, "low connector" 范围

// hasPairOrJoker — 部分行: 有同 rank 重复 OR 含鬼 (鬼=潜在配对) 即视为 "有信号", 跳过 mono.
// 不用 eval5/eval3, 它们对 < 5/3 牌返 Type=-1, 不可靠.
func hasPairOrJoker(cards []Card) bool {
	seen := make(map[uint8]bool, len(cards))
	for _, c := range cards {
		if c.IsJoker() {
			return true
		}
		r := c.Rank()
		if seen[r] {
			return true
		}
		seen[r] = true
	}
	return false
}

func MonoSplitBadness(gs *GameState) int {
	// 仅在 mid 与 bot 都没"信号"(对子/鬼)时生效, 不干扰 33 中 / 鬼追范 等正常摆法.
	if hasPairOrJoker(gs.Middle) || hasPairOrJoker(gs.Bottom) {
		return 0
	}
	midLow := []int{}
	botLow := []int{}
	for _, c := range gs.Middle {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		if r <= monoSplitLowThr {
			midLow = append(midLow, r)
		}
	}
	for _, c := range gs.Bottom {
		if c.IsJoker() {
			continue
		}
		r := int(c.Rank())
		if r <= monoSplitLowThr {
			botLow = append(botLow, r)
		}
	}
	bad := 0
	for _, m := range midLow {
		for _, b := range botLow {
			if m > b {
				bad++
			}
		}
	}
	return bad
}
