// 烟测: 跑一手 Go expertPlace5 + expertPlace3, 输出每轮决策 + 终局
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

func main() {
	r1Mult := flag.Float64("r1mult", 1.0, "R1 mult (1.0=high, 0.5=medium, 0.25=low)")
	seed := flag.Int64("seed", time.Now().UnixNano(), "rng seed")
	jokers := flag.Int("jokers", 0, "joker count 0/2/4")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	er := &ofc.ExpertRollout{
		Rng: rng,
		Cfg: ofc.RolloutConfig{R1Mult: float32(*r1Mult)},
	}

	state := ofc.NewGameState(*jokers)
	deck := ofc.MakeDeck(*jokers)
	// shuffle
	for i := len(deck) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}

	deckIdx := 0
	totalT0 := time.Now()
	for round := 1; round <= 5; round++ {
		state.Round = round
		n := 5
		if round != 1 {
			n = 3
		}
		dealt := deck[deckIdx : deckIdx+n]
		deckIdx += n
		t0 := time.Now()
		dealtStr := make([]string, len(dealt))
		for i, c := range dealt {
			dealtStr[i] = c.String()
		}
		fmt.Printf("R%d dealt: %s ", round, strings.Join(dealtStr, " "))
		if round == 1 {
			er.ExpertPlace5(state, dealt)
		} else {
			er.ExpertPlace3(state, dealt)
		}
		ms := time.Since(t0).Milliseconds()
		topStr := make([]string, len(state.Top))
		midStr := make([]string, len(state.Middle))
		botStr := make([]string, len(state.Bottom))
		for i, c := range state.Top {
			topStr[i] = c.String()
		}
		for i, c := range state.Middle {
			midStr[i] = c.String()
		}
		for i, c := range state.Bottom {
			botStr[i] = c.String()
		}
		fmt.Printf("→ top=[%s] mid=[%s] bot=[%s]  %dms\n",
			strings.Join(topStr, ","), strings.Join(midStr, ","), strings.Join(botStr, ","), ms)
	}
	totalMs := time.Since(totalT0).Milliseconds()
	score := state.Score()
	fmt.Printf("\nfinal: foul=%v fan=%v royalty=%d (top=%d mid=%d bot=%d)\n",
		score.Foul, score.Fantasy, score.Royalties, score.TopRoyalty, score.MidRoyalty, score.BotRoyalty)
	fmt.Printf("total time: %dms\n", totalMs)
}
