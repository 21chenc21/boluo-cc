package ofc

import "testing"

// 2026-06-18 用户发现 ypk-129630538-16 手3 R5: 支配过滤用 bottomDomScore 比底道,
// straight-blind 版把"鬼凑顺底"当对子(prim低)误判 < 鬼凑QQ对 → 删了避免犯规的真顺 → 逼犯规.
func TestBottomDomScore_JokerStraightBeatsPair(t *testing.T) {
	straight := bottomDomScore(parseHand("7c", "Td", "9c", "8c", "X")) // 7-8-9-10+鬼 = 顺
	qq := bottomDomScore(parseHand("7c", "Td", "9c", "Qh", "X"))       // 7-9-10-Q+鬼 = QQ对
	if straight <= qq {
		t.Fatalf("鬼凑顺 domScore 应 > 鬼凑QQ对(否则支配过滤误删真顺), got 顺=%d QQ=%d", straight, qq)
	}
}
func TestBottomDomScore_JokerFlushBeatsPair(t *testing.T) {
	flush := bottomDomScore(parseHand("2c", "5c", "9c", "Kc", "X")) // 4张梅花+鬼 = 同花
	pair := bottomDomScore(parseHand("2c", "5c", "9c", "Kh", "X"))  // K对(鬼凑)
	if flush <= pair {
		t.Fatalf("鬼凑同花 应 > 鬼凑对, got 花=%d 对=%d", flush, pair)
	}
}
