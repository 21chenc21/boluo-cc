# texas — 6-max NLHE CFR+NN solver

立项 2026-05-23. 见 [PROGRESS.md](PROGRESS.md) 当前进度.

## 架构 (三阶段)

```
训练 (offline 月级)    →    蒸馏 (offline 天级)     →    部署 (online ≤5s)
Go MCCFR blueprint           blueprint → policy NN        Go server + ONNX
+ card/bet abstraction       blueprint → value NN         可选轻量 search 兜底
                                                          单机 CPU 部署
```

## 选型理由

- **不走 Deep CFR (NN 进 CFR loop)**: cfr-dev v1 已撞 mode-collapse, 6p credit assignment 更难
- **不走 ReBeL**: 论文用 128 机 × 8 GPU, 2 人团队 6-12 个月做不完
- **不走 Pluribus 原版**: blueprint 几十 GB 表格 + 20s/decision real-time search, 单机部署不可行
- **走 Pluribus-lite blueprint + 蒸馏 NN 部署**: 训练沿用 Pluribus 验证路径, 部署沿用 OFC v0-dev 验证的 ONNX 模式

## Week 1-2 POC (Leduc Hold'em)

先在小游戏 (Leduc, ~3936 infoset) 验证整条链路:
1. tabular CFR 收敛 (exploitability < 0.01)
2. blueprint 蒸馏 NN, EV gap < 5%
3. ONNX 单机 CPU 推理 < 10ms

POC 不过 → 算法选型重来. 详见 PROGRESS.md.

## 目录

```
engine/
  leduc/          POC 用 Leduc 引擎
  nlhe/           (后期) 6-max NLHE 引擎
cfr/              external-sampling MCCFR + regret table + best-response
distill/          Python: blueprint → NN 训练 + ONNX 导出
server/           Go: ONNX 推理 + 延迟/并发 bench
cases/            手工 case (类比 v0-dev/cases)
scripts/          训练/蒸馏 pipeline 脚本
```
