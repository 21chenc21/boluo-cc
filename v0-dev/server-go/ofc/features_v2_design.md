# Features V2 Design — Pineapple OFC MLP

## ⚠️ 特征改动规约 (2026-05-19)

跟 V3 规约同 — 任何 feature 增/删/改必须配多状态单元测试 + 同步本文档. 详见 `features_v3_design.md` 顶部章节. V2 测试在 `features_v2_test.go` (27 个 test, 覆盖 A/B/C/D/E/F/G/H/I/K/L 11 个 group 各关键场景).

## 目标

替代现 90-d features (诊断出 case 2 / 14 等 fundamental MLP 错位的根源是缺关键 strategic signal)。

- **维度: 128** (2^7, 紧凑无 padding)
- **覆盖所有 strategic 决策信号**, 不依赖 MLP 从原始 board 推导
- **显式编码 joker wild 语义** + **deck-aware**

## 总维度: 128

| Group | 维度 | idx 范围 | 功能 |
|---|---|---|---|
| A | 8 | 0-7 | Board state (count, slots, round) |
| B | 24 | 8-31 | Hand tier per row (one-hot) |
| C | 22 | 32-53 | Top fantasy progress (核心修复) |
| D | 8 | 54-61 | Joker 全局状态 (count + eff_rank) |
| E | 12 | 62-73 | Suit 分布 per row |
| F | 12 | 74-85 | Straight draw 检测 |
| G | 17 | 86-102 | Deck awareness (rank + suit 剩余) |
| H | 5 | 103-107 | Foul 风险 |
| I | 7 | 108-114 | Pair preservation (mid/bot) |
| K | 13 | 115-127 | Joker 完成 hand-type 显式 |

(原 J 删除 — 5 个 feature 全部跟 E/F/C 重复)

---

## Group A: Board state (8, idx 0-7)

| idx | feature | 编码 |
|---|---|---|
| 0 | top_count | / 3 |
| 1 | mid_count | / 5 |
| 2 | bot_count | / 5 |
| 3 | top_slots_remain | / 3 |
| 4 | mid_slots_remain | / 5 |
| 5 | bot_slots_remain | / 5 |
| 6 | round_normalized | round / 5 |
| 7 | is_complete | binary |

---

## Group B: Hand tier per row (24, idx 8-31)

**Top** (6, idx 8-13): `HighCard / Pair<Q (2-J) / Pair_Q / Pair_K / Pair_A / Trips`

**Mid** (9, idx 14-22): `HighCard / Pair / TwoPair / Trips / Straight / Flush / FullHouse / Quads / SF`

**Bot** (9, idx 23-31): 同 Mid

**实现**: 用 `Evaluate3JokerCap` / `Evaluate5JokerCap` (含 joker cap-chain), 拿当前 best tier (joker 当 max-non-foul)。

---

## Group C: Top fantasy 进度 (22, idx 32-53) — 核心修复

**关键概念**: "**fantasy floor (底线)**" + "**upgrade potential (升级潜力)**"。

joker 是 wild — X+Q 顶 floor = QQ (joker 至少 play 成 Q), 摸到 K 升 KK, 摸到 A 升 AA。

| idx | feature | 编码 |
|---|---|---|
| 32-44 | top_pair_rank_onehot[13] (2→A) | one-hot, 顶 real pair 的 rank |
| 45 | top_has_real_pair | binary |
| 46 | top_has_wild_pair | binary (joker + ≥1 rank → 可配 pair) |
| 47 | top_has_real_trips | binary |
| 48-52 | top_fantasy_floor_tier_onehot[5] | one-hot: [none, QQ, KK, AA, trips] |
| 53 | top_can_upgrade_to_AA | binary (顶有 joker + 余 slot + deck 仍有 A) |

**Case 2 验证**:
- X+Qc 顶: floor=[0,1,0,0,0] (QQ), upgrade=1 → MLP 高 EV ✓
- X 单顶: floor=[1,0,0,0,0] (none), upgrade=1 → 低 EV

---

## Group D: Joker 全局状态 (8, idx 54-61)

**Deck 支持 0/2/4 jokers**。

| idx | feature | 编码 |
|---|---|---|
| 54 | jokers_on_top | / 4 |
| 55 | jokers_on_mid | / 4 |
| 56 | jokers_on_bot | / 4 |
| 57 | jokers_in_state_total | / 4 |
| 58 | jokers_in_deck | / 4 |
| 59 | joker_eff_rank_top | / 12 (无 joker → 0) |
| 60 | joker_eff_rank_mid | / 12 |
| 61 | joker_eff_rank_bot | / 12 |

**joker_eff_rank**: joker 在非-foul 前提下能 play 的最高 rank (复用 `Evaluate3/5JokerCap` 的 cap-chain)。

---

## Group E: Suit 分布 per row (12, idx 62-73)

| idx | feature | 编码 |
|---|---|---|
| 62-65 | top_suit_counts[♠♥♦♣] | / 3 |
| 66-69 | mid_suit_counts[♠♥♦♣] | / 5 |
| 70-73 | bot_suit_counts[♠♥♦♣] | / 5 |

---

## Group F: Straight draw (12, idx 74-85)

| idx | feature | 编码 |
|---|---|---|
| 74 | top_consecutive_max | / 3 |
| 75 | mid_consecutive_max | / 5 |
| 76 | bot_consecutive_max | / 5 |
| 77 | mid_has_4card_OE | binary |
| 78 | bot_has_4card_OE | binary |
| 79 | mid_straight_outs | / 8 (deck 中剩多少 outs) |
| 80 | bot_straight_outs | / 8 |
| 81 | top_high_count (≥T) | / 3 |
| 82 | mid_high_count | / 5 |
| 83 | bot_high_count | / 5 |
| 84 | mid_has_3consec_high | binary |
| 85 | bot_has_3consec_high | binary |

---

## Group G: Deck awareness (17, idx 86-102)

| idx | feature | 编码 |
|---|---|---|
| 86-98 | rank_remaining[13] (2→A) | / 4 |
| 99-102 | suit_remaining[4] | / 13 |

---

## Group H: Foul 风险 (5, idx 103-107)

| idx | feature | 编码 |
|---|---|---|
| 103 | foul_currently_inevitable | binary |
| 104 | top_strength_normalized | tier-based 0-1 |
| 105 | mid_strength_normalized | 0-1 |
| 106 | bot_strength_normalized | 0-1 |
| 107 | min_margin | (-1 ~ 1, 负 = foul-risk) |

---

## Group I: Pair preservation mid/bot (7, idx 108-114)

(top max_pair_rank 删 — 跟 Group C top_pair_rank_onehot 重复)
(pairs_clustered_correctly 删 — 启发式 leak)
(top_has_KQ_or_QJ 删 — niche)

| idx | feature | 编码 |
|---|---|---|
| 108 | mid_max_pair_rank | / 12 |
| 109 | bot_max_pair_rank | / 12 |
| 110 | mid_has_real_pair | binary |
| 111 | bot_has_real_pair | binary |
| 112 | mid_has_real_trips | binary |
| 113 | bot_has_real_trips | binary |
| 114 | bot_has_flush_potential | binary (bot ≥3 同色) |

---

## Group K: Joker 完成 hand-type (13, idx 115-127)

**判定**: row 内 (非-joker 同 rank/suit/consecutive 数 + joker 数) ≥ 阈值。

**Top** (1, idx 115):
| idx | feature | 触发 |
|---|---|---|
| 115 | top_has_wild_trips | 顶 (max_rank_count + joker_count) ≥ 3 |

**Mid** (6, idx 116-121):
| idx | feature | 触发 |
|---|---|---|
| 116 | joker_completes_pair_mid | (max_rank_count + jokers) ≥ 2 |
| 117 | joker_completes_trips_mid (三条) | ≥ 3 |
| 118 | joker_completes_quad_mid (金刚) | ≥ 4 |
| 119 | joker_completes_straight_mid | (consec_run + jokers) ≥ 5 |
| 120 | joker_completes_flush_mid | (max_suit_count + jokers) ≥ 5 |
| 121 | joker_completes_fullhouse_mid (葫芦) | ∃ r1≠r2: cnt[r1]+j1≥3 ∧ cnt[r2]+j2≥2, j1+j2≤total |

**Bot** (6, idx 122-127): 同 Mid

---

## 实现 API

```go
// BuildFeaturesV2 — 128-d. 替代 90-d BuildFeatures.
func BuildFeaturesV2(gs *GameState) []float32 {
    f := make([]float32, 128)
    fillBoardState(f[0:8], gs)
    fillHandTiers(f[8:32], gs)
    fillTopFantasy(f[32:54], gs)
    fillJokerState(f[54:62], gs)
    fillSuitDist(f[62:74], gs)
    fillStraightDraw(f[74:86], gs)
    fillDeckAware(f[86:103], gs)
    fillFoulRisk(f[103:108], gs)
    fillPairSignals(f[108:115], gs)
    fillJokerCompletes(f[115:128], gs)
    return f
}
```

**复用现有 helpers**:
- `Evaluate3JokerCap` / `Evaluate5JokerCap` / `ScoreHand` — hand tier
- `gs.GetRemainingDeck()` — deck-aware
- `IsFoul` — foul 检测

**新写 helpers** (~150 行):
- `consecutiveRunMax(cards) int`: 最长连续 rank 子串
- `straightOuts(cards, deck) int`: 完成 straight 在 deck 中剩 outs
- `jokerEffectiveRank(rowCards, otherRows) int`: cap-chain 反推

---

## 验证策略

写完 `BuildFeaturesV2` 后, 对几个 case 跑 trace, 人工 verify:

**Case 2** (X Qc 2d 5h 8s):
- candidate `头[X Qc]`: idx48-52 `floor_tier=[0,1,0,0,0]` (QQ), idx53 `upgrade_to_AA=1`
- candidate `头[X]`: idx48-52 `floor_tier=[1,0,0,0,0]` (none), idx46 `top_has_wild_pair=0`

**MLP 学**: floor_tier_QQ=1 → 大 value。MLP 自然倾向 candidate A。

---

## 实施路径

| Step | 工作 | 时长 |
|---|---|---|
| 1 | ✅ Review + 敲定 (现) | done |
| 2 | 写 `features_v2.go` (BuildFeaturesV2) | 0.5-1 天 |
| 3 | Unit tests + trace 验证 case 2/14/51 | 0.5 天 |
| 4 | 改 `train.go` / `alphazero-train` 适配 inDim=128 | 0.5 天 |
| 5 | First training iter (smoke) | 1 天 |
| 6 | Full AlphaZero loop (20 iter) | 2 天后台 |
| 7 | Bench + 调参 | 1 天 |
| **总** | | **~5-6 天** |
