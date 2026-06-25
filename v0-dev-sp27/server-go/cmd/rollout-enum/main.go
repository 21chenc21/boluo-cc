// rollout-enum — 给 R1 dealt cards, 枚举所有 placements (经 hard rule filter), 每个跑 K rollouts, 打 ranking
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

func parseCards(strs []string) []ofc.Card {
	out := []ofc.Card{}
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			panic("bad: " + s)
		}
		out = append(out, c)
	}
	return out
}

func placementStr(gs *ofc.GameState) string {
	return fmt.Sprintf("头[%s] 中[%s] 底[%s]", cardsStr(gs.Top), cardsStr(gs.Middle), cardsStr(gs.Bottom))
}

func cardsStr(cards []ofc.Card) string {
	if len(cards) == 0 {
		return ""
	}
	s := cards[0].String()
	for i := 1; i < len(cards); i++ {
		s += " " + cards[i].String()
	}
	return s
}

func main() {
	dealtFlag := flag.String("dealt", "6h Tc Jd 4d 3d", "R1 dealt cards (space-sep)")
	ckpt := flag.String("ckpt", "ckpts-v2-ema/round-001-acc89.json", "")
	K := flag.Int("k", 1000, "rollouts per candidate")
	seed := flag.Int64("seed", 42, "")
	topShow := flag.Int("top", 15, "show top N candidates")
	jokers := flag.Int("jokers", 0, "deck jokers (for rollout deck reconstruction)")
	foulCost := flag.Float64("foul-cost", 6, "")
	fanQQ := flag.Float64("fan-bonus-qq", 20, "")
	fanKK := flag.Float64("fan-bonus-kk", 40, "")
	fanAA := flag.Float64("fan-bonus-aa", 80, "")
	fanTrips := flag.Float64("fan-bonus-trips", 90, "")
	flag.Parse()

	if err := ofc.LoadWeightsFromFile(*ckpt); err != nil {
		panic(err)
	}

	// parse dealt
	dealtStrs := []string{}
	cur := ""
	for _, ch := range *dealtFlag {
		if ch == ' ' {
			if cur != "" {
				dealtStrs = append(dealtStrs, cur)
				cur = ""
			}
		} else {
			cur += string(ch)
		}
	}
	if cur != "" {
		dealtStrs = append(dealtStrs, cur)
	}
	if len(dealtStrs) != 5 {
		panic(fmt.Sprintf("dealt must be 5 cards, got %d", len(dealtStrs)))
	}
	dealt := parseCards(dealtStrs)

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = float32(*foulCost)
	cfg.QQFanBonus = float32(*fanQQ)
	cfg.KKFanBonus = float32(*fanKK)
	cfg.AAFanBonus = float32(*fanAA)
	cfg.TripsFanBonus = float32(*fanTrips)

	state := &ofc.GameState{
		UsedCards: map[string]bool{},
		Round:     1,
		NumJokers: *jokers,
	}
	for _, c := range dealt {
		state.UsedCards[c.ID()] = true
	}

	// enumerate + hard rule filter
	placements := ofc.GenerateRound1Actions(dealt, state)
	type cand struct {
		placement []ofc.Row
		gs        *ofc.GameState
	}
	seen := make(map[string]bool)
	cands := []cand{}
	for _, p := range placements {
		tmp := state.Clone()
		for i, c := range dealt {
			tmp.PlaceCard(c, p[i])
		}
		key := fmt.Sprintf("%s|%s|%s", cardsStr(tmp.Top), cardsStr(tmp.Middle), cardsStr(tmp.Bottom))
		if seen[key] {
			continue
		}
		seen[key] = true
		pc := make([]ofc.Row, len(p))
		copy(pc, p)
		cands = append(cands, cand{pc, tmp})
	}
	// Hard rule
	r1c := make([]ofc.R1Cand, len(cands))
	for i, c := range cands {
		r1c[i] = ofc.R1Cand{Placement: c.placement, GS: c.gs}
	}
	r1c = ofc.ApplyHardRulesR1(r1c, dealt, state)
	if len(r1c) < len(cands) {
		keep := make(map[string]bool)
		for _, c := range r1c {
			keep[fmt.Sprintf("%s|%s|%s", cardsStr(c.GS.Top), cardsStr(c.GS.Middle), cardsStr(c.GS.Bottom))] = true
		}
		filtered := []cand{}
		for _, c := range cands {
			k := fmt.Sprintf("%s|%s|%s", cardsStr(c.gs.Top), cardsStr(c.gs.Middle), cardsStr(c.gs.Bottom))
			if keep[k] {
				filtered = append(filtered, c)
			}
		}
		cands = filtered
	}

	fmt.Printf("=== dealt: %s (jokers=0, post-hardrule cands=%d, K=%d rollouts each) ===\n\n",
		*dealtFlag, len(cands), *K)

	type result struct {
		placement string
		q         float32
		fanRate   float32
		foulRate  float32
	}
	results := make([]result, len(cands))

	for i, c := range cands {
		rng := rand.New(rand.NewSource(*seed + int64(i)*1000))
		er := &ofc.ExpertRollout{Rng: rng, Cfg: cfg}
		var sum float32
		var fanCnt, foulCnt int
		for k := 0; k < *K; k++ {
			_, _, _ = er.QuickRolloutDetailed(c.gs.Clone(), 1)
			r := er.LastResult
			// 2026-05-19: 用 LastResult.FanBonus (cap-chain aware) 替代旧 classifyFanBonus.
			if r.IsFoul {
				sum += -cfg.FoulCost
				foulCnt++
			} else if r.IsFantasy {
				sum += r.RawRoyalty + r.FanBonus
				fanCnt++
			} else {
				sum += r.RawRoyalty
			}
		}
		results[i] = result{
			placement: placementStr(c.gs),
			q:         sum / float32(*K),
			fanRate:   float32(fanCnt) / float32(*K),
			foulRate:  float32(foulCnt) / float32(*K),
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].q > results[j].q })

	fmt.Printf("%-50s %8s %6s %6s\n", "placement", "mean Q", "fan", "foul")
	for i, r := range results {
		if i >= *topShow {
			break
		}
		marker := "  "
		if i == 0 {
			marker = "★ "
		}
		fmt.Printf("%s%-48s %8.2f %6.3f %6.3f\n", marker, r.placement, r.q, r.fanRate, r.foulRate)
	}
}

// DEPRECATED (2026-05-19) — cap-down 误算. 用 ofc.FantasyBonusFromBoard 替代.
func classifyFanBonus_DEPRECATED(top []ofc.Card, cfg *ofc.RolloutConfig) float32 {
	var realCnt [13]int
	jokerCnt := 0
	for _, c := range top {
		if c.IsJoker() {
			jokerCnt++
		} else {
			realCnt[c.Rank()]++
		}
	}
	realMax := 0
	for _, v := range realCnt {
		if v > realMax {
			realMax = v
		}
	}
	effMax := realMax + jokerCnt
	if effMax >= 3 {
		return cfg.TripsFanBonus
	}
	pairR := -1
	if jokerCnt >= 1 {
		for r := 12; r >= 0; r-- {
			if realCnt[r] > 0 {
				pairR = r
				break
			}
		}
		if pairR < 0 && jokerCnt >= 2 {
			pairR = 12
		}
	} else {
		for r := 12; r >= 0; r-- {
			if realCnt[r] >= 2 {
				pairR = r
				break
			}
		}
	}
	if pairR >= 12 {
		return cfg.AAFanBonus
	}
	if pairR >= 11 {
		return cfg.KKFanBonus
	}
	return cfg.QQFanBonus
}
