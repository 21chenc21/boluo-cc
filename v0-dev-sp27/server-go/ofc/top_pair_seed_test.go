package ofc
import "testing"
// #110 (2026-06-29): 顶有鬼且只配sub-QQ低对/无对 = 范种子, f102 中性0 (别钉低对/别当-1).
func TestF102_JokerSeedNeutral(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	f102:=func(top []Card)float32{
		g:=NewGameState(2); g.Top=top; g.Middle=mk("6h","6d","6c"); g.Bottom=mk("9d","8h","8c","8d","8s")
		for _,c:=range append(append(g.Top,g.Middle...),g.Bottom...){g.UsedCards[c.ID()]=true}
		return BuildFeaturesV3(g)[102]
	}
	if f102(mk("X"))!=0 { t.Fatal("孤鬼顶种子 该0(原-1)") }
	if f102(mk("X","7h"))!=0 { t.Fatal("鬼配77低对 该0(原0.417, 鬼是种子)") }
	if v:=f102(mk("7s","7h")); v<0.4||v>0.42 { t.Fatalf("真77对(无鬼) 该保留0.417 royalty, 得%.3f",v) }
	if v:=f102(mk("X","Qd")); v<0.8 { t.Fatalf("鬼配QQ(范) 该保留高分, 得%.3f",v) }
	if f102(mk("3s","7h"))!=-1 { t.Fatal("真无对(无鬼) 该保留-1") }
}
