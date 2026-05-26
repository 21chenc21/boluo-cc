# v0 Mac 工作流 — v16 AlphaZero (CURRENT)

完整 Mac 端流程: 拉 bundle → 跑训练 → 用 3player.html 真实对战观察 → 回 linux bench testcase → scp ckpt 给 prod.

---

## 0. Bundle 文件清单

scp 后 `~/agents/boluo-cc/v0-train/mac-bundle/` 里应有:

| 文件 | 用途 | 必需 |
|---|---|---|
| `ofc-train-mac` | 单步训练 (rollout / dataset 模式) | ✓ |
| `alphazero-train-mac` | AlphaZero 主 orchestrator (self-play→train→duel→promote) | ✓ |
| `duel-mac` | 两 ckpt 同手对战 (诊断用) | ✓ |
| `gen-oracle-mac` | v15 oracle dataset 生成 (legacy, 不推荐) | optional |
| `ofc-go-mac` | HTTP server (3player / fantasy / API) | ✓ (跑 UI 用) |
| `round-011-acc91-v9.json` | warm-start ckpt (R11 v9, 90-d, outDim=3) | ✓ (alphazero -warm-start) |
| `*.html / *.js / *.css` | 前端静态文件 (3player / fantasy / index) | ✓ (跑 UI 用) |

scp + 权限:
```bash
# Mac 端跑
scp -r 34.143.241.113:/home/chguang/boluo-cc/pineapple-ofc/v0/mac-bundle/ ~/agents/boluo-cc/v0-train/
chmod +x ~/agents/boluo-cc/v0-train/mac-bundle/{ofc-train-mac,alphazero-train-mac,duel-mac,gen-oracle-mac,ofc-go-mac}
mkdir -p ~/agents/boluo-cc/v0-train/{az-ckpts,games-db}
```

工作目录全程: `~/agents/boluo-cc/v0-train/`

---

## 1. AlphaZero 训练 — 一击必杀路线

### 完整启动命令

```bash
cd ~/agents/boluo-cc/v0-train && mac-bundle/alphazero-train-mac \
  -iters 30 \
  -games-per-iter 10000 \
  -mcts-sims 200 \
  -duel-games 100 \
  -gate-pct 55 \
  -train-bin mac-bundle/ofc-train-mac \
  -warm-start mac-bundle/round-011-acc91-v9.json \
  -ckpt-dir az-ckpts \
  -policy v0-az-r1 \
  -fan-bonus-qq 50 -fan-bonus-kk 80 -fan-bonus-aa 200 -fan-bonus-trips 250 \
  -foul-cost 20 \
  -phantom-opponents 2 \
  2>&1 | tee az-train.log
```

ckpt 输出: `~/agents/boluo-cc/v0-train/az-ckpts/iter-NNN.json`

### 配置说明

| flag | 推荐值 | 含义 |
|---|---|---|
| `-iters 30` | 30 | AlphaZero 迭代次数 (≥10 才看出趋势) |
| `-games-per-iter 10000` | 10000 | 每 iter self-play 局数 (与 sims 一起决定计算量) |
| `-mcts-sims 200` | 200 | MCTS 每决策 simulation 数; 训练用 200 inference 期可降到 100 |
| `-duel-games 100` | 100 | promotion gate 同手对战 game 数 |
| `-gate-pct 55` | 55% | 新模型 vs best 胜率 ≥55%(排除 draw)才 promote |
| `-warm-start ...v9.json` | r11 v9 | 初始 NN; outDim=3 → 第一 iter 训完会自动 promote 到 outDim=4 (warm-start chain) |
| `-fan-bonus-{qq,kk,aa,trips}` | 50/80/200/250 | 用户价值函数(AA fan 优先) |
| `-foul-cost 20` | 20 | 犯规惩罚 |
| `-phantom-opponents 2` | 2 | 3-player 模拟对手可见牌数(uniform 0..2) |

### 时间预估 (Mac M1+ 6-core)

| 阶段 | 单 iter 耗时 |
|---|---|
| Self-play 10000 games × 200 sims | ~3h |
| Train (load dataset → 40 epochs) | ~10 min |
| Duel new vs best (100 games × 200 sims × 2) | ~3 min |
| **合计** | **~3.2h / iter** |

30 iter ≈ **95h ≈ 4 天连续**. 中间 ckpt 可随时 scp 回 linux bench.

### 监控 — 关键日志

每 iter 开始/中段/结束会打这些行 (在 `az-train.log` 里 grep):

```
=== AlphaZero Iteration 5/30 ===
[az iter 5] self-play 10000 games (mcts-sims=200)...
[az iter 5] collected 320451 samples in 178 min
[az iter 5] training new NN → az-ckpts/iter-005.json
[az iter 5] trained in 8 min
[az iter 5] duel new vs best (100 games)...
[az iter 5] duel: new=58 best=39 draws=3  win-rate=59.8% (gate 55%)
[az iter 5] ✓ PROMOTE — best is now iter-005.json
```

或失败:
```
[az iter 5] duel: new=42 best=51 draws=7  win-rate=45.2% (gate 55%)
[az iter 5] ✗ DISCARD — best stays iter-003.json
```

特殊情况:
```
[az iter 1] outDim mismatch (new=4 vs best=3), AUTO-PROMOTE for warm-start chain
```
这是 iter 1 把 r11 v9 (outDim=3) 升级到 outDim=4 (含 policy head) 的特殊路径, 跳过 duel — 正常.

### 中途 scp ckpt 回 linux bench

每 iter 完都可以:
```bash
# Mac 端
scp ~/agents/boluo-cc/v0-train/az-ckpts/iter-005.json \
    34.143.241.113:/home/chguang/boluo-cc/pineapple-ofc/v0/az-ckpts/

# Linux 端
cd ~/boluo-cc/pineapple-ofc/v0
./run-testcase.sh az-ckpts/iter-005.json 3
```

### 头几 iter 异常正常

- iter 1 会自动 promote (outDim mismatch), **不要紧张**
- iter 2-5 testcase 可能先跌再升 — self-play 学新 policy, 跟旧 v14 的 testcase 不再对齐
- iter 10+ 才看出 真实 trend
- promotion fail 比 success 多 是常态; 只要每隔几 iter 有一次 promote 就 OK

### 中断后续训

如果 Mac 重启或主动 Ctrl-C:
```bash
# 找到最后 promote 的 ckpt
ls -lt ~/agents/boluo-cc/v0-train/az-ckpts/

# 接着跑 — 用最新 promote ckpt 当 warm-start
cd ~/agents/boluo-cc/v0-train && mac-bundle/alphazero-train-mac \
  -iters 25 \
  ...其他 flag 同上... \
  -warm-start az-ckpts/iter-008.json \
  2>&1 | tee -a az-train.log
```

---

## 2. 用 3player.html 真实对战观察 (重要)

**为什么需要**: testcase 只有 63 个, 即使 60/63 也只代表"用户标对的样本", 真实自对弈中会有其他异常 (诡异弃牌 / 错失 fantasy / 中道过强等). 3player.html 让你**看 AI 在随机牌局里的真实决策**.

### 步骤 1: 启动后端 server (任意时刻)

server 是个独立进程, 跟训练**互不影响**. 你可以一边训练一边开一个 server 看新 ckpt:

```bash
cd ~/agents/boluo-cc/v0-train && mac-bundle/ofc-go-mac \
  -addr :8001 \
  -static mac-bundle \
  -weights az-ckpts/iter-005.json \
  -db games-db/v0.db \
  2>&1 | tee server.log
```

flag 说明:
- `-addr :8001` — 监听端口 (跟训练无冲突)
- `-static mac-bundle` — 前端静态文件目录 (3player.html 在这里)
- `-weights ...` — 当前用的 ckpt; **想换 ckpt 就 Ctrl-C 重启 server**, 不需要改前端
- `-db games-db/v0.db` — 保存 episode 历史 (可选, 不传也行)

启动成功的日志:
```
[ofc-go] loaded weights from az-ckpts/iter-005.json
[ofc-go] solve cache enabled (max=2000)
[ofc-go] db opened: games-db/v0.db
[ofc-go] static dir: mac-bundle
[ofc-go] listening on tcp::8001
```

### 步骤 2: 浏览器打开

```
http://127.0.0.1:8001/3player.html
```

应能看到 3 个空棋盘 (P0/P1/P2) + 控制按钮.

页面顶部 round-info 会显示:
```
后端就绪 (level=medium, cache=0/2000) - 点"新对局"开始
```

如果显示 "⚠ 后端未就绪" → server 没起或端口冲突.

### 步骤 3: 配置 + 开局

页面顶部:
- **鬼牌数**: 选 `2 鬼` (训练默认 2 jokers)
- **AI 难度**: 选 `中 (~3s, 默认)` 起手; 想看慢一点选 `较强 (1.5x, ~10s)` 或 `更强 (2.0x, ~15s)`
- 点 **新 Episode**

界面 = 3 个棋盘并排 (P0/P1/P2 都是同一 AI), 下面有日志区显示每个玩家的决策.

### 步骤 4: 单步走 vs 自动跑

**单步**: 点 `下一轮` — AI 会决策当前轮 (R1 = 5张, R2-R5 = 3张), 摆完显示在棋盘. 看完想看下一轮再点.

**自动**: 点 `自动完成` — AI 一次跑完整 episode (5 轮 + 可能多手 fantasy 接龙). 中途可看每轮日志.

### 步骤 5: 看什么 (重点)

每个玩家日志列里会有:
```
R1 收到: Ah Ks Qd Jh 9c
放 头[Qd] 中[Jh 9c] 底[Ah Ks] (3.2s)
R2 收到: Td 5h 3s
弃 3s
放 头[Td] 底[5h] (1.1s)
...
```

**真实对战诊断点**:

1. **R1 摆牌合理性**:
   - AA / KK / QQ 上头? 是否抓 fantasy
   - 高对在哪 (中vs底)? 头不能比中强
   - 同色多张是否有 flush 苗子 (一般留底)

2. **discards 合理性** (R2-R5):
   - 弃的是无用牌 (rank 低且无 connectivity), 还是关键牌
   - "弃了一张能完 flush 的同色牌" → bug

3. **Foul 频率**:
   - 看右下角"犯规"红字; 自对弈 5% 内可接受
   - 太多 → ckpt 被 fan 诱导太狠

4. **Fantasy 触发**:
   - 顶 QQ/KK/AA/trips 显示绿"范特西"
   - 触发后**下一手开局自动发 14-17张**(前端处理), 看 AI 怎么 13-out-of-17 摆

5. **3 玩家可见牌 (deck-aware)**:
   - 页面下方有"场上可见牌 (XX张)"显示已发出的所有牌
   - AI 决策应能利用这个信息 (e.g. 看见对手 3 张同色 → 自己 flush 概率降, 应放弃)

### 步骤 6: 切 ckpt 对比

想对比 iter-005 vs iter-010 vs r11 v9 老 ckpt:

```bash
# 在 server 终端 Ctrl-C, 然后
mac-bundle/ofc-go-mac -addr :8001 -static mac-bundle \
  -weights az-ckpts/iter-010.json -db games-db/v0.db
```

刷新浏览器, **同样的牌局**(用相同 numJokers)对比手感. 注意每次"新 Episode"用的是不同 deck, 不是确定性比较 — 想确定性比较用 `duel-mac` (见 §3).

### 步骤 7: 加载 episode 历史 (可选)

server 用 `-db` 时, 每个完整 episode 自动存 SQLite. 想 SQL 查:
```bash
sqlite3 games-db/v0.db "SELECT id, length(json_extract(data,'$.rounds')) as rounds FROM games LIMIT 10;"
```

### 难度档位的实际含义 (服务端逻辑)

| 选项 | 后端等价 | 单决策耗时 |
|---|---|---|
| 低 | level=low (r1Mult=0.25) | ~1s |
| 中 | level=medium (r1Mult=0.5) | ~3s |
| 高 | level=high (r1Mult=1.0) | ~5s |
| 极快 0.5x | r1Mult=0.5 | ~1s |
| 较强 1.5x | r1Mult=1.5 | ~10s |
| 更强 2.0x | r1Mult=2.0 | ~15s |
| 极强 3.0x | r1Mult=3.0 | ~30s |

`r1Mult` 是 R1 simulation 倍数(R1 决策最重要, 所以单独控制).

### 常见问题

| 现象 | 原因 / 解 |
|---|---|
| 页面打开"⚠ 后端未就绪" | server 没起/端口冲突 → `lsof -i:8001`, kill 旧进程 |
| 摆牌很慢卡死 | level 选了 3.0x + R1, 单决策可达 30s, 等就好 |
| 对手棋盘乱码 / 鬼牌不显示 | 前端缓存; 浏览器硬刷新 (Cmd+Shift+R) |
| `-weights` 加载失败 | ckpt 路径错或 outDim/inDim 不匹配训练 → 看 server 日志 |

---

## 3. 同手对战诊断 — duel-mac

想对比 ckpt A vs ckpt B 在**完全相同 17 张牌**下谁分高 (排除 deck variance), 用 `duel-mac`:

```bash
cd ~/agents/boluo-cc/v0-train && mac-bundle/duel-mac \
  -ckpt1 az-ckpts/iter-005.json \
  -ckpt2 mac-bundle/round-011-acc91-v9.json \
  -games 200 \
  -mcts-sims 200 \
  -jokers 2 -phantom-opponents 2 \
  -fan-bonus-qq 50 -fan-bonus-kk 80 -fan-bonus-aa 200 -fan-bonus-trips 250 \
  -foul-cost 20
```

输出:
```
=== Duel Result ===
iter-005.json wins: 112 (56.0%)
round-011-acc91-v9.json wins: 78 (39.0%)
Draws:   10 (5.0%)
Avg score Δ (ckpt1 - ckpt2): +14.32
Win-rate of ckpt1 excl draws: 58.9% (gate 55% for promotion)
✓ ckpt1 PASSES gate vs ckpt2
```

注意:
- `-mcts-sims 0` → 用 ExpertPlace (无 MCTS), 看裸 NN 强度
- `-mcts-sims 200` → 跟训练 / 3player.html 一致
- 200 games duel 跑 ~6 min on M1

---

## 4. 回 linux bench testcase

scp ckpt 回 linux (**首次先在 linux 端 mkdir az-ckpts/**):
```bash
# Linux 端 (一次性)
ssh 34.143.241.113 mkdir -p /home/chguang/boluo-cc/pineapple-ofc/v0/az-ckpts

# Mac 端 (每次 scp ckpt)
scp ~/agents/boluo-cc/v0-train/az-ckpts/iter-NNN.json \
    34.143.241.113:/home/chguang/boluo-cc/pineapple-ofc/v0/az-ckpts/
```

linux 端 bench:
```bash
cd ~/boluo-cc/pineapple-ofc/v0

# 单 ckpt 详细报告 (含每 case 摆牌, 报告写到 test-reports/)
./run-testcase.sh az-ckpts/iter-NNN.json 3

# 多 ckpt 对比 (RUNS=N 跑 N 次取中位; bench-ckpts.sh 默认 PORT=18002)
RUNS=3 ./bench-ckpts.sh az-ckpts/iter-005.json az-ckpts/iter-010.json
```

testcase 解读:
- 当前 v14 baseline: 47/63 中位
- 目标: ≥50/63 突破天花板, ≥55/63 算 success
- **但**: 用户已说过"case 不完整, 真实对战还是有异常" → testcase ≥50 后, **以 3player.html 实战观察 + duel 胜率为准**, 别死磕 testcase 数字.

---

## 5. 故障排查

### 训练相关

| 现象 | 原因 / 解 |
|---|---|
| iter 1 直接 PROMOTE 不 duel | outDim mismatch auto-promote, 正常 |
| 每 iter 都 DISCARD | gate 太严 / self-play 数据不够; 看 self-play sample 数, 应 30-50万/iter |
| Self-play 慢于预期 | mcts-sims 200 × 13 决策/game × 10000 game ≈ 26M sim, M1 ~3h, M3 ~2h. 慢就降 sims=100 试 |
| Train 阶段崩 | 检查 `-train-bin` 路径, dataset 目录权限. `find az-dataset-iterN -name "*.jsonl.gz" \| head` 看文件 |
| Duel 阶段崩 | 通常是 ckpt outDim 不匹配; 看日志 `outDim mismatch` |

### Server / 3player 相关

| 现象 | 原因 / 解 |
|---|---|
| server 起不来 | 端口被占 → `lsof -i:8001` kill, 或换端口 `-addr :8002` |
| 浏览器空白 | `-static` 路径错 → 看 server 日志 "static dir: ..." 是否含 3player.html |
| /api/solve 500 | ckpt 加载失败 → server 启动日志找 LoadWeightsFromFile error |
| AI 永远 R1 摆同样 | cache 命中, 试 `curl http://localhost:8001/cache/clear` |

---

## 附录 A: v15 Oracle Workflow (legacy)

oracle dataset 已知有 optimistic bias (E[max] >= max[E]), 不推荐. 保留 reference.

```bash
# 步骤 1: 生成 dataset (~22h on M1)
cd ~/agents/boluo-cc/v0-train && mac-bundle/gen-oracle-mac \
  -num-games 2000 -workers 6 -jokers 2 \
  -r1-cap 0 -r1-multi-k 4 \
  -phantom-opponents 2 \
  -fan-bonus-qq 50 -fan-bonus-kk 100 -fan-bonus-aa 200 -fan-bonus-trips 400 \
  -foul-cost 20 \
  -out-dir oracle-dataset

# 步骤 2: 训练 (~30 min)
cd ~/agents/boluo-cc/v0-train && mac-bundle/ofc-train-mac \
  -dataset-dir oracle-dataset \
  -hours 0.5 -round-min 30 \
  -outdim 3 -h1 128 -h2 64 -indim 90 \
  -fan-w 0.40 -foul-w 0.10 -epochs 60 \
  -ckpt-dir ckpts -policy v0-v15-oracle
```

## 附录 B: v14 Rollout Workflow (legacy)

```bash
cd ~/agents/boluo-cc/v0-train && mac-bundle/ofc-train-mac \
  -hours 10 -round-min 30 -sims 200 -jokers 2 -workers 6 \
  -outdim 3 -h1 128 -h2 64 -indim 90 \
  -fan-w 0.40 -foul-w 0.10 \
  -fan-bonus-qq 50 -fan-bonus-kk 50 -fan-bonus-aa 200 -fan-bonus-trips 400 \
  -foul-cost 20 -phantom-opponents 2 -rollout-epsilon 0.1 \
  -weights mac-bundle/round-011-acc91-v9.json \
  -ckpt-dir ckpts -policy v0-v14
```

v14 stuck 在 testcase 47/63 中位 — 这是 AlphaZero 路线诞生的原因.

## 附录 C: CLI flag 速查 (rollout 模式 ofc-train-mac)

| flag | 含义 | 推荐 |
|---|---|---|
| `-hours` | 总训练时长 | 8-12 |
| `-round-min` | 每 round 多长 (min) | 30 |
| `-sims` | 每候选 N-rollout sim 数 | 200 |
| `-jokers` | 牌组鬼牌数 | 2 |
| `-workers` | 并发 worker | 4 (M1/M2) / 6 (M3 Pro+) |
| `-outdim` | 网络头数 (3=多头 4=含 policy) | 3 / 4 |
| `-h1` / `-h2` | hidden 层维度 | 128/64 |
| `-indim` | 输入特征维度 | 90 (v14+) |
| `-fan-w` / `-foul-w` | 多头 fan/foul BCE loss 权重 | 0.40 / 0.10 |
| `-policy-w` | 多头 policy BCE loss 权重 (outdim=4) | 1.0 |
| `-init-from-ckpt` | warm-start (alphazero 用, 复用 inDim/outDim) | (alphazero 自动) |
| `-warm-start` | 是否用前 round ckpt warm-start (round 2+) | true |
| `-weights` | silver-label policy 源 | round-011-acc91-v9.json |
| `-ckpt-dir` | ckpt 输出目录 | ckpts / az-ckpts |
| `-policy` | policy_version 标签 | v0-az-r1 |
