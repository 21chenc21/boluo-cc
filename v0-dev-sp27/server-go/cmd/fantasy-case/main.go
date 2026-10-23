// fantasy-case — 给一手范牌 (逗号分隔), 跑 ExpertPlaceFantasy 并对比指定摆法的计分
//
//	go run ./cmd/fantasy-case -dealt=4h,As,... -dc=4 [-top=.. -mid=.. -bot=..]
package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/boluo/v0-server/ofc"
)

func parse(s string) []ofc.Card {
	var out []ofc.Card
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		c, ok := ofc.ParseCard(t)
		if !ok {
			panic("bad card " + t)
		}
		out = append(out, c)
	}
	return out
}

func str(cs []ofc.Card) string {
	s := make([]string, len(cs))
	for i, c := range cs {
		s[i] = c.String()
	}
	return strings.Join(s, " ")
}

func show(name string, top, mid, bot []ofc.Card) {
	sc := ofc.ScoreHand(top, mid, bot)
	fmt.Printf("[%s]\n  top: %s\n  mid: %s\n  bot: %s\n  royalty top=%d mid=%d bot=%d total=%d foul=%v refan=%v\n",
		name, str(top), str(mid), str(bot), sc.TopRoyalty, sc.MidRoyalty, sc.BotRoyalty, sc.Royalties, sc.Foul,
		sc.TopEval.Type >= ofc.TypeThreeOfAKind || sc.BotEval.Type >= ofc.TypeFourOfAKind)
}

func main() {
	dealt := flag.String("dealt", "", "范牌, 逗号分隔")
	dc := flag.Int("dc", -1, "弃牌数 (默认 len-13)")
	top := flag.String("top", "", "对照摆法 top")
	mid := flag.String("mid", "", "对照摆法 mid")
	bot := flag.String("bot", "", "对照摆法 bot")
	anchors := flag.Bool("anchors", false, "打印每个 reFan 锚构出的 layout")
	replayF := flag.String("replay", "", "TSV 回放 prod 范牌日志")
	brute := flag.Bool("brute", false, "暴力枚举真最优 (refan 约束)")
	flag.Parse()
	if *replayF != "" {
		replay(*replayF)
		return
	}
	d := parse(*dealt)
	if *dc < 0 {
		*dc = len(d) - 13
	}
	if *anchors {
		for _, a := range ofc.FindReFanAnchors(d) {
			l := ofc.BuildLayoutFromAnchor(a, d, *dc)
			if l == nil {
				fmt.Printf("anchor %s [%s] → nil\n", a.Type, str(a.Cards))
				continue
			}
			show("anchor "+a.Type+" ["+str(a.Cards)+"]", l.Top, l.Middle, l.Bottom)
		}
	}
	r := ofc.ExpertPlaceFantasy(d, *dc)
	if r == nil {
		fmt.Println("AI: nil")
	} else {
		show("AI", r.Layout.Top, r.Layout.Middle, r.Layout.Bottom)
		fmt.Printf("  discards: %s\n", str(r.Layout.Discards))
	}
	if *top != "" {
		show("对照", parse(*top), parse(*mid), parse(*bot))
	}
	if *brute {
		b := bruteForce(d)
		show("暴力最优", b.top, b.mid, b.bot)
	}
}
