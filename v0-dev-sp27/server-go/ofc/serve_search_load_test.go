package ofc

import (
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

// 2026-07-05 (用户"保证10并发都触发搜索都正常, 5s内"): 并发压测 harness.
// 跑法: OFC_LOAD_CKPT=<ckpt> go test ./ofc -run TestServeSearchLoad -v
// 10 goroutine 各自在 std63 状态(R2 薄边, 必触发)上调 ExpertPlace3, wait 拉满保证无降级.
func TestServeSearchLoad(t *testing.T) {
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
	ServeSearchMargin = 2.5
	ServeSearchWait = 10 * time.Second // 压测: 不许降级, 都排队搜
	SetServeSearchSlots(2)
	ServeSearchWorkers = 2 // 2槽×2worker = 4核打满不超卖

	mk := func(ss ...string) []Card {
		var r []Card
		for _, s := range ss {
			c, _ := ParseCard(s)
			r = append(r, c)
		}
		return r
	}
	buildState := func() (*GameState, []Card) {
		g := &GameState{NumJokers: 2, Round: 2}
		g.Top, g.Middle, g.Bottom = mk("Qd"), mk("5c", "6c"), mk("3h", "9s")
		g.UsedCards = map[string]bool{}
		for _, id := range []string{"Qd", "5c", "6c", "3h", "9s", "Ad", "Ah", "As", "Ac"} {
			g.UsedCards[id] = true
		}
		return g, mk("Kh", "Ks", "4d")
	}

	const N = 10
	lat := make([]time.Duration, N)
	picksKK := make([]bool, N)
	var wg sync.WaitGroup
	t0 := time.Now()
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gs, dealt := buildState()
			er := &ExpertRollout{Rng: rand.New(rand.NewSource(int64(1000 + i))), Cfg: DefaultRolloutConfig}
			s := time.Now()
			er.ExpertPlace3(gs, dealt)
			lat[i] = time.Since(s)
			picksKK[i] = len(gs.Top) == 3 // KK 上顶 = 顶满3张 (Qd+Kh+Ks)
		}(i)
	}
	wg.Wait()
	total := time.Since(t0)

	var max time.Duration
	correct := 0
	for i := 0; i < N; i++ {
		if lat[i] > max {
			max = lat[i]
		}
		if picksKK[i] {
			correct++
		}
		t.Logf("req[%d]: %.2fs  KK上顶=%v", i, lat[i].Seconds(), picksKK[i])
	}
	t.Logf("总墙钟 %.2fs  最慢单请求 %.2fs  搜索选对(KK顶) %d/%d", total.Seconds(), max.Seconds(), correct, N)
	if max > 5*time.Second {
		t.Errorf("最慢请求 %.2fs 超 5s 预算", max.Seconds())
	}
	if correct < 8 {
		t.Errorf("搜索质量: 仅 %d/10 选对 KK 上顶 (期望 ≥8, 40 sims 噪声允许偶失)", correct)
	}
}
