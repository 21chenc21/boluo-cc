package ofc
import "testing"
func mpeMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}

func TestMidPairedRanks(t *testing.T){
	A:=int(RankA); K:=int(RankK)
	if !midPairedRanks(mpeMk("As","Ah"))[A]{t.Fatal("真AA应成对")}
	if !midPairedRanks(mpeMk("Xj0","As"))[A]{t.Fatal("鬼+A=AA应成对(含鬼)")}
	p:=midPairedRanks(mpeMk("Xj0","As","Kh"))
	if !p[A]{t.Fatal("鬼配最高A应成对")}
	if p[K]{t.Fatal("鬼配A后K是单张不该算对")}
	if midPairedRanks(mpeMk("As"))[A]{t.Fatal("孤A不该算对")}
}
// CSP: 中AA vs 底孤3 → 豁免0; 中单5 vs 底33对 → 仍罚(case26不受影响)
func TestCSP_MidPairExempt(t *testing.T){
	if v:=ConnectorSplitPenalty(Placement{RowMiddle,RowMiddle,RowBottom},mpeMk("As","Ah","3h"));v!=0{t.Fatalf("中AA vs 底3 应豁免0, 得%v",v)}
	if v:=ConnectorSplitPenalty(Placement{RowMiddle,RowBottom,RowBottom},mpeMk("5h","3c","3d"));v==0{t.Fatalf("中单5>底33对 应仍罚(case26), 得%v",v)}
}
// MidPlacedOverBot: 中AA(真/鬼)→0; 中单A→2
func TestMidPlacedOverBot_PairExempt(t *testing.T){
	st:=func(mid,bot []string)*GameState{g:=NewGameState(2);g.Round=1
		for _,c:=range mpeMk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range mpeMk(bot...){g.PlaceCard(c,RowBottom)};return g}
	pre:=NewGameState(2);pre.Round=1
	if v:=RnMidPlacedOverBotPlacedPenalty(st([]string{"As","Ah"},[]string{"3h"}),pre);v!=0{t.Fatalf("中真AA vs 底3 应豁免0, 得%v",v)}
	if v:=RnMidPlacedOverBotPlacedPenalty(st([]string{"Xj0","As"},[]string{"3h"}),pre);v!=0{t.Fatalf("中鬼AA vs 底3 应豁免0, 得%v",v)}
	if v:=RnMidPlacedOverBotPlacedPenalty(st([]string{"As"},[]string{"3h"}),pre);v!=2{t.Fatalf("中单A>底3 应仍罚2, 得%v",v)}
}
