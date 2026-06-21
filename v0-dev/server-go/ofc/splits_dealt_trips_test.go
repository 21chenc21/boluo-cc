package ofc

import "testing"

// 2026-06-18 s99局10 R1: 别为R1花组/同花奖拆 dealt 三条.
func TestSplitsDealtTrips_Fire(t *testing.T) {
	cards := parseHand("8c", "7s", "8h", "Qs", "8s")                       // 888+7s+Qs
	p := Placement{RowMiddle, RowBottom, RowMiddle, RowBottom, RowBottom}  // 8c中 8h中 8s底 → 888拆
	if !splitsDealtTrips(p, cards) {
		t.Fatalf("888拆中底 应 true")
	}
	if got := R1FlushGroupOnBotBonus(p, cards); got != 0 {
		t.Fatalf("拆三条时底花组奖应0, got %v", got)
	}
}
func TestSplitsDealtTrips_Together(t *testing.T) {
	cards := parseHand("8c", "7s", "8h", "Qs", "8s")
	p := Placement{RowBottom, RowMiddle, RowBottom, RowMiddle, RowBottom} // 888全底
	if splitsDealtTrips(p, cards) {
		t.Fatalf("888全底 应 false")
	}
}
