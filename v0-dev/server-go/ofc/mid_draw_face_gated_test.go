package ofc

import "testing"

// 2026-06-18 seed99局25 R4: 中道弱顺draw, 但发牌含能配中道对的牌没进中道 → 不奖MidDrawFace(别放弃成对追弱draw).
func TestMidDrawFaceGated_ForgoPair(t *testing.T) {
	gs := st([]string{}, []string{"7d", "8d", "5c", "2h"}, []string{}) // 5-7-8顺draw
	dealt := parseHand("8h", "2h", "3d")                              // 8h能配8d成88, 但没进中道
	if got := RnMidDrawFaceGated(dealt, gs); got != 0 {
		t.Fatalf("发牌含能配中道对的8h没进中道 → 不该奖, got %v", got)
	}
}
func TestMidDrawFaceGated_NoForgo(t *testing.T) {
	gs := st([]string{}, []string{"7d", "8d", "5c", "2h"}, []string{})
	dealt := parseHand("2h", "Kc", "Qd") // 无能配中道对的牌 (2h已进中道)
	if got := RnMidDrawFaceGated(dealt, gs); got != 2 {
		t.Fatalf("发牌无配中道对 → 中道draw照奖2, got %v", got)
	}
}
