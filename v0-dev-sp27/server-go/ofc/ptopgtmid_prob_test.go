package ofc
import "testing"
// #51 (2026-06-29): pTopGTMid 改真概率(pDraw/pRowAtLeast)+kicker, 不再乐观maxAchievable拍桶.
func TestPTopGTMid_ProbKicker(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	bld:=func(top,mid,bot []Card,d string)*GameState{g:=NewGameState(2);g.Top=top;g.Middle=mid;g.Bottom=bot;for _,c:=range append(append(g.Top,g.Middle...),g.Bottom...){g.UsedCards[c.ID()]=true};g.SetDiscard(mustParse(d));return g}
	pf:=func(g *GameState)float32{return BuildFeaturesV3(g)[89]}
	lo:=pf(bld(mk("X","Ad","4s"),mk("3d","Ac"),mk("X","6c"),"Kd")) // 顶AA+4 低kicker
	un:=pf(bld(mk("X","Ad"),mk("3d","4s","Ac"),mk("X","6c"),"Kd")) // 顶未满
	hi:=pf(bld(mk("X","Ad","Kh"),mk("3d","Ac"),mk("X","6c"),"4s")) // 顶AA+K 高kicker
	if !(lo<un && un<hi) { t.Fatalf("P(foul) 该 低kicker<未满<高kicker, 得 %.3f %.3f %.3f", lo,un,hi) }
	// 不再 flat: 三者必有显著差
	if hi-lo < 0.05 { t.Fatalf("kicker敏感该有≥0.05差, 得%.3f", hi-lo) }
}
