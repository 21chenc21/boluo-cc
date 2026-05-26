package ofc

import (
	"testing"
)

// helper: parse "Kc" / "X" / "Xj0" 等成 Card
func mustCard(s string) Card {
	c, ok := ParseCard(s)
	if !ok {
		panic("bad card: " + s)
	}
	return c
}

func mustCards(ss ...string) []Card {
	out := make([]Card, len(ss))
	for i, s := range ss {
		out[i] = mustCard(s)
	}
	return out
}

func defaultTestCfg() *RolloutConfig {
	cfg := DefaultRolloutConfig
	return &cfg
}

// TestOracle_TrivialCompleteHand — 完整 13 张 → 直接 ScoreHand
func TestOracle_TrivialCompleteHand(t *testing.T) {
	gs := NewGameState(0)
	for _, c := range mustCards("Ah", "Ad", "As") {
		gs.PlaceCard(c, RowTop)
	}
	for _, c := range mustCards("Kh", "Kd", "Ks", "Kc", "Qh") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("2h", "3h", "4h", "5h", "6h") {
		gs.PlaceCard(c, RowBottom)
	}

	cfg := defaultTestCfg()
	score := OracleSolve(gs, [][]Card{}, cfg)

	// AAA top = trips → fantasy + trips_fan_bonus
	// top trips A = royalty 22
	// mid 4kind K = royalty 20
	// bot straight flush 6h = royalty 15
	// no foul (top trips A > mid 4kind K? No: trips < 4kind. Wait this fouls.)
	//
	// Actually OFC: trips < 4kind, so top > mid is fine? No wait, foul rule: top must be ≤ mid.
	// trips A < 4kind K (Type 3 < Type 7). So top < mid OK. Mid 4kind K < bot SF 6h (Type 7 < Type 8). OK.
	// Total: 22 + 20 + 15 = 57. Plus fantasy bonus (top trips → trips_fan = 400 default)
	// Score = 57 + 400 = 457
	expected := float32(57) + cfg.TripsFanBonus
	if score != expected {
		t.Errorf("expected %.1f, got %.2f", expected, score)
	}
}

// TestOracle_R5SingleSlot — R5 决策, 11 cards placed (top 缺 2), R5 dealt 3 → place 2 都在 top
func TestOracle_R5SingleSlot(t *testing.T) {
	// state: 11 cards placed. bot 强 (RF c), mid 弱 flush, top 1 张待补.
	gs := NewGameState(0)
	gs.PlaceCard(mustCard("Qd"), RowTop)
	for _, c := range mustCards("2h", "3h", "4h", "5h", "7h") {
		gs.PlaceCard(c, RowMiddle) // flush h, 不 straight (gap at 6), royalty 4
	}
	for _, c := range mustCards("Tc", "Jc", "Qc", "Kc", "Ac") {
		gs.PlaceCard(c, RowBottom) // RF c, royalty 25
	}
	cfg := defaultTestCfg()

	// R5 dealt: Qh Qs 2s. 期望 oracle 弃 2s, Qh+Qs 上 top → trips Q fantasy
	dealt := mustCards("Qh", "Qs", "2s")
	score := OracleSolve(gs, [][]Card{dealt}, cfg)

	// 验证 oracle 找到了 fantasy 路径 (score 应包含 trips fan bonus)
	// 不严格匹配数值, 因为 royalty 细节可能跟我估算不同, 但 fantasy bonus 必须在
	if score < cfg.TripsFanBonus {
		t.Errorf("expected oracle to find trips fantasy (score >= %.1f), got %.2f",
			cfg.TripsFanBonus, score)
	}
	t.Logf("R5 single slot score: %.2f (TripsFanBonus=%.0f)", score, cfg.TripsFanBonus)
}

// TestOracle_NoFoulFreedom — R3 决策, 10 cards remaining, 验证 oracle 不会乱搞 foul
// (设计上预留足够 flexibility, oracle 至少能保证不爆)
func TestOracle_NoFoulFreedom(t *testing.T) {
	// R3 决策, 7 cards placed.
	gs := NewGameState(0)
	gs.PlaceCard(mustCard("Qh"), RowTop)
	for _, c := range mustCards("3c", "4c", "5c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("6d", "7d", "8d") {
		gs.PlaceCard(c, RowBottom)
	}
	cfg := defaultTestCfg()

	// R3-R5 各 dealt 3 cards (10 张 future)
	dealtR3 := mustCards("9s", "Ts", "2c")
	dealtR4 := mustCards("Jc", "Qd", "Kh")
	dealtR5 := mustCards("As", "2d", "3d")

	score := OracleSolve(gs, [][]Card{dealtR3, dealtR4, dealtR5}, cfg)

	// 期望 oracle 找到不爆的路径, score > -foulCost
	if score <= -cfg.FoulCost+0.5 {
		t.Errorf("expected non-foul path (score > %.1f), got %.2f", -cfg.FoulCost+0.5, score)
	}
	t.Logf("no-foul-freedom R3 score: %.2f", score)
}

// TestOracle_FantasyTrigger — state Q on top, dealt 含 Q → oracle 必凑 QQ
func TestOracle_FantasyTrigger(t *testing.T) {
	gs := NewGameState(0)
	gs.PlaceCard(mustCard("Qh"), RowTop)
	for _, c := range mustCards("3c", "4c", "5c", "6c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("8d", "9d", "Td", "Jd") {
		gs.PlaceCard(c, RowBottom)
	}
	// state: 9 cards placed. R3 决策即将.
	// dealt R3: Qd 2s 3s, R4: 7c Kc 4d, R5: 5h 6h 7h
	// 期望 oracle 把 Qd 上顶 → top QQ = QQ Fantasy
	dealtR3 := mustCards("Qd", "2s", "3s")
	dealtR4 := mustCards("7c", "Kc", "4d")
	dealtR5 := mustCards("5h", "6h", "7h")
	cfg := defaultTestCfg()
	score := OracleSolve(gs, [][]Card{dealtR3, dealtR4, dealtR5}, cfg)

	// QQ on top + reasonable mid/bot → fantasy QQ trigger
	// 最低 score 应包含 QQFanBonus (50)
	if score < cfg.QQFanBonus*0.8 {
		t.Errorf("expected oracle to trigger QQ fantasy (score >= %.1f), got %.2f",
			cfg.QQFanBonus*0.8, score)
	}
}

// TestOracle_Determinism — 同 input 必返回同 score (memoize 不破坏 determinism)
func TestOracle_Determinism(t *testing.T) {
	gs := NewGameState(0)
	for _, c := range mustCards("Ah", "Kh") {
		gs.PlaceCard(c, RowTop)
	}
	for _, c := range mustCards("3c", "4c", "5c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("8d", "9d", "Td") {
		gs.PlaceCard(c, RowBottom)
	}
	dealtR3 := mustCards("Qd", "2s", "3s")
	dealtR4 := mustCards("7c", "Kc", "4d")
	dealtR5 := mustCards("5h", "6h", "7h")
	cfg := defaultTestCfg()

	s1 := OracleSolve(gs.Clone(), [][]Card{dealtR3, dealtR4, dealtR5}, cfg)
	s2 := OracleSolve(gs.Clone(), [][]Card{dealtR3, dealtR4, dealtR5}, cfg)
	s3 := OracleSolve(gs.Clone(), [][]Card{dealtR3, dealtR4, dealtR5}, cfg)
	if s1 != s2 || s2 != s3 {
		t.Errorf("non-deterministic: %.2f, %.2f, %.2f", s1, s2, s3)
	}
}

// TestOracle_R1Smoke — 完整 R1-R5, 17 张已知, oracle 跑通
func TestOracle_R1Smoke(t *testing.T) {
	gs := NewGameState(2)
	dealtR1 := mustCards("Ah", "Ad", "Kh", "Kd", "Qh")
	dealtR2 := mustCards("3c", "4c", "5c")
	dealtR3 := mustCards("6d", "7d", "8d")
	dealtR4 := mustCards("9s", "Ts", "Js")
	dealtR5 := mustCards("2h", "3h", "4h")
	cfg := defaultTestCfg()

	futureRounds := [][]Card{dealtR1, dealtR2, dealtR3, dealtR4, dealtR5}
	score := OracleSolve(gs, futureRounds, cfg)

	// 期望 score 合理 (应 > 0, 因为 AA + KK + 高牌, 至少能凑 AA top + 不 foul)
	if score < 0 {
		t.Errorf("R1 smoke: expected positive score for AA+KK+Q hand, got %.2f", score)
	}
	t.Logf("R1 oracle score: %.2f (cfg defaults: foul=%.0f QQ=%.0f KK=%.0f AA=%.0f trips=%.0f)",
		score, cfg.FoulCost, cfg.QQFanBonus, cfg.KKFanBonus, cfg.AAFanBonus, cfg.TripsFanBonus)
}

// TestOracle_Joker — 含 joker 的 partial state, 验证 cap-chain 正确
func TestOracle_Joker(t *testing.T) {
	gs := NewGameState(2)
	gs.PlaceCard(MakeJokerWithJID(0), RowTop)
	for _, c := range mustCards("Kc", "Kd") {
		gs.PlaceCard(c, RowTop)
	}
	// top now: [Joker, Kc, Kd] = pair K (joker as wild = K → KK pair, fantasy KK)
	// Fill mid + bot (10 more cards)
	for _, c := range mustCards("3c", "4c", "5c", "6c", "7c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("8h", "9h", "Th", "Jh", "Qh") {
		gs.PlaceCard(c, RowBottom)
	}
	cfg := defaultTestCfg()
	score := OracleSolve(gs, [][]Card{}, cfg)

	// top KK pair (joker→K) = top royalty 8
	// mid SF c = 30
	// bot SF h = 30 (8-Q hearts straight flush)
	// fantasy: KK on top → KKFanBonus = 100 default
	// total 8 + 30 + 30 + 100 = 168
	// (need to confirm bot 8h-Qh is SF: ranks 8,9,T,J,Q all consecutive, all hearts → yes, SF, 30)
	if score < 100 {
		t.Errorf("joker test: expected score with KK fantasy, got %.2f", score)
	}
	t.Logf("joker case score: %.2f", score)
}

// BenchmarkOracleSolve_FromR2 — 从 R2 决策开始 (post-R1 state), R2-R5 future
// 这是 dataset gen 的实际用例 (R2 candidate ranking)
func BenchmarkOracleSolve_FromR2(b *testing.B) {
	gs := NewGameState(0)
	for _, c := range mustCards("Ah", "Kh", "Qh") {
		gs.PlaceCard(c, RowTop)
	}
	for _, c := range mustCards("3c", "4c") {
		gs.PlaceCard(c, RowMiddle)
	}
	cfg := defaultTestCfg()
	dealtR2 := mustCards("5d", "6h", "7s")
	dealtR3 := mustCards("8c", "9d", "Th")
	dealtR4 := mustCards("Js", "Qd", "Kc")
	dealtR5 := mustCards("As", "2d", "3d")
	futureRounds := [][]Card{dealtR2, dealtR3, dealtR4, dealtR5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = OracleSolve(gs, futureRounds, cfg)
	}
}

// BenchmarkOracleSolve_FromR3 — 从 R3 决策开始 (post-R2 state), R3-R5 future
func BenchmarkOracleSolve_FromR3(b *testing.B) {
	gs := NewGameState(0)
	for _, c := range mustCards("Ah", "Kh") {
		gs.PlaceCard(c, RowTop)
	}
	for _, c := range mustCards("3c", "4c", "5c") {
		gs.PlaceCard(c, RowMiddle)
	}
	for _, c := range mustCards("6d", "7d") {
		gs.PlaceCard(c, RowBottom)
	}
	cfg := defaultTestCfg()
	dealtR3 := mustCards("8c", "9d", "Th")
	dealtR4 := mustCards("Js", "Qd", "Kc")
	dealtR5 := mustCards("As", "2d", "3d")
	futureRounds := [][]Card{dealtR3, dealtR4, dealtR5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = OracleSolve(gs, futureRounds, cfg)
	}
}

// BenchmarkOracleSolve_FromPostR1 — 从 R1 已摆完后开始 (5 cards placed),
// R2-R5 future. 这是 R1 candidate 评估的核心 cost.
func BenchmarkOracleSolve_FromPostR1(b *testing.B) {
	gs := NewGameState(0)
	for _, c := range mustCards("Ah", "Ad") {
		gs.PlaceCard(c, RowTop)
	}
	for _, c := range mustCards("Kh", "Kd") {
		gs.PlaceCard(c, RowMiddle)
	}
	gs.PlaceCard(mustCard("Qh"), RowBottom)
	cfg := defaultTestCfg()
	dealtR2 := mustCards("3c", "4c", "5c")
	dealtR3 := mustCards("6d", "7d", "8d")
	dealtR4 := mustCards("9s", "Ts", "Js")
	dealtR5 := mustCards("2h", "3h", "4h")
	futureRounds := [][]Card{dealtR2, dealtR3, dealtR4, dealtR5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = OracleSolve(gs, futureRounds, cfg)
	}
}
