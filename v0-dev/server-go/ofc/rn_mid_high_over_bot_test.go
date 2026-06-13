package ofc
import "testing"
// 2026-06-14 用户提案: RnMidHighCardOverBotPenalty — 中放真牌>底锚+底未三条 → -8 (ypk-459082-15)
func TestMidHighOverBot_Fire(t *testing.T) {
	pre := st([]string{"Kd"}, []string{"3s"}, []string{"9c", "7d", "9h"}) // 底99 锚=9
	post := st([]string{"Kd"}, []string{"3s", "Jd"}, []string{"9c", "7d", "9h"}) // Jd进中, J>9
	if got := RnMidHighCardOverBotPenalty(post, pre); got != 5 {
		t.Fatalf("J进中越过底99锚 应罚5, got %v", got)
	}
}
func TestMidHighOverBot_Skip_MidTrips(t *testing.T) {
	pre := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s"}, []string{"Ts", "8h", "Js"}) // 中222
	post := st([]string{"Ks", "Kh"}, []string{"3d", "2c", "2d", "2s", "Kd"}, []string{"Ts", "8h", "Js"}) // 222+K trips
	if got := RnMidHighCardOverBotPenalty(post, pre); got != 0 {
		t.Fatalf("中已三条 高牌只kicker 应不罚(实战20), got %v", got)
	}
}
func TestMidHighOverBot_Skip_LowAdded(t *testing.T) {
	pre := st([]string{"Kd"}, []string{"3s"}, []string{"9c", "7d", "9h"})
	post := st([]string{"Kd"}, []string{"3s", "5d"}, []string{"9c", "7d", "9h"}) // 5d<9锚
	if got := RnMidHighCardOverBotPenalty(post, pre); got != 0 {
		t.Fatalf("中放牌<底锚 应不罚, got %v", got)
	}
}
