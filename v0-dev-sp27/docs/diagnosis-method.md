# 诊断方法:为什么 NN 学不会某个 case（缺特征 / over-value / reward / case 本身）

> 2026-06-29 立。核心工具 `cmd/bench-cases -featdiff -labelprobe`。
> 适用：某个 case 纯 NN（`DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1`）一直 ✗，要判断**根因**，而不是盲目"再训几轮"。

## 铁律

**关键点学不会 ≠ 没训透。** 一个干净的关键决策，特征+label 对的话，NN 必然能学会。学不会 = **特征有问题 / over-value 特征压住 / reward 不够 / case 判断本身错**，四者之一。靠"多训"不解决前三类。

## NN 学的是 `特征 → label`

所以只有两个东西决定能不能学会：
1. **特征**能不能区分 exp 摆 vs AI 摆（且方向对、值合理）。
2. **label**（rollout silver-label EV）偏不偏 exp。

## 三步法（顺序很重要）

### 1. featdiff — 有没有区分特征
`bench-cases -featdiff`：dump AI首选 post-state vs 期望[0] post-state 的 `BuildFeaturesV3` 全维 diff。
- **有强信号特征（Δ>0.3）方向对** → 特征不缺，往下走。
- **全弱（<0.1）** → **缺特征**，加 dedicated 特征（如 #90 三条 rank、#23/#24 强成手放错行）。

### 2. label probe — 标签偏哪边（金标准）
`bench-cases -featdiff -labelprobe`：直接对两个 post-state 各跑 N=500 次 `QuickRollout`（gen 同 cfg，含 seed bonus / fan bonus），量**平均 EV**。
- **`Δ(exp-AI) > 0`（标签偏 exp）但 NN 选 AI** → **over-value 特征压住了区分特征**。NN 该学会，是某个 descriptive 特征（pair rank / PairToTrips / midDevelopHeadroom…）把错位的成手当好事。→ **找那个 over-value 特征 cap/contextualize（治得了）**。
- **`Δ(exp-AI) ≤ 0`（标签偏 AI）** → **NN 学得对！** 问题在 label 侧：
  - reward 不够（seed bonus / rank 奖太小没翻转 EV）→ 调大 reward。
  - 或 **case 判断本身错**（exp 其实没比 AI 好）→ 重审 case，别硬训。

### 3. 特征值 sanity — 区分特征值合理吗
即使特征区分了，值也可能是 bug 算出来的。手算盘面真值对一遍。典型 bug：
- `maxAchievableCmpCapped` 给单对 QQ6 算出"可成四条 QQQQ"（#124 前）→ botMax 虚高。
- 旧 bench 二进制没重编译 → 特征跑的是改之前的代码（**改完 feature 一定重建 bench-cases，否则 featdiff 是旧值**）。

## over-value 特征家族（反复踩，都是同一个病）

descriptive 特征只描述"某行成了什么/能成什么"，**不管这成手是不是倒置 / 杀种子 / 不是范**：

| case | over-value 特征 | 病 | 治法 |
|---|---|---|---|
| #124 | PairToTrips f108 | 中22→222 算1.0，但 222>底JJ99 倒置 | foul cap（升 trips 值 > 下行 max → 0） |
| #23/#24 | pair rank f103 + O-行序 | 中KK 当好对，没看 >底QQ 倒置 | 新 W2 dim164 强成手放错行 |
| #110 | 顶 pair rank f102 + midDevelopHeadroom | 鬼配77低对当好事，杀了种子 | （待修）鬼配低对不压种子 |

**通用治法**：over-value 特征要 **foul-aware / context-aware** —— 升级成手时若**倒置（>下行 max-achievable）或杀掉更优选项（种子/范）**，就别给正分。跟 W组 / topTripsSeed 同口径（用 `maxAchievableCmpCapped` 做 ceil）。

## te gap 方向（别再记反）

`bench-cases` featdiff 头部 `te gap = TrainedEval(AI) − TrainedEval(exp)`（main.go `gap := teAI - teExp`）。
- **正 = value-head 把 AI 摆评得更高 = NN 真偏好错摆**（`⚠️NN真错强偏好错摆`）。**不是** "value-head 对、policy 没跟上"。

## 用法速查

```bash
# 纯 NN，失败 case 出 featdiff + label probe
DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 \
  server-go-bin/bench-cases -ckpt <ckpt>.json -cases cases/game-cases.json \
  -workers 0 -featdiff -labelprobe

# 输出每个失败 case:
#   ===== 实战 N | te gap=+X [..] | .. =====
#   >> LABEL PROBE: exp=.. AI=.. Δ(exp-AI)=+X
#      📊标签偏 exp/AI → ..分类..
#   f[..] .. AI=.. exp=.. Δ=..  (各维 diff)
```
