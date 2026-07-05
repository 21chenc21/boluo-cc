package main

// 2026-07-04 sp41: 种子家族 gen — 种"家族"不种"case" (用户拍板).
// 从结构约束内随机化的中局开局, 往后正常 gen (全候选 rollout label + margin + 探索).
// 与 case-train(死路, silver-label 硬灌 policy)本质不同: label 全是 rollout 真值,
// 只是把开局位置摆进数据荒区. 治"深组合 1e-4/局, 自然轨迹到不了"(#23 精确组合全数据集 0 条).

import (
	"fmt"
	"math/rand"

	"github.com/boluo/v0-server/ofc"
)

// seedSpec — 家族种子: 构造的中局 + 该轮发牌
type seedSpec struct {
	startRound int
	top        []ofc.Card
	mid        []ofc.Card
	bot        []ofc.Card
	dealt      []ofc.Card
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
	topRank := pickRank(rng, 11, 12)              // K/A
	botRank := pickRank(rng, 8, 10)               // T/J/Q
	dealtRank := pickRank(rng, 9, 12, topRank, botRank) // J~A, 避开已用

	s := &seedSpec{family: "lockBottom"}
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

// makeFamilySeed — 均匀选一个家族
func makeFamilySeed(rng *rand.Rand) *seedSpec {
	switch r := rng.Intn(10); {
	case r < 4:
		return seedLockBottom(rng)
	case r < 8:
		return seedJokerTopSeed(rng)
	default:
		return seedFoulBait(rng) // 20%
	}
}

// seedCards — 种子占用的全部牌 (从 deck 剔除用)
func (s *seedSpec) seedCards() []ofc.Card {
	var out []ofc.Card
	out = append(out, s.top...)
	out = append(out, s.mid...)
	out = append(out, s.bot...)
	out = append(out, s.dealt...)
	return out
}
