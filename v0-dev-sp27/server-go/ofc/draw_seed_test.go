package ofc
import "testing"
// #104 (2026-06-29): drawSeedScore 按概率注入"保种子"期权 — 底葫芦种子+中flush/顺draw(条件底能托).
func TestDrawSeedScore(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	build:=func(top,mid,bot []Card,disc string)*GameState{
		g:=NewGameState(2);g.NumJokers=2;g.Top=top;g.Middle=mid;g.Bottom=bot
		for _,c:=range append(append(append([]Card{},top...),mid...),bot...){g.UsedCards[c.ID()]=true}
		g.UsedCards[mustParse(disc).ID()]=true; return g
	}
	score:=func(g *GameState)float32{rr,sr,jr:=computeDeckRemaining(g);return drawSeedScore(g,rr,sr,jr)}
	// exp 保种子: 底QQ(3张2空→FH种子) + 中3黑桃flush draw
	exp:=score(build(mk("4c"),mk("3s","5s","7s"),mk("9h","Qd","Qh"),"6h"))
	// AI 杀种子: 底QQ4(4张1空, FH堵死) + 中2黑桃
	ai:=score(build(nil,mk("3s","5s","6h"),mk("9h","Qd","Qh","4c"),"7s"))
	if exp<=ai { t.Fatalf("保种子(%.3f)该 > 杀种子(%.3f)", exp, ai) }
	if ai>0.02 { t.Fatalf("底FH堵死(1空)该≈0, 得%.3f", ai) }
	// 无种子: 底高牌散+中无draw → ≈0
	none:=score(build(mk("4c"),mk("2h","8d"),mk("3c","9s","Kd"),"6h"))
	if none>0.1 { t.Fatalf("无种子该≈0, 得%.3f", none) }
	// #117: 底花draw种子 (底2d6dKd=3方块) 该被算; 破花(2s进底)该≈0
	botFlush:=score(build(mk("Qc"),mk("Js","3c","Jd","3h","6h"),mk("2d","6d","Kd"),"2s"))
	botBlock:=score(build(mk("Qc"),mk("Js","3c","Jd","3h"),mk("2d","6d","Kd","2s"),"6h"))
	if botFlush<=botBlock { t.Fatalf("#117 底花draw(%.3f)该 > 破花(%.3f)", botFlush, botBlock) }
	if botFlush<0.1 { t.Fatalf("#117 底3方块花种子该显著, 得%.3f", botFlush) }
}
