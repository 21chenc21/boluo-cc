package ofc
import "testing"
func rtfMk(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
func rtfSt(top,mid,bot []string)*GameState{g:=NewGameState(2);g.Round=4
	for _,c:=range rtfMk(top...){g.PlaceCard(c,RowTop)};for _,c:=range rtfMk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range rtfMk(bot...){g.PlaceCard(c,RowBottom)};return g}

// 110 留鬼: 顶[鬼]+空位 能种≤中666的合法trips → +25
func TestRTF_110_KeepJoker_Fire(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c"},[]string{"9d","8h","8c","8d","8s"})
	post:=rtfSt([]string{"Xj0"},[]string{"6h","6d","4c","6c","7h"},[]string{"9d","8h","8c","8d","8s"})
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=30{t.Fatalf("110留鬼(顶鬼能种≤6 trips) 应+30, 得%v",v)}
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
	if v:=RnR4TripsFantasyReachableBonus(post,pre);v!=30{t.Fatalf("48 3d→顶(333≤6合法范) 应+30, 得%v",v)}
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

// 2026-06-23 底4张+中4张(都发育) → bonus 30+5=35; 底5或中3 → 30
func TestRTF_Bot4Mid4_Plus5(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},[]string{"6h","6d","6s"},[]string{"8h","8c","8d"})
	post4:=rtfSt([]string{"Xj0"},[]string{"6h","6d","6s","Tc"},[]string{"8h","8c","8d","9d"}) // 中4底4
	if v:=RnR4TripsFantasyReachableBonus(post4,pre);v!=35{t.Fatalf("底4中4 应+5=35, 得%v",v)}
	post5:=rtfSt([]string{"Xj0"},[]string{"6h","6d","6s"},[]string{"8h","8c","8d","9d","Th"}) // 底5中3
	if v:=RnR4TripsFantasyReachableBonus(post5,pre);v!=30{t.Fatalf("底5中3 应=30(无+5), 得%v",v)}
}

// 2026-06-23 (用户bug): 底<中倒置 应0 — madeHandCmp一致比, 防 partialEvalTP 跨编码(满5张Eval5JokerCap vs <5张*15)误判底>中
func TestRTF_BotLtMid_Skip(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},nil,nil)
	if v:=RnR4TripsFantasyReachableBonus(rtfSt([]string{"Xj0"},[]string{"8h","8c","8d"},[]string{"6h","6c","6d","2s","3s"}),pre);v!=0{t.Fatalf("中888底666满(底<中倒置) 应0, 得%v",v)}
	if v:=RnR4TripsFantasyReachableBonus(rtfSt([]string{"Xj0"},[]string{"6h","6c","6d"},[]string{"8h","8c","8d","2s","3s"}),pre);v!=30{t.Fatalf("中666底888满(底>中) 应30, 得%v",v)}
}

// 2026-06-23 (用户): 底葫芦(三条带对) vs 中三条 — 葫芦type(6)>三条type(3), 即便底三条rank<中三条 仍底>中 → fire
func TestRTF_BotFullHouseVsMidTrips(t *testing.T){
	pre:=rtfSt([]string{"Xj0"},nil,nil)
	// 底66622葫芦(三条6 < 中三条8) 但葫芦>三条 → 底>中 → 30
	if v:=RnR4TripsFantasyReachableBonus(rtfSt([]string{"Xj0"},[]string{"8h","8c","8d"},[]string{"6h","6c","6d","2s","2c"}),pre);v!=30{t.Fatalf("底66622葫芦 > 中888三条 应30, 得%v",v)}
	// 对照: 底666纯三条(无对) < 中888三条 → 0 (倒置)
	if v:=RnR4TripsFantasyReachableBonus(rtfSt([]string{"Xj0"},[]string{"8h","8c","8d"},[]string{"6h","6c","6d","2s","3s"}),pre);v!=0{t.Fatalf("底666三条 < 中888三条 应0, 得%v",v)}
}
