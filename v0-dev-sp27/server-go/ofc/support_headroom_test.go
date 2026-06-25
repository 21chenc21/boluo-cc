package ofc

import "testing"

func shFeat(top, mid, bot []string) []float32 {
	mk := func(ss []string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	gs := &GameState{NumJokers: 2}
	gs.Top, gs.Middle, gs.Bottom = mk(top), mk(mid), mk(bot)
	gs.UsedCards = map[string]bool{}
	for _, row := range [][]Card{gs.Top, gs.Middle, gs.Bottom} {
		for _, c := range row {
			gs.UsedCards[c.ID()] = true
		}
	}
	f := make([]float32, 4)
	rr, sr, jr := computeDeckRemaining(gs)
	fillSupportHeadroom(f, gs, rr, sr, jr)
	return f
}

// 实战46: 222顶, 中88. 底777→中888超底夹不进=无解(负); 底999→888可夹(正).
func TestSupportHeadroom_TripsCeiling(t *testing.T) {
	f7 := shFeat([]string{"X", "2c", "2s"}, []string{"4s", "8c", "8s"}, []string{"7h", "7d", "7c", "Qc"})
	f9 := shFeat([]string{"X", "2c", "2s"}, []string{"4s", "8c", "8s"}, []string{"9h", "9d", "9c", "Qc"})
	if f7[0] >= 0 {
		t.Fatalf("底777 222顶无解 midSupportsTop 应<0, got %v", f7[0])
	}
	if f9[0] <= 0 {
		t.Fatalf("底999 222顶可托 midSupportsTop 应>0, got %v", f9[0])
	}
}

// 实战66: 中4477两对. 底2389只成低对<两对=托不住(负); 底QJT8成顺>两对(正).
func TestSupportHeadroom_BotSupportsMid(t *testing.T) {
	fLow := shFeat([]string{"5s"}, []string{"4s", "7d", "4c", "7s"}, []string{"2h", "3d", "8c", "9s"})
	fStr := shFeat([]string{"5s"}, []string{"4s", "7d", "4c", "7s"}, []string{"Qd", "8s", "Js", "Td"})
	if fLow[1] >= 0 {
		t.Fatalf("底低对托不住中两对 botSupportsMid 应<0, got %v", fLow[1])
	}
	if fStr[1] <= 0 {
		t.Fatalf("底成顺托得住中两对 botSupportsMid 应>0, got %v", fStr[1])
	}
}

// 顺子 rank-aware: 中9高顺 > 底7高顺 = 倒置(负); 底K高顺 > 中9高顺(正).
func TestSupportHeadroom_StraightRank(t *testing.T) {
	fInv := shFeat([]string{"As"}, []string{"5h", "6d", "7s", "8c", "9h"}, []string{"3d", "4c", "5s", "6h", "7d"})
	fOk := shFeat([]string{"As"}, []string{"5h", "6d", "7s", "8c", "9h"}, []string{"9s", "Tc", "Jd", "Qh", "Ks"})
	if fInv[1] >= 0 {
		t.Fatalf("中9高顺>底7高顺 倒置 botSupportsMid 应<0, got %v", fInv[1])
	}
	if fOk[1] <= 0 {
		t.Fatalf("底K高顺>中9高顺 botSupportsMid 应>0, got %v", fOk[1])
	}
}

// maxAchievableCmpCapped: 三条只从已有对发育, 不从单张硬凑 (防 222顶+中单4硬凑444误判可托).
func TestMaxAchievableCmpCapped_TripsFromPairOnly(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	rr := [13]int{}
	for i := range rr {
		rr[i] = 4
	}
	sr := [4]int{13, 13, 13, 13}
	// 中 [4s 8c 8s] (88对+单4), ceil=444三条值(3*13+2=41). 应回 ≤41 的最高: 不含888(45), 含88两对/444?
	//   444 需从单4硬凑 → 禁. 应是 88两对(2*13+6=32) 或更低.
	v := maxAchievableCmpCapped(mk("4s", "8c", "8s"), 2, rr, sr, 0, 41)
	if v >= 39 { // 39 = 444三条; 不该达到(单张不凑三条)
		t.Fatalf("不该从单4硬凑444 (cmpVal应<39), got %d", v)
	}
}
