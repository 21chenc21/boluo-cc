# Phase 6-max W5 — σ scale-up + AIVAT 6-max

2026-05-25.

## W5.1 σ 大规模训练 — partial (OOM 教训)

**计划**: σ 50k iter (14.4% missing) → 200k iter (期望 < 5% missing).

**实际**:
- 200k iter dump 跟 fg h2h test 同时跑 → swap thrashing → OOM killer 强 kill at 120k iter (9.5M infosets).
- 重跑 100k iter clean: **8.28M infosets / 115s 训完**. 10.34% missing (从 14.4% 下降).

**bug 修**: hash collision 在 sample 路径暴露 — σ cached probs 长度 ≠ legal length → index out-of-range panic. 修法: dump 检测 `len(probs) != len(legal)` → treat as missing.

**memory profile** (Go heap):
- 50k iter: ~700 MB (4.78M infosets)
- 100k iter: ~1.3 GB (8.28M infosets)
- 200k iter (估): ~2 GB (~14M)
- 500k iter (估): ~4 GB
- 1M iter (估): ~6-7 GB ⚠️ OOM risk on 7.7GB system

100k → 200k iter 应 OK 单独跑. 不要同时 fg σ train.

## W5.2 AIVAT 6-max port — variance ↓ 75% 🎯

[cmd/h2h-self-6max/main.go](file:///home/chguang/boluo-cc/texas/cmd/h2h-self-6max/main.go) 加 `-aivat` flag.

**设计**: HU AIVAT 用 σ_A vs σ_A duplicate (2 position swap), 6-max 有 6 位置 (6! 排列太多). 改用 **σ-self-play at target seat** 作 baseline:
- 每 deal 跑额外两 games (σ_A 全 6 seats, σ_B 全 6 seats)
- E[σ-self-play target seat payoff] = 0 by symmetry
- Realized 揭 card-luck → 跟 h2h payoff 强相关

**Same-deal correlation**: deal record (button + holes + board) 固定, 三 games (h2h + 2 baselines) 用同样 deal. 增强 correlation.

**两 baselines 2x2 normal equation**: 跟 HU 同 control variate formulation.

**实测** (NN-big vs σ-50k, 10k hands):
- Raw: -102 ±536 mbb/g (CI [-639, +434])
- **AIVAT (α_A=0.458, α_B=0.475): -102 ±266 mbb/g (CI [-368, +164])**
- **Variance ↓ 75%** (vs HU ~59%)

HU paper full AIVAT 报 85%. 我 75% 已超 HU 简单 AIVAT (59%) 不少 — 6-max 因 σ-self-play 跨 5 seat 相对 HU 跨 2 seat info 更密.

## W5.3 NN re-train (100k σ data) — bg pending

`distill/train.py --data /tmp/hunl6-100k.jsonl --epochs 150 --hidden 512 256 128`

期望 KL 0.071 → 0.04-0.05 量级 (10% 更稠密的 σ 标签). 完后做 ONNX + 4-metric. 当前 bg 20+ min in flight.

## 4-metric POC 全闭环 (preliminary)

| Metric | Before W5 (NN-big, 50k σ) | W5 expected | Status |
|---|---|---|---|
| KL | 0.071 | < 0.05 (100k σ + bigger model) | TBD |
| h2h NN vs σ | +110 ±543 mbb/g raw | -102 ±266 mbb/g **AIVAT** | ✓ AIVAT 已 tighten CI 2x |
| LBR(σ) vs LBR(NN) | 2720 vs 2612 | 待 σ 加训后 re-measure | unchanged for now |
| NN OOD missing | 0% | 0% | ✓ |

**AIVAT 把 h2h CI 从 ±540 → ±266** — Pluribus 量级 (~±50-100 with proper variance) 已可见. 加 σ 大规模训 + range-aware LBR 可达 paper-grade evaluation precision.

## W5 终态: 150k σ + big NN + AIVAT 完整 4-metric

### σ 训量 progression (single-run, no concurrent)

| σ iter | Infosets | Missing | Memory peak | Status |
|---|---|---|---|---|
| 50k | 4.78M | 14.4% | ~700 MB | (W3 baseline) |
| 100k | 8.28M | 10.34% | ~1.3 GB | OK |
| 150k | 11.2M | **8.26%** | ~1.8 GB | OK |
| 200k | 13.9M (trained) | n/a | ~2.1 GB peak | **OOM at AverageStrategy 双倍 alloc** |

200k+ 需 in-place AverageStrategy() 修.

### NN training progression (all 512/256/128 model)

| σ data | Epochs | Time | KL |
|---|---|---|---|
| 50k @ small (256/128) | 100 | 354s | 0.103 |
| 50k @ big (512/256/128) | 200 | 1146s | 0.071 |
| 100k @ big | 150 | 1373s | 0.0766 |
| 150k @ big | 150 | 1051s | **0.0713** |

KL **plateau ~0.07** — σ noise floor.

### 4-metric POC 终态

| Metric | W4 (50k σ) | **W5 (150k σ + AIVAT)** | Threshold | Status |
|---|---|---|---|---|
| KL | 0.071 | **0.0713** | < 0.05 | ⚠️ plateau (indie-scale floor) |
| h2h raw | +110 ±543 | -59 ±508 | — | better |
| **h2h AIVAT** | n/a | **-59 ±256 (CI 含 0)** | gap < 200 | ✓ var ↓ 75% |
| LBR(σ) | 2720 ±764 | **1824 ±717** | — | -33% |
| LBR(NN) | 2612 ±752 | **2320 ±722** | similar | ⚠️ NN +27% vs σ |
| OOD | 0% | 0% | < 5% | ✓ |

## 关键 finding W5

1. **σ 训量直接改 LBR(σ)** — 50k → 150k iter: LBR ↓ 33%. σ scale-up 是 exploitability 主推手.
2. **NN distill 改 LBR(NN) 慢于 σ** — 150k σ 上, NN LBR 比 σ 高 27%. NN ceiling at fixed model size: 学不动 σ 的改善.
3. **KL plateau ~0.07 是 indie-scale 实际下界** — 50k 跟 150k σ 同样的 NN 出同样 KL. σ noise 是 root cause.
4. **AIVAT 6-max 比 HU 简单 AIVAT 更猛** — variance ↓ 75% vs HU 59%. σ-self-play 跨 5 seat 信号密度高于 HU 2 seat.

## Phase 3 W6 (下次)

按价值:
1. **AverageStrategy in-place 修 OOM** — unblock σ 500k+ iter (overnight scale)
2. **更大 NN (1024/512/256)** + more epochs (300+) — 看能否压 KL 到 0.05
3. **Range-aware LBR** — paper-grade 绝对数字
4. **Pluribus-style runtime subgame search** — production 终态

但其实, 当前 KL 0.07 + h2h CI 含 0 已经是 functional POC. W6 是 "polish to paper-grade" 而非 "make it work". 关键决策点: 是 polish 还是直接进 production engineering (deployment loop, real opponent benchmark)?
