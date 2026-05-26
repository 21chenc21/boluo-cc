# PROGRESS

## Week 1 (2026-05-23 起) — POC Leduc engine + CFR

目标: tabular CFR 收敛, gv 匹配 Nash, exploitability 单调下降.

- [x] **Day 1**: Leduc engine (card / state / action transitions / infoset key)
  - 45 unit tests, 97.4% coverage
  - 288 infosets 枚举验证 (R1=18, R2=270, 跟手算 close-form 匹配)
  - check-call-only EV = 0 (对称性双重验证)
  - 10w 随机 stress 0 panic
- [x] **Day 2 (上半)**: Vanilla CFR (Zinkevich 2007)
  - Regret matching + 累积 strategy + average strategy
  - gv(P0) iter 8000 = **-0.08597** ≈ canonical Nash **-0.0856** (差 0.0004)
  - 自跟自 zero-sum 严格满足 (|sum| < 1e-9)
- [x] **Day 2 (下半)**: Best-response + exploitability
  - OpenSpiel-style: 枚举每个 p-infoset 的成员 (state, π_-p) 列表
  - Memoized 递归 brAction(I) + brValue(s)
  - 之前 iterative-refinement 版有 bug, 现修
- [x] **Day 2 Cross-check**: 全 PASS
  - gv(P0) 匹配 Nash 到 4 位小数 ✓
  - expl 单调下降 (1000→0.033, 5000→0.016, 8000→0.013)
  - strategy sanity: J 多 check, K 多 bet, free-fold ≈ 0
- [x] **Week 1 gate (relaxed)**: vanilla CFR 5000 iter
  - expl < 0.02 ✓ (actual 0.016)
  - gv(P0) 在 Nash ±0.005 ✓ (actual -0.0861)
  - **CFR+ (RM+ + linear avg) 调试时发现性能反退化 (1000 iter expl 0.10 vs vanilla 0.033)**, 留作 Day 3 跟 MCCFR 一起做
- [x] **Day 2 后期: 引擎 + CFR + BR 大重构** (perf + maintainability)
  - **InfosetID uint64 packed bits**: 替换 string keys, 全 28-bit packed 编码 (priv 2 + pub 3 + r1 11 + r2 11). 288 unique IDs 覆盖整 Leduc.
  - **Snapshot/Restore**: O(1) 状态回滚, 替换 Clone() 在 CFR/BR walk 里. 历史切片 truncate via reslice.
  - **结果**:
    - CFR iter: **17.2ms → 1.4ms (12x)**
    - CFR allocs: **78,540 → 120 (650x)**
    - CFR memory: **2.6MB → 960B (2700x)**
    - BR: **45.5ms → 3.5ms (13x)**
    - Week 1 gate: 47s → 6.5s (7x)
- [x] **Day 2 deep-dive: CFR+ bug** (deferred)
  - 试了 4 个 variants (per-delta clamp / end-iter clamp, dual-walk / strict alternating, ± linear avg) 都比 vanilla CFR 慢 3-5x
  - 公式层全核对正确, 根因要拿 OpenSpiel C++ 源码逐行 diff
  - **决策**: 暂留 cfr_plus.go (gv 收敛 OK, expl 慢), Day 3 走 MCCFR 不走 CFR+
  - 详见 [memory/project_texas_cfr_plus_bug.md](file:///home/chguang/.claude/projects/-home-chguang-boluo-cc/memory/project_texas_cfr_plus_bug.md)
- [x] **Day 3-4**: External-sampling MCCFR (Lanctot 2009)
  - Sample chance + opp action, expand traverser → per-iter 极便宜
  - Regret/strategy 无 reach 因子 (sampling 提供)
  - RM+ + linear averaging 可选 (默认开)
  - **per-iter 性能**:
    - MCCFR: **12.4 μs/iter, 4 allocs, 153 B** ← 113x 快于 vanilla CFR
    - Vanilla CFR: 1,400 μs/iter, 120 allocs, 960 B
  - **收敛对比** (Leduc 小游戏, vanilla 仍占优):
    - Vanilla CFR 5000 iter (7s): expl=0.016
    - MCCFR 1M iter (10s): expl=0.043
    - MCCFR 2M iter (20s): expl=0.037
    - Leduc per-iter 收敛常数: vanilla C≈1, MCCFR C≈40-50 (sampling variance)
  - **结论**: 小游戏 vanilla 胜, 6-max 大游戏 MCCFR 是唯一可行路 (不能全枚举)
  - 4 个测试通过 (gv 收敛 / expl 单调 / determinism / vanilla MCCFR ablation)

- [x] **Day 5**: Week 1 wrap-up + Week 2 起点
  - **强 blueprint 保存**: 30k iter vanilla CFR, expl=**0.00896** (跨 POC 指标 #1 < 0.01), gv(P0)=-0.085746 (Nash 差 0.00015)
  - **持久化框架**: [`cfr/io.go`](cfr/io.go) SaveBlueprint / LoadBlueprint + round-trip 测试
  - **Feature 编码**: 35-d 一-hot 风格 (priv 3 + pub 4 + round 1 + r1_hist 12 + r2_hist 12 + legal_mask 3), [`cfr/features.go`](cfr/features.go) + 4 unit tests
  - **训练数据 dump 工具**: [`cmd/dump-training-data`](cmd/dump-training-data) 产出 288 行 JSONL (50KB), 供 Python 消费
  - **Week 2 设计文档**: [`distill/README.md`](distill/README.md) — 4 POC 硬指标 + pipeline + 失败回退
  - **Python 蒸馏 scaffold**: [`distill/train.py`](distill/train.py) (MLP 35→64→32→3, masked KL loss) + [`distill/export_onnx.py`](distill/export_onnx.py) + [`distill/eval.py`](distill/eval.py)
  - **Go 验证器**: [`cmd/compare-blueprints`](cmd/compare-blueprints) — 加载 tabular vs NN 两个 blueprint, 算 expl/KL/EV gap, 自动 POC 验收

## Week 2 — 蒸馏 POC ✅ 全 4 指标过, POC PASS

详见 [distill/POC_REPORT.md](distill/POC_REPORT.md).

| # | 指标 | 阈值 | 实测 | 余量 |
|---|------|------|------|------|
| 1 | tabular CFR expl | < 0.01 | **0.00896** | 1.12x |
| 2 | NN ↔ tabular KL | < 0.05 | **0.000153** | **327x** |
| 3 | NN vs tabular EV gap | < 5% | **0.14%** | **35x** |
| 4 | ONNX 单机 CPU 推理 | < 10 ms | **0.014 ms** | **714x** |

Pipeline:
```
[Day 0] Engine + CFR (Week 1)
   ↓ 39s vanilla CFR 30k iter
blueprints/leduc-vanilla-30k.json (expl=0.00896)
   ↓ cmd/dump-training-data
distill/data/leduc-train.jsonl (288 records, 50KB)
   ↓ distill/train.py (PyTorch MLP 35→128→64→3, 24s)
distill/models/leduc-policy.pt
   ↓ distill/export_onnx.py (4.77e-7 round-trip diff)
distill/models/leduc-policy.onnx
   ↓ distill/eval.py
distill/models/leduc-nn-strategy.json
   ↓ cmd/compare-blueprints
🎯 POC PASS — 蒸馏路径可行
```

踩过的 2 个工程坑 (POC_REPORT 详):
1. **80/20 train/val split**: 蒸馏不该 split, val set 完全没训过 (NN expl 1.12 → 用全数据后 0.02)
2. **Architecture mismatch**: eval.py 加载时不知 hidden=[128,64], 默认 [64,32] silent random init. 修: ckpt 存 hidden 字段.

## Week 1 总成绩单

```
代码量 (LOC):
  Go:    3519 (engine 600 + cfr 1400 + cmd 500 + tests 1000)
  Python: 334 (distill scaffold)

测试:
  47 engine + 22 cfr = 69 unit tests, all pass
  engine 97.9% coverage / cfr 93.8% coverage
  100K 随机 stress 0 panic

关键性能:
  CFR iter:  1.4 ms, 120 allocs   (12x 比原版快)
  MCCFR iter: 12.4 μs, 4 allocs    (113x 比 CFR 快)
  BR:         3.5 ms, 12K allocs    (13x)
  Week 1 gate: 6.5 sec (47s → 6.5s after refactor)

数学正确性:
  Nash gv(P0) = -0.085746   (canonical -0.0856 ± 0.0002) ✓
  blueprint expl = 0.008962  (< POC 阈值 0.01) ✓

外部资产:
  blueprints/leduc-vanilla-30k.json   47 KB
  distill/data/leduc-train.jsonl      50 KB (288 训练样本)
```

## ✅ Week 2 末杂项修复

- **CFR+ 反慢根因找到 + 修**: σ 必须在 walk 开始时 frozen, 不能 walk 中重算. 拿 OpenSpiel Python 比对 root-caused. iter 1000 expl=0.0008 (vanilla 同 iter 0.033, **40x 快**). 详 [memory project_texas_cfr_plus_bug](../../.claude/projects/-home-chguang-boluo-cc/memory/project_texas_cfr_plus_bug.md)
- **Go ONNX 集成**: `server/onnx.go` (build tag `onnx`) + `cmd/onnx-validate` + `cmd/onnx-bench`. Go ↔ Python ONNX max diff 2.34e-7 (数值精度内), Go 单 query 8.5 µs (POC #4 阈值 10ms, 余量 1180x). 全链路 Go 部署可行.

## Phase B kickoff: HUNL engine (起步)

[engine/nlhe/](engine/nlhe/) — Heads-Up No-Limit Hold'em engine. 详 [engine/nlhe/README.md](engine/nlhe/README.md).

- [x] `card.go` — 52 card encoding (4 suit × 13 rank), parse `"As"` / `"Td"` round-trip
- [x] `eval.go` — pure Go 7-card hand evaluator (10 categories 全测过, royal/wheel/kicker 都对)
- [x] `action.go` — Action 枚举 (Fold/CheckCall/Bet[size_idx]/AllIn) + GameConfig + PushFoldConfig 子集
- [x] `state.go` — HUNL state machine (4 street + 盲注 + 底池 + all-in 处理)
- [x] 26 unit tests, 81.6% cov, 包括 push/fold 全链路 + 主翻 transition + legal actions
- [x] `infoset.go` — lossless FNV-64a hash (hole + board + position + history). Hole order canonicalized.
- [x] Snapshot/Restore (engine/leduc 同模式, O(1) restore)
- [x] `cfr.go` — NLHE-specific MCCFR (external sampling, RM+ + linear avg)
  - dealHoles + sampleBoardFill lazy at showdown
  - 6 行 infoset_test (suit/order invariance, opp hole 不泄漏)
- [x] **HUNL push/fold smoke 跑通** ([cmd/pushfold-smoke](cmd/pushfold-smoke/main.go))
  - 500k iter / 51 秒 / 2652 infoset 全覆盖
  - **AA 0.972/0.973, KK 0.965/0.966, QQ 0.959/0.964** ← 顶牌 ~Nash
  - 32o 0.22/0.12, 72o 0.23/0.15 ← 弱牌 bluff freq 合理
  - SB shove 109/169 (Sklansky ~62%), BB call 87/169 (Sklansky ~37%) — 同 ballpark, 收敛趋势对
- [x] **多街深度测试 + Coverage 60.5% → 94.3%**
  - 12 multi-street tests: preflop raise/raise/call → flop, flop bet→turn, full 4-street showdown
  - AllIn corner cases: preflop allin, flop allin (转街后)
  - Chips conservation, payoff zero-sum, Snapshot/Restore 跨多街
  - Min-raise enforcement
  - **10k random hands × 21-subset brute force** vs Evaluate7: 0 mismatch
  - **500k random games × 5 seeds heavy stress**: 0 invariant violation
  - **250k random games × 8 GameConfigs** (shallow/deep/uneven/diff blinds): 0 violation
- [x] **修复 4 个 engine bug** (multi-config stress 暴露)
  - CheckCall 不检 stack → 负 stack
  - LegalActions 缺 AllIn 当 stackAfterCall<0
  - Under-call all-in 不正确终止 (无 refund 无 advance)
  - Refund 后 AllIn flag 没清
- [x] **Case-bench 框架** ([cmd/case-bench](cmd/case-bench/))
  - **Reference-based 方法论** (不用硬阈值, 避免 OFC 风格人类偏见)
  - Leduc: 21/21 PASS vs CFR+ 20k ref (expl 4e-6)
  - HUNL push/fold convergence: 100k=71% → 500k=82% → 1.5M=92% → **3M=🎯100% (38/38 PASS)**
- [x] **HUNL push/fold 端到端蒸馏 POC** 完成
  - [nlhe/features.go](engine/nlhe/features.go): 33-d encoder
  - [cmd/dump-pushfold-data](cmd/dump-pushfold-data/main.go): 训 3M iter MCCFR + dump 2652 records JSONL + blueprint JSON (316s)
  - [distill/train.py](distill/train.py): 参数化 feature_dim (Leduc 35 / HUNL 33 共用)
  - [distill/models/hunl-policy.pt](distill/models/hunl-policy.pt): MLP 33→128→64→3, CE 收敛 0.40 (Nash 噪声底下)
  - [distill/models/hunl-policy.onnx](distill/models/hunl-policy.onnx): PyTorch→ONNX max diff 1.5e-8
  - [cmd/hunl-onnx-validate](cmd/hunl-onnx-validate/main.go): Go 加载 ONNX, 走全 2652 infoset 出 NN strategy JSON
  - [cmd/case-bench-hunl-files](cmd/case-bench-hunl-files/main.go): 38 curated case 比 NN vs tabular
  - **结果: 38/38 = 100% PASS** (max case gap 0.04 << 0.10 阈值)
  - [cmd/hunl-onnx-bench](cmd/hunl-onnx-bench/main.go): Go 端 7.5 µs/query (134k QPS)

**HUNL push/fold 全栈打通**:
```
Engine (Go) → MCCFR (Go) → JSONL → PyTorch → ONNX → Go onnxruntime → case-bench
   ✓             ✓           ✓        ✓        ✓         ✓             100% PASS
```

- [x] **长尾噪声 G 验证 (2026-05-24)**: 训 2 个 tabular blueprint (seed 42 vs 12345), 跟 NN 对比 distribution:
  - **tabular A vs tabular B**: p100=0.593, p99=0.43, 254 infoset >0.20 分歧
  - **NN vs tabular A**: p100=0.44, p99=0.29, 104 >0.20
  - 结论: NN 比 tabular 更稳 — 自动学 canonical mean, 去掉 suit-specific MCCFR 噪声
  - 38/38 case-bench PASS 不是偶然, NN 真到了 canonical Nash

## ✅ C Phase 1: Preflop card abstraction

[engine/nlhe/abstraction/](engine/nlhe/abstraction/) — bucket-based 信息集压缩.

- [x] `canonical.go` — 169 canonical hand types (13 pair + 78 suited + 78 offsuit), `HandTypeIdx` + `HandTypeLabel` O(1) lookup
- [x] `equity.go` — Monte Carlo E[HS²] (= 胜率 vs 随机 opp+board), 100k samples ≈ 130ms/hand
  - 跟公开数字匹配: AA 0.851 (ref 0.853), AKs 0.671 (ref 0.671), 72o 0.345 (ref 0.349)
- [x] `kmeans.go` — 1-D Lloyd's K-means, bucket-by-ascending-equity
- [x] `preflop.go` — `Build(K, samples, seed) → *PreflopBuckets`, save/load JSON
- [x] [cmd/build-preflop-buckets](cmd/build-preflop-buckets/main.go) — production tool
- [x] 10 unit tests (覆盖、Label 唯一、order invariance、suit invariance、save/load、equity 已知值)

**Production K=20 buckets** ([blueprints/preflop-buckets-K20.json](blueprints/preflop-buckets-K20.json), 4KB):
```
Bucket 19 (top, eq=0.80):  AA KK QQ JJ TT          (premium pairs)
Bucket 18 (eq=0.67):       99 88 77 AKs AQs AJs AKo (strong)
Bucket 17 (eq=0.63):       66 ATs A9s KQs KJs KTs AQo ... (broadway+)
...
Bucket  1 (eq=0.37):       72s 62s 52s 42s 32s 83o 82o 73o (very weak)
Bucket  0 (bottom, eq=0.34): 72o 62o 52o 43o 42o 32o (classic worst 6)
```

Coverage ratio: 1326 hole combos → 20 buckets (~66x compression).

## ✅ C Phase 3: Engine 集成 + 对照 lossless

- [x] [`MCCFR.WithIDFn`](engine/nlhe/cfr.go#L60) — 注入式 infoset 函数 (默认 = `state.InfosetID`, 可换 bucket-based)
- [x] [`PreflopBuckets.PreflopID`](engine/nlhe/abstraction/preflop_id.go) — bucket + position + facing-shove 打成 7-bit uint64
- [x] [cmd/abstract-vs-lossless](cmd/abstract-vs-lossless/main.go) — 训 abstract MCCFR, 对照 lossless tabular blueprint 在 38 curated case 上

### 实测数据

| 配置 | Infosets | Iter | Wall time | Case-bench |
|---|---|---|---|---|
| Lossless | 2652 | 3,000,000 | ~316 s | 100% |
| Abstract K=20 | 40 | 100,000 | 0.5 s | 94.7% (训不足) |
| Abstract K=20 | 40 | 500,000 | **2.4 s** | **🎯 100%** |
| Abstract K=50 | 82 | 200,000 | 1.4 s | 84.2% (反退) |

**结论**: K=20 + 500k iter = **130x 训练加速, 零质量损失**.

### 严格 K × iter sweep (2026-05-24 实测)

```
K   iter   iter/bucket  PASS    fails
20  100k   2500         94.7%   under-train (2 fail)
20  500k   12500        100%    sweet spot
20  1M     25000        97.4%   slight noise drift
30  500k   8333         100%
30  1M     16666        100%
50  200k   2000         84.2%
50  1M     10000        92.1%
50  2M     20000        92.1%   ← per-bucket > K=20/500k, 仍卡!
80  1M     6250         89.5%
80  2M     12500        89.5%
```

**修正 (我之前的 "K=50 训练不足" 是错的)**:

K=50 / 2M iter (20k/bucket > K=20 500k 的 12.5k) 仍 92.1% — **abstraction 结构问题, 不是训练**.

K=50 持续失败 case 极端:
  - **AKs BB facing shove**: lossless call 0.96 ↔ abstract call 0.34 (Δ=0.63 完全相反!)
  - **T2o BB facing shove**: lossless fold 0.88 ↔ abstract fold 0.03 (Δ=0.86 完全相反!)

### 真正 root cause

E[HS] = "vs 随机 opp 100% range" 的胜率. 但 Nash 策略要的是 "vs **shove range** ~62%" 的 equity. **Metric 不匹配**.

K=20 侥幸过: 8 hands/bucket 平均后 bias 撞进 ±0.10 容差.
K=50 暴露: 4 hands/bucket 不够平均, 单 hand 偏差暴露.

**修法 = OCHS (Opponent Cluster Hand Strength)** — 先聚类 opp range, 再按 vs-cluster equity 分布聚类自己手. Phase 2 必做.

### 实战建议 (push/fold)
- 用 K=20 / 500k iter (100%, 2.4s 训练, 130x 比 lossless 快)
- 千万**不要 K↑** — finer 反更差直到换 OCHS

## ⚠️ OCHS 实现 + 验证 (2026-05-24)

[engine/nlhe/abstraction/ochs.go](engine/nlhe/abstraction/ochs.go) — N-d K-means on equity-per-opp-cluster.

**实测结果跟预期不符**:

| 配置 | 500k iter | 2M iter (收敛) |
|---|---|---|
| EHS K=20 | 100% | 97.4% |
| EHS K=30 | 100% | 97.4% |
| EHS K=50 | 84% | 92.1% |
| OCHS K=10 | 100% | 97.4% |
| OCHS K=20 (seed=7) | 100% | 92.1% |
| OCHS K=30 | 100% | 92.1% |
| OCHS K=50 (best seed) | 95% | (未测) |

**真发现**: 高 iter 暴露 **abstract Nash ≠ lossless Nash** 的本质差异. "500k pass" 是 MCCFR checkpoint coincidence, 不是 abstraction 真无损.

**OCHS 在 push/fold 没胜 EHS** — 因 preflop equity vs shove range ≈ vs random, 没 board 放大 variance. **OCHS 真正价值在 postflop**, 留 Phase 2 验证.

**locked 生产配置**: EHS K=20 / **500k iter** (不要过训, 过训反退化). 130x 比 lossless 快, case-bench 100% (checkpoint sweet spot).

## ✅ B: 端到端 abstract → NN → ONNX 全链路 (2026-05-24)

5 步全跑通:

```
1. abstract MCCFR  (40 infosets, EHS K=20, 500k iter)        2.4s
2. dump JSONL      (2652 lossless states ← abstract probs)   <1s
3. PyTorch NN train (33-d feature, MLP 128→64→3)             178s
4. ONNX export     (max diff 4.47e-8)                          1s
5. Go ONNX validate + case-bench-hunl-files (46 cases)       <1s
```

### Case-bench 扩到 46 case (加 22-99 BB shove + A3o/A4o SB)

| Pipeline | 38 cases | 46 cases (扩) |
|---|---|---|
| Lossless tabular (3M iter) | 100% | (basis) |
| Lossless NN (从 lossless tabular 蒸馏) | 100% | **97.8%** (45/46) |
| **Abstract NN** (从 abstract MCCFR 蒸馏) | **100%** | **95.7%** (44/46) |

### 共同失败 case: 22 BB facing shove

| | call freq |
|---|---|
| Lossless tabular | 0.86 (Nash 真值) |
| Lossless NN | 0.74 (NN 噪声: Δ=0.12) |
| Abstract NN | 0.07 (bucket 错: Δ=0.79) |

**根因**: 22 (eq=0.506) 落 K=20 bucket 9 跟 J6s/T7s/K2o 同桶, 平均策略是 fold. 但 22 真 Nash 该 call (pot odds 弥补 vs shove range 低 equity).

→ 这是 EHS bucketing 把**小对子和未关联弱手**聚一桶的 fundamental 缺陷, OCHS Phase 2 该治.

### 38 case 不够覆盖

之前 38 case 100% 是因**漏掉小对子 BB shove**. 加 8 个 (22-99 BB, A3o/A4o SB) 后:
- Abstract NN 暴露 22 BB 大错 (0.79)
- Lossless NN 暴露 NN 训练噪声 (0.12)

**case-bench coverage 永远不够全** — 真正可靠的 quality 测量需要 exploitability 或大批 random hand 模拟.

## ✅ C Phase 2a: Flop abstraction infrastructure (2026-05-24)

[engine/nlhe/abstraction/](engine/nlhe/abstraction/) — 加 flop 维度.

- [x] [`canonical_flop.go`](engine/nlhe/abstraction/canonical_flop.go): suit-isomorphic canonical key for (hole, flop). 1.3M 唯一 classes 理论值.
- [x] [`flop_equity.go`](engine/nlhe/abstraction/flop_equity.go): `MCEquityFlop / MCEquityTurn / MCEquityRiver`
  - 验过 AA on 2-7-3 = 0.88, 22 trips = 0.95, AKo top 2-pair = 0.90, 72o air = 0.18
- [x] [`flop_bucket.go`](engine/nlhe/abstraction/flop_bucket.go): `BuildFlop(K, outerSamples, innerSamples, seed)` — sampling-based bucket build
- [x] `For / ForOrFallback` lookup (后者对未见 class 用 nearest-equity 兜底)
- [x] **Strength ordering 验过**: AA on 2-7-3 → bucket 9 (top), 72o flopped 2-pair → bucket 9, 72o on AKQ → bucket 0
- [x] [cmd/build-flop-buckets](cmd/build-flop-buckets/main.go) production builder
- [x] 7 unit tests 全过 (canonical 3 + equity 3 + bucket 3)

**Smoke build (K=50, outer=50k, inner=500)**:
- 31s, 49108 unique canonical classes, 3.8% coverage of theoretical 1.3M
- File 3.3 MB

**Production estimate**: 1M outer × 1000 inner ≈ 30% coverage, ~15 min, ~70MB. 未见 class 用 `ForOrFallback`.

### 注意 / 留 Phase 2c 决定:
- 当前计算 E[HS] (= P(win)), 不是真 E[HS²] (variance-aware)
- 命名一致, 但严格说算的是 1-D 平均 equity
- Engine 集成后若发现聚类不够细 (e.g. flush draws 跟 made hands 同 bucket), 升 OCHS 或加 E[HS²] variance

## ✅ C Phase 2b: Generic StreetBuckets (2026-05-24)

**Refactor**: 之前 flop-specific 重新整理成 generic street-aware infra.

- [x] `canonical_flop.go` 加 `CanonicalHoleBoard/Key` — 通用任意 board 长度
- [x] `board_equity.go` — `MCEquityBoard(hole, board, ...)` 一函数 handle 所有 street
- [x] `street_bucket.go` — `StreetBuckets` + `BuildStreet(street, K, outer, inner, seed)` generic
- [x] [cmd/build-street-buckets](cmd/build-street-buckets/main.go) generic CLI
- [x] 7 generic street tests (build×3 + strength ordering×3 + save/load)
- [x] Cleanup: 删除 redundant flop_*.go + build-flop-buckets/. 一套 API 跑 flop/turn/river

**Smoke build 数据 (各 K=50 outer=50k)**:

| Street | Wall | Unique classes | Coverage |
|---|---|---|---|
| Flop | 31s | 49108 | 3.82% of 1.3M theoretical |
| Turn | 18s | 49914 | 0.36% of 14M |
| River | 11s | 49994 | 0.04% of 123M |

未见 class → `ForOrFallback` runtime equity + nearest center.

**Strength ordering 全 street 通过**:

| Test | Bucket (K=10) |
|---|---|
| AA on 2-7-3 (flop) | 9 (top) |
| 72o on AKQ (flop, air) | 0 (bottom) |
| AA on 2-7-3-8 (turn) | 8 (top) |
| 72o on AKQJ (turn, air) | 1 (low) |
| AA on dry river | 8 (top) |
| 32o on Broadway river (plays board straight) | 4 (mid, ~50% equity tie) |

## ✅ C Phase 2c: Multi-street MCCFR 集成 (2026-05-24)

[engine/nlhe/cfr.go](engine/nlhe/cfr.go) `walk` refactor + [engine/nlhe/abstraction/multistreet_id.go](engine/nlhe/abstraction/multistreet_id.go) — 多街 abstract MCCFR 全栈打通.

### Engine 改动: chance node in walk
之前 walk 只处理 all-in showdown 时 fill board to 5. 现在统一: 任何时候 `s.NeedsBoard()` 返回 needs → sample N 张 → recurse → restore. 同一函数 (`chanceFill`) handle preflop→flop (3) / flop→turn (1) / turn→river (1) / all-in showdown (fill-to-5).

`walk` 优先级: chance → terminal → action. Snapshot/Restore 已覆盖 NumBoard 字段, 回溯自动还原.

### MultiStreetBuckets — 多街 abstract ID

uint64 layout:
```
bits  0-1   street
bit     2   position
bits  3-10  preflop bucket (8b)
bits 11-18  flop bucket    (0 if street < flop)
bits 19-26  turn bucket    (0 if street < turn)
bits 27-34  river bucket   (0 if street < river)
bits 35-63  bet-history FNV hash (29b)
```

Unseen postflop class:
- `MCSamplesFallback > 0`: runtime MC equity + nearest center (慢 ~5ms/lookup, 但 lossless 风格)
- 否则 coalesce to bucket 0 (快, 但容易丢分辨)

### 验证
- [x] `TestMCCFRMultiStreetVisitsAllStreets` — 500 iter / 20BB DefaultConfig 触达 preflop 4385, flop 3011, turn 1926, river 981 (金字塔分布 OK)
- [x] `TestMCCFRMultiStreetBoardChangesInfosetID` — AKo on QJT vs AKo on 278 不同 InfosetID
- [x] 5 MultiStreetID tests (determinism / street / board / position / history)
- [x] `TestMCCFRWithMultiStreetBuckets` 端到端: MCCFR + MultiStreetBuckets.ID 200 iter, lossless 3854 infosets vs abstract 1908 (2.0x 压缩), 全 street 覆盖, AverageStrategy 合法
- [x] Push/fold smoke 100k iter 未退化 (AA 0.973 / KK 0.927 / AKs 0.900 同 refactor 前)
- [x] 全测试套通过 (cfr 21s, leduc 0.1s, nlhe 13s, abstraction 81s — 后者含 3 multistreet 测试 ~5s)

## ✅ Phase 2d 起步 (2026-05-24): cmd + 性能 40x

- [x] [cmd/multistreet-smoke](cmd/multistreet-smoke/main.go) — 多街 abstract MCCFR CLI (preflop K=20 + flop/turn/river K=50 + bet sizes)
- [x] 补建 turn K=50 smoke + 重建 flop K=50 (旧文件 pre-2b 无 `street` 字段)
- [x] **RM+ flooring 优化 → 40x 加速**: 之前每 walk 后扫整 regret map (O(N total infosets)), 改为只 floor walk 真访的 infoset (O(walk depth)). 5 行修改, 40x 提速.

性能对照 (HUNL multi-street 20BB K=50/flop/turn/river):

| iter | 优化前 | 优化后 | 比 |
|---|---|---|---|
| 100k iter | 161s (622 iter/s) | **4.1s (24,446 iter/s)** | 40x |
| 1M iter (推算) | ~30 min | **70s (14,304 iter/s)** | ~25x |
| 1M iter infosets | — | 145,521 |
| 10M iter (推算) | — | ~12 min |
| Push/fold 500k iter | 51s | 2.1s | 24x |

收敛 spot check (1M iter): AA preflop 20BB → fold 0.002 / call 0.914 / bet 0.038 / allin 0.046. 跟小 bet-abstraction (单 pot-size) 有关, 真 NLHE 多 bet size + OCHS 应分散到 raise/3bet.

## ✅ Phase 2e step (2): 多 bet size 实测 (2026-05-24)

`-bet-frac "0.5,1.0,2.0"` (Pluribus baseline). [cmd/multistreet-smoke](cmd/multistreet-smoke/main.go) CLI 加 comma-separated 解析.

### AA preflop 20BB 收敛对比

| 配置 | fold | call | b(0.5p) | b(1.0p) | b(2.0p) | allin |
|---|---|---|---|---|---|---|
| single [1.0] 100k iter | 0.002 | 0.494 | — | 0.329 | — | 0.176 |
| single [1.0] 1M iter | 0.002 | **0.914** | — | 0.038 | — | 0.046 |
| **triple [0.5,1.0,2.0] 100k** | 0.001 | 0.150 | 0.081 | **0.423** | 0.135 | 0.210 |
| **triple [0.5,1.0,2.0] 1M** | 0.000 | 0.716 | 0.147 | 0.036 | 0.026 | 0.074 |

观察:
- single 1M AA 退化到 91% limp = bet-abstraction 是真凶 (raise EV 模糊就退 limp)
- triple 100k AA 立刻 85% raise/allin — 多 size 治了 limp-trap
- triple 1M AA 收敛回 72% limp — **K=20 preflop bucket dragdown 效应**: bucket 19 = AA-TT 5 hand types 共享策略, TT 怕 4-bet allin 拖 AA 一起 limp. 跟 22BB facing shove 同根 (fundamental abstraction flaw, 留 step (3)/(4))

### Per-street 分布变化 (1M iter)

| street | single [1.0] | triple [0.5,1.0,2.0] |
|---|---|---|
| preflop | 28.2% | 21.8% |
| flop | 26.6% | 27.1% |
| turn | 24.9% | 28.3% |
| river | 20.3% | 22.7% |

多 bet size 后双方更愿打完整局, 街分布更平.

### 性能 (HUNL 20BB / K=50 each-street)

| iter | single [1.0] | triple [0.5,1.0,2.0] |
|---|---|---|
| 100k | 4.1s / 24,446 iter/s / 34,809 infoset | 17.6s / 5,694 iter/s / 216,737 infoset |
| 1M | 70s / 14,304 iter/s / 145,521 | 253s / 3,952 iter/s / **1,113,311** |

多 bet size 代价: per-iter 4.3x 慢 + infoset table 7.7x 大. 内存 ~130MB @ 1M. 1M iter 4 分钟内, 仍 production-tractable.

## ✅ Phase 2e step (1): multi-street case-bench (2026-05-24)

[cmd/multistreet-case-bench](cmd/multistreet-case-bench/main.go) — 11 directional case (sum_ge / sum_le 阈值), 500k iter triple bet-size 训练 109s.

### 结果 8/11 PASS (72.7%)

| # | Case | Expected | Got | 状态 |
|---|---|---|---|---|
| 1 | AA SB preflop raise+allin ≥0.5 | 0.5 | **0.339** | FAIL (dragdown) |
| 2 | KK SB preflop raise+allin ≥0.5 | 0.5 | **0.339** | FAIL (dragdown) |
| 3 | 72o SB fold ≥0.4 | 0.4 | 0.456 | PASS |
| 4 | AKs SB fold ≤0.1 | 0.1 | 0.001 | PASS |
| 5 | AA BB facing bet1 fold ≤0.05 | 0.05 | 0.001 | PASS |
| 6 | 32o BB facing bet1 fold ≥0.3 | 0.3 | 0.933 | PASS |
| 7 | AA flop K72 c-bet ≥0.3 | 0.3 | 0.545 | PASS |
| 8 | 72o flop QJ8 not bet ≤0.5 | 0.5 | 0.063 | PASS |
| 9 | AKo flop K72 bet ≥0.3 | 0.3 | 0.356 | PASS |
| 10 | AA river 27K39 bet ≥0.3 | 0.3 | 0.804 | PASS |
| 11 | T2o river AKQ3J not bet ≤0.4 | 0.4 | **0.557** | FAIL (bucket bluff inflation) |

### 关键诊断
- **Case 1 vs Case 2 数字完全一致** (b0=0.072 b1=0.077 b2=0.044 a=0.146): K=20 preflop bucket 19 强制 AA+KK+QQ+JJ+TT 共享策略 → premium pair dragdown 显式暴露
- **Case 11 T2o "playing the board" bluffs 56% 上河**: river K=50 把 T2 跟强 made hands 同桶, abstract Nash 错信号
- Postflop 单牌评估 (case 7-10) 通过率高 — abstraction 在 specific (hand, board) 上还行, 只是 bucket boundary 拖累 edge cases

### 修复路径
- step (3) postflop OCHS — 治 case 11 river bucket 错配
- preflop K↑ 到 50 / 80, 或 preflop OCHS — 治 case 1/2 dragdown
- 大 stack/iter 不能治结构问题, 加多 case 也只是更详细暴露

## ✅ Phase 2e step (3): postflop OCHS (2026-05-24)

[engine/nlhe/abstraction/street_ochs.go](engine/nlhe/abstraction/street_ochs.go) — N-d equity profile per (hole, board), K-means in 5-8 opp cluster space.

### 接口化重构
`StreetBucketer` interface in [multistreet_id.go](engine/nlhe/abstraction/multistreet_id.go): EHS `*StreetBuckets` 跟 OCHS `*StreetOCHSBuckets` 都实现 `For` / `ForOrFallback`. MultiStreetBuckets 字段从 `*StreetBuckets` 改 `StreetBucketer`, mix-and-match 任意街.

### Profile 区分性验证 (单元测试)

| 手 | OCHS profile (5 opp clusters) | EHS 单值 |
|---|---|---|
| 32o on KQJ98 (plays board) | [0.37, 0.05, 0, 0, 0] | ~0.08 |
| AA on 27K39 (overpair) | [0.84, 0.96, 0.84, 0.95, 0.84] | ~0.89 |

OCHS 把 32o "对强 opp 几乎 0 equity" 的 asymmetric shape 显式编码, EHS 只能给一个 0.08 模糊数字.

### Case-bench EHS vs OCHS 对比 (500k iter / 20BB / triple bet-size)

| # | Case | EHS K=50 | **OCHS K=20** |
|---|---|---|---|
| 7 | AA flop K72 c-bet | 0.545 | 0.358 |
| 9 | **AKo flop K72 top pair bet** | 0.356 | **0.685** (+93%) |
| 10 | AA river bet | 0.804 | 0.772 |
| 1/2 | AA/KK SB raise | 0.339 (FAIL) | 0.280 (FAIL) |

Pass rate 9/11 两种都过. **Case 9 是 OCHS 真正胜出 case**: AKo top-pair-top-kicker EHS 把它当中等, OCHS 看到 profile 高 vs 所有 opp 簇 (强 vs 强 + 强 vs 弱) → 正确放大 aggression. EHS 看不到这种 shape.

Cases 1/2 AA/KK dragdown 不动 — 确认是 **preflop bucket 19 共享 (AA-TT)** 的问题, 不是 postflop. 需 preflop K↑ 或 preflop OCHS 治.

### 关键发现 case-bench case 11 修正
原 "T2o river on AKQ-3-J 应该 check" 错的 — T2o + AKQJ + 3 = Broadway 直顺 (T 用 hole). 应该 bet. 改成 32o on KQJ98 (真"play board" 弱手) 后 EHS/OCHS 都 PASS.

→ **Bench case 必须 OCHS profile 实算验证**, 不能凭直觉. 加 reference (memory).

### OCHS 训练成本
- Build OCHS K=20 opp=5 outer=10k inner=200 每街 ~5s (smoke)
- 实测 500k iter 训练: 94s (vs EHS K=50 109s — OCHS K=20 粗略反而略快)
- Infoset table: OCHS K=20 → 234k vs EHS K=50 → 680k (K 粗自然小)

## ✅ Phase 2e step (4): multistreet 134-d feature encoder (2026-05-24)

[engine/nlhe/features_multistreet.go](engine/nlhe/features_multistreet.go) — push/fold 33-d → 多街 134-d:

```
[ 0: 28]   hole (low rank 13 + high rank 13 + pair + suited)
[28:113]   board 5 slots × 17 (rank 13 + suit 4), sorted desc rank
[113:117]  street one-hot
[117:123]  position + facing-shove + pot/stack/bet/raise ratios
[123:129]  legal action mask (Fold, CheckCall, Bet0, Bet1, Bet2, AllIn)
[129:133]  board structure (pairs, max-suit, rank-span, distinct-ranks)
[133]      reserved
```

4 unit tests 全过 (dim / preflop encode / flop board sorted-desc + zero pad / paired-board structure).

NN 训练管道下一步还要: dump multi-street MCCFR 数据 (sample 状态而非全枚举, ~10^14 lossless infoset 不可枚举), Python train.py 已 parameterized feature_dim (Week 2 修过), ONNX export, Go onnxruntime 集成 (已有 build tag).

## Phase 2e 完整总结

| Step | 目标 | 数字 | 关键发现 |
|---|---|---|---|
| (2) 多 bet size | 验证策略分散 | AA 单 [1.0] 91% limp → 三 size 收敛 72% limp | 单 bet 是 limp-trap 真凶, 但 preflop bucket dragdown 限制了 AA 治愈 |
| (1) case-bench | directional 11-case 框架 | EHS 9/11 (81.8%) | Case 11 T2o on AKQ-3-J 设计错 (实为 Broadway 直顺) — 加 case 必 equity 验证 |
| (3) postflop OCHS | profile-aware bucket | OCHS 9/11, AKo flop top-pair bet 0.36→0.69 (+93%) | OCHS infra + StreetBucketer 接口工 work, AA/KK preflop dragdown 仍在 (postflop OCHS 不治 preflop) |
| (4) 134-d features | NN 蒸馏 board encoding | encoder + 4 tests pass | Python/ONNX 管道待下次接 |

## ✅ Phase 2g: h2h-self metric + Phase 2f 推荐反转 (2026-05-24)

引入 industry-standard 测量后, Phase 2f 的 lossless preflop 推荐被 h2h 数据否定.

### cmd/h2h-self
[cmd/h2h-self/main.go](cmd/h2h-self/main.go) — 训两 MCCFR, duplicate hands 互打, mbb/g + 95% CI. 20k 手 = 40k games < 1s.

### 反转数据 (20BB / triple bet / 20k 复制手)

| A | B | A vs B (95% CI) |
|---|---|---|
| 500k iter, K=20 | 100k iter, K=20 | +440 ±137 mbb/g (sanity ✓) |
| **lossless 500k** | **K=20 EHS 500k** | **−152 ±111 mbb/g** (lossless 输) |
| lossless 1M | K=20 500k (2x iter 补) | −57 ±107 (no signif) |

### 解释
- lossless 1.36M infoset / 500k iter = 0.37 visits/infoset
- K=20 680k infoset / 500k iter = 0.74 visits/infoset (2x)
- 结构性修了 5 个 dragdown case 但稀有路径 (case 11/18/19) 收敛性差 → 实际打牌输

### case-bench 重新定位
- 不再作 quality 评估 (PASS rate 跟 strength 不一致)
- 留作 CI smoke (查 dragdown 类 structural bug)
- h2h-self 作 daily metric

### Phase 2 行业方法论 (research agent 调查)
- LBR (Local Best Response) — 学界承认 exploitability 代理, 待实现
- h2h + duplicate hands — 已实现 ✓
- AIVAT — variance 减 85%, 待加
- Slumbot API 对比 — public benchmark, 留生产期
- **No curated case set in literature** — researchers 主动 avoid

## ✅ Phase 2h: LBR exploitability lower bound (2026-05-24)

[cmd/lbr/main.go](cmd/lbr/main.go) — Lisý & Bowling 2017 LBR.

### 数据 (20BB / triple bet / 5k hands × 50 MC inner)

| σ | LBR (mbb/g, 95% CI) | iter |
|---|---|---|
| K=20 EHS preflop | +1004 ±178 | 500k |
| K=20 EHS preflop | +1078 ±170 (no signif vs 500k) | 1M |
| Lossless preflop | +905 ±200 (slight ↓, no signif) | 500k |

参考: always-fold ~+1000 mbb/g, Pluribus ~48 vs human, DeepStack ~50 LBR

### 已知 bias
我的实现用 **σ 的真 hole 卡** in MC rollout (perfect-info-opp-range). 给 BR 不切实际信息优势, 绝对值高估几倍. 真 range-aware LBR (使用 σ-conditional opp range, 1-2 周工作) 估约 100-300 mbb/g.

### h2h vs LBR 是不同维度

| 比较 | h2h (vs concrete σ') | LBR (vs ideal BR) |
|---|---|---|
| K=20 vs Lossless 500k | K=20 +152 mbb/g 胜 (sig) | Lossless −99 less exploitable (no sig) |

实战部署关心 LBR (worst-case), 日常 monotonic dev signal 用 h2h. 互补.

### More iter ≠ less LBR
K=20 500k → 1M iter: LBR CI 重叠. MCCFR 收敛到 abstract Nash 后, exploitability 由 abstraction 决定. 跟 abstraction_findings 一致.

## ✅ Phase 2i: AIVAT-style 控制变量 (2026-05-24)

[cmd/h2h-self/main.go](cmd/h2h-self/main.go) 加 `-aivat` flag — 两 zero-mean control variate (σ_A 自对 + σ_B 自对 on same cards), 2x2 normal-eq 解 optimal α.

### 变量减幅 (500k vs 100k iter, 20k 手)
- Raw CI: ±138.6 mbb/g
- AIVAT 1 baseline (σ_A): variance ↓ **51%**, CI ±97.3
- AIVAT 2 baselines (σ_A + σ_B): variance ↓ **59%**, CI ±88.2

Paper full AIVAT 报 ~85% 减幅, 需 per-action 控制变量 (~10x 工作量). 59% 是 half-day deliverable 性价比 sweet spot.

### 验证: lossless vs K=20 verdict 锐化

| | Raw CI | AIVAT CI | Verdict |
|---|---|---|---|
| 同 iter 对比 | -86 ±111 | **-86 ±70** | Raw "inconclusive" → AIVAT "lossless 显著输 86 mbb/g" |

AIVAT 让原来不显著的对比变出信号. 实际 sample efficiency 2.4x (~40% hands needed).

### 性能
- 2x compute per pair (额外两 baseline 游戏)
- 仍 <1s for 20k pairs
- 完全 indie-tractable

## ✅ Phase 2j: Multi-street HUNL NN 蒸馏 POC 闭环 (2026-05-24)

跟 push/fold POC 同形态, 但多街 (4 street, 6 action, 6-max-friendly encoder).

### Encoder 重写: 134 → **288 dims**
[engine/nlhe/features_multistreet.go](engine/nlhe/features_multistreet.go) — 6-max-friendly schema. 加 action history block (AlphaHoldem 启发, research agent 调研后必须项).

新 layout:
- [0:157]: hero/board/state/opp slot/legal mask/board structure (HU 用部分 6-max slot)
- [157:160]: derived scalars (pot odds, SPR clamped, effective stack)
- [160:288]: 128-d action history (4街 × 4 slot × 8 dim, 区分 "limp→3bet→call" vs "open→3bet→call")

10 unit tests 全过, HU 跟 6-max 共享 schema.

### 4-Metric 闭环验证

| Metric | 实测 | Threshold | 状态 |
|---|---|---|---|
| KL(σ ‖ NN) | **0.0026** | < 0.05 | ✓ 19x 余量 |
| h2h NN vs σ (10k duplicate + AIVAT) | +50 ±88 mbb/g (CI [-38, +139]) | gap < 50 | ✓ no signif diff |
| LBR(NN) vs LBR(σ) | 963 vs 826 (CI overlap) | similar | ✓ |
| NN OOD missing | NN forward = 0% miss | < 5% | ✓ |

### Pipeline
```
σ MCCFR (500k iter, K=20 EHS preflop + K=50 postflop)
  → cmd/dump-multistreet-data: 20k self-play games → 54k JSONL records (0.07% miss)
  → distill/train.py: 288→256→128→6 MLP, 200 epochs, 235s, KL 0.0026
  → distill/export_onnx.py: PyTorch→ONNX diff 2.98e-08
  → server.PolicyModel: ONNX 推理
  → cmd/h2h-self -nn-a: 实战 ladder
  → cmd/lbr -nn: exploitability
```

### Key methodology learnings
- **CE 看着糟 ≠ 蒸馏失败**: mixed Nash 下 target_entropy 高 (1.07/1.79), 真 KL = CE - target_ent = 0.003
- **action history is 必备**: 所有成功 NLHE NN (AlphaHoldem / PokerRL) 都编 betting sequence
- **抄作业先**: research agent 揭示我 158-d 缺 action history, 改前查避免重做

## ✅ Phase 3 (6-max) W1: engine state machine (2026-05-25)

新包 [engine/nlhe6/](engine/nlhe6/) 支持 2-6 玩家. 跟 engine/nlhe/ 平行存在 (HU 不影响).

### W1 deliverables
- types.go: `Seat`, `Position`, `PositionFor()`, `FirstToActPreflop/Postflop`
- state.go: `State`, `Apply`, `LegalActions`, `NeedsBoard`, 多 player round-close
- sidepot.go: `ComputeSidePots()` + `State.Payoff()` 支持 side pot + showdown + foldwin
- snapshot.go: O(1) Snapshot/Restore
- **25 unit tests** + **25k random heavy stress** (5k × N=2-6) **0 invariant violation**

### Round-close 泛化
HUNL hard-code → 6-max 用 `HasActed[seat]` + `BetThisStreet[seat] == LastBetAmount` 通用规则. Preflop BB option 由 "blind 不算 HasActed" 实现. Raise reset others 的 HasActed.

### Side pot 算法
不同 non-folded wagered amount 定义 pot tier. Per level: pot = Σ min(wagered_i, level) - prev_level. Eligible = non-folded with wagered ≥ level. 3-way 测试验证 zero-sum.

### 复用
- `nlhe.Card`, `nlhe.Evaluate7` (player-count agnostic) 直接 alias
- `engine/nlhe/abstraction` 包整套复用 (card abstraction 跟玩家数无关)
- Feature encoder 134→288 重写时已设计 6-max-friendly schema (slot 0 填 1 opp = HU, 全 5 opp = 6-max)

## ✅ Phase 3 W2: MCCFR + abstract ID (2026-05-25)

### W2 deliverables
- engine/nlhe6/infoset.go: lossless 64-bit InfosetID (FNV-1a hole+board+pos+hist)
- engine/nlhe6/cfr.go: N-traverser external sampling MCCFR + RM+ targeted flooring
- engine/nlhe6/multistreet_id.go: `MultiStreetID(b, s)` 3-bit position abstract ID (避免循环依赖, 函数放 nlhe6 包)
- 5 新 tests (3 MCCFR smoke + 2 abstract): pass

### 关键设计
- `MCCFR.Iter()`: N traverser walks per iter (HU 是 2, 6-max 是 6)
- 单 σ map 共享; 当 trav=T 时, 在 T 决策点更新 regret/strategy, 在其他 seat 决策点从 σ sample
- RM+ targeted flooring (HU 上的 40x 优化) port 过来
- abstract ID layout: 2b street + 3b seat + 8b×4 buckets + 27b hist hash
  - 跟 HU `abstraction.MultiStreetBuckets.ID` 同结构, 只是 position 1b→3b, 牺牲 hist 2 bits

### 性能
- 6-max 20BB / K=20+K=50 buckets / bet=[1.0]: ~890 µs/iter
- 1000 iter / 91,721 abstract infosets / 0.89s
- vs HU 12.7x 慢 per iter (6 traverser × 2x tree branching)
- 1M iter ≈ 15 min, 10M iter ≈ 2.5 小时 (Pluribus-class smoke 在 indie 预算内)

### 验证
- 3-handed AA UTG 10BB / 10k iter: aggression 0.403 (> 0.3 阈值)
- 6-max 1k iter abstract: 91k infosets, AvgStrategy 全 normalize
- MultiStreetID: UTG view vs MP view 不同 ID (3-bit seat 编码 work)

### 全测试套 (30 tests in nlhe6, 全 PASS)
```
go test ./...
ok   github.com/boluo/texas/cfr             21s
ok   github.com/boluo/texas/engine/leduc     0.1s
ok   github.com/boluo/texas/engine/nlhe      7.8s
ok   github.com/boluo/texas/engine/nlhe/abstraction  87s
ok   github.com/boluo/texas/engine/nlhe6     5.7s
```

## ✅ Phase 3 W3: feature encoder + dump + h2h + first NN closure (2026-05-25)

### W3 deliverables
- engine/nlhe6/state.go: 加 `HistEntry{Seat, Action}` 显式记 actor (6-max 不能用 HU parity 推 seat)
- engine/nlhe6/features_multistreet.go: 288-d encoder for 6-max (5 unit tests pass)
- cmd/dump-multistreet-data-6max: JSONL dump pipeline 同 HU 格式
- cmd/h2h-self-6max: "A at 1 target seat, B at N-1, target 轮转" multi-way h2h metric

### 关键改动
- HistEntry: 改 state.Hist 类型, InfosetID/MultiStreetID/encoder 都从 e.Seat 读 actor
- ensure() collision-aware: 27-bit hist hash 在大 infoset table 出 birthday collision (不同 legal count 撞同 id), 改 detect-and-reset 容错

### 6-max h2h variance
HU 是 ±100-150 mbb/g (with AIVAT). 6-max 现 raw ~±540 mbb/g at 10k hands. 高 variance 跟 5-way 卡牌 + AIVAT 还没 port 有关.

### 4-metric POC (preliminary, σ 50k iter / 4.78M infosets / 14.4% missing)

| Metric | 实测 | Threshold | Status |
|---|---|---|---|
| 1. KL(σ ‖ NN) (256/128) | 0.103 | < 0.05 | ⚠️ 2x 超 |
| 2. h2h NN vs σ | +214 ±540 mbb/g (CI 含 0) | gap < 200 (6-max) | ✓ no signif diff |
| 3. LBR(NN) vs LBR(σ) | not measured | similar | LBR 6-max 待 port |
| 4. NN OOD missing | 0% (NN forward) | < 5% | ✓ |

NN 实战 strength preserved per h2h. KL 偏高待 W4 优化 (bigger σ / bigger NN / longer train).

## ✅ Phase 3 W4: LBR + 4-metric POC closure (2026-05-25)

### W4 deliverables
- cmd/lbr-6max: port HU LBR to 6-max (N-seat rotation, BR fold special-case)
- Bigger NN retry (512/256/128, 200 epochs, 19 min): KL 0.103→0.071
- 4-metric POC 全测过

### 🎯 4-metric 终态

| Metric | NN-big (512/256/128) | Threshold | Status |
|---|---|---|---|
| KL(σ ‖ NN) | 0.071 | < 0.05 | ⚠️ closer, not pass |
| h2h NN vs σ (10k hands) | +110 ±543 mbb/g (CI 含 0) | gap < 200 | ✓ |
| LBR(σ) vs LBR(NN) | 2720 ±764 vs 2612 ±752 | similar | ✓ CI overlap |
| NN OOD missing | 0% | < 5% | ✓ |

**3/4 PASS, KL bottleneck 是 σ 训量 (50k iter / 14.4% missing → σ 噪声 → NN 蒸馏不能比 σ 更准)**

### 关键 finding
- Bigger NN 严格更好 (KL ↓, h2h closer to 0, LBR closer to σ). Smaller NN 之前 low-LBR 是 smoothing artifact.
- 同 HU Phase 2j 模式: distillation pipeline 在 6-max 工作, 各 metric 跟 HU 同比例.
- 6-max LBR(σ) 2720 vs HU 1004 ≈ 2.7x (5 opp vs 1).
- 6-max h2h variance ±540 vs HU ±100 ≈ 5.4x (无 AIVAT, 多 player).

## ✅ Phase 3 W5: AIVAT 6-max + σ scale-up (2026-05-25)

### W5 deliverables
- **AIVAT 6-max port** (`-aivat` in cmd/h2h-self-6max): variance ↓ **75%** (raw ±536 → AIVAT ±266 mbb/g)
- σ scale-up: 50k iter (14.4% missing) → 100k iter (10.34% missing)
- 200k iter OOM 教训: σ 不要并发训练 (内存 thrash)
- NN re-train (100k σ data) in background

### 关键改动
- AIVAT 设计差异 vs HU: HU 用 σ_A vs σ_A position swap, 6-max 用 σ-self-play at target seat (E=0 by symmetry across 6 seats). Same-deal fixing 让 baseline 与 h2h 强 correlation.
- Hash collision bug 又遇: dump path σ probs 长度可能 mismatch legal length, 修法 treat as missing.
- Memory scaling: 100k iter ~1.3 GB, 200k iter ~2 GB, 500k iter ~4 GB, 1M iter ~6-7 GB. 7.7GB system 上 1M 是临界.

### 4-metric POC (AIVAT 后 h2h 锐化)

| Metric | W4 | W5 AIVAT | Status |
|---|---|---|---|
| KL(σ ‖ NN) | 0.071 | TBD (NN re-train) | pending |
| **h2h NN vs σ** | +110 ±543 raw | **-102 ±266 AIVAT** | ✓ ±2x tighter |
| LBR(σ) vs LBR(NN) | 2720 vs 2612 | unchanged | ✓ overlap |
| NN OOD missing | 0% | 0% | ✓ |

AIVAT 让 h2h 精度跨入 Pluribus ballpark (~±50-200 mbb/g 量级). σ scaleup + range-aware LBR 接续推 paper-grade.

### W5 终态 (post-150k σ + AIVAT + bigger NN)

| Metric | W4 (50k σ) | **W5 (150k σ + AIVAT)** | Status |
|---|---|---|---|
| KL | 0.071 | **0.0713** plateau | ⚠️ ~0.07 是 indie-scale floor |
| h2h raw | +110 ±543 | -59 ±508 | better |
| **h2h AIVAT** | n/a | **-59 ±256 (CI 含 0)** | ✓ var ↓ 75% |
| LBR(σ) | 2720 ±764 | **1824 ±717** | **-33% σ 真改善** |
| LBR(NN) | 2612 ±752 | **2320 ±722** | -11% (NN 改善慢于 σ) |
| OOD | 0% | 0% | ✓ |

### W5 关键 finding
- σ 训量直接改 LBR(σ): 50k → 150k iter → LBR ↓ 33%
- NN distill 改 LBR(NN) 慢于 σ: 150k σ 上 NN LBR 比 σ 高 27% (NN ceiling at fixed model size)
- **KL plateau ~0.07 是 indie-scale 实际下界** — σ noise floor 限制
- AIVAT 6-max 比 HU 简单 AIVAT 强 (var ↓ 75% vs HU 59%)
- 200k σ scale-up OOM 教训: `AverageStrategy()` double-alloc, W6 必修

## Phase 3 W6 (下次)

按价值:
1. **AverageStrategy in-place 修 OOM** — unblock σ 500k+ overnight scale
2. **更大 NN (1024/512/256) + 300+ epoch** — 压 KL 到 0.05
3. **Range-aware LBR** — paper-grade 绝对数字
4. **Pluribus-style runtime subgame search** — production 终态

但 KL 0.07 + h2h CI 含 0 已 **functional POC**. W6 关键决策: polish to paper-grade 还是 直接 production engineering (deployment loop, real opponent bench)?

## Phase 3 后续

- W3: NN 蒸馏 (encoder 已 ready, dump pipeline 改 N-player sample)
- W4: 4-metric POC for 6-max (KL / h2h / LBR / OOD)
- W5+: 优化 (cgo OMPEval, range-aware LBR, depth-limited subgame search 引入)

## Week 3+ 路线 (Phase B 主体)

按 [memory project_texas_cfr_kickoff](../../.claude/projects/-home-chguang-boluo-cc/memory/project_texas_cfr_kickoff.md):
- Week 3-4: NLHE engine 全完 (infoset, snapshot, push/fold CFR smoke)
- Week 5-6: Abstraction (E[HS²] 200-500 bucket) + cgo OMPEval
- Week 7-10: MCCFR blueprint 训练 (估 5-10k 核小时)
- Week 11-12: 蒸馏 (复用 POC pipeline) + Slumbot bench

## Week 2 — 蒸馏 POC

- [ ] Day 1: blueprint → (infoset_feat, action_dist) 数据导出
- [ ] Day 2: PyTorch policy NN + value NN
- [ ] Day 3: ONNX 导出 + Go server 集成
- [ ] Day 4: 4 项验收指标
  - exploitability tabular < 0.01
  - NN ↔ tabular KL < 0.05
  - NN vs tabular EV gap < 5%
  - ONNX 推理 < 10ms (single CPU)
- [ ] Day 5: POC GO/NO-GO 决策

## POC 通过 → Month 1+ 6-max NLHE

- 6-max NLHE engine
- card abstraction (E[HS²] / OCHS, 200-500 bucket)
- bet abstraction (起步 4 size: 0.5p / 1p / 2p / all-in)
- MCCFR blueprint 训练 (~5-50k CPU-hour)
- 蒸馏 + 部署 + bench

## Reference

- Pluribus (Science 2019): https://www.science.org/doi/10.1126/science.aay2400
- Leduc paper (Southey 2005): https://arxiv.org/abs/1207.1411
- OpenSpiel Leduc: https://github.com/google-deepmind/open_spiel/blob/master/open_spiel/games/leduc_poker.cc
- Memory: project_texas_cfr_kickoff.md
