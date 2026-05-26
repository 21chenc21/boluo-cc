# case-bench — Reference-based test case validator

## 方法论

不用 OFC v0-dev 的"AA shove ≥ 0.9" 硬阈值方式 — HUNL Nash 是混合策略, 硬阈值是人类偏见, 可能 reject 正确 Nash 模型或 pass 偏激模型 (详 [memory feedback_methodology_per_game_type](../../../.claude/projects/-home-chguang-boluo-cc/memory/feedback_methodology_per_game_type.md)).

替代方法 (reference-based):
1. **参考解** = 高 iter 自己 CFR/MCCFR 跑出来的 strategy (我的 CFR+ 已对照 OpenSpiel 验证过 ≈ Nash).
2. **Curated case** = 仅指定 infoset (private/board/history), 不指定 expected freq.
3. 每 case: 计算 candidate vs reference 每个 action 的 prob 差, **max |Δ| < 0.10 即通过**.
4. ≥ 95% pass = "模型基本可用". <95% = 模型未收敛 / 容量不够 / 训练有 bug.

## 使用

```bash
# Leduc — 用 blueprints/leduc-vanilla-30k.json 当候选, CFR+ 20k iter 当 ref
go run ./cmd/case-bench leduc

# 或指定其他候选
go run ./cmd/case-bench leduc some-other-blueprint.json

# HUNL push/fold (10bb 默认)
go run ./cmd/case-bench pushfold 10 1000000 2000000
#                                  └─ stack  └─ candidate iters  └─ ref iters
```

候选 vs 参考: 参考用更多 iter 是默认。如果你想测一个具体 blueprint 文件, 可以加 load 参数 (待加 flag, 现在只支持训练候选).

## 输出格式

```
ID   case                                          maxgap   result
-----------------------------------------------------------------
1    AA SB                                         0.0004   ✓
20   22 SB                                         0.1188   ✗
       fold       candidate=0.2405  ref=0.1217  Δ=+0.1188
       allin      candidate=0.7595  ref=0.8783  Δ=-0.1188
...
PASS: 35 / 38  (92.1%)
⚠ <95% pass — model questionable, see failures above
```

失败的 case 会列出每个 action 的 candidate vs ref vs Δ — 帮你定位哪个 infoset 收敛差.

## 当前 case 集

- **Leduc** ([leduc.go](leduc.go), 21 个):
  - R1 opening × 3 ranks (J/Q/K)
  - R1 facing check × 3 ranks
  - R1 facing bet × 3 ranks
  - R1 facing check-raise × 3 ranks
  - R2 paired board × 3 ranks
  - R2 with public, R2 facing bet 各 2-3 个

- **HUNL push/fold** ([hunl.go](hunl.go), 38 个):
  - SB opening premium (AA-AJs): 10 个
  - SB opening middle (22-T9s): 7 个
  - SB opening trash (72o-T2o): 5 个
  - BB facing shove premium (AA-AQs): 8 个
  - BB facing shove middle (55-KQs): 3 个
  - BB facing shove trash (72o-T2o): 5 个

## 验收 trend (HUNL push/fold)

| Candidate iter | Reference iter | PASS |
|---|---|---|
| 100k  | 500k | 71% (27/38) |
| 500k  | 1.5M | 82% (31/38) |
| 1.5M  | 3M | 92% (35/38) |
| **3M**    | **6M** | **🎯 100% (38/38)** |

收敛慢源: borderline 手 (QJs/94o/82o/T2o BB call) 在 Nash 真实混合区, 需更多 iter 才能稳.

## 加 case

直接编辑 `leduc.go` / `hunl.go` 内的切片. 比如:

```go
{ID: 80, Label: "AA paired AAA board",
 PrivRank: 2, PubRank: 2, R1Hist: []leduc.Action{leduc.ActionBetRaise, leduc.ActionBetRaise, leduc.ActionCheckCall}},
```

注意不要写 `expected freq` — 让 reference 决定.

## 局限

1. **Reference 不是真 Nash**. 是同 engine 高 iter 估计. 用 OpenSpiel 当真 Nash 更稳, 但 OpenSpiel HUNL push/fold 不直接支持 (universal_poker 配置复杂).
2. **±0.10 容差是经验值**. Limit 类游戏 (Leduc) 收敛后可降到 0.01; NLHE 因混合策略容差适当放宽.
3. **不测 exploitability**. 只测 "跟 reference 一致", 模型可能跟 ref 一致但都偏离 Nash. 需要单独 exploitability 计算 (Leduc 有, NLHE 大游戏需 abstraction 估计).
