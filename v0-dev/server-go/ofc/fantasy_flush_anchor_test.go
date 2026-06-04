package ofc

import "testing"

// 2026-06-03: FindNonRefanAnchors 补 flush anchor.
// 之前漏纯 flush (非 SF) anchor → 两花局被 QQ-top 盖过 (用户范手 16 张, 5 方块 + 5 梅花).
// 修前 ExpertPlaceFantasy 给 royalty 9 (QQ顶+两对+顺), 修后 12 (双花 / 顺+99+花, 等价最优).

func TestFantasy_TwoFlush_MaxRoyalty(t *testing.T) {
	// 16 张, 无鬼, 弃 3. 方块 2d5d7d9dQd + 梅花 3c4c6cAcQc 两个现成同花.
	dealt := parseHand("7d", "2s", "4h", "9d", "6c", "Qd", "4c", "6s",
		"Ac", "9s", "3c", "2d", "3h", "5d", "7s", "Qc")
	r := ExpertPlaceFantasy(dealt, 3)
	if r == nil {
		t.Fatal("ExpertPlaceFantasy returned nil")
	}
	if r.Sc.Foul {
		t.Fatalf("layout fouled: top=%v mid=%v bot=%v", r.Layout.Top, r.Layout.Middle, r.Layout.Bottom)
	}
	// 最优 royalty = 12 (底花4 + 中花8, 或 底花4+中顺4+顶99=4). 修前只有 9.
	if r.Royalty < 12 {
		t.Fatalf("royalty=%d, want >=12 (修前 bug 是 9). top=%v mid=%v bot=%v",
			r.Royalty, r.Layout.Top, r.Layout.Middle, r.Layout.Bottom)
	}
}

func TestFindNonRefanAnchors_IncludesFlush(t *testing.T) {
	// 5 张梅花 → 应产出 mid-flush + bot-flush anchor
	dealt := parseHand("3c", "4c", "6c", "Ac", "Qc", "2d", "5d", "7d", "9d", "Qd",
		"2s", "4h", "6s", "9s", "3h", "7s")
	anchors := FindNonRefanAnchors(dealt)
	hasMidFlush, hasBotFlush := false, false
	for _, a := range anchors {
		if a.Type == "mid-flush" {
			hasMidFlush = true
		}
		if a.Type == "bot-flush" {
			hasBotFlush = true
		}
	}
	if !hasMidFlush || !hasBotFlush {
		t.Fatalf("缺 flush anchor: midFlush=%v botFlush=%v", hasMidFlush, hasBotFlush)
	}
}
