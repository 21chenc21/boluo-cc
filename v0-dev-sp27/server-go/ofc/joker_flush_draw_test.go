package ofc
import "testing"
// #51 (2026-06-29): rowDrawStrength 鬼计入花色 (鬼是wild当任意门). 原bug skip鬼漏算 底2花+鬼=花draw.
func TestRowDrawStrength_JokerCountsToFlush(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	if v:=rowDrawStrength(mk("X","6c","Ac"),5); v<3 { t.Fatalf("🃏+2梅花(2slot) 鬼当梅花=3 该≥3, 得%d",v) }
	if v:=rowDrawStrength(mk("X","X","6c"),5); v<3 { t.Fatalf("2🃏+1梅花 该≥3, 得%d",v) }
	if v:=rowDrawStrength(mk("6c","Ac","2c"),5); v!=3 { t.Fatalf("3真梅花无鬼 该3, 得%d",v) }
	if v:=rowDrawStrength(mk("3c","6h","4s"),5); v!=0 { t.Fatalf("杂花无鬼 该0, 得%d",v) }
}
