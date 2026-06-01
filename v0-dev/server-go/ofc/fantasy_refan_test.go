package ofc

import (
	"testing"
)

// TestFindReFanAnchors_TripsWithJoker — 2026-06-01 regression:
// 老代码 FindReFanAnchors 用 `cs[:3-min(3-len(cs), 0)]` 越界读零值 Card "2s" 代替 joker,
// 致 pair + joker 凑 trips 的 fantasy anchor 全报废 (AAA/QQQ/TTT 都用 2s 假冒 X).
// 影响 ypk 实战 fantasy 局: AA pair on top + 2 flush = 21 royalty 出范,
// 修后 TTT trips on top + 2 flush = 30 royalty + re-fantasy.
func TestFindReFanAnchors_TripsWithJoker(t *testing.T) {
	dealtStr := []string{"8h", "6s", "2h", "7s", "Ah", "Js", "9h", "X", "Qd", "As", "Qh", "Kc", "Tc", "3c", "Ts", "4s"}
	dealt := make([]Card, 0, len(dealtStr))
	for _, s := range dealtStr {
		c, ok := ParseCard(s)
		if !ok {
			t.Fatalf("parse %q failed", s)
		}
		dealt = append(dealt, c)
	}

	anchors := FindReFanAnchors(dealt)
	// 期望 3 个 top-trips anchor: AAA / QQQ / TTT, 每个含 1 张 joker
	expectedRanks := map[uint8]bool{RankA: false, RankQ: false, RankT: false}
	for _, a := range anchors {
		if a.Type != "top-trips" {
			continue
		}
		if len(a.Cards) != 3 {
			t.Errorf("top-trips anchor len=%d, want 3, cards=%v", len(a.Cards), a.Cards)
			continue
		}
		jokerCnt := 0
		var realRank uint8 = 255
		for _, c := range a.Cards {
			if c.IsJoker() {
				jokerCnt++
			} else {
				realRank = c.Rank()
			}
		}
		if jokerCnt != 1 {
			t.Errorf("top-trips anchor expected 1 joker, got %d (cards=%v)", jokerCnt, a.Cards)
		}
		if _, ok := expectedRanks[realRank]; !ok {
			t.Errorf("top-trips anchor unexpected rank %d (cards=%v)", realRank, a.Cards)
		}
		expectedRanks[realRank] = true
	}
	for r, found := range expectedRanks {
		if !found {
			t.Errorf("missing top-trips anchor for rank %d (A=%d Q=%d T=%d)", r, RankA, RankQ, RankT)
		}
	}
}

// TestExpertPlaceFantasy_ReFanWithTrips — end-to-end: 验 fantasy 入口选 trips 锁范, 不再选 AA pair 出范.
func TestExpertPlaceFantasy_ReFanWithTrips(t *testing.T) {
	dealtStr := []string{"8h", "6s", "2h", "7s", "Ah", "Js", "9h", "X", "Qd", "As", "Qh", "Kc", "Tc", "3c", "Ts", "4s"}
	dealt := make([]Card, 0, len(dealtStr))
	for _, s := range dealtStr {
		c, _ := ParseCard(s)
		dealt = append(dealt, c)
	}

	r := ExpertPlaceFantasy(dealt, 3)
	if r == nil {
		t.Fatal("ExpertPlaceFantasy returned nil")
	}

	tripsOnTop := false
	if len(r.Layout.Top) == 3 {
		var rankCnt [13]int
		jokerCnt := 0
		for _, c := range r.Layout.Top {
			if c.IsJoker() {
				jokerCnt++
			} else {
				rankCnt[c.Rank()]++
			}
		}
		for _, n := range rankCnt {
			if n+jokerCnt == 3 {
				tripsOnTop = true
				break
			}
		}
	}
	if !tripsOnTop {
		t.Errorf("expected top trips (re-fantasy lock), got top=%v royalty=%d", r.Layout.Top, r.Royalty)
	}
	if r.Royalty < 25 {
		t.Errorf("expected royalty >= 25 (trips top + 2 flush = 30), got %d", r.Royalty)
	}
}
