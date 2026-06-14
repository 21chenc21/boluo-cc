package ofc

import "testing"

// 2026-06-14 候选支配过滤 (同顶中底大者赢): bottomDomScore 排序 + 高牌draw=tier0 守护.
func TestBottomDomScore_OrderAndDrawGuard(t *testing.T) {
	trips := bottomDomScore(parseHand("8c", "9d", "8h", "8s"))   // 888 三条
	twoPair := bottomDomScore(parseHand("8c", "9d", "8h", "9h")) // 88-99 两对
	pair := bottomDomScore(parseHand("8c", "9d", "8h", "2c"))    // 88 一对
	draw := bottomDomScore(parseHand("5h", "6h", "7h", "8h"))    // 5678同花 顺+花draw (高牌)
	if !(trips > twoPair && twoPair > pair) {
		t.Fatalf("强弱序应 三条>两对>一对, got %d/%d/%d", trips, twoPair, pair)
	}
	if draw >= 100 {
		t.Fatalf("高牌draw 应 tier0(<100) 免被支配删, got %d", draw)
	}
	if pair < 100 {
		t.Fatalf("一对 应 >=100(tier1), got %d", pair)
	}
}
