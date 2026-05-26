# Features V3 Design — Pineapple OFC MLP

## 设计目标

v3 ckpt (V2 features, 134-d → 132 取前 132) 多种训法都卡 **47/63 纯 MLP**:
- AZ self-play 50 iter 累计 2.4M samples → 47/63
- LR / sims / data 量调整都不动

**不是训练问题, 是表征上限**.

V3 设计原则:
1. **删 V2 中"派生" + "硬规则" features** — NN 不该靠这些
2. **加概率级 features** — `P(foul)`, `P(bot ≥ flush)`, `E[royalty]` 等, NN 直接看, 不用自己合成
3. **加非线性 EV signals** — AA vs QQ 量级差等
4. **加 OFC 核心机制 signals** — refan, last-round forcing 等

V2 → V3 总览:
- V2 **删** 65 dim (C/F/H/I/K/L 全删, NN 用更通用的概率代替)
- V2 **保留** 69 dim (A/B/D/E/G)
- V3 **加** ~59 dim (新概率 + non-linear EV + refan + 末轮)
- 总 **128 dim** (略低于 V2 134)

---

## ⚠️ 特征改动规约 (2026-05-19 用户要求)

**任何 feature 增/删/改 都必须满足**:

1. **每个新/改 feature 至少 2 个 unit test**: 一个"信号开"模拟 (摆特定牌验证 feature=期望值), 一个"信号关"模拟 (相反场景 feature=0/默认). 复杂 feature (如 LR 4-straight open vs gutshot) 要 cover 每个 branch.
2. **测试模拟真实牌局**, 不要只测合成 state. 用 `makeStateV3(t, top, mid, bot)` + 中文注释期望值原因.
3. **新增 group → 在本设计文档表格里加一行 + 单独一节描述每个 idx 含义** (跟 A/B/D/E/G/X/... 各组现有节风格一致).
4. **改动 `FeatureDimV3` 常量 → bump `DATA_VERSION` (mac-scripts/train_v3_iter.sh)**, 触发文件夹后缀变, 旧数据自动隔离.
5. **跑 `go test ./ofc/ -run TestV3` 全过才能 commit**.

违反这条规约的 feature 改动会让训练静默学错 (例: V2 大量 features 没单元测试, 14 dim signal 一直没人验证过实际行为), 浪费几小时到几十小时训练时间。

---

## 总维度: 147 (2026-05-19 v2: 131 → 147 加 Tier 1+2+3)

| Group | dim | idx 范围 | 中文 | V2 状态 |
|---|:-:|---|---|---|
| A | 8 | 0-7 | 棋盘状态 | V2 保留 |
| B | 24 | 8-31 | 各行手牌等级 | V2 保留 |
| D | 8 | 32-39 | 鬼牌全局状态 | V2 保留 |
| E | 12 | 40-51 | 各行花色分布 | V2 保留 |
| G | 17 | 52-68 | 牌堆剩余感知 | V2 保留 |
| X | 21 | 69-89 | 各行成牌概率 | V3 |
| F | 4 | 90-93 | Fantasy 粒度概率 (QQ/KK/AA/trips) | V3 |
| Y | 3 | 94-96 | 各行期望分 | V3 |
| Z | 5 | 97-101 | 高层 summary | V3 |
| U | 5 | 102-106 | 各行对子 rank | V3 |
| V | 5 | 107-111 | 对升 trips 条件概率 | V3 |
| T | 4 | 112-115 | 顶配 fantasy 锁信号 | V3 |
| C | 3 | 116-118 | 各行最大可达手型 | V3 |
| R5 | 2 | 119-120 | 末轮强制信号 | V3 |
| Q | 4 | 121-124 | 路径承诺 (沉没成本) | V3 |
| M | 3 | 125-127 | 各行 foul margin | V3 |
| S | 1 | 128 | 槽位平衡 | V3 |
| **N** | 2 | 129-130 | **弃牌主信号 (rank + premium-flag)** | **V3 (2026-05-19 真实现)** |
| **L** | 6 | 131-136 | **跨行 anti-pattern (V2 L 复用)** | **2026-05-19 Tier 1 新** |
| **LR** | 8 | 137-144 | **locked-royalty + 4-draw + pair-kicker** | **2026-05-19 Tier 2 新** |
| **N2** | 2 | 145-146 | **弃牌副信号 (拆 bot suit / connector)** | **2026-05-19 Tier 3 新** |

总: 8+24+8+12+17+21+4+3+5+5+5+4+3+2+4+3+1+2+6+8+2 = **147**

**Calibration 注**: 训练 label 用 `fan_bonus = [QQ:20, KK:40, AA:80, trips:90], foul_cost = 6`. F 组按这些权重做 P-weighted bonus expectation. Z0 (E[final_score]) 计算:
```
Z0 = Y0+Y1+Y2 + (F0×20 + F1×40 + F2×80 + F3×90) - 6 × X20
```

**注**: W 组 (refan 信号, 4 dim) **已删** — MLP 只处理 R1-R5 摆牌, 不进 fantasy 内部决策. Fantasy 进得了的差别 (QQ vs KK vs AA vs trips) **已经融进 fan_bonus 数值校准** (AA bonus 80 vs QQ 20 = 4× 量级差, 数据驱动). NN 学 label 时内化 fantasy 终值差异.

---

## Group A: 棋盘状态 (8 dim, idx 0-7) — V2 保留

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 0 | top_count | top 张数 / 3 | 顶道当前几张 (0/1/2/3) |
| 1 | mid_count | mid 张数 / 5 | 中道当前几张 |
| 2 | bot_count | bot 张数 / 5 | 底道当前几张 |
| 3 | top_slots_remain | top 余槽 / 3 | 顶道还能放几张 |
| 4 | mid_slots_remain | mid 余槽 / 5 | 中道还能放几张 |
| 5 | bot_slots_remain | bot 余槽 / 5 | 底道还能放几张 |
| 6 | round_normalized | round / 5 | 当前第几轮 (R1=0.2, R5=1.0) |
| 7 | is_complete | binary | 棋盘是否摆完 |

---

## Group B: 各行手牌等级 (24 dim, idx 8-31) — V2 保留

**Top** (6 dim, idx 8-13): one-hot `HighCard / Pair<Q (2-J) / Pair_Q / Pair_K / Pair_A / Trips`

**Mid** (9 dim, idx 14-22): one-hot `HighCard / Pair / TwoPair / Trips / Straight / Flush / FullHouse / Quads / SF`

**Bot** (9 dim, idx 23-31): 同 Mid

用 `Evaluate3JokerCap` / `Evaluate5JokerCap` (含 joker cap-chain). 中文: **每行当前最佳手型** (joker 当 wild).

---

## Group D: 鬼牌全局状态 (8 dim, idx 32-39) — V2 保留

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 32 | joker_total_count | 总 joker 数 / 4 | 全场 joker 总数 |
| 33 | joker_on_top | top joker 数 / 2 | 顶道有几张 joker |
| 34 | joker_on_mid | mid joker 数 / 2 | 中道有几张 |
| 35 | joker_on_bot | bot joker 数 / 2 | 底道有几张 |
| 36 | joker_in_deck | deck 剩 joker 数 / 4 | 牌堆还剩几张 joker |
| 37 | joker_eff_top | top joker 等效 rank / 12 | 顶 joker 当 wild 后实际 rank |
| 38 | joker_eff_mid | 同 | 中 同 |
| 39 | joker_eff_bot | 同 | 底 同 |

---

## Group E: 各行花色分布 (12 dim, idx 40-51) — V2 保留

每行每花色的张数 (3 行 × 4 花色 = 12). 例如:
- idx 40-43: top 各花色张数 / 3
- idx 44-47: mid 各花色张数 / 5
- idx 48-51: bot 各花色张数 / 5

中文: **每行 ♠♥♦♣ 各几张**.

---

## Group G: 牌堆剩余感知 (17 dim, idx 52-68) — V2 保留

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 52-64 | rank_remaining[13] | deck 剩该 rank 张数 / 4 | 每 rank 还剩几张 (2-A) |
| 65-68 | suit_remaining[4] | deck 剩该花色张数 / 13 | 每花色还剩几张 |

NN 看这 17 dim 知道 "deck 还有什么", 跟其他 features 联合算概率.

---

## Group X: 各行成牌概率 (22 dim, idx 69-90) — V3 核心新

**核心思想**: 用 hypergeometric 闭式 + 部分 MC 估算, 直接告诉 NN "这行最后会成什么牌的概率分布".

### Top (3 dim, idx 69-71)

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 69 | X0_p_top_pair_QKA | P(top final = pair Q+) | 顶最终凑成 QQ+ 对子的概率 (fantasy 门槛) |
| 70 | X1_p_top_trips | P(top final = trips) | 顶最终凑成 trips 的概率 (refan + 高 royalty) |
| 71 | X2_p_top_no_foul_vs_mid | P(top ≤ mid 最终) | 顶不超中道的概率 |

### Mid (8 dim, idx 72-79)

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 72 | X3_p_mid_pair | P(mid final = pair) | 中最终至少 pair |
| 73 | X4_p_mid_2pair | P(mid final = 2pair) | 中两对 |
| 74 | X5_p_mid_trips | P(mid final = trips) | 中三条 |
| 75 | X6_p_mid_straight | P(mid final = straight) | 中顺子 |
| 76 | X7_p_mid_flush | P(mid final = flush) | 中同花 |
| 77 | X8_p_mid_FH_plus | P(mid final ≥ FH) | 中葫芦及更高 |
| 78 | X9_p_mid_at_least_pair | P(mid ≥ pair) (累加) | 中至少对子 |
| 79 | X10_p_mid_GT_bot | P(mid > bot 最终) | **中超底的概率 (foul 主路径)** |

### Bot (9 dim, idx 80-88)

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 80 | X11_p_bot_pair | P(bot final = pair) | 底对子 |
| 81 | X12_p_bot_2pair | P(bot final = 2pair) | 底两对 |
| 82 | X13_p_bot_trips | P(bot final = trips) | 底三条 |
| 83 | X14_p_bot_straight | P(bot final = straight) | 底顺子 |
| 84 | X15_p_bot_flush | P(bot final = flush) | 底同花 |
| 85 | X16_p_bot_FH_plus | P(bot final ≥ FH) | 底葫芦+ |
| 86 | X17_p_bot_quads_plus | P(bot final ≥ quads) | 底四条+ (含 SF) |
| 87 | X18_p_bot_GE_pair_T | P(bot ≥ pair T) | 底 ≥ 10 对子 (常用对照) |
| 88 | X19_p_bot_GE_mid | **P(bot ≥ mid 最终)** | **底反超中道概率 (no-foul 概率)** |

### Foul 全局 (1 dim, idx 89)

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 89 | X20_p_foul_final | P(top > mid ∨ mid > bot) 完整 | **最终 foul 的概率** |

(原 X21 P(fantasy final) 删除, 由 F 组粒度替代)

---

## Group F: Fantasy 粒度概率 (4 dim, idx 90-93) — V3 新

**核心**: NN 不光看 P(fantasy), 还要看是哪个 fantasy 路径 (QQ/KK/AA/trips), 因为 bonus 量级差 4× (20 vs 80).

| idx | 名字 | 公式 | 中文 | bonus × |
|---|---|---|---|---|
| 90 | F0_p_top_final_QQ | P(top final = pair Q exact) | 顶最终凑 QQ pair (Q≠K≠A) | × 20 |
| 91 | F1_p_top_final_KK | P(top final = pair K exact) | 顶最终凑 KK pair | × 40 |
| 92 | F2_p_top_final_AA | P(top final = pair A exact) | 顶最终凑 AA pair | × 80 |
| 93 | F3_p_top_final_trips | P(top final = trips, any rank) | 顶最终 trips | × 90 |

公式 (例 F2 AA):
```
F2 = P(top 最终凑 AA exactly)
   ≈ 视 top 当前 A 数 + 槽位 + deck 剩 A 来算 hypergeo
   - top 已有 A pair: F2 = 1 - P(升 trips A) (因为 AA 是 stable 终态)
   - top 1 A: F2 = P(再摸 1 A in remaining slots, 不摸 2)
   - top 0 A: F2 = P(摸 2 A in remaining slots)
```

排除 (exact): 如 top 最终是 AAA trips, 算入 F3 不算 F2.

**为什么 exact**: NN 看 sum(F0..F3) 就是 P(fantasy) 但 Z0 计算 EV 时按 bonus 加权, 必须分开.

### 实现备注

- 闭式: hypergeometric 用于 "deck 剩 N 张, 抽 K 张, 至少含 X 张目标" 类问题
- 联合: "P(顺子) + P(同花) - P(交集)" 用 inclusion-exclusion
- 复合 (FH+): 简化分两步算
- 估算成本: ~50us/state, 比 K-rollout (5ms) 快 100×

---

## Group Y: 各行期望分 (3 dim, idx 94-96) — V3 新

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 94 | Y0_E_royalty_top | Σ royalty(type) × P(type) | 顶道期望 royalty (**不含** fantasy bonus, F 组单独算) |
| 95 | Y1_E_royalty_mid | 同 | 中道期望 royalty |
| 96 | Y2_E_royalty_bot | 同 | 底道期望 royalty |

Royalty 表 (Pineapple OFC):
- Top: pair 6=1, ..., pair Q=8, pair K=9, pair A=10, trips=15+
- Mid: pair 6+=1, ..., trips=2, straight=4, flush=8, FH=12, quads=20, SF=30
- Bot: 同 mid 但 ×2 (大约)

---

## Group Z: 高层 summary (5 dim, idx 97-101) — V3 新

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 97 | Z0_E_final_score | Y0+Y1+Y2 + (F0×20+F1×40+F2×80+F3×90) - 6×X20 | **当前 placement 的总期望分** (calibrated bonus) |
| 98 | Z1_top_strength_norm | top hand-type / 3 | 顶当前强度 (high/pair/trips → 0/0.33/1) |
| 99 | Z2_mid_strength_norm | mid hand-type / 9 | 中当前强度 (0-9 hand-type / 9) |
| 100 | Z3_bot_strength_norm | bot hand-type / 9 | 底当前强度 |
| 101 | Z4_phase | slots_total_remain / 13 | 游戏进度 (R1=1.0, R5=0) |

---

## Group U: 各行对子 rank (5 dim, idx 102-106) — V3 新

V2 I 组有 mid/bot max_pair_rank, 但**缺 top 的**. 补全 + 加细化.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 102 | U0_top_pair_rank | top max pair rank / 12 | 顶当前最大对子 rank (0=无, A=1.0) |
| 103 | U1_mid_pair_rank | mid 同 | 中对子 rank |
| 104 | U2_bot_pair_rank | bot 同 | 底对子 rank |
| 105 | U3_mid_2pair_high_rank | mid 两对中较大对 / 12 | 中两对的高对 rank |
| 106 | U4_bot_2pair_high_rank | bot 同 | 底两对的高对 rank |

---

## Group V: 对升 trips 条件概率 (5 dim, idx 107-111) — V3 新

**已知 row 有 pair, P(再摸到同 rank 升 trips) = ?**

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 107 | V0_p_top_pair_to_trips | P(top pair → trips) | 顶对升三条概率 |
| 108 | V1_p_mid_pair_to_trips | 同 mid | 中对升三条 |
| 109 | V2_p_bot_pair_to_trips | 同 bot | 底对升三条 |
| 110 | V3_p_mid_2pair_to_FH | P(mid 2pair → FH) | 中两对升葫芦 |
| 111 | V4_p_bot_2pair_to_FH | 同 bot | 底两对升葫芦 |

公式 (以 V0 为例):
```
pair_rank = top_pair_rank
deck_remain = G[rank_remaining[pair_rank]] × 4
top_slots = A[top_slots_remain] × 3
P = 1 - C(deck_remain_others, top_slots) / C(deck_total, top_slots)
```

---

## Group T: 顶配 fantasy 锁信号 (4 dim, idx 112-115) — V3 新

**核心**: bonus 表 (QQ=20, KK=40, AA=80, trips=90) 是 4× 跳跃, linear pair_rank 抓不到. 给 binary lock 信号 + 最大 reach.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 112 | T0_top_currently_pair_Q_plus | top 已 pair Q+ → 1 | 顶已锁 fantasy 阈值 |
| 113 | T1_top_currently_AA | top 已 AA pair → 1 | **AA lock** (bonus 80) |
| 114 | T2_top_currently_trips | top 已 trips (任意 rank) → 1 | **任何 trips lock** (触发进范, bonus 90) |
| 115 | T3_top_pair_max_rank_reachable | max pair rank top 可达 / 12 | 顶最高能锁啥 rank pair |

**T2 修正** (vs 之前): 不再特指 top_can_trip_A — 任何 rank trips 都**触发进范** (R1-R5 摆牌模型只看 fantasy 触发, 不管 refan 内部).

---

## Group C: 各行最大可达手型 (3 dim, idx 116-118) — V3 新

X 组给概率, 但 NN 看 P=0.001 跟 P=0 差别学不细. 给 binary 死路/活路上限.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 116 | C0_top_max_achievable | top max hand-type / 3 (高牌/对/三条) | 顶最强可达 |
| 117 | C1_mid_max_achievable | mid max hand-type / 9 | 中最强可达 |
| 118 | C2_bot_max_achievable | bot max hand-type / 9 | 底最强可达 |

例: mid=2♠5♣Jd 杂乱无连号无同色 → C1=pair/9=1/9 (flush/straight 全死).

例: bot=2♦5♦8♦ — 3 ♦ → C2=可达 flush=5/9 (用 G[♦ rem] 算).

复用 V2 已有 `maxAchievableHandType` (hard_rules.go).

---

## Group R5: 末轮强制信号 (2 dim, idx 119-120) — V3 新

R5 = 最后一轮, 必须 place, 没法 pivot. NN 应该知道 forced choice.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 119 | R5_0_is_last_round | round == 5 → 1 | 是末轮 |
| 120 | R5_1_forced_placement_count | 剩 slot / 3 | 末轮还需必填几张 (R5 摆 2/3) |

---

## Group Q: 路径承诺 (沉没成本) (4 dim, idx 121-124) — V3 新

**当前已经"投资"在某条路, switch 损失大**. NN 学 "已经 4 ♥ 了, 第 5 张不同色 = -EV"

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 121 | Q0_bot_commit_flush | bot 主色张数 / 5 | 底道同色承诺 (3=0.6, 4=0.8, 5=1.0) |
| 122 | Q1_bot_commit_straight | bot 连号最长子串 / 5 | 底道顺子承诺 |
| 123 | Q2_mid_commit_pair | mid 当前最大 same-rank count / 5 | 中道对子+承诺 |
| 124 | Q3_top_commit_fantasy | top 已 Q+ 张数 / 3 | 顶道 fantasy 承诺 |

---

## Group M: 各行 foul margin (3 dim, idx 125-127) — V3 新

X20 是全局 P(foul), **每行 margin 也重要**, NN 看 "我离 foul 还有多远".

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 125 | M0_mid_minus_top_margin | (mid type - top type) / 9 | 中比顶多几个 tier (正=安全, 负=foul) |
| 126 | M1_bot_minus_mid_margin | (bot type - mid type) / 9 | 底比中多几个 tier |
| 127 | M2_min_margin | min(上面两个) | 最紧那一对 (foul 风险点) |

---

## Group S: 槽位平衡 (1 dim, idx 128) — V3 新

R1 摆完 5 张, "top 满 + mid 空 + bot 满" 是 anti-pattern. 越均衡 R2-5 灵活度越高.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 128 | S0_slot_balance | 0.3 × min/max (全场) | 槽位平衡度 (sp15 v2 全场 scale 0.3) |

**2026-05-20 sp15 改动 (含 v1→v2 教训)**:
- 原公式 `min/max ∈ [0,1]` 在 R2-R3 误伤 commit 策略 (case 34 R2: 底 4-flush 5h6h7h8h 应选, 但 1+2+4 槽位被 S_slot=0.33 压制, 输给 1+3+3 平衡的 AI 错选).
- **v1 尝试** (round-gated, R<4 → 0): 太激进, 13 个 ckpt bench 全部 -10~-26 点 (acc84: 43→28). NN 学的 ~12.7 权重信号丢光.
- **v2 采用** (scale 0.3 全场): 信号方向保留, 强度减 70%. 同 ckpt bench 回到 +3~+19 (acc84: 43→46 净改进). case 34 仍能 flip (exp1 41.09 #1 vs AI 36.34 #3).
- 教训: 改 feature 不能直接清零, 要保留 signal 给 NN 用; scale 1.0→0.3 比 round-gate→0 安全得多.

---

## Group N: 弃牌主信号 (2 dim, idx 129-130) — V3 (2026-05-19 真实现)

case 42/45/50 该弃高牌不舍, **R2-R5 NN 看不到 "弃这张 vs 弃别张" EV 差**. 2026-05-19 从 placeholder 改真实现, 读 `gs.LastDiscard / gs.HasLastDiscard`. R1 无弃牌或 caller 未设 → 全 0.

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 129 | N0_discard_rank | rank of discarded / 12 (joker → 1.0) | 弃的 rank 大小 |
| 130 | N1_discard_premium | (rank ≥ Q OR joker) → 1 | 是否烧高价值牌 |

**Unit test**: `TestV3_N_NoDiscard / DiscardPremium / DiscardLowRank / DiscardJoker`.

---

## Group L: 跨行 anti-pattern (6 dim, idx 131-136) — 2026-05-19 Tier 1 新

V2 L 组复用 (60/63 数字虚但 anti-pattern 信号本身直观正确). 检测人眼一看就知道"摆错了"的结构。

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 131 | L0_pairs_split | 同 rank 跨行 split 数 / 4 | 拆同对 (Kc 顶 + Kd 中 → 0.25) |
| 132 | L1_flushgroup_split | 同色组跨行 split / 4 | 拆同花 |
| 133 | L2_connectors_split | 连张跨行 split / 6 | 拆顺子连张 (5h 中 + 6h 底 → 0.167) |
| 134 | L3_bot_min_minus_mid_max | (bot 最小 - mid 最大) / 12 | 底放比中小的牌 = kicker 异常 (-1 ~ +1) |
| 135 | L4_gap1_orphan | (rank N,N+2 同行 + N+1 别行) 数 / 4 | 24 同 + 3 别 → 3 被孤立 |
| 136 | L5_mid_minus_bot_fill_ratio | mid/5 - bot/5 (signed) | 中填得比底快 (anomaly) |

**Unit test**: `TestV3_L_*` 6 个, 每个对应一 idx.

---

## Group LR: locked-royalty + 4-draw + pair-kicker (8 dim, idx 137-144) — 2026-05-19 Tier 2 新

V3 概率级 features (X 组) 对"draw 类型" / "锁定 royalty" 表达不够离散, 加 LR 补。

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 137 | LR0_bot_locked_tier | bot 5 张已成 ≥ Straight: (type - Straight + 1) / 6 | 底锁了几级 royalty (Straight→1/6, SF→1) |
| 138 | LR1_mid_locked_tier | mid 5 张已成 ≥ Trips: (type - Trips + 1) / 7 | 中锁了几级 royalty (trips→1/7, SF→1) |
| 139 | LR2_bot_4flush | bot 4 张 1 空槽 + 4 同色 → 1 (joker wild) | 底 4-flush draw (1 张抽花) |
| 140 | LR3_bot_4straight_open | bot 4 连张 span=3 → 1 | 底 4 连 open-ended (8 outs) |
| 141 | LR4_bot_4straight_gutshot | bot 4 张 span=4 (含 A-low) → 1 | 底 4 张 gutshot (4 outs) |
| 142 | LR5_mid_4flush | 同 bot 4flush 但中行 | |
| 143 | LR6_mid_4straight_any | mid 4-straight open ∨ gutshot → 1 | 中 4-straight draw (压缩 1 dim) |
| 144 | LR7_pair_kicker_rank_max | max(mid/bot pair kicker rank) / 12 | 对子 kicker 强弱 (KK+A=1, KK+4=0.167) |

**Unit test**: `TestV3_LR_*` 10 个 (含 NotLocked / PairKickerLow vs Max 等对照).

---

## Group N2: 弃牌副信号 (2 dim, idx 145-146) — 2026-05-19 Tier 3 新

N 组 (rank + premium) 不够细, 加 N2 描述弃牌**结构影响**。

| idx | 名字 | 公式 | 中文含义 |
|---|---|---|---|
| 145 | N2-0_break_bot_suit_commitment | bot 有 ≥3 同色 ∧ 弃牌同色 → 1 | 弃牌拆 bot flush 承诺 (浪费同色) |
| 146 | N2-1_break_connector | 任一行有 rank N±1 (相邻 of 弃牌) → 1 | 弃牌拆 connector (浪费顺子连张) |

joker 弃牌 N2 全 0 (joker 没 suit / 没固定 rank).

**Unit test**: `TestV3_N2_BreakBotSuit / NoBreakBotSuit / BreakConnector / NoBreakConnector`.

---

## V3 vs V2 删的 features

V2 134 dim 中, V3 删掉:

| V2 Group | dim | V3 替代 |
|---|:-:|---|
| C top fantasy progress (22) | -22 | X0-X2, X21, T0-T2 cover |
| F straight draw (12) | -12 | X14 (P(bot straight)), X6 (P(mid straight)), Q1 (straight commitment) |
| H foul risk (5) | -5 | X20 (P(foul final)), M0-M2 (margin) |
| I pair preservation (7) | -7 | U0-U4 (pair rank), V0-V4 (trips P) |
| K joker completes (13) | -13 | X group 各 type 已覆盖 |
| L cross-row splits (6) | -6 | Q (commitment) + 数值 penalty 留在 score |

**删 65 dim, 加 55 dim, 净 -10 dim (134 → 124)**.

---

## 关键案例对照

### Case 35 (R2 mid 55 + 5d 上中 → 555 trips)

V3 features post-action:
- **X20 P(foul final)** = ~0.55 (mid trips, bot 弱)
- **X10 P(mid > bot)** = ~0.55
- **X19 P(bot ≥ mid)** = ~0.45 (反超概率)
- **Z0 E[final score]** = -10 (foul -20 × 0.55 + ...)
- M2 min_margin = -0.33 (mid > bot 当前)

NN 一眼看 Z0 = -10 不要这步, 选弃 5d.

### Case 50 (R5 As 上顶 → top AA > mid KK foul)

V3 features:
- **U0 top_pair_rank** = 1.0 (AA)
- **U1 mid_pair_rank** = 0.92 (KK)
- **T1 top_pair_eq_AA** = 1
- **M0 mid - top tier margin** = -1.0 (top 比 mid 高 1 tier!)
- **X20 P(foul final)** ≈ 1.0 (确定 foul)
- **R5_0 is_last_round** = 1
- **Z0 E[final_score]** = -20

NN 看到 X20 = 1 一定 foul, 选 7h 上顶.

### Case 8 (R1 双鬼 + 234 应分顶底)

V3 features for AI choice (头X 中X 底234):
- **S0 slot_balance** = 0/3 = 0 (极不均衡)
- 各 X 概率: 因为 234 全在 bot, P(bot 顺子) 高但 P(bot SF / quads) 都低
- Y2 E[royalty_bot] ≈ 0.6 × 4 (straight) = 2.4

V3 features for expected (头X 中234 底X):
- S0 slot_balance ≈ 0.6
- X14 P(mid straight) 跟 234 + 未来 deal 联合算

NN 应该看 S0 + Y 选更均衡的.

(注: 当前 V3 设计 case 8 可能仍难, 因为双鬼配低牌的策略很 subtle. 但 V3 至少给了 slot balance 信号.)

---

## 实现计划

### Phase 1: 概率计算辅助函数 (~150 LOC)

```go
// internal/feature_v3_probs.go
func hypergeoP(deckSize, deckTargetCount, drawCount, minTargetCount int) float32
func pTopPairQKAFinal(gs *GameState) float32
func pMidHandType(gs *GameState, handType int) float32
func pBotHandType(gs *GameState, handType int) float32
func pFoulFinal(gs *GameState) float32
func pFantasyFinal(gs *GameState) float32
// ... ~15 个辅助函数
```

### Phase 2: features_v3.go 主入口 (~150 LOC)

```go
const FeatureDimV3 = 131

func BuildFeaturesV3(gs *GameState) []float32 {
    f := make([]float32, FeatureDimV3)
    fillBoardState(f[0:8], gs)              // A
    fillHandTiers(f[8:32], gs)              // B
    fillJokerState(f[32:40], gs)            // D
    fillSuitDist(f[40:52], gs)              // E
    fillDeckAware(f[52:69], gs)             // G
    fillProbabilities(f[69:90], gs)         // X (21 dim, P(foul) at end)
    fillFantasyGranular(f[90:94], gs)       // F (新, 4 dim QQ/KK/AA/trips)
    fillExpectedRoyalty(f[94:97], gs)       // Y (3 dim)
    fillSummary(f[97:102], gs)              // Z (5 dim, Z0 用 calibrated bonus)
    fillPairRank(f[102:107], gs)            // U (5 dim, 含 top)
    fillPairToTrips(f[107:112], gs)         // V (5 dim)
    fillTopFantasyLocks(f[112:116], gs)     // T (4 dim, T2 fixed: any trips)
    fillMaxAchievable(f[116:119], gs)       // C (新, 3 dim per row max)
    fillLastRound(f[119:121], gs)           // R5 (2 dim)
    fillCommitment(f[121:125], gs)          // Q (4 dim)
    fillFoulMargin(f[125:128], gs)          // M (3 dim)
    fillSlotBalance(f[128:129], gs)         // S (1 dim)
    fillDiscard(f[129:131], gs)             // N (2 dim)
    return f
}
```

### Phase 3: trained_eval.go dispatch (~10 LOC)

```go
func BuildFeatures(gs *GameState, inDim int) []float32 {
    if inDim == FeatureDimV3 {
        return BuildFeaturesV3(gs)
    }
    // ... fallback to V2
}
```

### Phase 4: 单元测试 (~150 LOC)

每个新 group 至少 2-3 个 test:
- Group X: 验证 hypergeometric 公式
- Group Y: 验证 expected royalty 计算
- Group W: 验证 refan 检测
- Group T: 验证 AA != QQ non-linear
- ...

### Phase 5: 训练 V3 ckpt

- inDim 131, h1 512, h2 256, h3 128, outdim 4
- Fresh init (V2 ckpt 不兼容)
- 用现有 312K samples train ~50 epoch LR=0.001
- 1-2h Mac

### Phase 6: Bench + 对照

- DISABLE_MCTS=1 bench
- 期望: ≥ 50/63 (V2 47 突破)
- 不到 50: features 设计有 bug 或不够, 加更多
- 到 52+: V3 验证, 继续 AZ self-play 推 55+

---

## 风险

| 风险 | 缓解 |
|---|---|
| 概率计算 bug (hypergeo 错) | 每个公式单测 + 边界检测 |
| Fresh init 训不动 (类似 big-model-v1 失败) | 数据足 (312K), 设计稠密信号 |
| 128 dim 仍不够 | 加更多 (commit/discard/etc) |
| 删了 V2 旧 features 误删关键 | 训 V3 + 训 V2 对照 |

---

## 工程量

| Phase | 时 |
|---|:-:|
| 1 概率辅助 | 3-4h |
| 2 features_v3.go | 2-3h |
| 3 dispatch | 5min |
| 4 单元测试 | 2-3h |
| 5 训练 (Mac compute) | 1-2h |
| 6 bench + 分析 | 30min |
| **总** | **8-12h** |

一个工作日.

---

## 评估标准

V3 fresh init 训完, 纯 MLP bench:

| 结果 | 含义 | 行动 |
|---|---|---|
| **≥ 55/63** | 大突破, features 设计成功 | 继续 AZ self-play 推 58+ |
| **50-54/63** | 边际改善 | 看 fail case 分析, 加针对性 features |
| **48-49/63** | 小改善 | 看哪些 case 改善, 哪些没改善 |
| **47/63** | 持平 | 设计未到 root cause, 反思 |
| **≤ 46/63** | 倒退 | features 有 bug 或设计错, 回退 V2 |
