package ofc
import "testing"
func rtfMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
func rtfSt(top,mid,bot []string)*GameState{g:=NewGameState(2);g.Round=4
	for _,c:=range rtfMk(top...){g.PlaceCard(c,RowTop)};for _,c:=range rtfMk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range rtfMk(bot...){g.PlaceCard(c,RowBottom)};return g}

// 110 留鬼: 顶[鬼]+空位 能种≤中666的合法trips → +25
func TestRTF_110_KeepJoker_Fire(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c"},[]string{"9d","8h","8c","8d","8s"})
	post:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c","6c","7h"},[]string{"9d","8h","8c","8d","8s"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=25{t.Fatalf("110留鬼(顶鬼能种≤6 trips) 应+25, 得%v",v)}
}
// 110 7h占顶: 顶[鬼7h] 唯一trips=777>中6 倒置 → 0
func TestRTF_110_7hOnTop_Skip(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c"},[]string{"9d","8h","8c","8d","8s"})
	post:=rtfSt([]string{"Xj0","7h"},[]string{"6h","6d","4c","6c"},[]string{"9d","8h","8c","8d","8s"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=0{t.Fatalf("110 7h占顶(777>6倒置) 不该奖, 得%v",v)}
}
// 48 3d→顶: 顶[鬼3] 能成333≤中666 合法 → +25
func TestRTF_48_3dSeed_Fire(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"Xj1","4c","6d","6c"},[]string{"Qc","9h","Js","Kh"})
	post:=rtfSt([]string{"Xj0","3d"},[]string{"Xj1","4c","6d","6c"},[]string{"Qc","9h","Js","Kh","Th"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=25{t.Fatalf("48 3d→顶(333≤6合法范) 应+25, 得%v",v)}
}
// C 中成顺(非恰三条) → 0 (验证中恰三条守护)
func TestRTF_C_MidStraight_Skip(t *testing.T){
	pre:=rtfSt([]string{"4c"},[]string{"8h","6d","5d"},[]string{"9d","9s","8c","8d","8s"})
	post:=rtfSt([]string{"4c"},[]string{"8h","6d","5d","7h","9c"},[]string{"9d","9s","8c","8d","8s"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=0{t.Fatalf("C 中成5-9顺(非三条) 不该奖, 得%v",v)}
}
// 底未>中 → 0
func TestRTF_BotNotExceedMid_Skip(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c"},[]string{"2d","3h"})
	post:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c","6c"},[]string{"2d","3h"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=0{t.Fatalf("底未>中 不该奖, 得%v",v)}
}
