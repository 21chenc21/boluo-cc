package ofc

import "testing"

// 2026-06-18 手2 R4 ypk-129630538-5: 底4梅花花draw时, 非梅花牌凑对会破花 → 无底价值 (别罚封中道).
func TestCardHasBotValue_OffSuitBreaksFlush(t *testing.T) {
	bot := parseHand("5c", "Qc", "4c", "8c") // 4 梅花 花draw
	d5, _ := ParseCard("5d")                 // 方块5: 凑底5c对, 但破花
	if cardHasBotValue(d5, bot) {
		t.Fatalf("底4梅花花draw时 5d(破花) 不该算有底价值")
	}
	c2, _ := ParseCard("2c") // 梅花2: 补花
	if !cardHasBotValue(c2, bot) {
		t.Fatalf("梅花2 补花 应有底价值")
	}
}
func TestCardHasBotValue_NoFlushPairStillCounts(t *testing.T) {
	bot := parseHand("5c", "Qd", "4h", "8s") // 杂色无花draw
	d5, _ := ParseCard("5d")                 // 凑5对
	if !cardHasBotValue(d5, bot) {
		t.Fatalf("无花draw时 5d凑5对 应有底价值")
	}
}
