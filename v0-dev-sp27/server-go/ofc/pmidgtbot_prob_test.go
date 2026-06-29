package ofc
import "testing"
// #51 同根 (2026-06-29): pMidGTBot 改真概率 P(底≥中)(pDraw/pRowAtLeast), 不再粗桶乐观maxAchievable.
func TestPMidGTBot_Prob(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	bld:=func(top,mid,bot []Card,d string)*GameState{g:=NewGameState(2);g.Top=top;g.Middle=mid;g.Bottom=bot;for _,c:=range append(append(g.Top,g.Middle...),g.Bottom...){g.UsedCards[c.ID()]=true};g.SetDiscard(mustParse(d));return g}
	v:=func(g *GameState)float32{rr,sr,jr:=computeDeckRemaining(g);dt:=jr;for _,r:=range rr{dt+=r};return pMidGTBot(g,evalRowSafe(g.Middle,5,nil),evalRowSafe(g.Bottom,5,nil),rr,sr,jr,dt,5-len(g.Middle),5-len(g.Bottom))}
	inv:=v(bld(mk("2c","3c"),mk("Ks","Kh"),mk("Qs","Qd","6c"),"9d"))      // 中KK>底QQ6 倒置
	ok :=v(bld(mk("2c","3c"),mk("7s","7h"),mk("As","Ks"),"9d"))           // 中77<底AKs 正常
	hi :=v(bld(mk("2c","3c"),mk("Ah","9c"),mk("5s","5d"),"9d"))           // 中高牌
	if inv <= ok { t.Fatalf("倒置(%.3f)该 > 正常(%.3f)", inv, ok) }
	if hi > 0.2 { t.Fatalf("中高牌 foul 该极低, 得 %.3f", hi) }
	if inv < 0.25 { t.Fatalf("倒置 foul 该抬高(>0.25), 得 %.3f (旧粗桶只0.2)", inv) }
}
