package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 7案构型签名在 E 自打局中的出现频率 (用户: "会不会遇到, 不遇到就还好").
// 签名=触发该决策的结构条件(放宽到家族级), 命中=这类抉择真实出现一次.
// 跑法: OFC_PROBE_CKPT=<E> OFC_OCC_GAMES=1000 go test ./ofc -run TestCaseOccurrence -v
func TestCaseOccurrence(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	games := 1000
	if v := os.Getenv("OFC_OCC_GAMES"); v != "" {
		games = atoiDef(v, 1000)
	}
	MctsDisabled = true
	defer func() { MctsDisabled = false }()

	counts := map[string]int{}
	rng := rand.New(rand.NewSource(77))
	for g := 0; g < games; g++ {
		deck := MakeDeck(2)
		for j := len(deck) - 1; j > 0; j-- {
			k := rng.Intn(j + 1)
			deck[j], deck[k] = deck[k], deck[j]
		}
		// phantom 对手 0~2 (与 gen/3metric 同口径)
		opp := rng.Intn(3)
		gs := &GameState{NumJokers: 2, Round: 1, UsedCards: map[string]bool{}}
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(int64(g))), Cfg: DefaultRolloutConfig}
		er.Cfg.PureMLP = true
		di := 0
		phantomN := func(round int) int {
			if opp == 0 {
				return 0
			}
			if round == 1 {
				return opp * 5
			}
			return opp * 2
		}
		burn := 0
		for round := 1; round <= 5; round++ {
			// phantom 消耗
			want := phantomN(round)
			for k := 0; k < want && len(deck)-di-burn > 3; k++ {
				burn++
				gs.UsedCards[deck[len(deck)-burn].ID()] = true
			}
			var dealt []Card
			if round == 1 {
				dealt = deck[di : di+5]
				di += 5
			} else {
				dealt = deck[di : di+3]
				di += 3
			}
			gs.Round = round
			// ===== 签名计数 (决策前状态 + dealt) =====
			sig(counts, gs, dealt, round)
			if round == 1 {
				er.ExpertPlace5(gs, dealt)
			} else {
				er.ExpertPlace3(gs, dealt)
			}
		}
	}
	t.Logf("局数=%d", games)
	for _, k := range []string{"75型R1(对+三小连窗)", "102型R1(Broadway无对)", "23型R2(顶大对+发大对)", "63型R2(发KK+A死绝)", "67/104型R2(薄边杂牌)", "16型R3(鬼大牌顶+发三条大)", "110型R4(底四条+鬼顶未锁)"} {
		t.Logf("  %-28s %d 次 (%.2f%%/局)", k, counts[k], 100*float64(counts[k])/float64(games))
	}
}

func atoiDef(s string, d int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return d
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func sig(counts map[string]int, gs *GameState, dealt []Card, round int) {
	var rc [13]int
	jokers := 0
	maxR := -1
	for _, c := range dealt {
		if c.IsJoker() {
			jokers++
		} else {
			rc[c.Rank()]++
			if int(c.Rank()) > maxR {
				maxR = int(c.Rank())
			}
		}
	}
	pairs, trips := 0, 0
	pairRank := -1
	for r := 12; r >= 0; r-- {
		if rc[r] == 2 {
			pairs++
			if pairRank < 0 {
				pairRank = r
			}
		}
		if rc[r] >= 3 {
			trips++
		}
	}
	switch round {
	case 1:
		// 75型: 一对(8~J) + 三张互异小牌落5宽窗, 无≥Q, 无鬼
		if jokers == 0 && maxR < int(RankQ) && pairs == 1 && trips == 0 && pairRank >= 6 {
			var small []int
			for r := 0; r < 13; r++ {
				if rc[r] == 1 && r < pairRank {
					small = append(small, r)
				}
			}
			if len(small) == 3 && small[2]-small[0] <= 4 {
				counts["75型R1(对+三小连窗)"]++
			}
		}
		// 102型: 无对无鬼, ≥3张 9+区
		if jokers == 0 && pairs == 0 && trips == 0 {
			big := 0
			for r := 7; r <= 12; r++ {
				big += rc[r]
			}
			if big >= 3 {
				counts["102型R1(Broadway无对)"]++
			}
		}
	case 2:
		// 23型: 顶已锁天然大对(≥QQ) + 发来天然大对(≥JJ)
		topPair := topLockedPairRank(gs)
		if topPair >= int(RankQ) && pairs >= 1 && pairRank >= int(RankJ) {
			counts["23型R2(顶大对+发大对)"]++
		}
		// 63型: 发KK + A 死绝(可见4张)
		if rc[RankK] >= 2 {
			seenA := 0
			for id := range gs.UsedCards {
				if c, ok := ParseCard(id); ok && !c.IsJoker() && c.Rank() == RankA {
					seenA++
				}
			}
			if seenA >= 4 {
				counts["63型R2(发KK+A死绝)"]++
			}
		}
		// 67/104型: 发3张互异 ≤T 杂牌 (薄边)
		if jokers == 0 && pairs == 0 && trips == 0 && maxR <= int(RankT) {
			counts["67/104型R2(薄边杂牌)"]++
		}
	case 3:
		// 16型: 顶=鬼+天然≥Q + 发三张同rank ≥Q
		hasJ, hasBig := false, false
		for _, c := range gs.Top {
			if c.IsJoker() {
				hasJ = true
			} else if c.Rank() >= RankQ {
				hasBig = true
			}
		}
		if hasJ && hasBig && trips >= 1 {
			for r := int(RankQ); r <= 12; r++ {
				if rc[r] >= 3 {
					counts["16型R3(鬼大牌顶+发三条大)"]++
				}
			}
		}
	case 4:
		// 110型: 底已成四条 + 顶含鬼未满
		var brc [13]int
		for _, c := range gs.Bottom {
			if !c.IsJoker() {
				brc[c.Rank()]++
			}
		}
		quads := false
		for r := 0; r < 13; r++ {
			if brc[r] >= 4 {
				quads = true
			}
		}
		topJ := false
		for _, c := range gs.Top {
			if c.IsJoker() {
				topJ = true
			}
		}
		if quads && topJ && len(gs.Top) < 3 {
			counts["110型R4(底四条+鬼顶未锁)"]++
		}
	}
}
