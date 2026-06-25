package ofc

import "testing"

func wcMk(ss ...string) []Card {
	var r []Card
	for _, s := range ss {
		c, _ := ParseCard(s)
		r = append(r, c)
	}
	return r
}

// 64: AI 中[3-6-K] 全死牌 (无对/顺/花) ; exp 中[3-4-6] 是顺draw 0死.
func TestWasted_64MidStraightVsDead(t *testing.T) {
	aiMid := wastedInRow(wcMk("3c", "6h", "Kh"), 5, false)
	expMid := wastedInRow(wcMk("3c", "6h", "4s"), 5, false)
	if !(aiMid > expMid) {
		t.Fatalf("AI中[3-6-K]死牌(%d) 应 > exp中[3-4-6]顺draw死牌(%d)", aiMid, expMid)
	}
	if expMid != 0 {
		t.Fatalf("中[3-4-6]是顺draw, 死牌应0, 得%d", expMid)
	}
}

// 75: AI 底[TT+2c] 那个2是死牌 ; exp 底[TT] 干净.
func TestWasted_75BotPollution(t *testing.T) {
	aiBot := wastedInRow(wcMk("Td", "Ts", "2c"), 5, false)
	expBot := wastedInRow(wcMk("Td", "Ts"), 5, false)
	if !(aiBot > expBot) {
		t.Fatalf("AI底[TT+死2c](%d) 应 > exp底干净TT(%d)", aiBot, expBot)
	}
}

// 满行成手 = 0 死牌 (不误罚已成手的 kicker).
func TestWasted_FullRowZero(t *testing.T) {
	flush := wastedInRow(wcMk("Qd", "Td", "8d", "Kd", "4d"), 5, false) // 满花
	if flush != 0 {
		t.Fatalf("满花5张死牌应0, 得%d", flush)
	}
	pairFull := wastedInRow(wcMk("Kh", "Ks", "9c", "5d", "2s"), 5, false) // 满行KK+杂kicker
	if pairFull != 0 {
		t.Fatalf("满行(成手已定)死牌应0, 得%d", pairFull)
	}
}

// 特征范围 [0,1].
func TestWasted_Range(t *testing.T) {
	g := NewGameState(2)
	for _, c := range wcMk("3c", "6h", "Kh") {
		g.PlaceCard(c, RowMiddle)
	}
	f := BuildFeaturesV3(g)
	for i := 154; i < 157; i++ {
		if f[i] < 0 || f[i] > 1 {
			t.Fatalf("f[%d]=%.3f 超 [0,1]", i, f[i])
		}
	}
}
