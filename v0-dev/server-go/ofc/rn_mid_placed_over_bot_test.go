package ofc

import "testing"

// 2026-06-17 用户(局80): R1-R4 本轮放中道最大牌>放底道最大牌 → 底<中 → 罚2. 底有花/顺/对豁免.
func TestMidPlacedOverBot_Fire(t *testing.T) {
	pre := st([]string{"As"}, []string{"6s", "7h"}, []string{"9d", "Ks"})
	pre.Round = 2
	post := st([]string{"As"}, []string{"6s", "7h", "8h"}, []string{"9d", "Ks", "5d"})
	post.Round = 2
	if got := RnMidPlacedOverBotPlacedPenalty(post, pre); got != 2 {
		t.Fatalf("放中8h>放底5d+底死 应罚2, got %v", got)
	}
}
func TestMidPlacedOverBot_Skip_BotBigger(t *testing.T) {
	pre := st([]string{"As"}, []string{"6s", "7h"}, []string{"9d", "Ks"})
	pre.Round = 2
	post := st([]string{"As"}, []string{"6s", "7h", "5d"}, []string{"9d", "Ks", "8h"})
	post.Round = 2
	if got := RnMidPlacedOverBotPlacedPenalty(post, pre); got != 0 {
		t.Fatalf("放底8h>放中5d 应不罚, got %v", got)
	}
}
func TestMidPlacedOverBot_Skip_BotPair(t *testing.T) {
	pre := st([]string{}, []string{"6s", "7h"}, []string{"Td", "Th"})
	pre.Round = 2
	post := st([]string{}, []string{"6s", "7h", "8h"}, []string{"Td", "Th", "5d"})
	post.Round = 2
	if got := RnMidPlacedOverBotPlacedPenalty(post, pre); got != 0 {
		t.Fatalf("底TT对豁免 应不罚, got %v", got)
	}
}
