package ofc

// Placement — 一组放牌方案, 长度等于 cards 数量, 每元素是 Row
type Placement []Row

// RoundNAction — R2-5 的一手动作: 弃哪张 + 剩下 2 张怎么摆
type RoundNAction struct {
	DiscardIdx int       // dealt 中弃的 index
	Kept       []Card    // 保留的 2 张
	Placement  Placement // 长度 2, 对应 Kept 的位置
}

// GeneratePlacements — DFS 枚举所有合法放置 (与 JS generatePlacements 一致)
// 返回 placements, 每个 placement 长度 = len(cards), 元素是 Row
func GeneratePlacements(cards []Card, gs *GameState) []Placement {
	var results []Placement
	topCap := gs.TopSlots()
	midCap := gs.MidSlots()
	botCap := gs.BotSlots()
	current := make([]Row, len(cards))

	var dfs func(idx, nt, nm, nb int)
	dfs = func(idx, nt, nm, nb int) {
		if idx == len(cards) {
			cp := make(Placement, len(current))
			copy(cp, current)
			results = append(results, cp)
			return
		}
		// 顺序: top, middle, bottom (与 JS 一致)
		if nt < topCap {
			current[idx] = RowTop
			dfs(idx+1, nt+1, nm, nb)
		}
		if nm < midCap {
			current[idx] = RowMiddle
			dfs(idx+1, nt, nm+1, nb)
		}
		if nb < botCap {
			current[idx] = RowBottom
			dfs(idx+1, nt, nm, nb+1)
		}
	}
	dfs(0, 0, 0, 0)
	return results
}

// GenerateRound1Actions — R1 全摆
func GenerateRound1Actions(cards []Card, gs *GameState) []Placement {
	return GeneratePlacements(cards, gs)
}

// GenerateRoundNActions — R2-R5 弃 1 摆 2
func GenerateRoundNActions(cards []Card, gs *GameState) []RoundNAction {
	var actions []RoundNAction
	for d := 0; d < len(cards); d++ {
		kept := make([]Card, 0, len(cards)-1)
		for i, c := range cards {
			if i != d {
				kept = append(kept, c)
			}
		}
		placements := GeneratePlacements(kept, gs)
		for _, p := range placements {
			actions = append(actions, RoundNAction{
				DiscardIdx: d,
				Kept:       kept,
				Placement:  p,
			})
		}
	}
	return actions
}

// ApplyPlacement — 把 placement 应用到 state (会修改 state)
func ApplyPlacement(gs *GameState, cards []Card, p Placement) {
	for i, c := range cards {
		gs.PlaceCard(c, p[i])
	}
}
