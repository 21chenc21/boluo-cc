package main

import (
	"math/rand"
	"testing"

	"github.com/boluo/v0-server/ofc"
)

// F6 fanConflict 构型校验: 无重牌 / 张数=R4(2+3+4+发3) / 42型 底trips<顶对 (成顶trips必冒底)
func TestSeedFanConflict(t *testing.T) {
	n42, n46 := 0, 0
	for i := 0; i < 500; i++ {
		s := seedFanConflict(rand.New(rand.NewSource(int64(i))))
		if s.startRound != 4 {
			t.Fatalf("#%d startRound=%d", i, s.startRound)
		}
		if len(s.top) != 2 || len(s.mid) != 3 || len(s.bot) != 4 || len(s.dealt) != 3 {
			t.Fatalf("#%d 张数错: %d/%d/%d/%d", i, len(s.top), len(s.mid), len(s.bot), len(s.dealt))
		}
		seen := map[string]bool{}
		for _, c := range s.seedCards() {
			if seen[c.ID()] {
				t.Fatalf("#%d 重牌 %s", i, c.ID())
			}
			seen[c.ID()] = true
		}
		// 变体判别: 顶含鬼 = 46型
		joker := false
		for _, c := range s.top {
			if c.IsJoker() {
				joker = true
			}
		}
		if joker {
			n46++
		} else {
			n42++
			// 42型: 顶对 rank > 底 trips rank (成顶trips必冒底)
			topR := int(s.top[0].Rank())
			botR := int(s.bot[0].Rank())
			if topR <= botR {
				t.Fatalf("#%d 42型 顶对%d ≤ 底trips%d, 冲突不成立", i, topR, botR)
			}
			// 发牌含 P 第3张 (绝路诱惑)
			hasP := false
			for _, c := range s.dealt {
				if !c.IsJoker() && int(c.Rank()) == topR {
					hasP = true
				}
			}
			if !hasP {
				t.Fatalf("#%d 42型 发牌缺 P 第3张", i)
			}
		}
	}
	if n42 < 100 || n46 < 100 {
		t.Fatalf("变体分布失衡: 42型=%d 46型=%d", n42, n46)
	}
	_ = ofc.Card(0)
}

// F7 drawTrap + r1micro 102型 构型校验
func TestSeedDrawTrapAndBroadway(t *testing.T) {
	for i := 0; i < 500; i++ {
		s := seedDrawTrap(rand.New(rand.NewSource(int64(i))))
		if len(s.top) != 1 || len(s.mid) != 4 || len(s.bot) != 4 || len(s.dealt) != 3 {
			t.Fatalf("drawTrap #%d 张数错: %d/%d/%d/%d", i, len(s.top), len(s.mid), len(s.bot), len(s.dealt))
		}
		seen := map[string]bool{}
		for _, c := range s.seedCards() {
			if seen[c.ID()] {
				t.Fatalf("drawTrap #%d 重牌 %s", i, c.ID())
			}
			seen[c.ID()] = true
		}
		// 中 4 张连张; 发牌[0] 必须配中道某 rank (填对诱惑)
		midRanks := map[uint8]bool{}
		for _, c := range s.mid {
			midRanks[c.Rank()] = true
		}
		if !midRanks[s.dealt[0].Rank()] {
			t.Fatalf("drawTrap #%d 发牌[0] rank %d 不配中道", i, s.dealt[0].Rank())
		}
	}
	// r1micro 102型 (case 2): 5 张无对
	n102 := 0
	for i := 0; i < 2000; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		s := seedR1Micro(rng)
		if len(s.dealt) != 5 {
			t.Fatalf("r1micro #%d dealt=%d", i, len(s.dealt))
		}
		seen := map[string]bool{}
		rc := map[uint8]int{}
		joker := false
		for _, c := range s.dealt {
			if seen[c.ID()] {
				t.Fatalf("r1micro #%d 重牌", i)
			}
			seen[c.ID()] = true
			if c.IsJoker() {
				joker = true
			} else {
				rc[c.Rank()]++
			}
		}
		// 102型 = 无对无鬼
		hasPair := false
		for _, n := range rc {
			if n >= 2 {
				hasPair = true
			}
		}
		if !hasPair && !joker {
			n102++
		}
	}
	if n102 < 300 {
		t.Fatalf("102型 占比过低: %d/2000", n102)
	}
}
