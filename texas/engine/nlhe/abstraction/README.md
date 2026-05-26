# engine/nlhe/abstraction — Card abstraction for NLHE

## 设计目标

把 NLHE 信息集从 lossless O(10^14) 压到 O(10^6-10^7) bucket-based, 让 MCCFR blueprint 可训.

## Phase 1 (本 session): 仅 preflop

- 169 canonical preflop hand types (13 pairs + 78 suited + 78 offsuit)
- 用 E[HS²] (Monte Carlo) 估每种 hand 对随机 opp + 随机 board 的胜率
- 1-D K-means 聚类 169 → K buckets (默认 K=10)
- 保存 hand_type_idx → bucket_id mapping

## Phase 2 (下次): 后翻 (flop / turn / river)

- 每条街用 board-aware E[HS²] 或 OCHS (Opponent Cluster Hand Strength)
- (preflop_bucket, board_bucket_per_street) 联合 bucket
- 信息集压到 ~10^6

## Phase 3 (下次): engine 集成

- NLHE state 加 `AbstractInfosetID()` 方法, 跟 lossless `InfosetID()` 并存
- MCCFR 可选用 abstract 版
- 对比 abstract vs lossless 在 push/fold 上的 case-bench 通过率

## E[HS²] 解释

定义: 给定一手牌, 在所有可能 board completions 上, 计算每个完整 board 下的 hand strength (= 胜率 vs 随机 opp), 然后求**平方期望**.

- HS (终局): {0, 0.5, 1} — 输/平/赢
- HS² 内化了"方差敏感性" — 一手"高方差" (有时大赢有时大输) ≠ "稳赢" 即便平均胜率同
- Preflop 简化: HS² = HS (因 HS 在 showdown 是 0/0.5/1), 所以 E[HS²] = E[HS] = 胜率
- Postflop (turn/flop): HS 在更早 street 是连续值 [0,1], HS² 跟 HS 不等, 区分 "潜力大波动" 跟 "锁定边胜"

## 选 K (bucket 数) 的权衡

- K 小: blueprint 快收敛, 但 lossy 严重 (e.g. AA, KK 一桶 → push/fold 区分不开)
- K 大: 接近 lossless, blueprint 难训
- 经验值: 6-max NLHE preflop 用 ~169 (实际 lossless), postflop 用 200-500/街
- 我们 push/fold 用 10-20 看看
