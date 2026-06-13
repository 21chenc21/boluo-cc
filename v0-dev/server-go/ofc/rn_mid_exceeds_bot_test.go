package ofc
import "testing"
// 2026-06-13 RnMidExceedsBotPenalty — 中道成牌 > 底道 (违反 bot≥mid 倒置) → -15 (ypk-88080714-8)
func TestMidExceedsBot_Fire_KKoverQQ(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Ks","Kh"}, []string{"Qh","Qc","6h"}) // 中KK > 底QQ
	if got := RnMidExceedsBotPenalty(g); got != 18 { t.Fatalf("中KK>底QQ 应罚18, got %v", got) }
}
func TestMidExceedsBot_Skip_BotStronger(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Qh","Qc"}, []string{"Ks","Kh","6h"}) // 中QQ < 底KK
	if got := RnMidExceedsBotPenalty(g); got != 0 { t.Fatalf("中QQ<底KK 应0, got %v", got) }
}
func TestMidExceedsBot_Skip_BotNoPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Ks","Kh"}, []string{"5h","6s","7d"}) // 底无对 → 不比
	if got := RnMidExceedsBotPenalty(g); got != 0 { t.Fatalf("底未成对 应0, got %v", got) }
}
