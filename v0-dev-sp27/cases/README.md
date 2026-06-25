# case-train — 专项训练工具 (打地鼠)

给现成 ckpt 注入 hand-crafted hard-case 做监督, 短时间 finetune 修特定 case.

## 何时用 (重要)

**先决条件**: 已有 baseline ckpt **整体表现达标** (e.g., round-004 testcase 56/63).

case-train 是**修补工具**, 不是替代主训练. 用法顺序:

```
1. 主训练 (自对弈 silver-label)
   ├─ 8h+ 训出比 baseline 强的 ckpt
   ├─ 验证: testcase median > baseline
   └─ 问题: 总剩几个 hard case 反复错

2. case-train 微调 (打地鼠)
   ├─ 输入主训练最强 ckpt + hard-case JSON
   ├─ 5min finetune 强化 hard case 学习
   └─ 输出: case 修了 + 总体 testcase 不退步
```

**不能跳过 step 1**. 直接 case-train baseline → 过拟合 → 崩.

## 快速验证 (Linux)

```bash
cd /home/chguang/boluo-cc/v0-dev
./server-go-bin/case-train \
  -ckpt ckpts/round-004-baseline.json \
  -cases cases/hard.json \
  -out /tmp/test-fine.json \
  -epochs 5 -case-weight 1.0 -lr 0.0005

cp /tmp/test-fine.json ckpts/case-test.json
./run-testcase.sh ckpts/case-test.json
```

## CLI flag

| flag | 默认 | 含义 |
|---|---|---|
| `-ckpt` | (required) | 输入 ckpt 路径 |
| `-cases` | (required) | hard-case JSON 路径 |
| `-out` | `case-fine.json` | 输出 ckpt |
| `-epochs` | 50 | 训练 epoch (高=过拟合, 低=没学到) |
| `-lr` | 0.001 | 学习率 |
| `-case-weight` | 5.0 | case 样本 loss 权重 multiplier |
| `-mix-dataset` | (optional) | 混 baseline 数据防过拟合, 指定 oracle-dataset 目录 |
| `-mix-cap` | 5000 | baseline 混入上限 |
| `-policy` | `case-fine` | policy 标签 |

## 调参建议

**核心权衡**: case-weight × epochs × lr 决定"学得多猛".

- **太轻** (epochs<3, weight<1): case 没学到, testcase 不变
- **太重** (epochs>30, weight>3): 过拟合, baseline 退步 10+
- **甜区** (epochs=5-10, weight=1-2, lr=0.0005): case 学一点, 整体不崩

推荐起步: `-epochs 10 -case-weight 1.0 -lr 0.0005`. 不行往上调.

## cases JSON 格式

每个 case 是一个 object:

```json
{
  "name": "case 6: AA 顶 + KK 底 + 鬼中",
  "round": 1,
  "dealt": ["X", "Kc", "Kd", "Ah", "As"],
  "state": {
    "top": [],
    "middle": [],
    "bottom": [],
    "usedCards": []
  },
  "expected": {
    "top": ["Ah", "As"],
    "middle": ["X"],
    "bottom": ["Kc", "Kd"]
  },
  "wrongs": [
    {"top": ["X"], "middle": ["Kc", "Kd"], "bottom": ["Ah", "As"]},
    {"top": ["Kc", "Kd"], "middle": ["X"], "bottom": ["Ah", "As"]}
  ],
  "labelValue": 200,
  "weight": 2.0
}
```

字段说明:

| 字段 | 必填 | 含义 |
|---|---|---|
| `name` | ✓ | case 名 (log 显示用) |
| `round` | ✓ | 1-5 (第几 round) |
| `dealt` | ✓ | 这 round 发的牌 (R1=5张, R2-R5=3张) |
| `state` | ✓ | 当前 state (R1 全空, R2-R5 含前 round 摆好的) |
| `expected` | ✓ | 期望的摆法 (从 dealt 中分到 top/mid/bot) |
| `wrongs` | optional | 已知错误摆法 (会标 wrongLabel=0, 用于 ranking) |
| `labelValue` | optional | expected 标签分, default 200 (跟 AA fan-bonus 同) |
| `weight` | optional | 这 case 的样本权重, default 1.0 |

**Card 表示**:
- 普通牌: `Rs` 格式, 如 `Kc` (黑桃 K), `Td` (方片 T), `As` (黑桃 A)
- joker: `X`

**state 注意**:
- R1: top/middle/bottom 全空, usedCards 含已用牌 (如 deck-aware case 的 phantom 桌面 used)
- R2-R5: top/middle/bottom 含 R1+前 round 摆好的牌, usedCards 自动 = state 已摆 + 显式 usedCards (代码自动 union)

**wrongs**:
- 列出已观察到的 AI 错误摆法
- 标 wrongLabel=0 (vs expected=200), 给 MLP "expected 应远高于 wrong" 的 ranking 信号
- 不是必填, 但建议每 case 列 1-3 个

## 输出

新 ckpt 跟输入 ckpt 同 schema. 直接给 ofc-go-mac 加载用.

```bash
./mac-bundle/ofc-go-mac -addr :8001 -static . -weights /path/to/case-fine.json
```

## 已知限制

1. **过拟合敏感**: 31 sample finetune 50 epoch 必崩 baseline. 必须配低 epochs/weight.
2. **不解决 deck-aware 类 case**: f[75-88] 是 state 级 deck-aware feature, case-train 改一两个 case 不会让 MLP 对所有 deck-aware 决策都学会.
3. **不混 baseline**: 默认无 mix-dataset 时纯 case-only, 容易崩. 真正生产用需 `-mix-dataset` 加 baseline 防过拟合.

## 工作流总结

```
production (round-004, 56/63)
    │
    │  自对弈训练 8h
    ▼
v0-dev round-NNN (期望 ≥58/63)
    │
    │  case-train 5min (-epochs 5 -case-weight 1)
    ▼
v0-dev round-NNN-fine (期望 +1-3 case 修, 总分 ≥59/63)
```

第 1 步必须先达成 (整体框架优于 baseline), 第 2 步才有意义.
