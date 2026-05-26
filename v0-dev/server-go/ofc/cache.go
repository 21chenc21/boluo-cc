package ofc

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// SolveCache — 进程内 LRU. 同 (state, dealt, cfg) → 100% 命中, 直接返回缓存的
// layout/discards. 用 sync.Mutex 保护; size 上限按设置淘汰最久未用.
type SolveCache struct {
	mu     sync.Mutex
	max    int
	order  []string                  // 旧→新; tail 是最近使用
	values map[string]*solveCacheVal // key → entry
	hits   atomic.Int64
	misses atomic.Int64
}

type solveCacheVal struct {
	Layout   map[string][]string
	Discards []string
	idx      int // 在 order 中的下标
}

func NewSolveCache(max int) *SolveCache {
	return &SolveCache{
		max:    max,
		order:  make([]string, 0, max),
		values: make(map[string]*solveCacheVal, max),
	}
}

func (c *SolveCache) Get(key string) (*solveCacheVal, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	// 移到末尾 = 最近使用
	if v.idx != len(c.order)-1 {
		// O(n) 移动; max=2000 时单次 ~微秒级, 可接受
		copy(c.order[v.idx:], c.order[v.idx+1:])
		c.order[len(c.order)-1] = key
		// 更新 idx (key 之后的所有 entry 下标 -1)
		for i := v.idx; i < len(c.order); i++ {
			c.values[c.order[i]].idx = i
		}
	}
	return v, true
}

func (c *SolveCache) Set(key string, layout map[string][]string, discards []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.values[key]; ok {
		existing.Layout = layout
		existing.Discards = discards
		// 移到末尾
		if existing.idx != len(c.order)-1 {
			copy(c.order[existing.idx:], c.order[existing.idx+1:])
			c.order[len(c.order)-1] = key
			for i := existing.idx; i < len(c.order); i++ {
				c.values[c.order[i]].idx = i
			}
		}
		return
	}
	if len(c.order) >= c.max {
		// 淘汰 head
		old := c.order[0]
		delete(c.values, old)
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
		for i, k := range c.order {
			c.values[k].idx = i
		}
	}
	c.order = append(c.order, key)
	c.values[key] = &solveCacheVal{
		Layout:   layout,
		Discards: discards,
		idx:      len(c.order) - 1,
	}
}

func (c *SolveCache) Stats() (size, max int, hits, misses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.order), c.max, c.hits.Load(), c.misses.Load()
}

func (c *SolveCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order = c.order[:0]
	c.values = make(map[string]*solveCacheVal, c.max)
	c.hits.Store(0)
	c.misses.Store(0)
}

// BuildSolveKey — canonical solve cache key. 手写 JSON 保证 deterministic ordering.
// 2026-05-22: 加 "jk" (jokerCount) 区分 0/2/4 鬼局.
// 2026-05-23: 加 "tk" (topK) 区分 AI 难度 1/2/3. 注: topK >= 2 的 sample 是 stochastic, cache 命中会重放同 sample 结果, 通常不期望 cache 这种情况, 但 key 区分以防混误.
func BuildSolveKey(top, mid, bot []string, used []string, round int, dealt []string,
	discardCount int, mode string, r1Mult float32, jokerCount int, topK int) string {
	var b strings.Builder
	b.Grow(256)
	b.WriteString(`{"t":`)
	writeStrArrSorted(&b, top)
	b.WriteString(`,"m":`)
	writeStrArrSorted(&b, mid)
	b.WriteString(`,"b":`)
	writeStrArrSorted(&b, bot)
	b.WriteString(`,"u":`)
	writeStrArrSorted(&b, used)
	b.WriteString(`,"r":`)
	b.WriteString(strconv.Itoa(round))
	b.WriteString(`,"d":`)
	writeStrArrSorted(&b, dealt)
	b.WriteString(`,"dc":`)
	b.WriteString(strconv.Itoa(discardCount))
	b.WriteString(`,"mo":"`)
	b.WriteString(mode)
	b.WriteString(`","rc":{"r1Mult":`)
	b.WriteString(formatFloat(r1Mult))
	b.WriteString(`},"jk":`)
	b.WriteString(strconv.Itoa(jokerCount))
	b.WriteString(`,"tk":`)
	b.WriteString(strconv.Itoa(topK))
	b.WriteString(`}`)
	return b.String()
}

func writeStrArrSorted(b *strings.Builder, arr []string) {
	cp := make([]string, len(arr))
	copy(cp, arr)
	sort.Strings(cp)
	b.WriteByte('[')
	for i, s := range cp {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(s)
		b.WriteByte('"')
	}
	b.WriteByte(']')
}

func formatFloat(f float32) string {
	return strconv.FormatFloat(float64(f), 'g', -1, 32)
}
