# distill — Week 2 蒸馏 POC

把 Week 1 训出来的 tabular blueprint 蒸馏成 NN, 验证 4 项 POC 硬指标.

## POC 通过标准 (硬, 任一不过 → 重新评估算法路线)

| # | 指标 | 阈值 | Week 1 数据 / 当前位置 |
|---|------|------|---|
| 1 | tabular CFR exploitability | < 0.01 | ✅ **0.00896** (30k iter, [blueprints/leduc-vanilla-30k.json](../blueprints/leduc-vanilla-30k.json)) |
| 2 | NN ↔ tabular KL divergence | < 0.05 | 待 Week 2 训练 |
| 3 | NN vs tabular EV gap | < 5% | 待 Week 2 训练 |
| 4 | ONNX 单机 CPU 推理延迟 | < 10 ms | 待 Week 2 部署 |

## Pipeline

```
Week 1 输出                       Week 2 工作                   POC 验收
────────────                      ──────────                    ────────
blueprints/leduc-vanilla-30k.json
  ↓ cmd/dump-training-data
data/leduc-train.jsonl (288 行)
  ↓ distill/train.py (PyTorch)
models/leduc-policy.pt
  ↓ distill/export_onnx.py
models/leduc-policy.onnx
  ↓ server/ (Go + onnxruntime)
推理服务 + bench-latency.go
  ↓ distill/eval.py
NN strategy → cfr.Strategy
  ↓ cfr.Exploitability / GameValue
KL + EV gap + expl 报告
```

## Feature 编码 (35-d, [cfr/features.go](../cfr/features.go))

```
[0:3]   priv rank one-hot (J/Q/K)
[3:7]   pub rank one-hot (J/Q/K/none=slot 6)
[7]     round indicator (0 或 1)
[8:20]  R1 history: 4 positions × 3 actions = 12 dims
[20:32] R2 history: same
[32:35] legal-action mask (Fold/CheckCall/BetRaise)
```

设计决策:
- **One-hot 而不是 embedding**: 训练数据极小 (288 samples), 简单线性输入避免过拟合
- **History pad to 4 positions**: Leduc 单轮最多 4 actions
- **Legal mask 入 feature**: NN 学到对 illegal action 输出 ~0; 部署侧也用此 mask 后归一化
- **悬而未决**: 是否需要 value head — POC 只做 policy 蒸馏; 部署优化时再加 value 跑 search

## NN 架构 (POC 首版)

```python
MLP(
    input_dim = 35,
    hidden = [64, 32],
    activation = ReLU,
    output = 3 (action logits),
)
total params ≈ 35*64 + 64*32 + 32*3 + biases ≈ 4500
```

参数量极小 (4.5K) 跟数据规模 (288) 匹配, 避免过拟合.

Loss: cross-entropy 蒸馏 (target = blueprint probs):

```
L = -Σ_{infoset} Σ_a target_prob(a) × log NN_prob(a)
```

带 legal mask 在 NN 输出 logits 上 mask 然后 softmax.

## 文件 (Week 2 待建)

```
distill/
├── README.md           (此文件)
├── requirements.txt    pip 依赖
├── data/
│   └── leduc-train.jsonl       (Week 2 起点, Go dump 输出)
├── train.py            训练 PyTorch policy NN
├── export_onnx.py      torch → ONNX 转换
├── eval.py             NN → cfr.Strategy → exploitability/KL/EV gap
└── models/
    ├── leduc-policy.pt        训练输出
    └── leduc-policy.onnx       部署用
```

```
server/  (Go 侧)
├── onnx_infer.go       加载 ONNX, 批量推理
└── cmd/bench-latency/main.go   延迟压测
```

## Week 2 Day-by-day plan

- **Day 1**: PyTorch train.py, 训出 leduc-policy.pt, KL < 0.05 验证
- **Day 2**: export_onnx.py + 在 Python 端验证 ONNX 推理跟 PyTorch 一致
- **Day 3**: Go server/onnx_infer.go + bench-latency, 验证 < 10 ms
- **Day 4**: eval.py 全链路: NN → Strategy → exploitability, 验证 expl ≈ blueprint expl
- **Day 5**: POC 验收报告; **GO/NO-GO** 决策

## 不在 Week 2 POC 范围

- 6-max NLHE engine (Phase B+)
- Card abstraction / bet sizing
- Multi-hand bigpot feature ([memory project_texas_multihand_features](../../../.claude/projects/-home-chguang-boluo-cc/memory/project_texas_multihand_features.md))
- Value head (POC 只做 policy)
- Real-time search (POC 是纯 NN 部署)

## 失败回退

POC 任一指标不过, 回 [project_texas_cfr_kickoff](../../../.claude/projects/-home-chguang-boluo-cc/memory/project_texas_cfr_kickoff.md) 重审算法路线:

- **指标 1 不过**: CFR 没收敛够 → 跑更多 iter / 上 CFR+ / MCCFR
- **指标 2 不过**: NN 容量不够 / loss 函数错 → 增加 hidden / 改 loss / 调 lr
- **指标 3 不过**: KL 看着 OK 但 EV 异常 → 检查 illegal action handling / 数据分布
- **指标 4 不过**: NN 太大 / ONNX runtime 问题 → 减层数 / int8 quantize / 改 runtime
