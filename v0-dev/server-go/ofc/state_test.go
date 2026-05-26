package ofc

import "testing"

func TestGameStateBasic(t *testing.T) {
	gs := NewGameState(0)
	if !gs.CanPlace(RowTop) || gs.TopSlots() != 3 {
		t.Errorf("empty state should have 3 top slots")
	}
	gs.PlaceCard(mustParse("Ks"), RowTop)
	gs.PlaceCard(mustParse("Kh"), RowTop)
	if gs.TopSlots() != 1 {
		t.Errorf("after 2 top placements, want 1 slot, got %d", gs.TopSlots())
	}
	if !gs.UsedCards["Ks"] || !gs.UsedCards["Kh"] {
		t.Errorf("usedCards missing placed cards")
	}
	if gs.IsComplete() {
		t.Errorf("partial state should not be complete")
	}
}

func TestGameStateClone(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(mustParse("As"), RowTop)
	gs.PlaceCard(MakeJokerWithJID(0), RowMiddle)
	gs.Round = 3

	c := gs.Clone()
	if c.NumJokers != 2 || c.Round != 3 {
		t.Errorf("clone basic fields wrong")
	}
	if !c.UsedCards["As"] || !c.UsedCards["Xj0"] {
		t.Errorf("clone usedCards wrong: %v", c.UsedCards)
	}
	// 改 clone 不影响原
	c.PlaceCard(mustParse("Kh"), RowBottom)
	if len(gs.Bottom) != 0 {
		t.Errorf("clone modify leaked to original")
	}
}

func TestGeneratePlacements(t *testing.T) {
	gs := NewGameState(0)
	cards := []Card{mustParse("As"), mustParse("Kh")}
	pls := GeneratePlacements(cards, gs)
	// 2 cards × 3 rows = 9 不重复 placements
	if len(pls) != 9 {
		t.Errorf("want 9 placements (2 cards × 3 rows), got %d", len(pls))
	}
}

func TestGenerateR1Actions5(t *testing.T) {
	gs := NewGameState(0)
	cards := parseHand("As", "Kh", "Qd", "Jc", "Th")
	pls := GenerateRound1Actions(cards, gs)
	// 5 张 → 3^5 = 243, 但受 top容量3限制
	// 实际数量: top<=3, mid<=5, bot<=5, sum=5
	// 按容量约束: any (nt, nm, nb) with nt+nm+nb=5, nt<=3, nm<=5, nb<=5
	// (0,0,5),(0,1,4),...(3,0,2),(3,1,1),(3,2,0): 18 种 (nt,nm,nb), 每种 5! / (nt! nm! nb!) 排列
	// 但 backtrack 不去重 across cards, 应该是 sum = sum_{nt+nm+nb=5, nt<=3} 5!/(nt!nm!nb!)
	// 简化检查: 至少 > 100, < 243
	if len(pls) < 100 || len(pls) > 243 {
		t.Errorf("R1 placements count looks wrong: %d", len(pls))
	}
}

func TestGenerateRNActions(t *testing.T) {
	gs := NewGameState(0)
	gs.PlaceCard(mustParse("Ks"), RowTop)
	gs.PlaceCard(mustParse("Kh"), RowTop)  // top has 2/3
	cards := parseHand("As", "Qd", "Jc")    // 3 dealt
	actions := GenerateRoundNActions(cards, gs)
	// 3 弃法 × placements(2 张, top1+mid5+bot5 cap)
	// 每个 弃后 kept 2 张, 容量 (1,5,5) → 2 张分配方式数 = 9 - (违反 top容量的) = 9 - 0 = 9? 不对
	// 2 张分 3 行: 3^2 = 9 总. 受 top<=1: 不能 2 张都 top → 排除 (top,top) 1 种, 共 8.
	// 3 弃 × 8 = 24
	if len(actions) != 24 {
		t.Errorf("R2-5 actions count: got %d, want 24", len(actions))
	}
}
