package ofc
import "testing"
func fovfMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
func fovfSt(top,mid,bot []string)*GameState{g:=NewGameState(2);g.Round=3
	for _,c:=range fovfMk(top...){g.PlaceCard(c,RowTop)};for _,c:=range fovfMk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range fovfMk(bot...){g.PlaceCard(c,RowBottom)};return g}

// ypk-84869450-8 R3: 顶Qc 中JJ 底6622两对. 顶要QQ则中需≥QQ=JJ两对, 但JJ两对>底66两对 → 倒置 → 假范
func TestFOVF_MidPairChainBottom_Foul(t *testing.T){
	g:=fovfSt([]string{"Qc"},[]string{"Js","4c","Jd"},[]string{"2d","6d","Kd","2s","6h"})
	if !fantasyOnlyViaFoul(g){t.Fatal("顶Qc中JJ底6622: 中JJ升两对>底66两对 应判假范(true)")}
}
// 反例: 底 KKQQ 两对 撑得住 JJ 两对 → 真范 (false)
func TestFOVF_BottomSupportsMidPair_OK(t *testing.T){
	g:=fovfSt([]string{"Qc"},[]string{"Js","4c","Jd"},[]string{"Kd","Kh","Qs","Qd","2s"})
	if fantasyOnlyViaFoul(g){t.Fatal("底KKQQ两对撑得住中JJ两对 应真范(false)")}
}

// FantasyLost ③ 同修: 中有对P<Q 进范需升两对, 底要撑两对不是中当前单对
func TestFantasyLost_MidPairChainBottom(t *testing.T){
	g:=fovfSt([]string{"Qc"},[]string{"Js","4c","Jd"},[]string{"2d","6d","Kd","2s","6h"})
	if !FantasyLost(g){t.Fatal("顶Qc中JJ底6622: 中升JJ两对>底66两对 FantasyLost应=true")}
	// 反例: 底能成KK66撑得住 → 不lost
	g2:=fovfSt([]string{"Qc"},[]string{"Js","4c","Jd","2s"},[]string{"Kd","Kh","Qs","Qd","2c"})
	if FantasyLost(g2){t.Fatal("底KKQQ撑得住中JJ两对 FantasyLost应=false")}
}
