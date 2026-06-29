package ofc
import "testing"
// W2 dim164 (2026-06-29 #23/#24): 强成手放错行. 底成对+ 且 中>底 → -1.
func TestStrongHandMisplaced(t *testing.T){
	mk:=func(ss ...string)[]Card{var r []Card;for _,s:=range ss{r=append(r,mustParse(s))};return r}
	f:=func(top,mid,bot []Card)float32{
		g:=NewGameState(2); g.Top=top; g.Middle=mid; g.Bottom=bot
		for _,c:=range append(append(g.Top,g.Middle...),g.Bottom...){g.UsedCards[c.ID()]=true}
		return BuildFeaturesV3(g)[164]
	}
	// #23 AI 中KK>底QQ → -1 ; exp 中空<底KKQQ → 0
	if f(nil,mk("Ks","Kh"),mk("Qh","Qc","6h"))!=-1 { t.Fatal("#23 AI 中KK>底QQ 该-1") }
	if f(nil,nil,mk("Qh","Qc","6h","Ks","Kh"))!=0 { t.Fatal("#23 exp 中空 该0") }
	// #24 AI 中KK22>底QQ → -1 ; exp 中22<底KKQQ → 0
	if f(mk("Ac","As"),mk("Ks","Kh","2s","2c"),mk("Qh","Qc","6h"))!=-1 { t.Fatal("#24 AI 该-1") }
	if f(mk("Ac","As"),mk("2s","2c"),mk("Qh","Qc","6h","Ks","Kh"))!=0 { t.Fatal("#24 exp 该0") }
	// 反例: 底高牌花draw(无成对) 中99 → gate排除, 0 (别误伤发育底)
	if f(nil,mk("9s","9d"),mk("2h","5h","8h","Kh"))!=0 { t.Fatal("底高牌draw 中对 不该触发") }
	// 反例: 正常 底强中弱 (中22 底KKK三条) → 0
	if f(nil,mk("2s","2c"),mk("Ks","Kh","Kd","7c","8c"))!=0 { t.Fatal("底KKK>中22 正常 该0") }
}
