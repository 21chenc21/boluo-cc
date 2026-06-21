package ofc

import "testing"

// 2026-06-17 RnHighCardWrongRowPenalty — 实战16(ypk-111870282-16): 中底各1张, 两行死, 高牌埋中道 → 罚4
func TestHighCardWrongRow_Fire(t *testing.T) {
	pre := st([]string{"Kh"}, []string{"3s", "2c"}, []string{"7c", "Qd"})
	post := st([]string{"Kh"}, []string{"3s", "2c", "8c"}, []string{"7c", "Qd", "5s"}) // 8→中 5→底 (8>5)
	if got := RnHighCardWrongRowPenalty(post, pre); got != 4 {
		t.Fatalf("8中5底(高牌埋中) 应罚4, got %v", got)
	}
}
func TestHighCardWrongRow_Skip_Correct(t *testing.T) {
	pre := st([]string{"Kh"}, []string{"3s", "2c"}, []string{"7c", "Qd"})
	post := st([]string{"Kh"}, []string{"3s", "2c", "5s"}, []string{"7c", "Qd", "8c"}) // 5→中 8→底 (5<8)
	if got := RnHighCardWrongRowPenalty(post, pre); got != 0 {
		t.Fatalf("5中8底(高牌进底, 对) 应0, got %v", got)
	}
}
func TestHighCardWrongRow_Skip_Draw(t *testing.T) {
	pre := st([]string{"Kh"}, []string{"3s", "2c"}, []string{"7c", "8c"})                 // 底2梅花
	post := st([]string{"Kh"}, []string{"3s", "2c", "Jd"}, []string{"7c", "8c", "9c"})    // 底3梅花=花draw, J中>9底
	if got := RnHighCardWrongRowPenalty(post, pre); got != 0 {
		t.Fatalf("底有花/顺draw 应0(非纯高牌梯度), got %v", got)
	}
}
func TestHighCardWrongRow_Skip_Joker(t *testing.T) {
	pre := st([]string{"Kh"}, []string{"3s", "2c"}, []string{"7c", "Qd"})
	post := st([]string{"Kh"}, []string{"3s", "2c", "X"}, []string{"7c", "Qd", "5s"}) // 中放鬼
	if got := RnHighCardWrongRowPenalty(post, pre); got != 0 {
		t.Fatalf("鬼不比大小 应0, got %v", got)
	}
}
