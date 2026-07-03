package ofc

import "testing"

// 2026-07-03 (#1 bug): pRowFlush 必须把行内已放的鬼算作 wild 花色 (旧版漏 → 有鬼的花draw报0).
func TestPRowFlush_CountsRowJoker(t *testing.T) {
	rr, sr := roBase() // 满牌堆
	dt := 0
	for _, r := range rr {
		dt += r
	}
	// 底 [🃏 Jd 8d] = 2真方块(idx suit) + 鬼, 2空 → 摸2张方块成花. 应 > 0.
	row := roMk("Xj0", "Jd", "8d")
	p := pRowFlush(row, sr, 0, dt, 2, dt)
	if p <= 0 {
		t.Fatalf("#1: 底[🃏 Jd 8d] 2真方块+鬼+2空 是真花draw, pRowFlush 应>0, got %.4f", p)
	}
	// 对照: 无鬼 [Jd 8d 3s] (2方块+1黑桃) 2空 → need 3方块>2空 → 0 (不该假阳)
	row2 := roMk("Jd", "8d", "3s")
	if p2 := pRowFlush(row2, sr, 0, dt, 2, dt); p2 > 0.001 {
		t.Fatalf("无鬼2方块+2空 need3>2空, 应=0, got %.4f", p2)
	}
	// 双鬼 + 3真同色 = 已成花 (rowHasS+2鬼 ≥5) → 1.0
	if p2 := pRowFlush(roMk("Xj0", "Xj1", "2d", "5d", "9d"), sr, 0, dt, 0, dt); p2 != 1 {
		t.Fatalf("3方块+2鬼=已成花, 应=1, got %.4f", p2)
	}
}
