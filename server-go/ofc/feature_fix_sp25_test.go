package ofc

import "testing"

// 2026-06-14 sp25 特征修复: dim106 三条/金刚不再 -1 哨兵 + F_fantasyGran 鬼锁顶范.
func TestSp25_TwoPairHighRank_TripsQuadsNotSentinel(t *testing.T) {
	if twoPairHighRank(parseHand("8c", "9d", "8h", "8s")) < 0 {
		t.Fatal("888三条 不该返回 -1 哨兵 (dim106 修)")
	}
	if twoPairHighRank(parseHand("8c", "8h", "8s", "8d")) < 0 {
		t.Fatal("8888金刚 不该返回 -1 (dim106 修)")
	}
	if got := twoPairHighRank(parseHand("9c", "9d", "8h", "8s")); got != 7 {
		t.Fatalf("99-88两对高对9 应=rank7, got %v", got)
	}
	if twoPairHighRank(parseHand("8c", "8h", "2d", "3s")) != -1 {
		t.Fatal("单对88(非两对) 应 -1")
	}
}

func TestSp25_JokerTopMadePairRank(t *testing.T) {
	if got := jokerTopMadePairRank(parseHand("As", "Kc", "X")); got != 12 {
		t.Fatalf("鬼+A满顶=AA, 应=A(rank12), got %v (F_fantasyGran 修)", got)
	}
	if jokerTopMadePairRank(parseHand("As", "Kc", "Qd")) != -1 {
		t.Fatal("无鬼 应 -1")
	}
	if jokerTopMadePairRank(parseHand("Ks", "Kc", "X")) != -1 {
		t.Fatal("已真对KK(鬼升三条) 应 -1 (非 exact pair)")
	}
}

// 2026-06-14 uniform 扩展: partial 顶含鬼 (已锁范) 也算 exact pair, 不只满顶.
func TestSp25_PartialTopJokerLocked(t *testing.T) {
	// 满顶 [As Kc X] = AA (joker锁A): P(exact AA pair) 应 = 1 (满顶不能升AAA)
	gsFull := st([]string{"As", "Kc", "X"}, nil, nil)
	rr, _, jr := computeDeckRemaining(gsFull)
	dt := jr
	for _, n := range rr {
		dt += n
	}
	if got := pTopFinalPairExact(gsFull, RankA, rr, jr, dt, 0); got < 0.99 {
		t.Fatalf("满顶 [As Kc X]=AA 应 P=1, got %v", got)
	}
	// partial 顶 [Ac X] (1空位) = AA 已锁: P(exact AA) 应 高 (>0.5, =1-P(catch A/joker升AAA))
	gsPart := st([]string{"Ac", "X"}, nil, nil)
	rr2, _, jr2 := computeDeckRemaining(gsPart)
	dt2 := jr2
	for _, n := range rr2 {
		dt2 += n
	}
	if got := pTopFinalPairExact(gsPart, RankA, rr2, jr2, dt2, 1); got < 0.5 {
		t.Fatalf("partial [Ac X] 已锁AA 应 P>0.5 (原低估), got %v", got)
	}
}

// 2026-06-14 成手行序 O 组 dim147-149; 2026-07-04 sp39 砍(固化清零, #23/#24 partial 冤罚假信号).
// 老断言(partial 罚)正是砍因 — 反转为"恒 0"断言, 防有人无意复活.
func TestSp25_MadeRowOrder(t *testing.T) {
	fm := []float32{9, 9, 9}
	fillMadeRowOrder(fm, st(nil, []string{"Kc", "Kd"}, []string{"4c", "4s", "2s"}))
	for i, v := range fm {
		if v != 0 {
			t.Fatalf("O组已砍(sp39), fm[%d] 应恒=0, got %v", i, v)
		}
	}
}
