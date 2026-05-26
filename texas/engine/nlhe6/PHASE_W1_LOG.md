# Phase 6-max W1 — engine state machine

完成于 2026-05-25。

## 改动 + 验证记录

### W1.0 Design doc — README.md
- 决策:开新包 `engine/nlhe6/` 而非扩 `engine/nlhe/`(HU hard-code 太多,扩会破现有 25 tests)
- 复用 nlhe.Card + nlhe.Evaluate7(player-count agnostic)
- Position model: seat-relative-to-button, HU 特例(button = SB)
- Round close rule(关键泛化):"all non-folded non-all-in have HasActed AND BetThisStreet matches LastBetAmount"
- Side pot: 算法直接在 README 描述

### W1.1 types.go + action.go — 7 tests pass
- `Seat` (uint8), `Position` enum (BTN/SB/BB/UTG/MP/CO)
- `PositionFor(seat, button, n)`: HU 用 SB/BB,n≥3 用 6 个 position
- `FirstToActPreflop(button, n)`: HU SB(=button), n≥3 UTG(button+3)
- `FirstToActPostflop(button, n)`: 任 n 均是 button+1 mod n
- `Action` + `GameConfig` + `DefaultConfigN(n)`
- Validation: `go test ./engine/nlhe6/ -run 'TestPosition|TestFirstToAct|TestNextSeat'` → 7/7 PASS

### W1.2 state.go — 10 state tests pass

关键设计:
- Round close: `roundClosed()` 跑遍所有 non-folded non-all-in, 要求 HasActed AND BetThisStreet == LastBetAmount
- Preflop BB option: blind posting 不算 HasActed,BB 需要明确 check/raise 才算"closed"
- Raise reset: 任 raise 后 `resetOthersHasActed(raiser)` 把其他非 folded 非 all-in 的 HasActed 清 false
- AllIn: 上 raise(newBet > LastBetAmount)走 raise 逻辑;under-raise 不触发 reset
- `nextActiveSeat`: 跳 folded + all-in,clockwise 找下一个 actable seat
- `advanceStreetOrShowdown`: river 或全 all-in → Terminal; 否则下一街 + reset BetThisStreet/HasActed/LastBet/LastRaise

Tests covered:
- HU/6-max blind post 正确
- HU SB fold → BB win SB chip
- HU SB call + BB check → advance to flop
- 6-max fold-around: UTG→MP→CO→BTN→SB fold,BB 拿 blinds
- 6-max limped round: 6 calls (UTG-BB) → advance to flop, postflop SB first
- 6-max UTG raise + 5 folds → UTG wins
- 3-handed preflop raise/call/call → flop
- UTG all-in + 5 folds → UTG wins
- UTG all-in + 4 folds + BB call → showdown (NeedsBoard=5)

Validation: `go test ./engine/nlhe6/` → 17/17 PASS (cumulative)

### W1.3 sidepot.go — 7 sidepot+payoff tests pass

`ComputeSidePots(wagered, folded)` 算法:
- 不同非-folded wagered amount 定义 pot tier level
- 每 level pot = Σ min(wagered_i, level) - prev_level
- Eligible at level = 非 folded with wagered >= level

`State.Payoff(seat)`:
- FoldWin: sole survivor takes Σ_others wagered, others lose own wagered
- Showdown: per-SubPot, best handRank wins amount (split if tie)
- 注意: 当 split pot 时 chip remainder (Amount % len(winners)) 丢弃,zero-sum 允差 ≤ NumPlayers-1

Tests covered:
- 2-way equal: 1 pot 100 amount, eligible both
- Folded contributes: 20+50+50 = 120 单 pot
- Layered all-in: A 30 (all-in), B/C 100 → main 90 + side 140
- 3-level: 20/50/100/100 → 3 sub-pots 80+90+100 = 270 (zero-loss)
- FoldWin payoff: SB fold → SB -1, BB +1
- HU showdown AA vs 22 dry board → AA +2, 22 -2, zero-sum
- 3-way side pot showdown: A AA all-in 20, B 22 all-in 50, C KK 50
  - Main pot 60 → AA wins → A +40
  - Side pot 60 → KK wins → C +10
  - B -50, total 0

Validation: `go test ./engine/nlhe6/ -run 'SidePot|Payoff'` → 7/7 PASS

### W1.4 snapshot.go — O(1) Snapshot/Restore

Mirrors `engine/nlhe`. 字段:Stacks/Wagered/BetThisStreet/HasActed/Folded/AllIn/NumBoard/Street/Button/Cur/HistLen/LastBetAmount/LastRaiseSize/Terminal/FoldWinner. Hole 不 snapshot (immutable). Board past NumBoard 不 read.

Validation: 包括在 W1.5 heavy stress 内 (每 17 steps 做一次 snap→apply→restore round-trip check).

### W1.5 heavy stress — 25,000 random games

`TestHeavyStress_RandomGames`:
- For n in {2,3,4,5,6}: 5000 random games
- Random button + shuffled deck + random legal action sampling
- Each game ≤ 200 steps safety bound (实际典型 5-30 步)
- Per-step invariants:
  - No negative stacks
  - LegalActions 非空 (除非 terminal)
- Per-game invariants:
  - 终态有效 (Terminal=true)
  - Zero-sum: |Σ Payoff(seat)| ≤ NumPlayers-1 (chip remainder allowance)
- Snap/restore round-trip: every 17 steps, snap → apply → restore, check Cur/Pot/Stacks match

**结果 (n=2 到 n=6 每 5k games)**:
| n | foldWin | showdown | multiway show | allInGames | zeroSumFails |
|---|---|---|---|---|---|
| 2 | 3072 | 1928 | 0 | 1909 | 0 |
| 3 | 1683 | 3317 | 1193 | 3294 | 0 |
| 4 | 879 | 4121 | 2469 | 4103 | 0 |
| 5 | 402 | 4598 | 3510 | 4594 | 0 |
| 6 | 229 | 4771 | 4141 | 4768 | 0 |

总 25,000 games / 25,000 snap/restore / 0 invariant 违反 / 0 zero-sum fail. 0.25s 跑完.

修过一次 bug: 测试循环中"Terminal 跳出"会漏掉 showdown 需要的 board 补全; 改成 board-deal 先 Terminal-check 后 (跟 engine/nlhe 的 cfr.go chanceFill 同 pattern).

## 全测试套

```
go test ./...
ok   github.com/boluo/texas/cfr             19.385s
ok   github.com/boluo/texas/engine/leduc     0.098s
ok   github.com/boluo/texas/engine/nlhe      5.656s
ok   github.com/boluo/texas/engine/nlhe/abstraction  91.877s
ok   github.com/boluo/texas/engine/nlhe6     0.562s ✓
```

25 tests in `engine/nlhe6/`,全 pass,无回归.

## W2 起点

剩下:
- MCCFR walk: 2 → 6 traverser. External sampling 现在 traverser 一次, opp 一次 (HU 2 个 walk/iter). 6-max 应是 6 traverser/iter (每个 seat 一次).
- MultiStreetBuckets.ID layout 升级: 1-bit position → 3-bit
- h2h-self / lbr / AIVAT 改 multi-way (sample N seats per pair)
- abstraction package 直接复用 (card abstraction 跟玩家数无关)
- Feature encoder: 已经 6-max-friendly schema, 直接复用 (slot 0 填的方式从 HU 改为 6-max)
