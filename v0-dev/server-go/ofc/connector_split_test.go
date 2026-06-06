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
