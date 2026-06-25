package ofc

import "testing"

// pRowStraight 挑牌模型重写 — deck-aware + selection-aware, 治旧 (5/13)^need 低估.
func TestPRowStraightSelect(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			r = append(r, mustParse(s))
		}
		return r
	}
	// 全新牌堆 (每 rank 4 张, 无鬼) 减去 row
	deck := func(row []Card) (rankRem [13]int, jokerRem, deckTotal int) {
		for r := 0; r < 13; r++ {
			rankRem[r] = 4
		}
		jokerRem = 0
		for _, c := range row {
			if !c.IsJoker() {
				rankRem[c.Rank()]--
			}
		}
		deckTotal = jokerRem
		for _, n := range rankRem {
			deckTotal += n
		}
		return
	}

	oe := mk("4s", "5h", "6d", "7c") // 开口顺 (差3或8)
	rr, jr, dt := deck(oe)
	pOE := pRowStraight(oe, rr, jr, dt, 1, 6)

	gut := mk("4s", "5h", "7d", "8c") // 卡顺 (差6)
	rr2, jr2, dt2 := deck(gut)
	pGut := pRowStraight(gut, rr2, jr2, dt2, 1, 6)

	made := mk("4s", "5h", "6d", "7c", "8s")
	rr3, jr3, dt3 := deck(made)
	pMade := pRowStraight(made, rr3, jr3, dt3, 0, 0)

	scat := mk("2s", "7h", "Kd") // 散牌无顺draw
	rr4, jr4, dt4 := deck(scat)
	pScat := pRowStraight(scat, rr4, jr4, dt4, 2, 6)

	t.Logf("开口顺=%.3f 卡顺=%.3f 成顺=%.3f 散牌=%.3f (旧(5/13)^1=0.385)", pOE, pGut, pMade, pScat)

	if pMade != 1 {
		t.Fatalf("成顺应=1, 得 %.3f", pMade)
	}
	if !(pOE > pGut) {
		t.Fatalf("开口顺(%.3f) 应 > 卡顺(%.3f)", pOE, pGut)
	}
	if !(pOE > 0.385) {
		t.Fatalf("开口顺(%.3f) 应 > 旧(5/13)=0.385 (挑牌模型抬高)", pOE)
	}

	// deck-aware: 卡顺缺的 6 (idx4) 全死 → 0
	rrDead := rr2
	rrDead[4] = 0
	if got := pRowStraight(gut, rrDead, jr2, dt2, 1, 6); got != 0 {
		t.Fatalf("卡顺缺6但6全死 应=0, 得 %.3f", got)
	}

	// selection-aware: cardsSeen 多 → 概率 ≥
	if pMore := pRowStraight(oe, rr, jr, dt, 1, 9); pMore < pOE {
		t.Fatalf("cardsSeen 9 应 ≥ cardsSeen 6: %.3f vs %.3f", pMore, pOE)
	}
}
