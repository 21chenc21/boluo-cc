package ofc
import "testing"
func dsMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
// 顺draw强度: 3-4-6(连张窗内3张) > 3-6-K(分散)
func TestDrawStr_StraightVsScatter(t *testing.T){
	str:=rowDrawStrength(dsMk("3c","6h","4s"),5)  // 3-4-6: 窗3-4-5-6-7 有3,4,6=3
	sct:=rowDrawStrength(dsMk("3c","6h","Kh"),5)   // 3-6-K: 最佳窗≤2
	if !(str>sct){ t.Fatalf("3-4-6顺potential(%d) 应>3-6-K(%d)",str,sct) }
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
