package ofc
import "testing"
// #24 bug1 (2026-07-01): partialEval 漏两对 — 4张两对被当一对.
func TestPartialEvalTwoPair(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	if e:=evalRowSafe(mk("2s","2c","Ks","Kh"),5,nil); e.Type!=TypeTwoPair{t.Fatalf("22KK(4张)该 TwoPair, 得 type=%d",e.Type)}
	if e:=evalRowSafe(mk("As","Ah","Ks","Kh"),5,nil); e.Type!=TypeTwoPair{t.Fatalf("AAKK(4张)该 TwoPair, 得 type=%d",e.Type)}
	// 高对rank: 22KK 的两对 value 该 > 2233
	hi:=evalRowSafe(mk("2s","2c","Ks","Kh"),5,nil); lo:=evalRowSafe(mk("2s","2c","3s","3c"),5,nil)
	if hi.Value<=lo.Value{t.Fatalf("22KK两对value(%d)该>2233(%d)",hi.Value,lo.Value)}
	// 回归: 单对/三条不受影响
	if e:=evalRowSafe(mk("2s","2c"),5,nil); e.Type!=TypePair{t.Fatalf("22该Pair, 得%d",e.Type)}
	if e:=evalRowSafe(mk("2s","2c","2d"),5,nil); e.Type!=TypeThreeOfAKind{t.Fatalf("222该Trips, 得%d",e.Type)}
	// #22 bug: 金刚 (4张 KKKK / KKK+鬼) 别当三条
	if e:=evalRowSafe(mk("Kc","Kd","Ks","Kh"),5,nil); e.Type!=TypeFourOfAKind{t.Fatalf("KKKK(4真)该金刚, 得%d",e.Type)}
	if e:=evalRowSafe(mk("Kc","Kd","Xj0","Kh"),5,nil); e.Type!=TypeFourOfAKind{t.Fatalf("KKK+鬼(4张)该金刚, 得%d",e.Type)}
	if e:=evalRowSafe(mk("Kc","Kd","Xj0"),5,nil); e.Type!=TypeThreeOfAKind{t.Fatalf("KK+鬼(3张)该三条, 得%d",e.Type)}
}
// #24 bug2: pMidGTBot 中两对要 rank-aware (底要≥中的具体两对, 不是随便成个两对).
func TestPMidGTBot_TwoPairRank(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	bld:=func(top,mid,bot []Card,d string)*GameState{g:=NewGameState(2);g.Round=3;g.Top=top;g.Middle=mid;g.Bottom=bot;for _,c:=range append(append(top,mid...),bot...){g.UsedCards[c.ID()]=true};g.SetDiscard(mustParse(d));return g}
	v:=func(g *GameState)float32{rr,sr,jr:=computeDeckRemaining(g);dt:=jr;for _,r:=range rr{dt+=r};return pMidGTBot(g,evalRowSafe(g.Middle,5,nil),evalRowSafe(g.Bottom,5,nil),rr,sr,jr,dt,5-len(g.Middle),5-len(g.Bottom))}
	// 中22KK(K高两对) 压 底QQ6 → 底要QQQ或配K才超 → foul 高
	hi:=v(bld(mk("Ac","As"),mk("2s","2c","Ks","Kh"),mk("Qh","Qc","6h"),"2d"))
	if hi<0.45{t.Fatalf("中KK高两对压底QQ foul该≥0.45(实战54.8%%), 得%.3f",hi)}
}

// #110 (2026-07-01): pRowFullHouseProb outs-aware, 替换旧 pTrips*0.3 flat 粗估.
func TestFHProbOutsAware(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	g:=NewGameState(2);g.NumJokers=2;g.Round=4
	g.Middle=mk("6h","6d","4c","6c");g.Bottom=mk("9d","8h","8c","8d","8s")
	for _,c:=range append(g.Middle,g.Bottom...){g.UsedCards[c.ID()]=true}
	rr,sr,jr:=computeDeckRemaining(g);dt:=jr;for _,r:=range rr{dt+=r};cs:=cardsSeenRemaining(g)
	pfh:=pRowAtLeast(g.Middle,TypeFullHouse,rr,sr,jr,dt,1,cs)
	// 中666,4 剩1空成FH要配kicker, 真实~0.1-0.2, 不该是旧粗估0.3
	if pfh>=0.28{t.Fatalf("中666,4(1空)成FH该<0.28(旧粗估0.3虚高), 得%.3f",pfh)}
	if pfh<=0.02{t.Fatalf("该>0(能配4或摸joker), 得%.3f",pfh)}
}
