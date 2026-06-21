package ofc

import "testing"

// 2026-06-17 用户(局96): R1 底成对+中道松高牌>底kicker → 底散牌<中散牌, 罚2. 单张无对无顺无花时.
func TestR1HighCardBotKicker_Fire(t *testing.T) {
	// 局96: 发 Kc Qd 2h Kd 3c → AI 中[Qd 2h] 底[Kc Kd 3c]. Q>底kicker3 → 罚2
	cards := parseHand("Kc", "Qd", "2h", "Kd", "3c")
	p := Placement{RowBottom, RowMiddle, RowMiddle, RowBottom, RowBottom} // Kc底 Qd中 2h中 Kd底 3c底
	if got := R1HighCardShouldBeBotKickerPenalty(p, cards); got != 2 {
		t.Fatalf("Q埋中(底KK3, 中Q-2) 应罚2, got %v", got)
	}
}
func TestR1HighCardBotKicker_Skip_QtoBot(t *testing.T) {
	// 正解 中[2h 3c] 底[Kc Qd Kd]: Q当底kicker, 中无>Q散牌 → 不罚
	cards := parseHand("Kc", "Qd", "2h", "Kd", "3c")
	p := Placement{RowBottom, RowBottom, RowMiddle, RowBottom, RowMiddle} // Kc底 Qd底 2h中 Kd底 3c中
	if got := R1HighCardShouldBeBotKickerPenalty(p, cards); got != 0 {
		t.Fatalf("Q进底当kicker(底KKQ, 中2-3) 应不罚, got %v", got)
	}
}
func TestR1HighCardBotKicker_Skip_MidHasDraw(t *testing.T) {
	// 中道有花draw (Qh Jh 同花) → 高牌是draw一部分, 不罚
	cards := parseHand("Kc", "Qh", "Jh", "Kd", "3c")
	p := Placement{RowBottom, RowMiddle, RowMiddle, RowBottom, RowBottom} // 底KK3, 中 Qh Jh 同花draw
	if got := R1HighCardShouldBeBotKickerPenalty(p, cards); got != 0 {
		t.Fatalf("中道花draw 应不罚, got %v", got)
	}
}
