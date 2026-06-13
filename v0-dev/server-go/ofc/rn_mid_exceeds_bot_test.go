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
// 2026-06-13 两对盲区修复: 中两对(KK22) > 底单对(QQ) 倒置, 原 partialEval 漏罚
func TestMidExceedsBot_Fire_MidTwoPairOverBotPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"2s","2c","Ks","Kh"}, []string{"Qh","Qc","6h"}) // 中KK22两对 > 底QQ对
	if got := RnMidExceedsBotPenalty(g); got != 18 { t.Fatalf("中两对>底单对 应罚18(两对盲区修复), got %v", got) }
}
func TestMidExceedsBot_Skip_MidPairBelowBotPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"2s","2c","3s","4c"}, []string{"Qh","Qc","6h","9h"}) // 中22单对 < 底QQ单对
	if got := RnMidExceedsBotPenalty(g); got != 0 { t.Fatalf("中22单对<底QQ 不应罚(两对修复无误伤), got %v", got) }
}
// partialEvalTP 自身: 两对识别 + 取高对
func TestPartialEvalTP_TwoPair(t *testing.T) {
	hv := partialEvalTP([]Card{mustCard("2s"), mustCard("2c"), mustCard("Ks"), mustCard("Kh")})
	if hv.Type != TypeTwoPair { t.Fatalf("KK22 应识别两对, got type %v", hv.Type) }
	// 高对 K 编码应 > 低对在前
	lo := partialEvalTP([]Card{mustCard("2s"), mustCard("2c"), mustCard("3s"), mustCard("3c")}) // 33+22
	if !HandExceeds5(hv, lo) { t.Fatalf("KK22 应 > 3322") }
}
func TestPartialEvalTP_JokerMakesTripsNotTwoPair(t *testing.T) {
	// KK + 鬼 → 应补三条 (优于两对), 不停在两对
	hv := partialEvalTP([]Card{mustCard("Ks"), mustCard("Kh"), mustCard("X"), mustCard("2c")})
	if hv.Type != TypeThreeOfAKind { t.Fatalf("KK+鬼 应补三条, got type %v", hv.Type) }
}
