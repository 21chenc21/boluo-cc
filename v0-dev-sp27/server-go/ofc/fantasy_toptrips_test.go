package ofc

import (
	"strings"
	"testing"
)

func parseCardsCSV(t *testing.T, s string) []Card {
	var out []Card
	for _, x := range strings.Split(s, ",") {
		c, ok := ParseCard(x)
		if !ok {
			t.Fatalf("bad card %s", x)
		}
		out = append(out, c)
	}
	return out
}

// 2026-10-23 用户范手: AAAA 时顶 trips 锚只取 cs[:3] (As Ah Ac), 漏 Ah Ac Ad 顶 + As 进中凑黑桃同花.
// 旧: 顶AAA/中A高顺/底444 66 = 32. 暴力最优 36.
func TestFantasyTopTripsAllSuitCombos(t *testing.T) {
	d := parseCardsCSV(t, "4h,As,Ah,Qs,Ts,2c,6c,Qh,Ac,9c,4d,4s,6s,Jh,2h,Ks,Ad")
	r := ExpertPlaceFantasy(d, 4)
	if r == nil || r.Sc.Foul {
		t.Fatalf("nil/foul result")
	}
	if r.Royalty != 36 {
		t.Errorf("royalty=%d want 36 (top %v mid %v bot %v)", r.Royalty, r.Layout.Top, r.Layout.Middle, r.Layout.Bottom)
	}
	if r.Sc.MidEval.Type != TypeFlush {
		t.Errorf("mid type=%d want flush", r.Sc.MidEval.Type)
	}
}

// 2026-10-23 用户范手: A鬼鬼 顶锚下 BuildTopTripsLayout 的 beam 只给 10 分且非 nil →
// 穷举 enumMidBot 不跑, 被 底SF+中金刚(35) 盖过. 暴力最优 36 (顶A鬼鬼/中23456/底777鬼J).
func TestFantasyTopTripsBeamNotBlockingEnum(t *testing.T) {
	d := parseCardsCSV(t, "7s,3d,3h,Xj0,7d,Xj1,7c,4h,Td,Xj2,2d,Js,Ac,5d,8s,6c")
	r := ExpertPlaceFantasy(d, 3)
	if r == nil || r.Sc.Foul {
		t.Fatalf("nil/foul result")
	}
	if r.Royalty != 36 {
		t.Errorf("royalty=%d want 36 (top %v mid %v bot %v)", r.Royalty, r.Layout.Top, r.Layout.Middle, r.Layout.Bottom)
	}
	if r.Sc.TopEval.Type != TypeThreeOfAKind {
		t.Errorf("top type=%d want trips", r.Sc.TopEval.Type)
	}
}
