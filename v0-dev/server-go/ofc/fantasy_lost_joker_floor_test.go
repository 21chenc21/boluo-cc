package ofc

import "testing"

// 2026-06-23 (用户 ypk-141164874-8 R4): 中道含鬼 foul-floor 用真牌成手, 鬼别锁成三条+.

// midMadeFloorReal: 中[2s 🃏 2h] 真牌=22对, 不该读成 222 三条.
func TestMidMadeFloorReal_JokerPairNotTrips(t *testing.T) {
	mid := parseHand("2s", "X", "2h")
	floor := midMadeFloorReal(mid)
	full := madeHandCmp(mid) // 鬼当2 → 222三条
	if floor >= full {
		t.Fatalf("floor应低于鬼-trips值: floor=%d full=%d", floor, full)
	}
	if want := madeHandCmp(parseHand("2s", "2h")); floor != want {
		t.Fatalf("floor应=真牌22成手 %d, got %d", want, floor)
	}
}

// 真三条(3张真2)→ floor 仍三条 (鬼压不低于真牌).
func TestMidMadeFloorReal_RealTripsStays(t *testing.T) {
	mid := parseHand("2s", "2h", "2c", "X")
	if want := madeHandCmp(parseHand("2s", "2h", "2c")); midMadeFloorReal(mid) != want {
		t.Fatalf("真222三条 floor 应=三条 %d, got %d", want, midMadeFloorReal(mid))
	}
}

// FantasyLost: J→底 post (顶[X Ah]AA 中[2s X 2h] 底JJ99) → 鬼可压低中道≤底 → 不该 lost.
func TestFantasyLost_JokerMidFlexNotFoul(t *testing.T) {
	post := st([]string{"X", "Ah"}, []string{"2s", "X", "2h"}, []string{"8s", "Jh", "9h", "9s", "Js"})
	if FantasyLost(post) {
		t.Fatalf("中鬼可压低保两对≤底JJ99, 不该判 FantasyLost(假范)")
	}
}

// 控制: 中道真两对(无鬼可压) 严格 > 底满5张弱手追不上 → 仍该判 lost (没破坏检测).
func TestFantasyLost_RealMidExceedsBot_StillLost(t *testing.T) {
	// 顶[As](可达AA) 中[Ks Kh Qs Qh 9c](满, KKQQ两对) 底[2c 3d 7h 9s Jc](满, J高无对追不上) → 中>底 foul, 范死.
	post := st([]string{"As"}, []string{"Ks", "Kh", "Qs", "Qh", "9c"}, []string{"2c", "3d", "7h", "9s", "Jc"})
	if !FantasyLost(post) {
		t.Fatalf("中KKQQ两对满 > 底J高满 追不上 → 应判 FantasyLost")
	}
}
