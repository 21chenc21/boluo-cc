package ofc

import "testing"

// 2026-06-17 RnMidPairCompletesTwoPairBonus — 实战14: 真牌配对中道已有单张成两对 → +3
func TestMidPairCompletesTwoPair_Fire(t *testing.T) {
	pre := st([]string{"5s"}, []string{"4s", "7d", "Qh", "4c"}, []string{"Qd", "8s", "Js", "Td"})        // 中 44 + 7d单
	post := st([]string{"5s"}, []string{"4s", "7d", "Qh", "4c", "7s"}, []string{"Qd", "8s", "Js", "Td"}) // 7s配7d → 4477两对
	if got := RnMidPairCompletesTwoPairBonus(post, pre); got != 3 {
		t.Fatalf("7s配7d成4477两对 应+3, got %v", got)
	}
}
func TestMidPairCompletesTwoPair_Skip_NotTwoPair(t *testing.T) {
	pre := st([]string{"5s"}, []string{"4s", "7d", "Qh", "4c"}, []string{"Qd", "8s", "Js", "Td"})
	post := st([]string{"5s"}, []string{"4s", "7d", "Qh", "4c", "8c"}, []string{"Qd", "8s", "Js", "Td"}) // 8c → 仍44单对
	if got := RnMidPairCompletesTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("8c没成两对 应0, got %v", got)
	}
}
func TestMidPairCompletesTwoPair_Skip_AlreadyTwoPair(t *testing.T) {
	pre := st([]string{"5s"}, []string{"4s", "4c", "7d", "7h"}, []string{"Qd", "8s", "Js", "Td"})        // 中已 4477 两对
	post := st([]string{"5s"}, []string{"4s", "4c", "7d", "7h", "Qh"}, []string{"Qd", "8s", "Js", "Td"})
	if got := RnMidPairCompletesTwoPairBonus(post, pre); got != 0 {
		t.Fatalf("中本来就两对 应0, got %v", got)
	}
}
