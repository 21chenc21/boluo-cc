package ofc

import (
	"testing"
)

// TestFantasyBonus_CapDownAA — 修复前 bug 场景:
// top=[X, A, 5] mid=pair-9 → joker 必须 cap 到 5 (otherwise pair-A > pair-9 foul).
// 旧版手算 (jokerCnt=1, realCnt[A]=1) → pairR=A → AAFanBonus=80 (错!)
// 新版走 cap-chain te → te=pair-5 (joker as 5) 或 high-card → fantasy=false → 0 (正确).
func TestFantasyBonus_CapDownAA(t *testing.T) {
	top := []Card{MakeJoker(), MakeCard(RankA, SuitH), MakeCard(Rank5, SuitC)}
	mid := []Card{MakeCard(Rank9, SuitS), MakeCard(Rank9, SuitC), MakeCard(Rank2, SuitH), MakeCard(Rank3, SuitH), MakeCard(Rank4, SuitH)}
	bot := []Card{MakeCard(RankK, SuitS), MakeCard(RankK, SuitC), MakeCard(RankK, SuitD), MakeCard(Rank5, SuitH), MakeCard(Rank6, SuitH)}
	bonus, isF := FantasyBonusFromBoard(top, mid, bot, 20, 40, 80, 90)
	if isF {
		t.Errorf("[X,A,5] vs pair-9: cap forces joker → pair-5, NOT fantasy. got isF=%v bonus=%.1f, want false 0", isF, bonus)
	}
}

// TestFantasyBonus_CapDownKK — top=[X, K, K] mid=pair-A
// joker 想配 trips-K, 但 trips > pair-A foul. cap → joker 当 kicker → pair-K.
// 旧版手算 jokerCnt=1 realMax=2 effMax=3 → TripsFanBonus=90 (错!)
// 新版 te=pair-K → KKFanBonus=40 (正确).
func TestFantasyBonus_CapDownKK(t *testing.T) {
	top := []Card{MakeJoker(), MakeCard(RankK, SuitH), MakeCard(RankK, SuitC)}
	mid := []Card{MakeCard(RankA, SuitS), MakeCard(RankA, SuitC), MakeCard(Rank2, SuitH), MakeCard(Rank3, SuitH), MakeCard(Rank4, SuitH)}
	bot := []Card{MakeCard(RankA, SuitD), MakeCard(RankA, SuitH), MakeCard(Rank5, SuitH), MakeCard(Rank6, SuitH), MakeCard(Rank7, SuitH)}
	// Note: bot 有 2 A, mid 有 2 A, top 有 2 K. 总 A 4 张 OK.
	bonus, isF := FantasyBonusFromBoard(top, mid, bot, 20, 40, 80, 90)
	// te 应该是 pair-K (joker 作 A kicker 或低 rank kicker). pair-K < pair-A (mid) OK.
	// pair-K = KK fantasy.
	if !isF {
		t.Fatalf("[X,K,K] vs pair-A: cap → pair-K (KK fantasy). got isF=%v", isF)
	}
	if bonus != 40 {
		t.Errorf("[X,K,K] vs pair-A: want KKFanBonus=40 (NOT trips 90). got bonus=%.1f", bonus)
	}
}

// TestFantasyBonus_GenuineAA — top=[A, A, K], no joker, real AA. mid weak.
// 不 cap, te=pair-A → AAFanBonus=80.
func TestFantasyBonus_GenuineAA(t *testing.T) {
	top := []Card{MakeCard(RankA, SuitH), MakeCard(RankA, SuitC), MakeCard(RankK, SuitD)}
	mid := []Card{MakeCard(RankK, SuitS), MakeCard(RankK, SuitC), MakeCard(Rank2, SuitH), MakeCard(Rank3, SuitH), MakeCard(Rank4, SuitH)}
	bot := []Card{MakeCard(RankA, SuitS), MakeCard(RankA, SuitD), MakeCard(Rank5, SuitH), MakeCard(Rank6, SuitH), MakeCard(Rank7, SuitH)}
	// Wait — 4 A used? top 2, bot 2 = 4 A. mid 2 K, top 1 K = 3 K. OK.
	// top pair-A, mid pair-K, bot pair-A. top pair-A > mid pair-K → foul.
	// Hmm need to ensure non-foul. Let me re-design.
	_ = top
	_ = mid
	_ = bot
	// Use simpler: mid trips-7, bot quads K.
	top2 := []Card{MakeCard(RankA, SuitH), MakeCard(RankA, SuitC), MakeCard(Rank2, SuitD)}
	mid2 := []Card{MakeCard(Rank7, SuitH), MakeCard(Rank7, SuitC), MakeCard(Rank7, SuitD), MakeCard(Rank3, SuitH), MakeCard(Rank4, SuitH)}
	bot2 := []Card{MakeCard(RankK, SuitS), MakeCard(RankK, SuitC), MakeCard(RankK, SuitD), MakeCard(RankK, SuitH), MakeCard(Rank5, SuitH)}
	bonus, isF := FantasyBonusFromBoard(top2, mid2, bot2, 20, 40, 80, 90)
	if !isF {
		t.Fatalf("real AA top vs trips-7 mid vs quads-K bot: AA fantasy. got isF=%v", isF)
	}
	if bonus != 80 {
		t.Errorf("real AA top: want AAFanBonus=80, got %.1f", bonus)
	}
}

// TestFantasyBonus_GenuineTrips — top=[7, 7, 7] real trips. mid > trips-7.
func TestFantasyBonus_GenuineTrips(t *testing.T) {
	top := []Card{MakeCard(Rank7, SuitH), MakeCard(Rank7, SuitC), MakeCard(Rank7, SuitD)}
	// mid needs > trips-7. Use straight 9-T-J-Q-K.
	mid := []Card{MakeCard(Rank9, SuitS), MakeCard(RankT, SuitC), MakeCard(RankJ, SuitH), MakeCard(RankQ, SuitC), MakeCard(RankK, SuitC)}
	// bot needs > mid straight. Use higher straight T-J-Q-K-A.
	bot := []Card{MakeCard(RankT, SuitD), MakeCard(RankJ, SuitS), MakeCard(RankQ, SuitS), MakeCard(RankK, SuitS), MakeCard(RankA, SuitS)}
	bonus, isF := FantasyBonusFromBoard(top, mid, bot, 20, 40, 80, 90)
	if !isF {
		t.Fatalf("trips-7 top: fantasy. got isF=%v bonus=%.1f", isF, bonus)
	}
	if bonus != 90 {
		t.Errorf("trips top: want TripsFanBonus=90, got %.1f", bonus)
	}
}

// TestFantasyBonus_FoulZero — foul board → 0 bonus, no fantasy
func TestFantasyBonus_FoulZero(t *testing.T) {
	// top trips-A > mid pair-K → foul
	top := []Card{MakeCard(RankA, SuitH), MakeCard(RankA, SuitC), MakeCard(RankA, SuitD)}
	mid := []Card{MakeCard(RankK, SuitS), MakeCard(RankK, SuitC), MakeCard(Rank2, SuitH), MakeCard(Rank3, SuitH), MakeCard(Rank4, SuitH)}
	bot := []Card{MakeCard(RankA, SuitS), MakeCard(Rank5, SuitH), MakeCard(Rank6, SuitH), MakeCard(Rank7, SuitH), MakeCard(Rank8, SuitH)}
	bonus, isF := FantasyBonusFromBoard(top, mid, bot, 20, 40, 80, 90)
	if isF || bonus != 0 {
		t.Errorf("foul board: want 0 / false. got bonus=%.1f isF=%v", bonus, isF)
	}
}
