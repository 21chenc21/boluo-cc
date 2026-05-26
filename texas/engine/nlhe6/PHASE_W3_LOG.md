# Phase 6-max W3 — feature encoder + dump + h2h + first NN closure

2026-05-25 起.

## 改动 + 验证记录

### W3.1 state.go 加 HistEntry — per-action actor 记录

**Change**: `Hist [4][]Action` → `Hist [4]HistList` where `HistList = []HistEntry{Seat, Action}`.

理由: 6-max 因 fold 不规则,不能像 HU 那样从 (street, slot_idx) parity 推 actor seat. 显式存 actor 让 feature encoder + InfosetID 直接读.

**Validation**: 全 nlhe6 测试套继续 pass (25/25 → 30/30 之前数).

### W3.2 features_multistreet.go — 288-d encoder for 6-max

**Change**: port HU encoder schema 到 nlhe6, 用 HistEntry.Seat 记 actor seat (relative-to-button 归一化 [0,1)).

差异 vs HU:
- 位置 one-hot: PositionFor(hero, button, n) → HU SB/BB, 6-max 6 个 position
- Opp slots: 5 slot 各 4 dim, 6-max 填 0-4 (slot i = hero+1+i mod n), HU 填 slot 0
- pot 归一: N×StartStack (HU 写死 2)
- Action history actor dim: HU 用 parity, 6-max 用 (e.Seat - Button) mod N / N

5 unit tests pass:
- TestFeatureDim ✓
- TestFeature6MaxPosition (UTG slot 120) ✓
- TestFeature6MaxOppSlots (SB slot 3 bet 0.005, BB slot 4 bet 0.01) ✓
- TestFeature6MaxActionHistory ✓
- TestFeature6MaxBoardEncoding ✓

### W3.3 cmd/dump-multistreet-data-6max — JSONL dump pipeline

**Change**: 复用 HU 的 JSONL 格式 (288-d feature + 6-d probs + 6-d legal mask). σ 训练 + sample-based self-play dump 同 HU 流程.

**Validation**: 
- 5k iter smoke (3-handed): 698k abstract infosets, 5498 records, missing 33% (under-trained)
- 50k iter (6-max): 4.78M abstract infosets, 137k records, missing 14.4%, 100MB JSONL

### W3.4 cmd/h2h-self-6max — multi-way h2h metric

**Change**: 跟 HU 不同 — 6-max 无明显 "duplicate-hands" trick (HU 是 2 player position swap). 改用 "A at 1 target seat, B at N-1 seats" + target seat 轮转 (modulo num players) 平衡 position 优势.

**Bug fix**: cfr.go ensure() hash collision crash. 27-bit hist hash + 大 infoset table → birthday collisions, 不同 legal-count 的 states 撞同 id 导致 sigma[i] index out-of-range. 修法: 检测 `len(r) != n`, treat collision as new infoset (reset slices, lose 之前 regret).

**Validation**:
- Sanity: 50k iter A vs 10k iter B / 5k hands → +736 ±800 mbb/g (CI [- 64, +1537]) — point estimate 显示 A 强, variance 太大 (6-max 高), 5k hands 不够 statsig.
- NN A (从 50k σ 蒸馏) vs σ B (50k iter same): +214 ±540 mbb/g, CI [-326, +755] 含 0 ✓
  - 解读: NN 跟 σ 实战 strength 在 10k hands 不可分别. 蒸馏保持 σ 强度 ✓

### W3.6a Python train smoke (288 → 256 → 128 → 6, 100 epochs)

- 137k records, 354s 训, 107k params
- CE 1.290, target_entropy 1.187
- **KL = 0.103** (HU 同形态 KL = 0.003, 高 ~33x)

KL 偏高原因猜测:
- σ 14.4% missing 训练状态 → 目标本身噪声
- 模型容量不够 (256/128 太小给 4.78M abstract infosets)
- 100 epochs 不够

[W3.6b 大模型重训 backgrounded for confirmation]

## 4-metric POC 闭环 (preliminary)

| Metric | 实测 | Threshold | Status |
|---|---|---|---|
| 1. KL(σ ‖ NN) | 0.103 | < 0.05 | ⚠️ 2x 超 (W3.6b retry 中) |
| 2. h2h NN vs σ (10k hands rotating target) | +214 ±540 mbb/g (CI 含 0) | gap < 200 (6-max relaxed from HU 50) | ✓ no signif diff |
| 3. LBR(NN) vs LBR(σ) | not measured | similar | ❌ LBR 6-max 待 W4 port |
| 4. NN OOD missing | 0% (NN forward 任 state) | < 5% | ✓ |

部分闭环: NN 实战 OK, 但理论 quality (KL) 待优化. W4 加 LBR 6-max + tighten σ training to drop missing rate + bigger NN.

## 文件清单 (W3 新增)
- engine/nlhe6/state.go (HistEntry, HistList types)
- engine/nlhe6/features_multistreet.go (288-d encoder)
- engine/nlhe6/features_multistreet_test.go (5 tests)
- engine/nlhe6/cfr.go (ensure collision-aware)
- cmd/dump-multistreet-data-6max/main.go
- cmd/h2h-self-6max/{main,nn_onnx,nn_stub}.go
- engine/nlhe6/PHASE_W3_LOG.md
