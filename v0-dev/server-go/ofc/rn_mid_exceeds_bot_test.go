package ofc
import "testing"
// 2026-06-13 RnMidExceedsBotPenalty — 中道成牌 > 底道 (违反 bot≥mid 倒置) → -15 (ypk-88080714-8)
func TestMidExceedsBot_Fire_KKoverQQ(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Ks","Kh"}, []string{"Qh","Qc","6h"}) // 中KK > 底QQ
	if got := RnMidExceedsBotPenalty(g, g); got != 18 { t.Fatalf("中KK>底QQ 应罚18, got %v", got) }
}
func TestMidExceedsBot_Skip_BotStronger(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Qh","Qc"}, []string{"Ks","Kh","6h"}) // 中QQ < 底KK
	if got := RnMidExceedsBotPenalty(g, g); got != 0 { t.Fatalf("中QQ<底KK 应0, got %v", got) }
}
func TestMidExceedsBot_Skip_BotNoPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"Ks","Kh"}, []string{"5h","6s","7d"}) // 底无对 → 不比
	if got := RnMidExceedsBotPenalty(g, g); got != 0 { t.Fatalf("底未成对 应0, got %v", got) }
}
// 2026-06-13 两对盲区修复: 中两对(KK22) > 底单对(QQ) 倒置, 原 partialEval 漏罚
func TestMidExceedsBot_Fire_MidTwoPairOverBotPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"2s","2c","Ks","Kh"}, []string{"Qh","Qc","6h"}) // 中KK22两对 > 底QQ对
	if got := RnMidExceedsBotPenalty(g, g); got != 18 { t.Fatalf("中两对>底单对 应罚18(两对盲区修复), got %v", got) }
}
func TestMidExceedsBot_Skip_MidPairBelowBotPair(t *testing.T) {
	g := st([]string{"Ac","As"}, []string{"2s","2c","3s","4c"}, []string{"Qh","Qc","6h","9h"}) // 中22单对 < 底QQ单对
	if got := RnMidExceedsBotPenalty(g, g); got != 0 { t.Fatalf("中22单对<底QQ 不应罚(两对修复无误伤), got %v", got) }
}
// 2026-06-17 实战18(ypk-111870282-18): 中333锁死, 底[KsJdTh]→[KsJdTh Js]=JJ 真牌发育追赶 → 不罚
func TestMidExceedsBot_Skip_BotDeveloping(t *testing.T) {
	mid := []string{"3s", "4c", "3h", "6h", "3c"} // 333 三条(满, 本轮不动)
	pre := st([]string{"X"}, mid, []string{"Ks", "Jd", "Th"})        // 底 K高
	post := st([]string{"X"}, mid, []string{"Ks", "Jd", "Th", "Js"}) // 底 JJ (本轮发育)
	if got := RnMidExceedsBotPenalty(post, pre); got != 0 {
		t.Fatalf("底真牌发育(K高→JJ)追中333 应0不罚, got %v", got)
	}
}
// 反例: KK→中 倒置(底QQ本轮没变) 仍罚 — 发育豁免不误放中道膨胀
func TestMidExceedsBot_Fire_MidInflated_RealPre(t *testing.T) {
	pre := st([]string{"Ac", "As"}, []string{"Ks"}, []string{"Qh", "Qc", "6h"})        // 中1张, 底QQ
	post := st([]string{"Ac", "As"}, []string{"Ks", "Kh"}, []string{"Qh", "Qc", "6h"}) // KK→中, 底QQ没动
	if got := RnMidExceedsBotPenalty(post, pre); got != 18 {
		t.Fatalf("KK→中(底没变) 应罚18, got %v", got)
	}
}

// partialEvalTP 自身: 两对识别 + 取高对
func TestPartialEvalTP_TwoPair(t *testing.T) {
	hv := partialEvalTP([]Card{mustCard("2s"), mustCard("2c"), mustCard("Ks"), mustCard("Kh")})
	if hv.Type != TypeTwoPair { t.Fatalf("KK22 应识别两对, got type %v", hv.Type) }
	// 高对 K 编码应 > 低对在前
	lo := partialEvalTP([]Card{mustCard("2s"), mustCard("2c"), mustCard("3s"), mustCard("3c")}) // 33+22
	if !HandExceeds5(hv, lo) { t.Fatalf("KK22 应 > 3322") }
}
func TestPartialEvalTP_JokerMakesTripsNotTwoPair(t *testing.T) {
	// KK + 鬼 → 应补三条 (优于两对), 不停在两对
	hv := partialEvalTP([]Card{mustCard("Ks"), mustCard("Kh"), mustCard("X"), mustCard("2c")})
	if hv.Type != TypeThreeOfAKind { t.Fatalf("KK+鬼 应补三条, got type %v", hv.Type) }
}

// 2026-06-18 局12(seed99 R2): 中[X 5h 5c]鬼借555三条, 底[Kc Kd]真KK更高对未满 → 不罚(底必发育反超)
func TestMidExceedsBot_Skip_JokerTripsBotHigherPair(t *testing.T) {
	g := st([]string{"As", "Ad"}, []string{"X", "5h", "5c"}, []string{"Kc", "Kd"}) // 中鬼555, 底KK
	if got := RnMidExceedsBotPenalty(g, g); got != 0 {
		t.Fatalf("中鬼借三条 vs 底更高真对 应0不罚, got %v", got)
	}
}

// 反例: 中鬼借三条但底对rank更低(22) → 小对追不上锁死555, 仍罚
func TestMidExceedsBot_Fire_JokerTripsBotLowerPair(t *testing.T) {
	g := st([]string{"As", "Ad"}, []string{"X", "5h", "5c"}, []string{"2c", "2d"}) // 中鬼555, 底22
	if got := RnMidExceedsBotPenalty(g, g); got != 18 {
		t.Fatalf("中鬼借三条 vs 底更低对(22) 应罚18, got %v", got)
	}
}

// 2026-06-22 ypk-84869450-8 R3: 中JJ+3h(成手仍JJ没变强) + 底66发育(高牌→对,未满) → 底能成KK66反超 → 豁免不罚
func TestMidExceedsBot_MidKickerBotDevelop_Exempt(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{c,_:=ParseCard(s);r=append(r,c)};return r}
	st:=func(mid,bot []string)*GameState{g:=NewGameState(2);g.Round=3
		for _,c:=range mk("Qc"){g.PlaceCard(c,RowTop)};for _,c:=range mk(mid...){g.PlaceCard(c,RowMiddle)};for _,c:=range mk(bot...){g.PlaceCard(c,RowBottom)};return g}
	post:=st([]string{"Js","4c","Jd","3h"},[]string{"2d","6d","Kd","6h"})
	pre:=st([]string{"Js","4c","Jd"},[]string{"2d","6d","Kd"})
	if v:=RnMidExceedsBotPenalty(post,pre);v!=0{t.Fatalf("中JJ加kicker+底66发育 应豁免0, 得%v",v)}
}
