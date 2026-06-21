package ofc

import "testing"

// 2026-06-17 RnAceToMidSupportTopAABonus — 局91: 顶AA, Ace+低牌→中(A-4轮子托AA) → +4
func TestAceToMidTopAA_Fire(t *testing.T) {
	post := st([]string{"Ac", "Ah"}, []string{"4h", "Ad"}, []string{"Td", "9s", "Qs", "Js", "8h"})
	a := &RoundNAction{Kept: []Card{mustCard("4h"), mustCard("Ad")}, Placement: []Row{RowMiddle, RowMiddle}}
	if got := RnAceToMidSupportTopAABonus(a, post); got != 4 {
		t.Fatalf("顶AA+Ad低牌进中 应+4, got %v", got)
	}
}
func TestAceToMidTopAA_Skip_HighClutter(t *testing.T) {
	post := st([]string{"Ac", "Ah"}, []string{"Kc", "Ad"}, []string{"Td", "9s", "Qs", "Js", "8h"}) // 中有Kc高杂
	a := &RoundNAction{Kept: []Card{mustCard("Kc"), mustCard("Ad")}, Placement: []Row{RowMiddle, RowMiddle}}
	if got := RnAceToMidSupportTopAABonus(a, post); got != 0 {
		t.Fatalf("中有K高杂 应0, got %v", got)
	}
}
func TestAceToMidTopAA_Skip_NoAceToMid(t *testing.T) {
	post := st([]string{"Ac", "Ah"}, []string{"Kc", "4h"}, []string{"Td", "9s", "Qs", "Js", "8h"}) // Ace被弃没进中
	a := &RoundNAction{Kept: []Card{mustCard("Kc"), mustCard("4h")}, Placement: []Row{RowMiddle, RowMiddle}}
	if got := RnAceToMidSupportTopAABonus(a, post); got != 0 {
		t.Fatalf("没Ace进中 应0, got %v", got)
	}
}
func TestAceToMidTopAA_Skip_TopNotAA(t *testing.T) {
	post := st([]string{"Kc", "Kh"}, []string{"4h", "Ad"}, []string{"Td", "9s", "Qs", "Js", "8h"}) // 顶KK非AA
	a := &RoundNAction{Kept: []Card{mustCard("4h"), mustCard("Ad")}, Placement: []Row{RowMiddle, RowMiddle}}
	if got := RnAceToMidSupportTopAABonus(a, post); got != 0 {
		t.Fatalf("顶非AA 应0, got %v", got)
	}
}
