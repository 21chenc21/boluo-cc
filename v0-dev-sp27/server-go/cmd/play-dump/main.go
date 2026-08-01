// play-dump — 随机发牌打 N 整局 (pureMLP sp26), 每轮 dump 摆法. 给人看 solver 实际布局.
// 用法: play-dump -ckpt best.json -games 100 -jokers 2 [-seed S]
// 输出每行: gameIdx|round|发牌|顶|中|底|丢  (card 空格分隔); 每局后一行 ===
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

func cardsStr(cs []ofc.Card) string {
	s := ""
	for i, c := range cs {
		if i > 0 {
			s += " "
		}
		s += c.String()
	}
	return s
}

func cp(cs []ofc.Card) []ofc.Card { return append([]ofc.Card(nil), cs...) }

// diff — after 比 before 多出的牌 (multiset)
func diff(after, before []ofc.Card) []ofc.Card {
	cnt := map[ofc.Card]int{}
	for _, c := range before {
		cnt[c]++
	}
	var out []ofc.Card
	for _, c := range after {
		if cnt[c] > 0 {
			cnt[c]--
		} else {
			out = append(out, c)
		}
	}
	return out
}

// dw — 显示宽 (CJK=2). pad — 左对齐到显示宽 w. 对齐用户 python formatter (W=[18,10,17,17,5]).
func dw(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2E80 {
			w += 2
		} else {
			w++
		}
	}
	return w
}
func pad(s string, w int) string {
	for dw(s) < w {
		s += " "
	}
	return s
}

// sameSet — 两个 card 列表是否同一 multiset (顺序无关)
func sameSet(a, b []ofc.Card) bool {
	if len(a) != len(b) {
		return false
	}
	cnt := map[ofc.Card]int{}
	for _, c := range a {
		cnt[c]++
	}
	for _, c := range b {
		cnt[c]--
		if cnt[c] < 0 {
			return false
		}
	}
	return true
}

type divRec struct {
	game, round                            int
	top, mid, bot, dealt                   string
	rTop, rMid, rBot, rDisc                string // 规则ON pick
	nTop, nMid, nBot, nDisc                string // 纯NN pick
	foul, fan                              bool
}

type roundRow struct {
	dealt, tn, mn, bn, disc string
	ntn, nmn, nbn           string // 纯NN pick (csvdiff)
	isDiff                  bool
}
type gameRec struct {
	rounds []roundRow
	res    string
	foul   bool
	fan    bool
}

func main() {
	ckpt := flag.String("ckpt", "", "ckpt path")
	games := flag.Int("games", 100, "")
	jokers := flag.Int("jokers", 2, "")
	seed := flag.Int64("seed", 0, "")
	pretty := flag.Bool("pretty", false, "对齐表格输出 (发牌|顶|中|底|丢)")
	foulOnly := flag.Bool("foulonly", false, "pretty 模式只出 FOUL 局")
	tag := flag.String("tag", "", "pretty 表头标签")
	csv := flag.Bool("csv", false, "CSV输出 (局,轮,发牌,顶,中,底,弃,结果) 手机表格app友好")
	diffMode := flag.Bool("diff", false, "规则审计: 每轮对比 规则ON vs 纯NN pick, 输出分歧CSV")
	csvDiff := flag.Bool("csvdiff", false, "CSV (局,轮,发牌,顶,中,底,弃) + 纯NN(N顶,N中,N底) + 差异列 + 结果, 所有轮")
	flag.Parse()
	if !*diffMode { // diff 模式自管 flags; 非diff 时读 env (净效果对照用)
		if os.Getenv("DISABLE_HARD_RULES") == "1" {
			ofc.HardRulesDisabled = true
		}
		if os.Getenv("DISABLE_SOFT_RULES") == "1" {
			ofc.SoftRulesDisabled = true
		}
	}
	if rs := os.Getenv("DISABLE_RULES"); rs != "" { // 逐条关 (ablation)
		for _, n := range strings.Split(rs, ",") {
			ofc.DisabledRules[strings.TrimSpace(n)] = true
		}
	}
	// sp46 保险丝/搜索接线 (与 bench-cases/server 同款)
	if v := os.Getenv("OFC_SB_PENALTY"); v != "" {
		var p float64
		fmt.Sscanf(v, "%f", &p)
		ofc.ServeSBPenalty = p
	}
	if os.Getenv("OFC_KEEP_FILTERS") != "" {
		ofc.KeepFiltersPureNN = true
	}
	if v := os.Getenv("OFC_SEARCH_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			ofc.ServeSearchWorkers = n
		}
	}
	if v := os.Getenv("OFC_SEARCH_CAP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= ofc.ServeSearchBatch {
			ofc.ServeSearchCap = n
		}
	}
	if v := os.Getenv("OFC_SERVE_SEARCH"); v != "" {
		var m float64
		fmt.Sscanf(v, "%f", &m)
		ofc.ServeSearchMargin = float32(m)
		fmt.Fprintf(os.Stderr, "[play-dump] OFC_SERVE_SEARCH=%.2f 薄边搜索 ON (cap %d)\n", m, ofc.ServeSearchCap)
	}
	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(*seed))
	if err := ofc.LoadWeightsFromFile(*ckpt); err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
	ofc.MctsDisabled = true
	cfg := ofc.DefaultRolloutConfig
	cfg.PureMLP = true

	recs := make([]gameRec, 0, *games)
	var divs []divRec
	nFoul, nFan := 0, 0
	for g := 0; g < *games; g++ {
		divStart := len(divs)
		deck := ofc.MakeDeck(*jokers)
		rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
		my := deck[:17]
		state := ofc.NewGameState(*jokers)
		er := &ofc.ExpertRollout{Rng: rng, Cfg: cfg}
		var rec gameRec
		for round := 1; round <= 5; round++ {
			state.Round = round
			var dealt []ofc.Card
			if round == 1 {
				dealt = my[0:5]
			} else {
				start := 5 + (round-2)*3
				dealt = my[start : start+3]
			}
			bt, bm, bb := cp(state.Top), cp(state.Middle), cp(state.Bottom)
			var stateNN *ofc.GameState
			if *diffMode || *csvDiff {
				stateNN = state.Clone() // pre-state 克隆给纯NN
			}
			if round == 1 {
				er.ExpertPlace5(state, dealt) // 规则ON (flags 默认 false)
			} else {
				er.ExpertPlace3(state, dealt)
			}
			tn, mn, bn := diff(state.Top, bt), diff(state.Middle, bm), diff(state.Bottom, bb)
			placed := append(append(cp(tn), mn...), bn...)
			disc := diff(dealt, placed)
			rr := roundRow{dealt: cardsStr(dealt), tn: cardsStr(tn), mn: cardsStr(mn), bn: cardsStr(bn), disc: cardsStr(disc)}
			if *diffMode || *csvDiff {
				ofc.HardRulesDisabled = true
				ofc.SoftRulesDisabled = true
				erNN := &ofc.ExpertRollout{Rng: rng, Cfg: cfg}
				if round == 1 {
					erNN.ExpertPlace5(stateNN, dealt)
				} else {
					erNN.ExpertPlace3(stateNN, dealt)
				}
				ofc.HardRulesDisabled = false
				ofc.SoftRulesDisabled = false
				nTn, nMn, nBn := diff(stateNN.Top, bt), diff(stateNN.Middle, bm), diff(stateNN.Bottom, bb)
				isDiff := !sameSet(tn, nTn) || !sameSet(mn, nMn) || !sameSet(bn, nBn)
				rr.ntn, rr.nmn, rr.nbn, rr.isDiff = cardsStr(nTn), cardsStr(nMn), cardsStr(nBn), isDiff
				if *diffMode && isDiff {
					nPlaced := append(append(cp(nTn), nMn...), nBn...)
					nDisc := diff(dealt, nPlaced)
					divs = append(divs, divRec{game: g + 1, round: round,
						top: cardsStr(bt), mid: cardsStr(bm), bot: cardsStr(bb), dealt: cardsStr(dealt),
						rTop: cardsStr(tn), rMid: cardsStr(mn), rBot: cardsStr(bn), rDisc: cardsStr(disc),
						nTop: cardsStr(nTn), nMid: cardsStr(nMn), nBot: cardsStr(nBn), nDisc: cardsStr(nDisc)})
				}
			}
			rec.rounds = append(rec.rounds, rr)
			if !*pretty && !*csv && !*diffMode && !*csvDiff {
				fmt.Printf("%d|%d|%s|%s|%s|%s|%s\n", g, round,
					cardsStr(dealt), cardsStr(tn), cardsStr(mn), cardsStr(bn), cardsStr(disc))
			}
		}
		// 终局结果
		sc := ofc.ScoreHand(state.Top, state.Middle, state.Bottom)
		rec.foul, rec.fan = sc.Foul, sc.Fantasy
		rec.res = fmt.Sprintf("royalty=%d", sc.Royalties)
		if sc.Foul {
			rec.res = "FOUL"
			nFoul++
		} else if sc.Fantasy {
			rec.res += " 范"
			nFan++
		}
		if *diffMode {
			for i := divStart; i < len(divs); i++ {
				divs[i].foul, divs[i].fan = sc.Foul, sc.Fantasy
			}
		}
		if !*pretty && !*csv && !*diffMode && !*csvDiff {
			fmt.Printf("=%d=%s|%s|%s\n", g, cardsStr(state.Top), cardsStr(state.Middle), cardsStr(state.Bottom))
			fmt.Printf("===%s\n", rec.res)
		}
		recs = append(recs, rec)
	}
	if *diffMode {
		fmt.Print("\xEF\xBB\xBF") // UTF-8 BOM, Windows Excel 不乱码
		fmt.Println("局,轮,顶,中,底,发牌,R顶,R中,R底,R弃,N顶,N中,N底,N弃,结果")
		res := func(d divRec) string {
			if d.foul {
				return "FOUL"
			}
			if d.fan {
				return "范"
			}
			return "ok"
		}
		for _, d := range divs {
			fmt.Printf("%d,R%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
				d.game, d.round, d.top, d.mid, d.bot, d.dealt,
				d.rTop, d.rMid, d.rBot, d.rDisc, d.nTop, d.nMid, d.nBot, d.nDisc, res(d))
		}
		fmt.Fprintf(os.Stderr, "[diff] %d局 中 %d 个决策分歧 (规则改了NN选择)\n", *games, len(divs))
		return
	}
	if *pretty {
		// 对齐表格 (列起点: 发牌0/顶18/中28/底44/丢60). 局号 1-based.
		fmt.Printf("【%d局 seed%d %s】 FOUL %d%% / 范 %d%%\n\n", *games, *seed, *tag, nFoul*100 / *games, nFan*100 / *games)
		for g, rec := range recs {
			if *foulOnly && !rec.foul {
				continue
			}
			mark := rec.res
			if rec.foul {
				mark = "🔴FOUL"
			} else if rec.fan {
				mark = "🟢" + rec.res
			}
			fmt.Printf("%s 局%d  %s\n", "──────────────────────────────────────────────────────────", g+1, mark)
			fmt.Println(pad("发牌", 18) + pad("顶", 10) + pad("中", 17) + pad("底", 17) + pad("丢", 5))
			for _, r := range rec.rounds {
				fmt.Printf("%s%s%s%s%s\n", pad(r.dealt, 18), pad(r.tn, 10), pad(r.mn, 17), pad(r.bn, 17), r.disc)
			}
			fmt.Println()
		}
	}
	if *csv {
		fmt.Print("\xEF\xBB\xBF") // UTF-8 BOM, Windows Excel 不乱码
		fmt.Println("局,轮,发牌,顶,中,底,弃,结果")
		for g, rec := range recs {
			if *foulOnly && !rec.foul {
				continue
			}
			res := rec.res // FOUL / royalty=N / royalty=N 范
			res = strings.Replace(res, "royalty=", "", 1)
			for ri, r := range rec.rounds {
				fmt.Printf("%d,R%d,%s,%s,%s,%s,%s,%s\n", g+1, ri+1, r.dealt, r.tn, r.mn, r.bn, r.disc, res)
			}
		}
	}
	if *csvDiff {
		fmt.Print("\xEF\xBB\xBF") // UTF-8 BOM, Windows Excel 不乱码
		fmt.Println("局,轮,发牌,顶,中,底,弃,N顶,N中,N底,差异,结果")
		for g, rec := range recs {
			if *foulOnly && !rec.foul {
				continue
			}
			res := strings.Replace(rec.res, "royalty=", "", 1)
			for ri, r := range rec.rounds {
				diffMark := ""
				if r.isDiff {
					diffMark = "差异"
				}
				fmt.Printf("%d,R%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
					g+1, ri+1, r.dealt, r.tn, r.mn, r.bn, r.disc,
					r.ntn, r.nmn, r.nbn, diffMark, res)
			}
		}
	}
}
