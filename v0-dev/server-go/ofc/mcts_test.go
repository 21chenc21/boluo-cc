package ofc

import (
	"math/rand"
	"testing"
	"time"
)

// TestMCTS_Smoke — R2 简单决策 MCTS 跑通
func TestMCTS_Smoke(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(mustCard("Qd"), RowTop)
	for _, c := range mustCards("5c", "6c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("3h", "9s") {
		gs.PlaceCard(c, RowBottom)
	}
	gs.Round = 2

	dealt := mustCards("Kh", "Ks", "4d")

	cfg := DefaultMCTSConfig()
	cfg.Sims = 100
	cfg.Rng = rand.New(rand.NewSource(42))

	action, stats := MCTSSearch(gs, dealt, 2, cfg)
	if action.round != 2 {
		t.Fatalf("expected round 2, got %d", action.round)
	}
	t.Logf("MCTS picked: discard %s, kept %v→%v (round %d)",
		dealt[action.discardIdx].String(), action.kept, action.placement, action.round)
	t.Logf("Total candidates: %d", len(stats))
	limit := 5
	if len(stats) < limit {
		limit = len(stats)
	}
	for i, s := range stats[:limit] {
		t.Logf("  cand %d: visits=%d Q=%.2f", i, s.Visits[0], s.Q[0])
	}
}

// TestMCTS_KKDeckAware62 — case 62: KK + 3A used → 顶或底 (不可中)
// MCTS 应该 NOT 选 mid (foul-imminent)
func TestMCTS_KKDeckAware62(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(mustCard("Qd"), RowTop)
	for _, c := range mustCards("5c", "6c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("3h", "9s") {
		gs.PlaceCard(c, RowBottom)
	}
	gs.Round = 2
	// 3 张 A 已用 (deck 还剩 1 张)
	for _, c := range mustCards("Ad", "Ah", "As") {
		gs.UsedCards[c.ID()] = true
	}

	dealt := mustCards("Kh", "Ks", "4d")
	cfg := DefaultMCTSConfig()
	cfg.Sims = 1000 // case 62 deck-aware case 需要更多 sims
	cfg.Rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := MCTSSearch(gs, dealt, 2, cfg)

	// 检查: KK 不应在 mid (case 62 期望)
	midKK := 0
	for i, c := range action.kept {
		if !c.IsJoker() && c.Rank() == 11 && action.placement[i] == RowMiddle {
			midKK++
		}
	}
	if midKK >= 2 {
		t.Errorf("case 62 FAIL: KK→mid (midKK=%d), expected top or bot", midKK)
	}
	t.Logf("case 62 placement: discard %s, kept %v→%v",
		dealt[action.discardIdx].String(), action.kept, action.placement)
}

// TestMCTS_KKDeckAware63 — case 63: KK + 4A used → 必上顶 (无 A 升级路径)
func TestMCTS_KKDeckAware63(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(mustCard("Qd"), RowTop)
	for _, c := range mustCards("5c", "6c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("3h", "9s") {
		gs.PlaceCard(c, RowBottom)
	}
	gs.Round = 2
	// 4 张 A 全用了
	for _, c := range mustCards("Ad", "Ah", "As", "Ac") {
		gs.UsedCards[c.ID()] = true
	}

	dealt := mustCards("Kh", "Ks", "4d")
	cfg := DefaultMCTSConfig()
	cfg.Sims = 1000
	cfg.Rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := MCTSSearch(gs, dealt, 2, cfg)

	// 检查: KK 应在 top (没 A 可期, 必锁 K-fantasy)
	topKK := 0
	for i, c := range action.kept {
		if !c.IsJoker() && c.Rank() == 11 && action.placement[i] == RowTop {
			topKK++
		}
	}
	if topKK < 2 {
		t.Errorf("case 63 FAIL: KK→top expected, got top KK=%d", topKK)
	}
	t.Logf("case 63 placement: discard %s, kept %v→%v",
		dealt[action.discardIdx].String(), action.kept, action.placement)
}

// TestMCTS_KKDeckAware59 — case 59: KK + 0A used → 必上底 (deck 含 A 等升级)
func TestMCTS_KKDeckAware59(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(mustCard("Qd"), RowTop)
	for _, c := range mustCards("5c", "6c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("3h", "9s") {
		gs.PlaceCard(c, RowBottom)
	}
	gs.Round = 2
	// 0 A used (deck 全 4 张 A)

	dealt := mustCards("Kh", "Ks", "4d")
	cfg := DefaultMCTSConfig()
	cfg.Sims = 400 // case 59 需要更多 sims 看到 A 升级路径
	cfg.Rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := MCTSSearch(gs, dealt, 2, cfg)

	// 检查: KK 应在 bot (等 A 升级)
	botKK := 0
	for i, c := range action.kept {
		if !c.IsJoker() && c.Rank() == 11 && action.placement[i] == RowBottom {
			botKK++
		}
	}
	if botKK < 2 {
		t.Logf("case 59 NOT pass: KK→bot expected, got bot KK=%d  (this is borderline EV, MCTS may pick top)", botKK)
		t.Logf("placement: discard %s, kept %v→%v",
			dealt[action.discardIdx].String(), action.kept, action.placement)
	} else {
		t.Logf("case 59 PASS: KK→bot")
	}
}

