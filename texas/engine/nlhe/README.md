# engine/nlhe — Heads-Up No-Limit Hold'em engine

## Scope (HUNL, NOT 6-max)

- 2 player (SB on button = P0, BB = P1)
- 4 street: preflop, flop, turn, river
- Standard 52-card deck (no jokers, unlike Pineapple OFC)
- Stack: 100 BB (200 chips assuming BB=2 / SB=1) — configurable
- Showdown via 5-from-7 hand evaluation

## 跟 Leduc 引擎的差异

| | Leduc | HUNL |
|---|---|---|
| Deck | 6 cards (2 suits × 3 ranks) | 52 cards (4 suits × 13 ranks) |
| Private cards | 1 / player | 2 / player |
| Public cards | 1 (round 2) | 5 (3 flop + 1 turn + 1 river) |
| Streets | 2 | 4 |
| Bet structure | Limit (size固定 R1=2 R2=4) | **No-Limit (任意 chip 至 all-in)** |
| Max raises/round | 2 | unbounded (until all-in) |
| Showdown rule | pair-of-board > high private | 5-from-7 高/对/two-pair/三条/.../royal flush |
| Infoset count | 288 | ~10^14 (lossless), ~10^7 with abstraction |

## 关键设计决策 (本 README 锁定)

### A. Bet sizing — 离散化 (与 6-max Pluribus 同思路)

无限的 chip 数下注**不能枚举**。本 engine 强制下注集合从 `BetSizes []float64` 选择,
以 pot 比例为单位。默认起步:

```go
DefaultBetSizes = []float64{0.5, 1.0, 2.0}  // 0.5pot, 1pot, 2pot
```

加上 Fold / Check / Call / AllIn → **6 个 action** 离散.
Off-tree mapping (对手下非整数倍 pot 时映射到最近): runtime 处理, 不入 engine.

### B. Action 编码

```go
type Action struct {
    Kind ActionKind  // Fold, CheckCall, Bet, AllIn
    Size uint8       // 仅 Bet/Raise 有意义, 索引 BetSizes; 0 means undefined
}

const (
    ActionFold     ActionKind = 0
    ActionCheckCall            = 1  // check 或 call, 跟 Leduc 同
    ActionBet                  = 2  // 加 Size 区分 0.5pot / 1pot / 2pot
    ActionAllIn                = 3
)
```

NN policy head 输出维度 = `2 + len(BetSizes) + 1` (Fold + CheckCall + 各 Bet + AllIn).

### C. Infoset 编码 — 初版 lossless, 后期 lookup table

initial: lossless string/uint128 包 (hole 2 card + board 0-5 cards + position + 全 history).
abstraction 阶段: replace card portion with bucket_id (E[HS²] 或 OCHS, 200-500 bucket/street).

### D. Hand eval — cgo bind OMPEval (后期), 现阶段 pure Go

POC 用 pure Go (慢但对). 性能瓶颈出现后 swap cgo OMPEval (~100x).
Engine 接口隐藏 eval 实现, 可换不破坏 API.

### E. Engine + abstraction 同期

- card eval lossless (engine 自己做)
- abstraction (E[HS²]/OCHS) 作 separate package, engine 通过 `AbstractFn` 调
- `state.HoleBucket(0)` / `state.BoardBucket()` 在 abstraction 模式下返 bucket_id, lossless 模式返 card-derived hash

## 文件组织 (本 session 完成)

```
engine/nlhe/
├── README.md          (此文件 — 设计冻结)
├── card.go            52 card 编码
├── eval.go            pure Go 7-card hand eval (5-from-7)
├── action.go          Action 编码 + BetSizes 配置
├── state.go           HUNL state machine: street transitions, betting tree
└── *_test.go          unit tests
```

## 跟现有 engine/leduc 关系

- 完全独立目录, **不共享**包. Leduc 是 toy, NLHE 是生产.
- 但**复用模式**: Snapshot/Restore, InfosetID uint64 packing, 同一 Strategy map[uint64][]float64 接口.
- CFR / MCCFR / BR 三个 cfr/ 包的 solver 后期会泛型化, 接口接 NLHE engine. Phase B+ 工作.

## 下一步 (本 session 之后)

- [ ] Action history encoding for infoset (variable length, hash to uint64)
- [ ] Abstraction package (engine/nlhe_abs?) — bucket assignment for cards
- [ ] CFR solver 对接 NLHE (大概率换接口为 `Game` interface, 抽象 Leduc/NLHE)
- [ ] cgo OMPEval (性能优化)
- [ ] HUNL push/fold smoke test (验证 engine + CFR 跑通)
