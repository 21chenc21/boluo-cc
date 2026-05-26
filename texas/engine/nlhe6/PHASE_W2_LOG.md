# Phase 6-max W2 — MCCFR + abstract ID

完成于 2026-05-25.

## 改动 + 验证记录

### W2.1 infoset.go — lossless 64-bit InfosetID
- Hero hole (canonicalized sort) + public board + (Cur, Button, NumPlayers) + per-street Hist
- FNV-1a 64-bit. 10^10+ infosets 收敛碰撞率 ~negligible.

### W2.2 cfr.go — N-traverser external sampling MCCFR
- Per `Iter()`: NumPlayers traversers each take 1 walk
- `runTraverser(seat)`: random button + dealHoles(2N cards) + walk
- `walk(s, trav)`:
  - Chance node (NeedsBoard): chanceFill → recurse → restore
  - Terminal: Payoff(trav)
  - Cur == trav: expand all legal actions, compute regret + linear-avg strategy
  - Cur != trav: sample from σ, recurse
- RM+ targeted flooring (Phase 2d optimization): only floor walkVisited (not full map sweep)
- 复用 nlhe `regretMatching` pattern (inlined here as same logic)

### W2.3 cfr_test.go — 3 MCCFR smoke tests
- `TestMCCFRRunsNoPanic`: 200 iter 3-player, normalized AvgStrategy ✓
- `TestMCCFRMultiStreetVisitsAllStreets`: 500 iter 3-player 20BB,见 preflop=14655 / flop=7771 / turn=4148 / river=2272 (金字塔分布合理)
- `TestMCCFRStrongHandPrefersAggression`: 3-handed 10BB, AA UTG **aggression 0.403 after 10k iter** (> 0.3 threshold ✓)

### W2.4 multistreet_id.go — `MultiStreetID(b, s)` + `MultiStreetIDFn(b)`

Layout (uint64):
- bits 0-1: street
- bits 2-4: actor seat (3b, 0-5)
- bits 5-12: preflop bucket (8b)
- bits 13-20: flop bucket
- bits 21-28: turn bucket
- bits 29-36: river bucket
- bits 37-63: history hash (27b, FNV-32 trim)

Diff vs HU `abstraction.MultiStreetBuckets.ID`:
- Position 1b → 3b
- Hist hash 29b → 27b (碰撞率仍 ~1/2^27 per cell, OK)
- Pre/flop/turn/river bucket 各 8b 不变 (K ≤ 256)

避免循环依赖: ID 实现放 nlhe6 包内 (nlhe6 import abstraction 看 buckets fields; abstraction 不知 nlhe6).

### W2.5 cfr_abstract_test.go — 6-max abstract MCCFR smoke
- 加载 shared K=20 preflop + K=50 flop/turn/river buckets (HUNL 训过的)
- `TestMCCFRAbstractSmoke6Max`: 6-max DefaultConfigN(6), 20BB, bet=[1.0], 1000 iter → **91,721 abstract infosets / 0.89s**, AvgStrategy normalized ✓
- `TestMultiStreetIDDifferent6Max`: UTG view vs MP view after UTG fold → 不同 ID ✓

## 性能

Per iter cost (6-max 20BB):
- 1000 iter / 0.89s = ~890 µs/iter
- vs HUNL nlhe (~70 µs/iter for 100k iter): 12.7x slower per iter
- Make sense: 6 traverser × ~2x bigger tree per walk

Extrapolated:
- 10k iter ≈ 9s
- 100k iter ≈ 90s
- 1M iter ≈ 15 min
- 10M iter ≈ 2.5 hours (overnight scale, Pluribus-class smoke)

## 全测试套

```
go test ./...
ok   github.com/boluo/texas/cfr                     21.074s
ok   github.com/boluo/texas/engine/leduc             0.105s
ok   github.com/boluo/texas/engine/nlhe              7.830s
ok   github.com/boluo/texas/engine/nlhe/abstraction 86.745s
ok   github.com/boluo/texas/engine/nlhe6             5.726s ← W1 + W2 全过
```

nlhe6 共 30 tests (7 types + 10 state + 7 sidepot/payoff + 3 MCCFR smoke + 2 abstract MCCFR + 1 heavy stress). 0 violation.

## W3 起点

- h2h-self / lbr / AIVAT: 6-max 多 player 扩展 (N seats vs σ_self baseline)
- dump-multistreet-data: nlhe6 版本 (sample N-player games)
- 134-d → 288-d feature encoder: 直接复用 (slot 0 填的方式从 HU 改为 6-max,看 `// HU:` 注释)
- 注: feature encoder 当前在 `engine/nlhe` 包,需要 port 一份 / 加 nlhe6 版本 / 改成函数接受 interface

实战 6-max 训练 + 蒸馏 + 4-metric POC. 跟 HUNL POC 流程同, encoder + abstraction 直接复用.
