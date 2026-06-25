package ofc

import "testing"

// 2026-06-05: 三条 A R1 不该被强制 AAA 全上顶 (foul trap). ypk-63963466-4.
// r1RuleNoSplitDealtPair: trips+ 允许拆; r1RuleDealtBigPair_Top: 只要求一对上顶.

func TestR1NoSplitDealtPair_TripsAllowsSplit(t *testing.T) {
	// 3 张 A: 2 上顶 + 1 去底 → 允许 (trips 可拆)
	cards := parseHand("Ah", "Jd", "Ac", "2s", "As")
	p := Placement{RowTop, RowBottom, RowTop, RowMiddle, RowBottom} // Ah顶 Jd底 Ac顶 2s中 As底 → 2 aces 顶(Ah,Ac) 1 底(As)
	if !r1RuleNoSplitDealtPair(p, cards) {
		t.Fatal("trips A 拆开应允许 (return true), got false")
	}
}

func TestR1NoSplitDealtPair_PairStillNoSplit(t *testing.T) {
	// 恰好 2 张 A 拆到两行 → 仍禁止 (pair 不拆)
	cards := parseHand("Ah", "Jd", "Ac", "2s", "8s")
	p := Placement{RowTop, RowMiddle, RowBottom, RowMiddle, RowBottom} // Ah顶 Ac底 → 拆
	if r1RuleNoSplitDealtPair(p, cards) {
		t.Fatal("pair A 拆开应禁止 (return false), got true")
	}
}

func TestR1DealtBigPair_TripsThirdAceElsewhereOK(t *testing.T) {
	// 3 张 A: 2 上顶 (一对锁范) + 第3张去底 → 允许
	cards := parseHand("Ah", "Jd", "Ac", "2s", "As")
	p := Placement{RowTop, RowBottom, RowTop, RowMiddle, RowBottom} // Ah,Ac 顶, As 底
	if !r1RuleDealtBigPair_Top(p, cards) {
		t.Fatal("三条A 一对上顶+第3张去底 应允许, got false")
	}
}

func TestR1DealtBigPair_RequiresPairOnTop(t *testing.T) {
	// 3 张 A 但只 1 张上顶 → 禁止 (一对都没上顶, 没锁范)
	cards := parseHand("Ah", "Jd", "Ac", "2s", "As")
	p := Placement{RowTop, RowMiddle, RowBottom, RowMiddle, RowBottom} // 只 Ah 上顶
	if r1RuleDealtBigPair_Top(p, cards) {
		t.Fatal("三条A 只1张上顶 应禁止, got true")
	}
}

func TestR1DealtBigPair_AAAllTopStillOK(t *testing.T) {
	// 恰好 2 张 A 都上顶 → 允许 (原行为不变)
	cards := parseHand("Ah", "Jd", "Ac", "2s", "8s")
	p := Placement{RowTop, RowMiddle, RowTop, RowBottom, RowBottom} // Ah,Ac 顶
	if !r1RuleDealtBigPair_Top(p, cards) {
		t.Fatal("AA 都上顶 应允许, got false")
	}
}
