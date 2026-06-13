package ofc
import "testing"
// 2026-06-14 RnPreserveTopAAChaseBonus — top 鬼+QQ/KK 留空位 + A/鬼活 → +2 (ypk-185336138-22)
func TestPreserveTopAA_Fire_KK(t *testing.T) {
	g := st([]string{"X", "Ks"}, []string{"4h", "3c", "5s"}, []string{"Td", "7s"}) // 鬼+K=KK, 留空, ace全活
	if got := RnPreserveTopAAChaseBonus(g); got != 2 {
		t.Fatalf("鬼+KK 留空位+A活 应 +2, got %v", got)
	}
}
func TestPreserveTopAA_Fire_QQ(t *testing.T) {
	g := st([]string{"X", "Qc"}, []string{"4h", "3c"}, []string{"Td", "7s"})
	if got := RnPreserveTopAAChaseBonus(g); got != 2 {
		t.Fatalf("鬼+QQ 应 +2, got %v", got)
	}
}
func TestPreserveTopAA_Skip_Full(t *testing.T) {
	g := st([]string{"X", "Ks", "2h"}, []string{"4h", "3c"}, []string{"Td", "7s"}) // 满3张, 锁死无空位
	if got := RnPreserveTopAAChaseBonus(g); got != 0 {
		t.Fatalf("top满 应不奖, got %v", got)
	}
}
func TestPreserveTopAA_Skip_LowPair(t *testing.T) {
	g := st([]string{"X", "2h"}, []string{"4h", "3c"}, []string{"Td", "7s"}) // 鬼+2=22 < QQ
	if got := RnPreserveTopAAChaseBonus(g); got != 0 {
		t.Fatalf("鬼+低对 应不奖, got %v", got)
	}
}
func TestPreserveTopAA_Skip_LoneJoker(t *testing.T) {
	g := st([]string{"X"}, []string{"4h", "3c"}, []string{"Td", "7s"}) // 孤鬼, 没成对
	if got := RnPreserveTopAAChaseBonus(g); got != 0 {
		t.Fatalf("孤鬼顶 应不奖, got %v", got)
	}
}
