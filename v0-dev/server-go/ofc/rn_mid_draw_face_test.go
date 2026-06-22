package ofc

import "testing"

// 2026-06-17 RnMidDrawFaceBonus — 中道全单张 + 顺面/花面 → +2
func TestMidDrawFace_StraightFire(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6h", "4s"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 3-4-6 顺面
	if got := RnMidDrawFaceBonus(g); got != 2 {
		t.Fatalf("中3-4-6顺面 应+2, got %v", got)
	}
}
func TestMidDrawFace_FlushFire(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6c", "9c"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 3 梅花面
	if got := RnMidDrawFaceBonus(g); got != 2 {
		t.Fatalf("中3同花面 应+2, got %v", got)
	}
}
func TestMidDrawFace_Skip_NoFace(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "6h", "Kh"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 3-6-K 无顺无花
	if got := RnMidDrawFaceBonus(g); got != 0 {
		t.Fatalf("中无顺无花 应0, got %v", got)
	}
}
func TestMidDrawFace_Skip_HasPair(t *testing.T) {
	g := st([]string{"X", "As"}, []string{"3c", "3h", "4s"}, []string{"Qd", "Td", "8d", "Kd", "2d"}) // 中 33 对
	if got := RnMidDrawFaceBonus(g); got != 0 {
		t.Fatalf("中有对 应0, got %v", got)
	}
}


// 2026-06-23 底成花+ + 中道顺draw(89T) → MidDrawFace 2+5=7; 底非花+ → 2
func TestMidDrawFace_BotFlushStraightDraw_Plus5(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
	st:=func(mid,bot []string)*GameState{g:=NewGameState(2);g.Round=3
		for _,c:=range mk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range mk(bot...){g.PlaceCard(c,RowBottom)};return g}
	dealt:=mk("3c","Xj0","Td")
	if v:=RnMidDrawFaceGated(dealt,st([]string{"8h","9h","Td"},[]string{"5c","Jc","Tc","Qc","7c"}));v!=7{t.Fatalf("底花+中89T顺draw 应+5=7, 得%v",v)}
	if v:=RnMidDrawFaceGated(dealt,st([]string{"8h","9h","Td"},[]string{"5c","5d","Tc","Qh","7s"}));v!=2{t.Fatalf("底55对(非花+) 应=2无+5, 得%v",v)}
	// 守护: 中道花draw(≥3同花)+底花 → 不给+5 (防中花>底花倒置)
	if v:=RnMidDrawFaceGated(mk("Kc","Xj0","6h"),st([]string{"2h","4h","6h"},[]string{"5c","Jc","Tc","Qc","7c"}));v!=2{t.Fatalf("中花draw+底花 守护应=2无+5, 得%v",v)}
}
