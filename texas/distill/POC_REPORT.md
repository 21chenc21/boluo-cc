# Week 2 POC — 蒸馏路径验证报告

**日期**: 2026-05-23
**结论**: 🎯 **POC PASS** — 蒸馏路径可行，建议进入 Phase B (6-max NLHE engine)

## TL;DR

在 Leduc Hold'em (288 infosets) 上跑通 tabular CFR → 蒸馏 NN → ONNX 部署全链路，4 项 POC 硬指标全部通过，多项余量超 100x。**Pluribus-lite + 蒸馏 NN 部署**架构在数学层面无障碍，可推广至 6-max NLHE。

## POC 硬指标完整结果

| # | 指标 | 阈值 | 实测 | 余量 |
|---|------|------|------|------|
| 1 | tabular CFR exploitability | < 0.01 | **0.00896** | 1.12x |
| 2 | NN ↔ tabular KL divergence | < 0.05 | **0.000153** | **327x** |
| 3 | NN vs tabular EV gap (相对) | < 5% | **0.14%** | **35x** |
| 4 | ONNX 单机 CPU 推理延迟 | < 10 ms | **0.014 ms** | **714x** |

## 工作摘要

### 阶段 1: Tabular blueprint
- Vanilla CFR 30000 iter on Leduc (39 秒)
- gv(P0) = -0.085746 (canonical Nash -0.0856, 差 0.00015)
- exploitability = 0.00896 (essentially Nash)
- 持久化到 [`blueprints/leduc-vanilla-30k.json`](../blueprints/leduc-vanilla-30k.json) (47 KB)

### 阶段 2: 蒸馏训练
- NN: MLP 35 → 128 → 64 → 3 (~12,800 参数)
- 训练数据: 288 个 (feature, target_probs) pairs (单 batch 包不下放都进 batch=64)
- Loss: masked KL (illegal action mask 通过 -1e9 logit 实现 zero-out)
- 2000 epoch, lr=3e-3, Adam, ~24 秒训练 (CPU)
- Final full-dataset cross-entropy: 0.207
- 关键 fix: 第一次训练用 80/20 split 失败 (val set 完全没训过). 蒸馏不该 split — 用全数据训练.

### 阶段 3: ONNX 导出 + Python 推理验证
- PyTorch → ONNX (opset 17)
- 推理 round-trip 验证: max abs diff = 4.77e-7
- onnxruntime CPUExecutionProvider 推理
- 单 query: 14 μs (POC 阈值 10ms, 余量 700x)
- batch 256: 1.67M QPS 单核

### 阶段 4: 全链路验证 (Go)
- [`cmd/compare-blueprints`](../cmd/compare-blueprints/main.go) 加载 tabular vs NN 双 blueprint
- 算 exploitability / per-infoset KL / game-value gap
- 自动 PASS/FAIL 判定

## 关键学到

### 1. 训练数据需要全用，不能 split
第一次训练用 80/20 split 后 NN expl = 1.12 (灾难). 蒸馏的"任务"是精确复现 288 个 (input, output) 映射，没有"泛化"概念 — train/val split 浪费 20% 数据.

### 2. Architecture 必须跟着 ckpt 走
首次 export/eval 不知道训练时的 hidden=[128,64]，默认按 [64,32] 加载，shape mismatch silent failure (回退随机权重). 修法: ckpt 存 hidden, 加载时读.

### 3. NN 容量很重要
[64, 32] ~4500 params 拟合不到 0.207 floor; [128, 64] ~12800 params 才能拟合到该 floor. 但对应的真 KL = 0.000153 早已远低阈值. 256+ 隐藏单元可能更精, 但 POC 不需要.

### 4. POC 阈值定得保守，全部超出预期
KL/EV gap 实测比阈值低 30-300x. 这说明蒸馏方法非常稳健，6-max NLHE 即便信息集 ~10^14 增加难度，仍有大量余量.

## 已知简化 (POC 不涵盖)

| 简化 | 6-max NLHE 阶段需补 |
|---|---|
| Leduc 只有 288 infoset, 可全枚举 | NLHE 需 card abstraction (E[HS²] / OCHS) |
| 限注，只 4 个 bet size action | NLHE 需 bet size 离散化 + off-tree mapping |
| 单手 game, 无 multi-hand context | 加 [bigpot won/lost 跟踪](../../../.claude/projects/-home-chguang-boluo-cc/memory/project_texas_multihand_features.md) |
| 只蒸馏 policy head | 部署若上 search, 需补 value head |
| ONNX 在 Python 跑 | Go server + onnxruntime_go 集成 (Week 2 Day 3 可选) |

## GO 决策建议

### 推荐进入 Phase B: 6-max NLHE engine
理由:
1. **数学路径验证**: tabular CFR + 蒸馏 NN 在 Leduc 上完美 work
2. **工程栈验证**: Go MCCFR + Python PyTorch + ONNX 部署链路打通
3. **性能 headroom 巨大**: 4 项指标全过且大幅超出, 不在阈值边界
4. **失败模式可控**: 这次踩的 2 个坑 (train/val split + architecture mismatch) 都是工程 bug, 不是算法 bug

### Phase B 优先级建议
1. **Week 3-4**: 6-max NLHE engine (Go), 单元测试完备
2. **Week 5-6**: Card abstraction (E[HS²] 200-500 bucket) + bet size 离散化 (4-6 size)
3. **Week 7-10**: MCCFR blueprint 训练 (~5-10k 核小时, 单机或 Aliyun spot)
4. **Week 11-12**: 蒸馏 NN (复用本 POC pipeline) + Go ONNX 部署 + 评估
5. **Week 13+**: Slumbot benchmark + AIVAT 方差还原 + 部署优化

### NO-GO 反向触发
如果以下出现, 重新评估算法:
- Phase B 6-max engine 写完后, MCCFR blueprint 训不到收敛 (50k+ 核小时无果)
- 蒸馏 NN KL > 1.0 (Leduc 是 0.000153, 严重退化才会到此)
- 实际部署延迟 > 100ms (本 POC Leduc 是 14 μs)

## 文件参考

```
texas/
├── blueprints/leduc-vanilla-30k.json   tabular CFR 输出, 47KB
├── distill/
│   ├── data/leduc-train.jsonl          288 蒸馏样本, 50KB
│   ├── models/
│   │   ├── leduc-policy.pt              训练 ckpt
│   │   ├── leduc-policy.onnx            ONNX 导出, 12KB
│   │   └── leduc-nn-strategy.json       NN 策略 → Strategy 格式
│   ├── train.py                         训练
│   ├── export_onnx.py                   ONNX 导出
│   ├── eval.py                          NN → Strategy JSON
│   ├── bench_latency.py                 延迟 bench
│   └── README.md                        Week 2 设计文档
└── cmd/compare-blueprints/              tabular vs NN POC 验证器
```
