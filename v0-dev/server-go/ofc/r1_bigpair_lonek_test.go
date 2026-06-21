package ofc
import "testing"

// 2026-06-18 R1BigPairOnBotBonus: 大对(≥T)放底锚行 +2 (局32). 守护 botN≥3 + kicker≥8.
func TestR1BigPairOnBot_Fire_TTK(t *testing.T) {
	cards := parseHand("Td", "9h", "Ks", "Tc", "Ah")
	p := Placement{RowBottom, RowMiddle, RowBottom, RowBottom, RowTop} // 底 Td Ks Tc = TTK
	if got := R1BigPairOnBotBonus(p, cards); got != 2 {
		t.Fatalf("TTK底(大对+高kicker) 应+2, got %v", got)
	}
}
func TestR1BigPairOnBot_Skip_BareTT(t *testing.T) {
	cards := parseHand("Td", "Th", "3h", "9s", "Ks")
	p := Placement{RowBottom, RowBottom, RowMiddle, RowMiddle, RowTop} // 底 Td Th = 光秃TT(2张)
	if got := R1BigPairOnBotBonus(p, cards); got != 0 {
		t.Fatalf("光秃TT底(botN<3) 应0(防std22 K丢顶), got %v", got)
	}
}
func TestR1BigPairOnBot_Skip_LowKicker(t *testing.T) {
	cards := parseHand("Td", "Ts", "2c", "5h", "4h")
	p := Placement{RowBottom, RowBottom, RowBottom, RowMiddle, RowMiddle} // 底 Td Ts 2c (2c低)
	if got := R1BigPairOnBotBonus(p, cards); got != 0 {
		t.Fatalf("TT+低2c底(可成中道2-4-5顺draw, 实战75) 应0, got %v", got)
	}
}

// 2026-06-18 R1LoneKingOnTopPenalty: 孤K上顶 -2 (用户). KK/K+鬼(范)不罚.
func TestR1LoneKingTop_Fire(t *testing.T) {
	cards := parseHand("9d", "Kh", "3d", "9c", "6s")
	p := Placement{RowBottom, RowTop, RowMiddle, RowBottom, RowMiddle} // Kh孤顶
	if got := R1LoneKingOnTopPenalty(p, cards); got != 2 {
		t.Fatalf("孤K上顶 应+2罚, got %v", got)
	}
}
func TestR1LoneKingTop_Skip_KK(t *testing.T) {
	cards := parseHand("Kh", "Ks", "3d", "9c", "6s")
	p := Placement{RowTop, RowTop, RowMiddle, RowBottom, RowBottom} // KK顶(范)
	if got := R1LoneKingOnTopPenalty(p, cards); got != 0 {
		t.Fatalf("KK顶(范种子) 不罚, got %v", got)
	}
}
func TestR1LoneKingTop_Skip_KJoker(t *testing.T) {
	cards := parseHand("Kh", "X", "3d", "9c", "6s")
	p := Placement{RowTop, RowTop, RowMiddle, RowBottom, RowBottom} // K+鬼顶=KK范
	if got := R1LoneKingOnTopPenalty(p, cards); got != 0 {
		t.Fatalf("K+鬼顶(KK范) 不罚, got %v", got)
	}
}
