package ofc

import (
	"math/rand"
	"os"
	"testing"
)

// 触发率测量: 100 整局 pure-NN, 数 top1-top2 < margin 的决策占比 (dry-run 不真搜).
// 跑法: OFC_LOAD_CKPT=<ckpt> go test ./ofc -run TestServeSearchTriggerRate -v
func TestServeSearchTriggerRate(t *testing.T) {
	ckpt := os.Getenv("OFC_LOAD_CKPT")
	if ckpt == "" {
		t.Skip("需 OFC_LOAD_CKPT")
	}
	if err := LoadWeightsFromFile(ckpt); err != nil {
		t.Fatalf("load: %v", err)
	}
	MctsDisabled = true
	HardRulesDisabled = true
	SoftRulesDisabled = true
	ServeSearchDryRun = true
	defer func() { ServeSearchDryRun = false; ServeSearchMargin = 0 }()

	for _, margin := range []float32{2.5} {
		ServeSearchMargin = margin
		ServeSearchDecCount, ServeSearchTrigCount = 0, 0
		rng := rand.New(rand.NewSource(42))
		const GAMES = 60
		for g := 0; g < GAMES; g++ {
			state := NewGameState(2)
			deck := MakeDeck(2)
			for i := len(deck) - 1; i > 0; i-- {
				j := rng.Intn(i + 1)
				deck[i], deck[j] = deck[j], deck[i]
			}
			er := &ExpertRollout{Rng: rand.New(rand.NewSource(rng.Int63())), Cfg: DefaultRolloutConfig}
			for round := 1; round <= 5; round++ {
				state.Round = round
				if round == 1 {
					er.ExpertPlace5(state, deck[0:5])
				} else {
					start := 5 + (round-2)*3
					er.ExpertPlace3(state, deck[start:start+3])
				}
			}
		}
		t.Logf("margin=%.1f: 决策 %d, 薄边触发 %d (%.1f%%) + foulfuse触发 %d (%.2f%%) → 每局(9手)期望搜索 %.2f 次",
			margin, ServeSearchDecCount, ServeSearchTrigCount,
			100*float64(ServeSearchTrigCount)/float64(ServeSearchDecCount),
			ServeSearchFoulTrigCount,
			100*float64(ServeSearchFoulTrigCount)/float64(ServeSearchDecCount),
			9*float64(ServeSearchTrigCount+ServeSearchFoulTrigCount+ServeSearchFanTrigCount)/float64(ServeSearchDecCount))
		t.Logf("  fanfloor触发 %d (%.2f%%)", ServeSearchFanTrigCount,
			100*float64(ServeSearchFanTrigCount)/float64(ServeSearchDecCount))
	}
}
