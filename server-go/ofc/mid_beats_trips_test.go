package ofc

import "testing"

// 2026-07-04 (用户抓): pTopTrips legalFactor 0.3 拍桶 → pMidBeatsTripsRank 真概率.
// 中差1张葫芦(outs活) vs outs全死, 托住顶trips的概率必须分得开.
func TestPMidBeatsTripsRank(t *testing.T) {
	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	build := func(mid, bot []string) (*GameState, [13]int, [4]int, int, int) {
		g := &GameState{NumJokers: 2, Round: 4}
		g.Top, g.Middle, g.Bottom = mk("Xj0", "7h"), mk(mid...), mk(bot...)
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		var rankRem [13]int
		var suitRem [4]int
		for r := 0; r < 13; r++ {
			rankRem[r] = 4
		}
		for s := 0; s < 4; s++ {
			suitRem[s] = 13
		}
		jokerRem := 2
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				if c.IsJoker() {
					jokerRem--
				} else {
					rankRem[c.Rank()]--
					suitRem[c.Suit()]--
				}
			}
		}
		deckTotal := jokerRem
		for _, v := range rankRem {
			deckTotal += v
		}
		return g, rankRem, suitRem, jokerRem, deckTotal
	}

	// strong: 中 66-44 两对 1空位, FH outs 活 (6s/4s/4h 都在堆里) → P(中>777) 明显 >0
	gS, rrS, srS, jS, dtS := build([]string{"6h", "6d", "4c", "4d"}, []string{"9c", "9d", "Tc", "Th", "2c"})
	pStrong := pMidBeatsTripsRank(gS, 5 /*7*/, rrS, srS, jS, dtS, 1, cardsSeenRemaining(gS))
	// weak: 同构中道, 但 FH outs 全被底占死 (6s 4s 4h) → P ≈ 0
	gW, rrW, srW, jW, dtW := build([]string{"6h", "6d", "4c", "4d"}, []string{"6s", "4s", "4h", "9c", "9d"})
	pWeak := pMidBeatsTripsRank(gW, 5, rrW, srW, jW, dtW, 1, cardsSeenRemaining(gW))
	if pStrong <= pWeak {
		t.Errorf("FH outs活 应 > outs死: strong=%.3f weak=%.3f", pStrong, pWeak)
	}
	if pWeak > 0.05 {
		t.Errorf("outs全死 P(中>777) 应≈0(≤0.05), got %.3f", pWeak)
	}

	// 端到端 (sp43b 链条版): 老 strong/weak 的底都是满两对 9T — 无论中怎么长, 底都跟不上 → 链=0 (正确判死)
	ptS := pTopTrips(gS, rrS, srS, jS, dtS, 1)
	ptW := pTopTrips(gW, rrW, srW, jW, dtW, 1)
	if ptS != 0 || ptW != 0 {
		t.Errorf("底满两对跟不上任何中超手, 链应=0: strong=%.4f weak=%.4f", ptS, ptW)
	}
	// 活底变体: 底 999T+1空(FH draw) 能跟上中FH → 链 > 0
	gL, rrL, srL, jL, dtL := build([]string{"6h", "6d", "4c", "4d"}, []string{"9c", "9d", "9h", "Tc"})
	ptL := pTopTrips(gL, rrL, srL, jL, dtL, 1)
	if ptL <= 0 {
		t.Errorf("底999T有FH draw能跟, 链应>0, got %.4f", ptL)
	}
}
