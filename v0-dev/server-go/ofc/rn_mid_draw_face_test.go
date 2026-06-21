package ofc

import "testing"

// 2026-06-17 RnMidDrawFaceBonus — 中道全单张 + 顺面/花面 → +2
func TestMidDrawFace_StraightFire(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6h", "4s"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 3-4-6 顺面
	if got := RnMidDrawFaceBonus(g); got != 2 {
		t.Fatalf("中3-4-6顺面 应+2, got %v", got)
	}
}
func TestMidDrawFace_FlushFire(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6c", "9c"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 3 梅花面
	if got := RnMidDrawFaceBonus(g); got != 2 {
		t.Fatalf("中3同花面 应+2, got %v", got)
	}
}
func TestMidDrawFace_Skip_NoFace(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6h", "Kh"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 3-6-K 无顺无花
	if got := RnMidDrawFaceBonus(g); got != 0 {
		t.Fatalf("中无顺无花 应0, got %v", got)
	}
}
func TestMidDrawFace_Skip_HasPair(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "3h", "4s"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 33 对
	if got := RnMidDrawFaceBonus(g); got != 0 {
		t.Fatalf("中有对 应0, got %v", got)
	}
}

