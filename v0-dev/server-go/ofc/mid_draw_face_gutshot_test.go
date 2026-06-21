package ofc
import "testing"

// 2026-06-18 s99局78 R2: 4张中道卡顺(gutshot, 非4-flush) 弱draw 不加 MidDrawFace
func TestMidDrawFace_SkipGutshot4(t *testing.T) {
	gs := st([]string{}, []string{"9s", "6d", "7s", "Ts"}, []string{}) // 6,7,9,T 卡顺(需8)+3黑桃弱
	if got := RnMidDrawFaceGated([]Card{}, gs); got != 0 {
		t.Fatalf("4张中道卡顺 应不加, got %v", got)
	}
}

// 反例: 4张开口顺 仍奖
func TestMidDrawFace_KeepOpen4(t *testing.T) {
	gs := st([]string{}, []string{"6d", "7s", "8c", "9h"}, []string{}) // 6789开口
	if got := RnMidDrawFaceGated([]Card{}, gs); got != 2 {
		t.Fatalf("4张开口顺 应+2, got %v", got)
	}
}

// 反例: 4张4-flush draw(即便gutshot) 仍奖
func TestMidDrawFace_KeepFlush4(t *testing.T) {
	gs := st([]string{}, []string{"6s", "7s", "9s", "Ts"}, []string{}) // 4黑桃强flush
	if got := RnMidDrawFaceGated([]Card{}, gs); got != 2 {
		t.Fatalf("4张4-flush 应+2, got %v", got)
	}
}

// 反例: 3张draw 仍奖(非4张)
func TestMidDrawFace_Keep3card(t *testing.T) {
	gs := st([]string{}, []string{"9s", "6d", "7s"}, []string{})
	if got := RnMidDrawFaceGated([]Card{}, gs); got != 2 {
		t.Fatalf("3张中道draw 应+2, got %v", got)
	}
}
