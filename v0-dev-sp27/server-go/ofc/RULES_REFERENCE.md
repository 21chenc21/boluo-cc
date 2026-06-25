# 太子(prod sp26)规则清单 — 源码抽取 RULES_REFERENCE

> 自动从 `hard_rules.go` / `expert_place.go` 抽取 (2026-06-25). ⚠️ 仅 prod(太子)推理用;sp29 gen 纯NN 全关.

## 一、硬规则 (filter — 直接 reject 候选, 无分值)
| 轮 | 规则 | 触发 |
|---|---|---|
| R1 | r1RuleDealtBigPair_Top | dealt 有 AA pair → 必须 上顶 (锁 fantasy) 不处理 KK (要看 deck 还有没 A, 较复杂) |
| R1 | r1RuleSplitDoubleJoker | dealt 有 2+ jokers → 不能都堆同一行 (留 wild 灵活性) |
| R1 | r1RuleTopMustAllowFantasy | R1 摆完 top 3 张但不能 fantasy → reject |
| Rn | rnRuleNoDiscardJoker | 不弃 joker |
| Rn | rnRuleTopMustAllowFantasy | RN action 摆完 top 3 张但不能 fantasy → reject |
| Rn | rnRuleFantasyPossible | — |

## 二、R1 软规则 (bonus/penalty)
| 规则 | ± | 分值 | 触发/作用 |
|---|---|---|---|
| Foul | − | 20 | ============ FoulImminentPenalty (通用, R1-R5) ============ 2026-05-17: 老 R4FoulImminentPenalty 只覆 R4 mid+bot 满 + top 缺 1. 通用化: 任何 partial state 下检测 fou |
| R1JokerOnTopWithAA | − | 20 | ============ R1 soft bonus/penalty (替原硬 filter) ============ 2026-05-17: 用户要求把以下 R1 硬规则改成 score 调整, 让 prerank/MCTS 仍能 override  R1JokerOnTopWithAAPena |
| ConnectorSplit | − | 算式 | — |
| R1FourInRow | − | 算式 | R1 任意 row (mid/bot) 4 张或 5 张全堆, 强 draw / 同 rank 集中 除外 → 扣分 例外 (4-row): - 4-flush (4 同色) 或 ≥4-straight (4 连张): 强 draw - ≥3 同 rank (trips 或 quads, 同 row |
| R1IncoherentRow | − | 算式 | R1 mid/bot 行 ≥3 张, 但既无 pair/trips, 又非纯色, 也非 ≥4-straight 潜力 → -2 即 "毫无成型潜力" 的杂烩行 |
| R1TopNonAKX | − | 算式 | R1 top 含非 A/K/joker 卡 → 每张 -5 (2026-05-17 加重 2→5) 例外: 该 rank 在 usedCards 已 ≥3 张 (deck-aware, 余 ≤1) — 此时凑 trips fantasy 可行 joker 不算 (wild) Pattern 4 修复 |
| R1HighCardBotKicker | − | 2 | R1: 底恰成一对 + 中道有松高牌 > 底kicker → 该高牌该当底对kicker, 罚1. 2026-06-17 用户(局96 seed11 FOUL): 发KK+Q23, AI Q埋中(中Q-2)+3当底kicker(KK3); 应 Q进底当kicker(KKQ强)+23留中. 高牌进底梯 |
| R1LoneKingOnTop | − | 2 | R1 顶道放孤 K(非成对/无鬼配)-2 (用户 2026-06-18: "KQ上头-2"). K 上顶是弱范种子(配 KK 概率低), 进底/中当高张更值; 只 A 上顶才强(R1SingleAOnTopBonus). (Q 上顶已被 R1TopNonAKXPenalty 罚 +5, 故只补 K. |
| R1MidOverBotCard | − | 2 | R1 1-2-2 结构, 中道任一真牌 > 底道任一真牌 → +2. 强制 4 张非顶牌里最高 2 张进底(底=最强行). 治实战54: 8 该进底凑(9,8), 不是 5 进底. 比 RnMidPlacedOverBotPlaced(只比 maxMid vs maxBot)更严: 逐张比, 防"高 |
| MidPlacedOverBot | − | 2 | R1-R4: 本轮"放中道的最大牌 > 放底道的最大牌" → 大牌放中了 → 底<中 → 罚2. 2026-06-17 用户(局80 R2): 放中道8h > 放底道5d → 该把大牌放底. 底道有花/顺draw 或对/成手 → 豁免. 跟 局96 R1HighCardShouldBeBotKick |
| R1JokerWithAOnTop | + | 16 | dealt 含 X + 单 A (非 AA pair) AND 二者都在顶 → +10 (替 r1RuleJokerWithA_OnTop; 鼓励配 AA fantasy) |
| R1SingleAOnTop | + | 10 | dealt 单 A 无 joker 无 AA pair, A 上顶 → +10 (替 r1RuleSingleA_OnTop) |
| R1FlushGroupOnBot | + | 5 | — |
| R1SingleJokerNoAOnTop | + | 5 | dealt 恰好 1 张 joker 且无 A, joker 放顶 → +5 用户 2026-06-03 (ypk-178127178-8 R1 [8h X 7c Qc 3c]): 单鬼无 A 时 NN 错把鬼埋中道配低张 (88), 应把鬼留顶 (追范/保持灵活). 无 A 限定避开 "鬼+A 配 |
| R1BotDraw | + | 2/5 | R1 底道有成型 3-card draw → +2. 鼓励把 draw 放最强行(底). 治实战44: 567 该整组进底(顺draw)别拆. 覆盖 3条 + 3连张(5-window内3+张). 3花已由 R1FlushGroupOnBot(+5)管, 不重复. gap 小时(44=0.34)+2 |
| R1BigPairOnBot | + | 2 | R1 大对(≥T)放底道 +2 (用户 2026-06-18 局32). 底是最强行, 大对锚底稳行序 + 留中道/顶道灵活. 小幅 prior, 只 tip 近平局(局32 TT→中 vs →底 gap1.7), 不压 fantasy/flush 强信号. 只 R1. |
| R1SameSuit | + | 算式 | — |

## 三、Rn 软规则 (bonus/penalty)
| 规则 | ± | 分值 | 触发/作用 |
|---|---|---|---|
| Foul | − | 20 | ============ FoulImminentPenalty (通用, R1-R5) ============ 2026-05-17: 老 R4FoulImminentPenalty 只覆 R4 mid+bot 满 + top 缺 1. 通用化: 任何 partial state 下检测 fou |
| JokerSameRow | − | 10 | R2-R5 软 penalty (+10): post-action mid 或 bot 任一行含 ≥2 鬼牌 → 罚 10. 鼓励 X 分散 (不堆 mid 或 bot), 给 top fantasy lock 留余地. 2026-06-01 加: ypk-98042186-4 R2 case — |
| LoneAceMidJokerTop | − | 8 | R2-R5 软罚 (+8 penalty): 鬼在顶 + 本轮往中道塞 1 张孤 A (中道最终恰 1 张A, 不成对). 2026-06-05 (实战16): 鬼+Q在顶升AA时, 废A应放底(留中道干净凑两对托顶AA), 放中是死A高张 (没第2张A可配对) → 堵两对位 + 顶AA托不住. 正 |
| TopTripsOver | − | 10.0/16/18 | 顶把"已锁的 QQ+ 范对子"升成三条, 但中道现成牌型托不住该三条 → foul 风险, 罚. 2026-06-13 (ypk-70123850-2 R4): pre-top KK (已锁 15张范, 且 KK 对 < mid 222 三条 = 安全) + 发 Kd → 凑 KKK, KKK 三条 |
| QuadsJokerWaste | − | 算式 | 某行 4 张真同 rank (真四条) 且同行有鬼 → 鬼废成 kicker → 罚 (ypk-94634314-14). |
| MidExceedsBot | − | 18 | 候选造成"中道成牌 > 底道成牌"(违反 bot≥mid) → foul 倒置, 罚. 通用. 2026-06-13 (ypk-88080714-8 R2): bot=QQ, AI 把 KK→中 → 中KK > 底QQ = 倒置必犯规结构. 本质是"中比底大"(不依赖 top); KK 该放底跟 Q |
| HighCardWrongRow | − | 4 | R2-R5 本轮"中底各放1张真牌", 两行都死(无对无花draw无顺draw), 且放中的牌 rank > 放底的牌 → 高牌该进底(bot≥mid 梯度), 罚. 2026-06-17 实战16(ypk-111870282-16): 8c→中 5s→底 错(8>5 高牌埋中), 该 5→中 8→ |
| MidHighOverBot | − | 10 | 用户提案 (2026-06-14): 本轮往中道放的真牌 > 底道锚 且 底未成三条 → 罚. 含义: 大于底锚的高牌该进底道(强行), 别浪费在中道. 底锚 = 底成对→最高对子rank; 底未成对→底max真牌. ypk-459082-15 R2 (底99, Jd 进中) / ypk-45908 |
| MidPlacedOverBot | − | 2 | R1-R4: 本轮"放中道的最大牌 > 放底道的最大牌" → 大牌放中了 → 底<中 → 罚2. 2026-06-17 用户(局80 R2): 放中道8h > 放底道5d → 该把大牌放底. 底道有花/顺draw 或对/成手 → 豁免. 跟 局96 R1HighCardShouldBeBotKick |
| LoneSubQTop | − | 2 | 太子专属 (2026-06-14, 实战28 ypk-185336138-28): 本轮起手往**空顶**放 1 张 **≥中道最大真牌 且 <Q** 的牌, 而底道已成对+未满 → 罚 -2. 该牌在顶**零范路径 + foul险**: ① 自配对 < QQ 不是对范; ② 升三条范又会犯规(需  |
| RedundantHighLockedAA | − | 算式 | — |
| DeadLowKickerFanTop | − | 4.0 | 顶已是范级对(QQ+, 含鬼配A/K/Q)时, 本轮把**低死 kicker(≤9)** 拍上顶把顶填满(2→3张) → 罚 -2.5. 范早锁死, 第3张低牌零增益; 它进底(配对/凑葫芦)或中(成对种子)更值. 局30 R3 (seed99): 顶[Ad X]=AA, 2h该进底配999凑999 |
| MidKickerBotFlush | − | 算式 | 2026-06-17 实战1 (sp26 value-head弱, 太子原生留花). 用户思路: 中道恰成三条(死kicker场景) + 本轮往中塞的非鬼牌 C, 若 C 对底道有价值 (底靠它能成花/成顺) → 罚3 (C 在中是死kicker浪费, 该进底). ⚠️ stopgap, 重训让 v |
| TopPairOvercommit | − | 6 | 2026-06-17 (std63-61): 本轮把顶做成 QQ+/KK made对(非AA), 且牌堆 A+鬼 ≥3 (升 AA 有望, 该留顶等 AA 范 > KK 范) → 罚6. 安全阀: 中底都已稳托住该对 = 锁对是稳范, 不罚. |
| SingleJokerTopA | + | 8 | R2-R5 软 bonus (+8): 孤鬼(或鬼+sub-Q)在顶时, 放 1 张 A 上顶追 AA 范. 2026-06-05 (ypk-32571722-17 R3: top=[X] 发 3A, NN 误埋 AA→中 而非单 A 上顶追范). 触发:  ① pre-top 有鬼, 且"鬼能配出 |
| TopTripsFan | + | 5 | top 凑成 foul-safe 三条 (re-fan 锚 + 最高范 tier) → +bonus. 2026-06-11 (ypk-102367562-12 R4): top=[X X 3c]=333三条 vs top=[X X Ts]=AA对(被 mid 888 cap 住). NN valu |
| JokerAOnTop | + | 16 | 本轮鬼+A 上顶锁 AA 范 → +10. 补 NN 对"鬼+A 锁顶范"的系统性低估. ⚠️ 这类软规则是针对**当前太子 NN 的具体偏差**校准的 (magnitude/触发都依赖太子的 te).  换模型 (尤其 sp24 激进版重奖 AA/范, NN 偏好会变) → 整套软硬规则可能需要* |
| MidPairTwoPair | + | 3 | 本轮往中道放的真牌配对中道已有单张, 把中道做成两对 → 奖. 2026-06-17 用户明确要求 (实战14 ypk-111870282-14: 7s 配中道 7d 成 4477 两对). ⚠️ 用户要这个**即便底道托不住** — 单人 solver 中道两对 royalty=0 + 底 gut |
| MidDrawFace | + | -2 | — |
| BotDrawFace | + | 2 | 行全单张(无对无鬼) + 有顺面(3+在5-rank窗口)或花面(3+同花) → +2. 2026-06-17 用户准则: "都是单张有顺面花面加分". 单张攒顺/花draw 有价值(顺/花royalty + outs多). 用于中道(实战11: 4s接3-4-6顺托顶范)和底道(局23: 底8-9 |
| MidTwoPairBotDraw | + | 2 | RnMidDrawFaceGated — 中道draw面奖, 但本轮把"能配中道对子的真牌"放到了中道以外(顶/底), 留中道弱draw → 不奖. 2026-06-18 (seed99局25 R4): 中[7d 8d 5c], 8h能配8d成88(中道能成的最大对), prod却8h→顶留中道5- |
| JokerHighSeedTop | + | 4 | R2-R4 本轮鬼→顶 + post-top = 鬼+恰1张真≥Q(K/Q, 种 KK/QQ范) + 顶未满 → +4. 2026-06-17 局70: 鬼+Kh→顶 博 KK 范种子(鬼灵活, 可配未来A成AA), value head 偏埋鬼(gap~3) → 奖翻过. A 走 RnJokerA |
| AceToMidVsTopAA | + | 4 | 顶成AA + 本轮Ace→中道 + 中道无≥T高牌杂(保轮子/低位向) + 中道还没成对 → +4. 2026-06-17 局91: 顶AA成+底顺成, 中道必须≥AA否则foul; Ad该进中(凑中AA 或 A-4轮子顺 A2345 压顶AA), 别弃Ace塞Kc(K高压不过AA必foul). v |
| MidMakeTwoPair | + | 8 | 本轮把中道做成 ≥两对, 且底道 > 中道 (维持 bot>mid 不倒置) → +8. 通用. 底已比中强时(底三条/顺/更高两对), 中凑两对是安全的强中. 用 partialEvalTP(两对感知). 底 ≤ 中 不奖 → 防 case9(弃鬼凑中两对) / 防 mid>bot 倒置. 202 |
| PreserveTopAA | + | 2 | top 恰 鬼+1真(QQ/KK, 已是范对)且留 1 空位 + deck 还有 A 或鬼 (可补上顶升 AA/KKK) → +2. 鼓励"K上头留空位等A 升 AA 范", 别用废 kicker 填满 top 锁死 KK. 2026-06-14 (ypk-185336138-22 R2: top= |
| R4TripsFanReach | + | 算式 | 正向版 (用户 2026-06-22, 实战110/48; 取代旧 RnLowCardOnLockedTop 罚版): R4 + 中道恰=三条(rank M) + 底>中(盘锁死) 时, 若顶 placement 后**仍能成 ≤M 的合法 trips 范** (顶范种子没被占死) → +25. 奖 |
| AceToTopSeed | + | 8 | — |
| R2BotPairMidDraw | + | 3 | RnAceToTopSeedBonus — RN 本轮把单 A 放上"非范级顶"(<QQ) → +8 (seed AA 追范, 别弃/埋A). 局56 R3 (s99): 顶[Qd], As该→顶做AQ(后续Ac来成AA范). 范特西率优先. 守护: 顶pre非范级对(QQ+已锁不需seed) +  |

## 四、支配过滤 (expert_place.go)
**硬支配** (2026-06-14): 同顶+同中 → 底道成手严格大者支配, 删底小的. 仅 `bottomDomScore≥100`(made-pair+) 且底满5张; 底花/底顺 draw 永不删. 治89X (888三条>88/99两对 value-head低估). **无分值,直接删候选**.

**软支配** (2026-06-18): `domSoftPen = domSoftScale(2.0) × dom分差`, 单候选封顶 `domSoftCap=3.0`. sibling-relative 小幅 tie-break, 不强删.

## ⚠️ 维护提醒
- 软规则 magnitude 是按**太子偏差**校准 (模型特定). 换模型 (sp29) 必逐条 on/off 重评.
- 改规则必同步本文件 (用户准则: 改规则/feature 必更新 reference).
