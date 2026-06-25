package ofc

import (
	"hash/fnv"
	"sort"
	"strconv"
)

// HashSolveInput — 输入哈希: 用于 per-request deterministic RNG seed.
// 相同 (state, dealt, round) → 相同 seed → 相同 rollout 序列 → 相同 AI 选择.
// 跟 bench / trace 端调用同函数, 保证两边可重现.
//
// hash 内容:
//   - state.Top, .Middle, .Bottom 各卡 ID
//   - state.UsedCards (sorted)
//   - dealt 各卡 ID
//   - round 编号
func HashSolveInput(state *GameState, dealt []Card, round int) uint64 {
	h := fnv.New64a()
	h.Write([]byte("R"))
	h.Write([]byte(strconv.Itoa(round)))
	h.Write([]byte("|T"))
	for _, c := range state.Top {
		h.Write([]byte(c.ID()))
	}
	h.Write([]byte("|M"))
	for _, c := range state.Middle {
		h.Write([]byte(c.ID()))
	}
	h.Write([]byte("|B"))
	for _, c := range state.Bottom {
		h.Write([]byte(c.ID()))
	}
	h.Write([]byte("|U"))
	used := make([]string, 0, len(state.UsedCards))
	for k := range state.UsedCards {
		used = append(used, k)
	}
	sort.Strings(used)
	for _, k := range used {
		h.Write([]byte(k))
	}
	h.Write([]byte("|D"))
	for _, c := range dealt {
		h.Write([]byte(c.ID()))
	}
	return h.Sum64()
}
