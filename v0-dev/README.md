# v0-dev — round-004 + 6 新 feature 继续训练 (2026-05-09)

基于 production round-004-acc91 (56-d, h1=256/h2=128, testcase 56/63)
通过 **feature-extension warm-start** 扩到 96-d 继续 finetune.

## 加的 8 个新 feature (f[90-97]) — v0-dev 独有

### f[90-95] (基础)

| feature | 含义 | 修哪些 case |
|---|---|---|
| **f[90] top_fantasy_locked_tier** | top 已 lock 的 fantasy tier 值 (0/0.2/0.32/0.8/1.0) | case 6: AA 顶 = 0.8 vs 鬼 顶 = 0 |
| **f[91] top_chasable_tier** | 还能凑出的最高 tier (deck-aware, A→K→Q 优先) | case 31: visible A<3 → AA 还能追则不追 KK |
| **f[92] bot_synergy_score** | 底道 promise 强度 (made/seed 0-1) | case 19: 4 黑桃底花 = 0.85 |
| **f[93] mid_synergy_score** | 中道 promise 强度 | case 15 plan2 |
| **f[94] non_synergy_ordering_warning** | bot/mid 都没 synergy 但 mid 平均 rank > bot 时 = 1 | case 24/56 高散排序 |
| **f[95] top_safe_count** | top 上 cards 是否 deck-exhausted (rank remain ≤ 1) | 用户: "5 已 3 张被使用 → 5 上顶 safe" |

### f[96-97] (用户 nuance 补充)

| feature | 含义 | 修哪些 case |
|---|---|---|
| **f[96] top_anti_foul_safety** | 全 state 综合: AA+鬼 dealt 时 prefer AA→top + 鬼→mid/bot (0.9), pure A+鬼 chase 时 prefer 鬼+A→top (1.0) | case 6 nuance + 鬼+A 安全追 |
| **f[97] joker_flex_position_value** | 鬼放 mid/bot + row 有 anchor → 高分 (鼓励鬼下放灵活) | case 6 关键: 鬼→mid/bot 比鬼→top 强 |

### redundant 清理 (零置)

f[12,13,15,16,18,19,27,71] 标记 redundant, 在 buildFeatures 末尾 zero out (保 index 兼容 round-004 W1).

加上现有 90-d (f[0-89]) 包括:
- f[7-9] topPR == Q/K/A
- f[55-63] joker max-pair, canReachTrips
- f[64-71] mono / 2-card flush/straight seed
- f[72-74] top redundant / bot/mid high flush seed
- f[75-88] **deck-aware** remain A/K/Q/2-J + remain joker
- f[89] mid pair > bot max (foul-imminent)

## 核心: "保 A 范+" 双层实现

**Feature 层**:
- f[90] = 0.8 if top has AA pair locked → MLP 直接看到 "locked AA"
- f[91] = 0.8 if AA still chasable (deck remain ≥ 1) → MLP 看到 "AA 还能追"

**Label 层** (rollout silver-label):
- 用户 cfg 里 `-fan-bonus-aa 200`, AA 范赏 200 分写进每次 rollout 终局
- 50 rollouts 平均, 保 AA 状态平均得分明显高
- 自动让 MLP 学到 "保 AA chasable" 是好事

两层一致, 强信号.

## 训练命令 (Mac)

scp 新 binary:
```bash
scp -r 34.143.241.113:/home/chguang/boluo-cc/v0-dev ~/agents/boluo-cc/
chmod +x ~/agents/boluo-cc/v0-dev/mac-bundle/{ofc-train-mac,ofc-go-mac}
md5 ~/agents/boluo-cc/v0-dev/mac-bundle/ofc-train-mac
# 应 fc37402c14cb0af5ed216308cf3b0069
```

启动训练:
```bash
cd ~/agents/boluo-cc/v0-dev && mac-bundle/ofc-train-mac \
  -hours 8 -round-min 30 -sims 200 -jokers 2 -workers 6 \
  -outdim 3 -h1 256 -h2 128 -indim 98 \
  -fan-w 0.40 -foul-w 0.10 \
  -fan-bonus-qq 50 -fan-bonus-kk 80 -fan-bonus-aa 200 -fan-bonus-trips 250 \
  -foul-cost 20 -phantom-opponents 2 -rollout-epsilon 0.1 \
  -weights ckpts/round-004-baseline.json \
  -init-from-ckpt ckpts/round-004-baseline.json \
  -ckpt-dir ckpts -policy v0-dev-r1-96d 2>&1 | tee train.log
```

**两个 flag 都要传**:
- `-weights ckpts/round-004-baseline.json` = round-004 当 rollout policy (生 label 用强 baseline)
- `-init-from-ckpt ckpts/round-004-baseline.json` = round-004 当 MLP 初始权重 (feature-extension warm-start, 老 56 列保, 新 40 列 zero-init)

## 启动后看到的关键 log (验证 work)

```
[train] loaded weights from ckpts/round-004-baseline.json
warm-start FEATURE EXTENSION: ckpt inDim=56 → train inDim=96 (新 42 feature, W1 列 zero-init, 旧 56 列权重保留)
yMean=18.155 yStd=36.894 (preserved from prev) | sample yMean=...
ranking acc: 0.90+
```

如果看到 "falling back to NewMLP" 或 acc < 70% → 出 bug, 联系我.

## 期望

- r1 (~30 min): yMean ~25, acc 90%+ (旧权重保留)
- r2-r5 (~2.5h): testcase 应稳定上 56 (round-004 baseline 不退步)
- r10+ (~5h): 期望 testcase 突破 60+/63 (新 feature 学到位)
- r16 (8h 满): 期望 ≥58 中位

## 中途 bench

每 round 出 ckpt 在 `ckpts/round-NNN-accXX.json`, 可中途 scp 回 linux bench 看 testcase.

## 不动 production

`v0-dev/ckpts/round-004-baseline.json` = production round-004 复制
`v0-dev/ckpts/round-NNN-*.json` = 训练新 ckpt
**只有 bench median > 56** 才考虑替换 production.

## 之后想做

- 如果 8h training testcase 还卡 ≤56 → 看哪些 case 没修好, 加更针对性 feature 或人工构造 hard case
- 如果突破 60 → 替换 production
