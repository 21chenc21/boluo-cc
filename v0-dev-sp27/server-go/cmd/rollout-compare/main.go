// rollout-compare — 给两个 R1 placement, 各跑 N rollouts, 比真实 mean Q
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"

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

func makeState(top, mid, bot []string) *ofc.GameState {
	gs := &ofc.GameState{
		Top:       parseCards(top),
		Middle:    parseCards(mid),
		Bottom:    parseCards(bot),
		UsedCards: map[string]bool{},
		Round:     1, // R1 placed
	}
	for _, c := range gs.Top {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Middle {
		gs.UsedCards[c.ID()] = true
	}
	for _, c := range gs.Bottom {
		gs.UsedCards[c.ID()] = true
	}
	return gs
}

func runRollouts(name string, gs *ofc.GameState, n int, cfg *ofc.RolloutConfig, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	er := &ofc.ExpertRollout{Rng: rng, Cfg: *cfg}
	scores := make([]float32, n)
	var sum, sumSq float64
	for i := 0; i < n; i++ {
		s := er.QuickRollout(gs.Clone(), 1)
		scores[i] = s
		sum += float64(s)
		sumSq += float64(s) * float64(s)
	}
	mean := sum / float64(n)
	variance := (sumSq / float64(n)) - mean*mean
	stddev := math.Sqrt(variance)
	stderr := stddev / math.Sqrt(float64(n))
	fmt.Printf("%-50s mean=%.3f  σ=%.3f  SE=%.3f  N=%d  range=[%.2f, %.2f]\n",
		name, mean, stddev, stderr, n, mean-2*stderr, mean+2*stderr)
}

func main() {
	ckpt := flag.String("ckpt", "ckpts-v2-ema/round-001-acc89.json", "")
	n := flag.Int("n", 1000, "rollouts per candidate")
	seed := flag.Int64("seed", 42, "")
	flag.Parse()

	if err := ofc.LoadWeightsFromFile(*ckpt); err != nil {
		panic(err)
	}

	cfg := ofc.DefaultRolloutConfig
	cfg.FoulCost = 20
	cfg.QQFanBonus = 50
	cfg.KKFanBonus = 70
	cfg.AAFanBonus = 200
	cfg.TripsFanBonus = 200

	fmt.Printf("=== R1 dealt 7s 2d Ah 4c Ts, %d rollouts each ===\n\n", *n)

	// 候选 A: 1+2+2 balanced (user 偏好)
	a := makeState([]string{"Ah"}, []string{"2d", "4c"}, []string{"7s", "Ts"})
	runRollouts("A: 头[Ah] 中[2d 4c] 底[7s Ts] (1+2+2)", a, *n, &cfg, *seed)

	// 候选 B: 2+2+1 (MCTS Mac 选)
	b := makeState([]string{"2d", "Ah"}, []string{"7s", "4c"}, []string{"Ts"})
	runRollouts("B: 头[2d Ah] 中[7s 4c] 底[Ts] (2+2+1)", b, *n, &cfg, *seed+1)

	// 候选 C: 1+3+1 (MCTS Linux init-n=20 选过)
	c := makeState([]string{"Ah"}, []string{"2d", "4c", "Ts"}, []string{"7s"})
	runRollouts("C: 头[Ah] 中[2d 4c Ts] 底[7s] (1+3+1)", c, *n, &cfg, *seed+2)
}
