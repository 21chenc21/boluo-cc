# Mac Path C: V3 features 147-d Self-play (随机起点)

## 一句话
冷启动 V3 NN(无 V2 translate),从 default heuristic 起步 self-play,期望 V3 features 在 NN 自己当老师时 *能* 学到 signal,目标突破 47/63。

## 跑
```bash
cd v0-dev
NO_TRANSLATE=1 bash mac-scripts/train_v3_selfplay.sh 10 200
```
- `10` = iters(可调,~25-30 min/iter rollouts=100,10 iter ~5h)
- `200` = games/iter
- `NO_TRANSLATE=1` 关键!**不加** 会自动 translate V2→V3 当起点(那是 DSW path A)

## 启动会打印验证
```
[v3-sp] === SCRIPT_VERSION=2026-05-19-sp3 ...
[v3-sp] NO_TRANSLATE=1: 跳过 V2→V3 翻译, 冷启动 ...
[iter 1]   rollout policy = embed default (cold start)   ← iter-1 用 heuristic
[iter N]   rollout policy = v3-train-i147-sp/iter-K/round-... ← iter-2+ 用上 iter best
```

## 每 iter
- gen 200 games, rollouts=100 per candidate(慢 5×,exploration 强)
- train warm-start from current best
- bench:
  1. testcase pass (bench-cases)
  2. 3-metric duel vs best (bench-3metric, 200 games same-hand)
- promote rule:testcase ↑ OR fantasy ↑ OR (score↑ AND tc/fan stable)
- testcase 跌 ≥5 → 强制 DISCARD(guard)

## 文件夹
- `v3-dataset-i147-sp/iter-N/` 累积 silver-label shards
- `v3-train-i147-sp/iter-N/` 每 iter ckpt
- `v3-train-i147-sp/best.json` 当前最强 V3 NN(symlink)

## 中断 / 续跑
直接 ctrl-c,再跑同命令。脚本会:
- 检测 best.json 存在 → bench 取分续跑
- gen 接续之前的 shards(`resume from shard-N`)

## 预期趋势
- iter-1:25-30/63(default heuristic 跑出的标签训练)
- iter-2~5:25-35/63 震荡(self-play 起步噪声大)
- iter-5+:**如果 V3 features 真有用**,应该上 40+;**如果没用**,平台 30 附近震荡
- 30 iter 上 50+(memory `project_az_round1` V2 self-play 趋势:53/63 in 4 iter,但 V1 跑 14 iter 没赢 baseline)

最差情况:跟之前 distill 一样卡 25-30。但 self-play 不是 distill,**没有 teacher ceiling 限制**,理论上能突破。

## 终止条件参考
看 v3-train-i147-sp/best.json bench 数字:
- 连续 3 iter 不进步 → 大概率 plateau,停
- testcase > 47(超过 V2 baseline)→ V3 features 验证成功
- testcase > 53 → 接近 V2 self-play 历史峰值
