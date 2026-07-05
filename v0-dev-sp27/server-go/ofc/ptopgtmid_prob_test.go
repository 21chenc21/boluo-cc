package ofc
import "testing"
// #51 (2026-06-29): pTopGTMid 改真概率(pDraw/pRowAtLeast)+kicker, 不再乐观maxAchievable拍桶.
func TestPTopGTMid_ProbKicker(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	bld:=func(top,mid,bot []Card,d string)*GameState{g:=NewGameState(2);g.Top=top;g.Middle=mid;g.Bottom=bot;for _,c:=range append(append(g.Top,g.Middle...),g.Bottom...){g.UsedCards[c.ID()]=true};g.SetDiscard(mustParse(d));return g}
	pf:=func(g *GameState)float32{return BuildFeaturesV3(g)[89]}
	// 2026-07-06 (#46): 改天然AA顶 — 鬼顶可降级后威胁=0 (见 joker_floor_foul_test), kicker敏感只对锁死的天然顶有意义.
	//   三态只变顶, 中/底恒定 (原版 un 的 mid 多一张, 状态不可比).
	lo:=pf(bld(mk("As","Ad","4s"),mk("3d","Ac"),mk("X","6c"),"Kd")) // 顶AA+4 低kicker
	un:=pf(bld(mk("As","Ad"),mk("3d","Ac"),mk("X","6c"),"Kd"))      // 顶未满 kicker待定
	hi:=pf(bld(mk("As","Ad","Kh"),mk("3d","Ac"),mk("X","6c"),"Kd")) // 顶AA+K 高kicker
	if !(lo<un && un<hi) { t.Fatalf("P(foul) 该 低kicker<未满<高kicker, 得 %.3f %.3f %.3f", lo,un,hi) }
	// 不再 flat: 三者必有显著差 (2026-07-06 状态改可比后真实spread≈0.04, 阈值随调)
	if hi-lo < 0.03 { t.Fatalf("kicker敏感该有≥0.03差, 得%.3f", hi-lo) }
}
