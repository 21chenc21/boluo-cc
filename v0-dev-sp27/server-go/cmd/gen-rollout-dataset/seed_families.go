package main

// 2026-07-04 sp41: 种子家族 gen — 种"家族"不种"case" (用户拍板).
// 从结构约束内随机化的中局开局, 往后正常 gen (全候选 rollout label + margin + 探索).
// 与 case-train(死路, silver-label 硬灌 policy)本质不同: label 全是 rollout 真值,
// 只是把开局位置摆进数据荒区. 治"深组合 1e-4/局, 自然轨迹到不了"(#23 精确组合全数据集 0 条).

import (
	"fmt"
	"math/rand"

	"encoding/json"
	"github.com/boluo/v0-server/ofc"
	"log"
	"os"
)

// seedSpec — 家族种子: 构造的中局 + 该轮发牌
type seedSpec struct {
	startRound int
	top        []ofc.Card
	mid        []ofc.Card
	bot        []ofc.Card
	dealt      []ofc.Card
	extraUsed  []ofc.Card // 行外已见牌 (真人板: 对手可见牌/已弃牌) — 记 UsedCards + 剔 deck
	family     string
}

var rankChars = "23456789TJQKA"
var suitChars = "cdhs"

// cardPool — 防重复取牌
type cardPool struct {
	used map[string]bool
	rng  *rand.Rand
}

func newCardPool(rng *rand.Rand) *cardPool {
	return &cardPool{used: map[string]bool{}, rng: rng}
}

// tryTake — rank 固定, 确定性扫 4 花色(随机顺序). 真耗尽才返回 false.
// (2026-07-04 崩过: 旧版随机试32次, 3/4占用时 1e-4 概率误报耗尽, 3400次调用必中.)
func (p *cardPool) tryTake(rank int) (ofc.Card, bool) {
	for _, s := range p.rng.Perm(4) {
		id := fmt.Sprintf("%c%c", rankChars[rank], suitChars[s])
		if !p.used[id] {
			p.used[id] = true
			c, _ := ofc.ParseCard(id)
			return c, true
		}
	}
	return 0, false
}

// take — rank 固定取一张. 调用方保证该 rank 未耗尽 (takeN ≤4).
func (p *cardPool) take(rank int) ofc.Card {
	c, ok := p.tryTake(rank)
	if !ok {
		panic("cardPool: rank exhausted")
	}
	return c
}

// takeN — 同 rank 取 N 张
func (p *cardPool) takeN(rank, n int) []ofc.Card {
	out := make([]ofc.Card, n)
	for i := 0; i < n; i++ {
		out[i] = p.take(rank)
	}
	return out
}

// takeLow — 随机低牌 rank ∈ [lo,hi], 避开 avoid ranks. rank 耗尽则换 rank 重抽.
func (p *cardPool) takeLow(lo, hi int, avoid ...int) ofc.Card {
	for {
		r := lo + p.rng.Intn(hi-lo+1)
		bad := false
		for _, a := range avoid {
			if r == a {
				bad = true
				break
			}
		}
		if bad {
			continue
		}
		if c, ok := p.tryTake(r); ok {
			return c
		}
	}
}

func (p *cardPool) joker(idx int) ofc.Card {
	id := fmt.Sprintf("Xj%d", idx)
	p.used[id] = true
	c, _ := ofc.ParseCard(id)
	return c
}

// pickRank — 从候选里随机选一个, 避开 avoid
func pickRank(rng *rand.Rand, lo, hi int, avoid ...int) int {
	for {
		r := lo + rng.Intn(hi-lo+1)
		bad := false
		for _, a := range avoid {
			if r == a {
				bad = true
				break
			}
		}
		if !bad {
			return r
		}
	}
}

// ============ F1: premium 早锁底 + 顶约束 (#23/#24 家族) ============
// 结构: 顶已锁高对(K/A) + 底强对(T~Q) + 发来能锁两对的对子.
// 决策张力: 锁死底两对(exp线, 中道窄窗) vs 留活/劈对(AI旧病).
func seedLockBottom(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	topRank := pickRank(rng, 11, 12)                    // K/A
	botRank := pickRank(rng, 8, 10)                     // T/J/Q
	dealtRank := pickRank(rng, 9, 12, topRank, botRank) // J~A, 避开已用

	s := &seedSpec{family: "lockBottom"}
	if rng.Intn(4) == 0 {
		// 100型 (2026-07-06): 高牌顶 + 中已有对 + 发小对 — 小对进中做两对托高顶(exp) vs 小对下底(倒置雷).
		s.startRound = 2
		s.top = []ofc.Card{p.take(pickRank(rng, 10, 12))} // Q~A 孤张顶
		mr := pickRank(rng, 2, 5)                         // 中对 4~7
		s.mid = append(p.takeN(mr, 2), p.takeLow(0, 3, mr))
		s.bot = []ofc.Card{p.takeLow(3, 6, mr)}
		sp := pickRank(rng, 0, 3, mr) // 小对 2~5
		s.dealt = append(p.takeN(sp, 2), p.takeLow(5, 8, mr, sp))
		return s
	}
	s.top = p.takeN(topRank, 2)
	s.bot = append(p.takeN(botRank, 2), p.takeLow(0, 5, botRank))
	if rng.Intn(2) == 0 {
		s.startRound = 2 // 5 placed: 2+0+3
	} else {
		s.startRound = 3 // 7 placed: 2+2+3
		if rng.Intn(2) == 0 {
			midRank := pickRank(rng, 0, 4, botRank)
			s.mid = p.takeN(midRank, 2) // 中低对 (#24 形)
		} else {
			a := p.takeLow(0, 5, botRank)
			s.mid = []ofc.Card{a, p.takeLow(0, 5, botRank)}
		}
	}
	s.dealt = append(p.takeN(dealtRank, 2), p.takeLow(0, 6, botRank, dealtRank))
	return s
}

// ============ F2: 鬼守顶范种子 + 强底 (#110/#120 家族) ============
// 结构: 中 trips 面 + 底 FH/quads 托死 + 顶留鬼(或鬼在手).
// 决策张力: 鬼独守顶摸 QKA 进范(exp) vs 鬼配小牌锁低对(AI旧病).
func seedJokerTopSeed(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	m := pickRank(rng, 0, 5) // 中 trips 面 rank 2~7
	s := &seedSpec{family: "jokerTopSeed", startRound: 4}

	// 底: 50% quads / 50% FH (rank 避开 m)
	q := pickRank(rng, 2, 12, m)
	if rng.Intn(2) == 0 {
		s.bot = append(p.takeN(q, 4), p.takeLow(0, 7, m, q)) // quads+kicker
	} else {
		pr := pickRank(rng, 0, 12, m, q)
		s.bot = append(p.takeN(q, 3), p.takeN(pr, 2)...) // FH
	}

	if rng.Intn(2) == 0 {
		// 变体 A (#110 形): 顶[🃏], 中 对m+低张 (9 placed: 1+3+5)
		s.top = []ofc.Card{p.joker(0)}
		s.mid = append(p.takeN(m, 2), p.takeLow(0, 6, m, q))
		// 发牌: m 第3张(补中trips) + 低牌(鬼配对诱惑) + 中张
		s.dealt = []ofc.Card{p.take(m), p.takeLow(0, 6, m, q), p.takeLow(6, 9, m, q)}
	} else {
		// 变体 B (#120 形): 顶[], 中 trips m+低张, 鬼在发牌 (9 placed: 0+4+5)
		s.mid = append(p.takeN(m, 3), p.takeLow(0, 6, m, q))
		s.dealt = []ofc.Card{p.joker(0), p.takeLow(0, 6, m, q), p.takeLow(7, 10, m, q)}
	}
	return s
}

// ============ F3: 必爆诱饵 (std45 家族) ============
// 结构: 中 4 张同花(连张=SF诱饵/散张=花诱饵) + 底 4 张大牌 5-span 无花无对(max=顺) + 顶 1 高牌.
// 诱饵张进中 → 中成花/SF(tier5/8) > 底最大顺(tier4) = 必foul. 教"f89=1 一票否决"
// (policy 从不主动走必死线 → 数据零声量 → royalty军团(f95/f141)压过 f89. 全候选写样本, 诱饵线自带 label=-6.)
func seedFoulBait(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "foulBait", startRound: 4}
	suit := rng.Intn(4)
	mkSuited := func(rank int) ofc.Card {
		id := fmt.Sprintf("%c%c", rankChars[rank], suitChars[suit])
		p.used[id] = true
		c, _ := ofc.ParseCard(id)
		return c
	}
	var trapRank int
	if rng.Intn(2) == 0 {
		// 连张: base..base+3 同花, 诱饵 = base+4 (成 SF)
		base := rng.Intn(4) // 2..5 起
		for r := base; r < base+4; r++ {
			s.mid = append(s.mid, mkSuited(r))
		}
		trapRank = base + 4
	} else {
		// 散张 4 同花 (带 gap, 不成顺面), 诱饵 = 任一同花第5张 (成花)
		ranks := []int{0, 3, 6, 9} // 2,5,8,J 带 gap
		for _, r := range ranks {
			s.mid = append(s.mid, mkSuited(r))
		}
		trapRank = 11 // K 同花
	}
	trap := mkSuited(trapRank)
	// 底: 4 张 5-span 大牌区 (T~A), 无对; 花色轮转避免第二个花诱饵
	span := []int{8, 9, 11, 12} // T J K A (Q 留作顺 out)
	for i, r := range span {
		st := (suit + 1 + i%3) % 4 // 错开花色, 最多2张同色
		id := fmt.Sprintf("%c%c", rankChars[r], suitChars[st])
		if p.used[id] {
			s.bot = append(s.bot, p.take(r))
		} else {
			p.used[id] = true
			c, _ := ofc.ParseCard(id)
			s.bot = append(s.bot, c)
		}
	}
	// 顶: 1 张 Q~A
	s.top = []ofc.Card{p.take(pickRank(rng, 10, 12))}
	// 发牌: 诱饵 + 2 张低牌填充 (避开诱饵花色的低牌, 别造第二诱饵)
	s.dealt = []ofc.Card{trap, p.takeLow(0, 6), p.takeLow(0, 6)}
	return s
}

// ============ F5: R1 微摆位 (#94/#75/std1 家族, 2026-07-06) ============
// R1 五张的精细分配: 大对+大侧牌(94型) / 大对+低连张材料(75型) / 高对+杂牌+鬼(std1型).
// 老师(margin 600)在几百变体里演示"什么时候 245 是底顺材料、什么时候 2 是该上头的废牌" —
// context-dependent 偏好只能数据教, 不能规则拍 (2026-07-05 实验: #75 的 2c 该下底).
func seedR1Micro(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "r1micro", startRound: 1}
	switch rng.Intn(4) {
	case 0: // 94型: 大对(T~Q) + 两张大侧牌(K/A) + 杂牌
		pr := pickRank(rng, 8, 10)
		s.dealt = append(p.takeN(pr, 2), p.take(pickRank(rng, 11, 12)), p.take(pickRank(rng, 11, 12)), p.takeLow(2, 7, pr))
	case 1: // 75型: 大对(T~A) + 三张低连张材料 (base..base+2 或带gap)
		pr := pickRank(rng, 8, 12)
		base := rng.Intn(4) // 2..5
		s.dealt = append(p.takeN(pr, 2), p.take(base), p.take(base+1), p.take(base+3))
	case 2: // 102型 (2026-07-06): 无对 Broadway 区大牌 3~4 张 + 中张 — 拆三行 vs 全堆底.
		// #102: ThQhKd9s8s → exp 头Kd 中8s9s 底ThQh, AI 全下底. 死牌/分散美学只能数据教.
		n := 3 + rng.Intn(2) // 3~4 张大牌 (9~A 区), 全不同 rank
		var used []int
		for len(s.dealt) < n {
			r := pickRank(rng, 7, 12, used...)
			s.dealt = append(s.dealt, p.take(r))
			used = append(used, r)
		}
		for len(s.dealt) < 5 { // 补中张 6~9 (制造中道连张材料)
			r := pickRank(rng, 4, 7, used...)
			s.dealt = append(s.dealt, p.take(r))
			used = append(used, r)
		}
	default: // std1型: 高对(K/A) + 鬼 + 两张杂牌
		pr := pickRank(rng, 11, 12)
		s.dealt = append(p.takeN(pr, 2), p.joker(0), p.takeLow(0, 5, pr), p.takeLow(4, 9, pr))
	}
	return s
}

// ============ F6: 范×foul 冲突角落 (#42/#46 家族, 2026-07-06) ============
// 毒角落实锤: f89>0.8∧f107 全库仅 73 样本且被 46型假警报 alias 污染 → NN 学成"高foul+范潜力=+19".
// f89 鬼降级修好后特征分得开, 这里定向灌对比标签: 绝路线 rollout≈+5 vs 安路线≈+53, NN 学排序.
// (organic 密度 0.024%, 300局/iter 撞不到 — 只能种.)
func seedFanConflict(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "fanConflict", startRound: 4}
	// 2026-07-06 二批: 42型 30% / 46型 40%(加密, iter-3仍未裸翻) / 16型 30%(新)
	roll := rng.Intn(10)
	if roll < 3 {
		// 42型: 顶大对P + 底三条B(<P) + 发P第3张 — 顶成 trips P > 底 trips B = 必foul绝路,
		// 安路 = 大牌上头锁 P 对范 / 鬼中. 张力: trips范140 的诱惑 vs f89≈0.9.
		P := pickRank(rng, 9, 12)  // J~A
		B := pickRank(rng, 6, P-1) // 8~(P-1), 底三条被顶trips冒
		s.top = p.takeN(P, 2)
		s.bot = append(p.takeN(B, 3), p.takeLow(0, 5, B, P))
		mr := pickRank(rng, 0, 4, B, P) // 中弱: 低对+单 (42 的 344 形), 托不住顶 trips
		s.mid = append(p.takeN(mr, 2), p.takeLow(0, 5, mr, B, P))
		safe := p.take(pickRank(rng, 8, 12, P, B)) // 安全线材料 (42 的 Qs)
		if rng.Intn(2) == 0 {
			s.dealt = []ofc.Card{p.take(P), safe, p.joker(0)}
		} else {
			s.dealt = []ofc.Card{p.take(P), safe, p.takeLow(0, 6, P, B)}
		}
	} else if roll < 7 {
		// 46型: 顶[🃏+低牌x] + 底强三条 + 发 x 配对张 — 锁 xxx 顶(窄范4.5%) vs 留鬼自由行(宽范21.7%).
		// 非 foul 张力 (鬼可降级恒安全), 教"鬼顶 free-roll 价值".
		x := pickRank(rng, 0, 3) // 2~5
		s.top = []ofc.Card{p.joker(0), p.take(x)}
		B := pickRank(rng, 4, 11, x)
		s.bot = append(p.takeN(B, 3), p.take(pickRank(rng, 8, 12, B, x)))
		a := pickRank(rng, 2, 8, x, B)
		b := pickRank(rng, 2, 8, x, B, a)
		c := pickRank(rng, 2, 8, x, B, a, b)
		s.mid = []ofc.Card{p.take(a), p.take(b), p.take(c)}
		s.dealt = []ofc.Card{p.take(x), p.take(pickRank(rng, 2, 8, x, B)), p.takeLow(0, 6, x, B)}
	} else {
		// 16型 (2026-07-06 新): 顶[🃏+大牌Q~A] + 发三张同rank大牌(trip-A类) — 第三张同权牌的去向.
		// exp: 双A进中(保底范: 弃第三张后场上无牌能超中AA, 顶鬼配Q恒范恒安全) / AI旧病: A头+A中拆散.
		s.startRound = 3
		big := pickRank(rng, 10, 12) // Q~A 顶搭子
		s.top = []ofc.Card{p.joker(0), p.take(big)}
		ta := pickRank(rng, 10, 12, big) // trip rank (≠顶搭子)
		// 中 2 张杂 + 底 3 张强(对子/三条面)
		s.mid = []ofc.Card{p.takeLow(0, 6, ta, big), p.takeLow(0, 6, ta, big)}
		br := pickRank(rng, 6, 9, ta, big)
		s.bot = p.takeN(br, 3)
		s.dealt = p.takeN(ta, 3)
	}
	return s
}

// ============ F8: 过度填充/废鬼 (#22 家族, 2026-07-06) ============
// 结构: 底 [RR🃏](鬼当第3张R, 高价值) + 发 [R R x] — 双R全下底则鬼沦为quads kicker(废),
// exp: 一张R即凑quads(鬼=第4R), 另一张R是白赚材料(头/中). 教"行内鬼的边际价值守恒".
func seedOverfill(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "overfill", startRound: 3}
	R := pickRank(rng, 9, 12) // J~A
	s.top = []ofc.Card{p.joker(0)}
	mr := pickRank(rng, 0, 5, R)
	s.mid = append(p.takeN(mr, 2), p.takeLow(0, 6, R, mr))
	s.bot = append(p.takeN(R, 2), p.joker(1))
	s.dealt = append(p.takeN(R, 2), p.takeLow(0, 6, R, mr))
	return s
}

// ============ F9: 死A梯度重端 (std63 家族, 2026-07-06) ============
// 结构: 顶大牌孤张 + 发 KK + extraUsed 塞 3~4 张死A — A绝版则 KK 就是顶的天花板, 该锁头进范.
// 只种重端(3~4死A, 账本与老师同向+5.2); 轻端(1~2死A)不种 — 账本轻微反对老师(头KK+2.0),
// 反向标签会砸 organic 的"KK默认下底"先验. 原料 f64(A剩余)一直在, 教交互不加特征 (用户拍板).
func seedDeadAce(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "deadAce", startRound: 2}
	s.top = []ofc.Card{p.take(pickRank(rng, 9, 10))} // J/Q 孤张
	s.mid = []ofc.Card{p.takeLow(3, 5), p.takeLow(3, 5)}
	s.bot = []ofc.Card{p.takeLow(0, 2), p.takeLow(6, 8)}
	s.dealt = append(p.takeN(11, 2), p.takeLow(0, 6, 11)) // KK + 杂
	nDead := 3 + rng.Intn(2)                              // 3~4 张死A
	for i := 0; i < nDead; i++ {
		s.extraUsed = append(s.extraUsed, p.take(12))
	}
	return s
}

// ============ F7: 留draw陷阱 (std47 家族, 2026-07-06) ============
// 结构: 顶 1 大牌孤张 + 中 4 连张卡顺(50% 同花=花draw叠加) + 底 4 张强对/两对 + 发[中填对张, 大牌, 低牌].
// 张力: 弃填对张保卡顺(绝路: draw不进则顶垃圾>中 → 倒置) vs 填对锁中(exp, 顶用大牌发育).
// std47: 中3c4c5c6c 发2h9h5h — AI 弃5h保draw 头2h9h, exp 5h填55锁中. f158/f161 draw强度只奖"紧"不算后路.
func seedDrawTrap(rng *rand.Rand) *seedSpec {
	p := newCardPool(rng)
	s := &seedSpec{family: "drawTrap", startRound: 4}
	base := rng.Intn(4) // 中连张 base..base+3 (2..5 起, 卡两头顺)
	suited := rng.Intn(2) == 0
	if suited {
		suit := rng.Intn(4)
		for r := base; r < base+4; r++ {
			id := fmt.Sprintf("%c%c", rankChars[r], suitChars[suit])
			p.used[id] = true
			c, _ := ofc.ParseCard(id)
			s.mid = append(s.mid, c)
		}
	} else {
		for r := base; r < base+4; r++ {
			s.mid = append(s.mid, p.take(r))
		}
	}
	// 顶: 1 张 Q~A 孤张
	s.top = []ofc.Card{p.take(pickRank(rng, 10, 12))}
	// 底: 强对 K/A + 两张杂 (强但非成手, 不构成 42 型冲突)
	bp := pickRank(rng, 11, 12)
	s.bot = append(p.takeN(bp, 2), p.takeLow(4, 8, bp, base, base+1, base+2, base+3), p.takeLow(4, 8, bp))
	// 发牌: 填对张(配中连张任一 rank) + 大牌(顶发育材料) + 低牌
	fill := p.take(base + rng.Intn(4))
	// 低牌填充范围 0..5: base=0 时 avoid 盖满 0..3 会死循环 (2026-07-06 烟测抓)
	s.dealt = []ofc.Card{fill, p.take(pickRank(rng, 7, 9)), p.takeLow(0, 5, base, base+1, base+2, base+3)}
	return s
}

// ============ 真人板种子 (sp46, 2026-07-06): prod solve_log 的真实人类板 ============
// serve 分布直接进训练分布 — 治"陌生板"的正餐. JSON 格式:
//
//	[{"round":3,"top":["Ac"],"middle":["5c","6c"],"bottom":["3h","9s"],"dealt":["Kh","Ks","4d"],"used":["Ad","Ah"]},...]
//
// label 照旧 rollout 真值; 无效条目(牌数不对/重牌)跳过.
type realStateJSON struct {
	Round  int      `json:"round"`
	Top    []string `json:"top"`
	Middle []string `json:"middle"`
	Bottom []string `json:"bottom"`
	Dealt  []string `json:"dealt"`
	Used   []string `json:"used"`
}

var realSeeds []*seedSpec

func loadRealSeeds(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[gen] seed-states 读取失败: %v", err)
		return 0
	}
	var rows []realStateJSON
	if err := json.Unmarshal(data, &rows); err != nil {
		log.Printf("[gen] seed-states 解析失败: %v", err)
		return 0
	}
	parse := func(ss []string, seen map[string]bool) ([]ofc.Card, bool) {
		var out []ofc.Card
		for _, x := range ss {
			c, ok := ofc.ParseCard(x)
			if !ok || seen[c.ID()] {
				return nil, false
			}
			seen[c.ID()] = true
			out = append(out, c)
		}
		return out, true
	}
	for _, r := range rows {
		if r.Round < 1 || r.Round > 5 {
			continue
		}
		wantPlaced := 0
		wantDealt := 5
		if r.Round >= 2 {
			wantPlaced = 5 + 2*(r.Round-2)
			wantDealt = 3
		}
		if len(r.Top)+len(r.Middle)+len(r.Bottom) != wantPlaced || len(r.Dealt) != wantDealt {
			continue
		}
		seen := map[string]bool{}
		top, ok1 := parse(r.Top, seen)
		mid, ok2 := parse(r.Middle, seen)
		bot, ok3 := parse(r.Bottom, seen)
		dealt, ok4 := parse(r.Dealt, seen)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		// used 里剔掉已在行/dealt 的 (solve_log 的 usedCards 通常含全部)
		var extra []ofc.Card
		for _, x := range r.Used {
			c, ok := ofc.ParseCard(x)
			if !ok || seen[c.ID()] {
				continue
			}
			seen[c.ID()] = true
			extra = append(extra, c)
		}
		if len(top) > 3 || len(mid) > 5 || len(bot) > 5 {
			continue
		}
		realSeeds = append(realSeeds, &seedSpec{
			startRound: r.Round, top: top, mid: mid, bot: bot,
			dealt: dealt, extraUsed: extra, family: "realboard",
		})
	}
	return len(realSeeds)
}

func pickRealSeed(rng *rand.Rand) *seedSpec {
	if len(realSeeds) == 0 {
		return nil
	}
	return realSeeds[rng.Intn(len(realSeeds))]
}

// makeFamilySeed — 按权重选一个家族 (2026-07-06 二批: 剩余钉子全覆盖 —
// 46加密+16型→fanConflict, 22→overfill, 100→lockBottom/100型, 63重端→deadAce)
func makeFamilySeed(rng *rand.Rand) *seedSpec {
	switch r := rng.Intn(16); {
	case r < 3:
		return seedLockBottom(rng) // 3/16 (23/24维持 + 100型新)
	case r < 5:
		return seedJokerTopSeed(rng) // 2/16 (110/120 已解, 维持)
	case r < 7:
		return seedFoulBait(rng) // 2/16
	case r < 10:
		return seedR1Micro(rng) // 3/16 (94/75/std1/102 四变体)
	case r < 13:
		return seedFanConflict(rng) // 3/16 (42型/46型加密/16型)
	case r < 14:
		return seedDrawTrap(rng) // 1/16 (std47)
	case r < 15:
		return seedOverfill(rng) // 1/16 (22)
	default:
		return seedDeadAce(rng) // 1/16 (63重端)
	}
}

// makeNamedFamilySeed — 指定家族生产 (单家族 A/B 质检).
func makeNamedFamilySeed(rng *rand.Rand, name string) *seedSpec {
	switch name {
	case "lockBottom":
		return seedLockBottom(rng)
	case "jokerTopSeed":
		return seedJokerTopSeed(rng)
	case "foulBait":
		return seedFoulBait(rng)
	case "r1micro":
		return seedR1Micro(rng)
	case "fanConflict":
		return seedFanConflict(rng)
	case "drawTrap":
		return seedDrawTrap(rng)
	case "overfill":
		return seedOverfill(rng)
	case "deadAce":
		return seedDeadAce(rng)
	default:
		panic("未知种子家族: " + name)
	}
}

// seedCards — 种子占用的全部牌 (从 deck 剔除用)
func (s *seedSpec) seedCards() []ofc.Card {
	var out []ofc.Card
	out = append(out, s.top...)
	out = append(out, s.mid...)
	out = append(out, s.bot...)
	out = append(out, s.dealt...)
	out = append(out, s.extraUsed...)
	return out
}
