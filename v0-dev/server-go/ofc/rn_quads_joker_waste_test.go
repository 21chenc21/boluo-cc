package ofc
import "testing"
// 2026-06-13 RnQuadsJokerWastePenalty — 真四条+同行鬼 (鬼废kicker) → -15 (ypk-94634314-14)
func TestQuadsJokerWaste_Fire(t *testing.T) {
	g := st([]string{"X"}, []string{"2d","7s","7d"}, []string{"Kc","Kd","X","Ks","Kh"}) // bot 4真K+鬼
	if got := RnQuadsJokerWastePenalty(g); got != 15 { t.Fatalf("4真K+鬼 应罚15, got %v", got) }
}
func TestQuadsJokerWaste_Skip_3RealPlusJoker(t *testing.T) {
	g := st([]string{"X"}, []string{"2d","7s","7d"}, []string{"Kc","Kd","X","Ks"}) // bot 3真K+鬼=四条(鬼当第4张) OK
	if got := RnQuadsJokerWastePenalty(g); got != 0 { t.Fatalf("3真K+鬼(鬼有用) 应0, got %v", got) }
}
func TestQuadsJokerWaste_Skip_NoJoker(t *testing.T) {
	g := st([]string{"Ah"}, []string{"2d","7s","7d"}, []string{"Kc","Kd","Ks","Kh","2h"}) // 真四条无鬼 OK
	if got := RnQuadsJokerWastePenalty(g); got != 0 { t.Fatalf("纯四条无鬼 应0, got %v", got) }
}
