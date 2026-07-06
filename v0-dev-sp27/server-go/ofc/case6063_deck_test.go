package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 2026-07-06 用户假设: 搜索 rollout 不带 usedCards? 对照实验:
// 60(1A死) vs 63(4A死) 同板同摆法, rollout 均值该不同(deck-aware)还是相同(deck-blind).
// 跑法: OFC_PROBE_CKPT=<ckpt> go test ./ofc -run TestCase6063DeckAware -v
func TestCase6063DeckAware(t *testing.T) {
	ckpt := os.Getenv("OFC_PROBE_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_PROBE_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	mkState := func(topKK bool, deadAces []string) *GameState {
		g := &GameState{NumJokers: 2, Round: 2}
		if topKK {
			g.Top = mkCards(t, "Qd", "Kh", "Ks")
			g.Middle = mkCards(t, "5c", "6c")
			g.Bottom = mkCards(t, "3h", "9s")
		} else {
			g.Top = mkCards(t, "Qd")
			g.Middle = mkCards(t, "5c", "6c")
			g.Bottom = mkCards(t, "3h", "9s", "Kh", "Ks")
		}
		g.UsedCards = map[string]bool{}
		for _, row := range [][]Card{g.Top, g.Middle, g.Bottom} {
			for _, c := range row {
				g.UsedCards[c.ID()] = true
			}
		}
		for _, a := range deadAces {
			c, _ := ParseCard(a)
			g.UsedCards[c.ID()] = true
		}
		d, _ := ParseCard("4d")
		g.UsedCards[d.ID()] = true
		g.SetDiscard(d)
		return g
	}
	one := []string{"Ad"}
	four := []string{"Ad", "Ah", "As", "Ac"}
	const N = 1000
	run := func(name string, gs *GameState) float64 {
		er := &ExpertRollout{Rng: rand.New(rand.NewSource(60)), Cfg: DefaultRolloutConfig}
		var sum float64
		fan, foul := 0, 0
		for k := 0; k < N; k++ {
			er.QuickRolloutDetailed(gs.Clone(), 2)
			r := er.LastResult
			switch {
			case r.IsFoul:
				sum += -float64(er.Cfg.FoulCost)
				foul++
			case r.IsFantasy:
				sum += float64(r.RawRoyalty + r.FanBonus)
				fan++
			default:
				sum += float64(r.RawRoyalty)
			}
		}
		t.Logf("%-24s mean=%6.2f foul=%4.1f%% fan=%5.1f%%", name, sum/N, 100*float64(foul)/N, 100*float64(fan)/N)
		return sum / N
	}
	t.Log("== 60语境 (1A死): 老师=底KK ==")
	t60top := run("60: 头KK", mkState(true, one))
	t60bot := run("60: 底KK", mkState(false, one))
	t.Log("== 63语境 (4A死): 老师=头KK ==")
	t63top := run("63: 头KK", mkState(true, four))
	t63bot := run("63: 底KK", mkState(false, four))
	t.Logf("60: 底-头 优势=%.2f (老师要>0) | 63: 头-底 优势=%.2f (老师要>0)", t60bot-t60top, t63top-t63bot)
}
