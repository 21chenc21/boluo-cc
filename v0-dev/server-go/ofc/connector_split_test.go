package ofc

import "testing"

// 2026-06-05: ConnectorSplitPenalty 连张 split 部分跳过成对/三条的 rank.
// JJJ+QQ (J-Q 相邻但是 made trips+pair, 非顺子连张) 被误罚 +30 → 避开 QQ+JJJ. ypk JcQcJdQdJs.

func TestConnectorSplit_TripsPair_NoConnectorPenalty(t *testing.T) {
	// JJJ+QQ: Qc Qd 中, Jc Jd Js 底. 连张 split 部分应为 0 (剩 hierarchy ~18).
	cards := parseHand("Jc", "Qc", "Jd", "Qd", "Js")
	p := Placement{RowBottom, RowMiddle, RowBottom, RowMiddle, RowBottom} // Jc底 Qc中 Jd底 Qd中 Js底
	pen := ConnectorSplitPenalty(p, cards)
	if pen >= 30 {
		t.Fatalf("JJJ+QQ 连张 split 不该罚 (pair/trips 非顺子连张), got %v (应仅剩 hierarchy ~18)", pen)
	}
}

func TestConnectorSplit_RealStraightDraw_StillPenalized(t *testing.T) {
	// 单 J + 单 Q (真顺子连张) 拆两行 → 仍罚 (≥5)
	cards := parseHand("Jc", "Qd", "7h", "3s", "2c")
	p := Placement{RowMiddle, RowBottom, RowBottom, RowTop, RowTop} // Jc中 Qd底 → J-Q 拆
	pen := ConnectorSplitPenalty(p, cards)
	if pen < 5 {
		t.Fatalf("单 J+单 Q 真连张拆开 应罚 ≥5, got %v", pen)
	}
}

func TestConnectorSplit_PairTogether_NoPenalty(t *testing.T) {
	// QQ 同行 (都在底) → 无连张 split (本就同行); 加个无关高牌, 验不乱罚
	cards := parseHand("Qc", "Qd", "Jc", "Jd", "Js")
	p := Placement{RowBottom, RowBottom, RowBottom, RowBottom, RowBottom} // 全底, 无拆
	pen := ConnectorSplitPenalty(p, cards)
	if pen != 0 {
		t.Fatalf("全在底无拆, 应 0, got %v", pen)
	}
}


// 2026-06-14 ypk-9109834-4: 底道三条(222) + 中道高单牌, mid>bot hierarchy 部分不该罚.
// made set 远强于中道单张, 无 foul 威胁. (低对 count==2 仍罚 = case 26.)
func TestConnectorSplit_TripsInBottom_NoHierarchyPenalty(t *testing.T) {
	// 8h 中, Td+222 底: 中道单 8 vs 底三条 2 — 不该罚 hierarchy.
	cards := parseHand("8h", "Td", "2d", "2c", "2s")
	p := Placement{RowMiddle, RowBottom, RowBottom, RowBottom, RowBottom} // 8h中 Td底 2d2c2s底
	pen := ConnectorSplitPenalty(p, cards)
	if pen >= 5 {
		t.Fatalf("底三条222 + 中单8, hierarchy 不该罚 (made set), got %v", pen)
	}
}

// case 26 守护: 底"低对"(count==2) + 中高单 仍是真 foul 险, 必须保留罚.
func TestConnectorSplit_LowPairInBottom_StillPenalized(t *testing.T) {
	// Kh 中, 33 底(低对) + 2 张闲: 中 K > 底 3 → 仍罚 (foul 险, K 可成对压过 33).
	cards := parseHand("Kh", "3d", "3c", "7s", "8s")
	p := Placement{RowMiddle, RowBottom, RowBottom, RowTop, RowTop} // Kh中 3d3c底(对) 7s8s顶
	pen := ConnectorSplitPenalty(p, cards)
	if pen < 3 {
		t.Fatalf("底低对33 + 中高单K, 仍该罚 hierarchy (case 26), got %v", pen)
	}
}


// 2026-06-14 ypk X9s2s4d5d: 中[4d 5d](同花连张) vs 底[9s 2s]. 底 9 压过中 4/5, 底无对,
// 2s 只是被压制的无关低单 → mid>bot hierarchy 不该罚 (否则逼拆同花连张). 连张 4-5 低(<6)也不罚.
func TestConnectorSplit_DominatedLowSingle_NoHierarchy(t *testing.T) {
	cards := parseHand("9s", "2s", "4d", "5d", "Qh") // Qh 占位放顶
	p := Placement{RowBottom, RowBottom, RowMiddle, RowMiddle, RowTop} // 9s2s底 4d5d中 Qh顶
	pen := ConnectorSplitPenalty(p, cards)
	if pen != 0 {
		t.Fatalf("底[9s 2s](9压过中4/5,无对) + 中[4d 5d] 不该罚 hierarchy, got %v", pen)
	}
}

// 守护: 中道高单牌 > 底道最强张 → 仍罚 (中道可能整体压过底道, std21).
func TestConnectorSplit_MidToppsBottom_StillPenalized(t *testing.T) {
	cards := parseHand("4d", "9h", "6c", "7d", "As") // As 顶占位
	p := Placement{RowMiddle, RowMiddle, RowBottom, RowBottom, RowTop} // 4d9h中 6c7d底 As顶
	pen := ConnectorSplitPenalty(p, cards)
	if pen < 3 {
		t.Fatalf("中9h > 底最强7 → 仍该罚 hierarchy (std21), got %v", pen)
	}
}
