package ofc
import "testing"
func dsMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
// 2026-06-28: draw强度改纯花, 顺交 B组 rowStraightTightness(已分档). 验证顺不再算进 draw强度.
func TestDrawStr_FlushOnly(t *testing.T){
	str:=rowDrawStrength(dsMk("3c","6h","4s"),5)  // 3-4-6 顺型但杂花 → 0 (顺不算了)
	if str!=0 { t.Fatalf("顺型杂花 draw强度应0(顺交B组), 得%d",str) }
	fl:=rowDrawStrength(dsMk("3c","6c","4c"),5)   // 3梅花 → 3
	if fl<3 { t.Fatalf("3同花应≥3, 得%d",fl) }
}
// 花draw: 3同花 强
func TestDrawStr_Flush(t *testing.T){
	fl:=rowDrawStrength(dsMk("3s","5s","7s"),5)  // 3黑桃
	if fl<3 { t.Fatalf("3同花 draw强度应≥3, 得%d",fl) }
}
// 满行=0 (成手定型)
func TestDrawStr_FullZero(t *testing.T){
	if rowDrawStrength(dsMk("Qd","Td","8d","Kd","4d"),5)!=0 { t.Fatal("满行应0") }
}
