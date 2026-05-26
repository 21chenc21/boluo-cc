# ofc-dev-v3 部署 / 运维手册

V3 NN (147-d features) Pineapple OFC AI 部署. 单 Go binary, 同进程 HTTP + 静态文件 + SQLite + LRU cache + NN inference.

**当前部署 (2026-05-25)**: sp19 iter-3 r1 太子 ckpt, md5 = `239f9a9b06f11034ecbe33889d456cb8`. 5 seed × 200 games 验证为 sp19 全 10 iter 最强 (fan 63.2, score 4884, foul 58.2).

---

## 1. 目录结构

```
ofc-dev-v3/
├── start.sh                # 启动脚本 (默认 :8002, cache 关)
├── server-go/              # Go source (从 v0-dev 同步)
│   ├── cmd/server/main.go
│   ├── ofc/                # NN inference + hard rules + features
│   └── API.md              # API 文档
├── server-go-bin/          # 编译好的 binary (start.sh 自动 build)
│   └── ofc-dev-v3
├── big-models/
│   ├── best.json           # 当前部署 ckpt
│   └── best.json.bak-*     # 历史 ckpt 备份
├── 3player.html            # 前端 (AI 类型 + AI 难度 dropdown)
├── game.js / app.js / solver.js  # 前端 JS
├── cases/                  # 测试 case
└── games.db                # sqlite 对局存储
```

---

## 2. Mac ↔ Linux rsync 同步

### 从 Linux 拉到 Mac (常用)

Mac 终端跑:

```bash
mkdir -p ~/agents/boluo-cc/ofc-dev-v3

rsync -avz \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='*.log' \
  --exclude='*.db' \
  --exclude='server-go-bin/' \
  --exclude='big-models/*.json.bak*' \
  chguang@34.143.241.113:/home/chguang/boluo-cc/ofc-dev-v3/ \
  ~/agents/boluo-cc/ofc-dev-v3/
```

排除项:
- `server-go-bin/`: Mac 自己 build, 不拉 Linux binary
- `*.db`: 运行时数据, 不同步
- `*.json.bak*`: 历史备份 ckpt 不需要
- `.git`: git 数据由 git 管, 不在 rsync 范围

### 从 Mac 推到 Linux (回传 ckpt 用)

Mac 终端跑:

```bash
rsync -avz \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='*.log' \
  --exclude='*.db' \
  --exclude='server-go-bin/' \
  ~/agents/boluo-cc/ofc-dev-v3/ \
  chguang@34.143.241.113:/home/chguang/boluo-cc/ofc-dev-v3/
```

通常用于把新训出的 ckpt 部署到 Linux 生产 server。

---

## 3. 启动 / 停止 server

### 默认启动 (调试期 cache 关)

```bash
cd ~/agents/boluo-cc/ofc-dev-v3  # 或 /home/chguang/boluo-cc/ofc-dev-v3
./start.sh
```

输出:
```
[start] ofc-dev-v3 on :8002, weights=big-models/best.json (V3 sp15 57/63), MCTS=on (level 生效), static=.
[ofc-go] loaded weights from /.../big-models/best.json
[ofc-go] solve cache disabled (SOLVE_CACHE_SIZE=0)
[ofc-go] listening on :8002
```

### 生产启动 (开 cache)

```bash
SOLVE_CACHE_SIZE=5000 ./start.sh
```

### 后台启动

```bash
nohup ./start.sh > /tmp/ofc-dev-v3-8002.log 2>&1 &
```

### 停止

```bash
kill $(pgrep -f "ofc-dev-v3.*8002")
```

### 健康检查

```bash
curl http://localhost:8002/api/health | jq
```

---

## 4. 切换 ckpt

```bash
cd ~/agents/boluo-cc/ofc-dev-v3
# 备份旧的
cp big-models/best.json big-models/best.json.bak-$(date +%Y%m%d-%H%M%S)
# 部署新的
cp /path/to/new-ckpt.json big-models/best.json
# 验证 md5
md5sum big-models/best.json  # 或 Mac: md5
# 重启
kill $(pgrep -f "ofc-dev-v3.*8002"); sleep 1; ./start.sh
```

历史 ckpt md5:
- sp17 iter-1 r1: `417d7c4e1dd29cc805815160dfcc32ea` (broken features, 59/63)
- sp18 iter-2 r1: `a8b182d2019ff83a5a763fdfbf986626` (broken features, 64/66)
- sp18 iter-3 r1: `fccfaad72e860880c0383e7ff25e7b56` (broken features, 64/66)
- **sp19 iter-3 r1**: **`239f9a9b06f11034ecbe33889d456cb8`** ⭐ 当前生产 (fixed features, 64/66, 5-seed 验证最强)

---

## 5. 前端使用

打开 `http://localhost:8002/3player.html`

### Dropdown 配置

**AI 类型** (MCTS 档位):
- ⭐ **纯 MLP** (default, 推荐): R1~280ms, 直接 NN value-head
- MCTS 低 (R1~7s, 不推荐): MCTS r1Mult=0.25, 实测退步
- MCTS 中 (R1~17s, 不推荐): r1Mult=0.5
- MCTS 高 (R1~56s, 不推荐): r1Mult=1.0

**AI 难度** (NN top-K sample):
- ⭐ **难度 1 - 最强** (default): top-1 deterministic, fantasy 63.2 / score 4884 / foul 58.2 (5 seed 平均)
- 难度 2 - 中等: R1 top-2 50/50 sample, fantasy 61.6 / score 4593 / foul 60.4
- 难度 3 - 简单: R1 top-3 sample, fantasy 56.0 / score 4271 / foul 60.2

R2-R5 永远 top-1 deterministic (保 endgame quality, 实测全 round sample 灾难性 -75% score).

### 生产推荐

最强 AI: **AI 类型 = 纯 MLP, AI 难度 = 1** (default, ~280ms / R1)

---

## 6. API 调用

完整文档: `server-go/API.md`

### 简版 curl

```bash
# 生产推荐 (pureMLP + 难度 1)
curl -X POST http://localhost:8002/api/solve \
  -H 'Content-Type: application/json' \
  -d '{
    "round": 1,
    "state": {"top":[], "middle":[], "bottom":[], "usedCards":[]},
    "dealt": ["Ks","Kh","5d","9c","2s"],
    "discardCount": 0,
    "jokerCount": 2,
    "pureMLP": true,
    "topK": 1
  }'
```

关键字段 (易忘):
- ⚠️ `jokerCount` 必传 (0=无鬼 / 2=Pineapple 标准 / 4=双副). 不传按 0, 2 鬼场景结果偏!
- ⚠️ `pureMLP: true` 生产强烈推荐 (跳 MCTS, 280ms vs 17s)
- `topK`: 1=最强 / 2=中等 / 3=简单 (R1 only sample)

---

## 7. Linux server 信息

```
host: chguang@34.143.241.113
path: /home/chguang/boluo-cc/ofc-dev-v3/
port: :8002
binary: server-go-bin/ofc-dev-v3
ckpt:   big-models/best.json (md5 239f9a9b06f11034ecbe33889d456cb8)
```

### SSH 登录

```bash
ssh chguang@34.143.241.113
```

### 远程查日志

```bash
ssh chguang@34.143.241.113 'tail -50 /tmp/ofc-dev-v3-8002.log'
```

---

## 8. 训练 / bench (开发用)

### 本地 build binary

```bash
cd server-go
go build -o ../server-go-bin/ofc-dev-v3 ./cmd/server
```

### bench 测 ckpt (并行, 3-30s)

Mac 上:
```bash
cd ~/agents/boluo-cc/v0-dev  # bench-cases 在 v0-dev/server-go-bin/
DISABLE_MCTS=1 ./server-go-bin/bench-cases \
  -ckpt path/to/ckpt.json \
  -cases cases/all-tests-expanded.json
```

### 200-game 对战 (3-metric duel, 2 min)

```bash
./server-go-bin/bench-3metric \
  -new path/to/new.json \
  -best path/to/best.json \
  -games 200 -seed 42
```

### 5 seed × 200 games (推荐, 真实平均)

```bash
for s in 42 100 200 300 400; do
  ./server-go-bin/bench-3metric -new NEW -best BEST -games 200 -seed $s | grep NEW_FAN -A6
done
```

⚠️ **不要只用 seed=42 promote**: seed=42 是 outlier, mac script 之前 promote 经常因为 seed=42 lucky 误立太子. 真实验证用 5 seed 平均 (见 [project_v3_2026_05_22_perf_pureMLP_lowpair] memory).

---

## 9. 已知 critical fix 历史

| 日期 | 修 | 影响 |
|---|---|---|
| 2026-05-20 | TrainedEval Round bug (trainedEvalImpl 重建 gs 丢 Round/LastDiscard/NumJokers) | 整个 V3 NN 训练/推理 features broken, 必须 rebuild |
| 2026-05-22 | RN soft helpers 收 postState 跳 Clone | R3 pureMLP 2.8s → < 100ms |
| 2026-05-22 | server 加 pureMLP 字段透传 (跨 fork 漏迁移) | R1 8.1s → 280ms (22x) |
| 2026-05-22 | 删 r1RuleLowPair_OnMid 漏洞规则 | J5599 必爆 → 不爆 |
| 2026-05-22 | jokerCount 透传 chain 5 处 + jokerRem 不再写死 2 | 0/4 鬼局 features 正确; MC rollout deck 真鬼数 |
| 2026-05-22 | features_v3 cap-aware fix (partial mid 假 cap 错砍 top KK) | sp18 broken-trained ckpt 直接用 fixed features +3 case 突破 |
| 2026-05-23 | 加 topK API 字段 (AI 难度 1/2/3) | 前端 deterministic vs stochastic 选择 |
| 2026-05-25 | 5 seed PK 验证: sp19 iter-3 r1 真太子, mac promote seed=42 outlier 假象 | 部署 iter-3 r1 不改 |

详情见 `~/.claude/projects/-home-chguang-boluo-cc/memory/MEMORY.md` 索引.

---

## 10. 常见运维问题

### Q: 改了 features_v3.go 后 NN 行为变了?

A: 修了 features 后, 训练 + 推理两边 features 必须同步. 如果只改推理, NN 在 broken features 上学过, 修后 inference features 跟 NN 内化的 mapping 不一致, 可能崩 (但 2026-05-22 cap fix 是例外: 大部分 dim 一样, 只少数失活, 不重训反而 +3 case).

### Q: cache 该开还是关?

A: 调试期 `SOLVE_CACHE_SIZE=0` (start.sh default), 生产 `5000`. cache key 含 jokerCount + topK + r1Mult + state, 区分各种配置.

### Q: 同 dealt 出同摆法?

A: pureMLP top-1 deterministic, 同 state 必同摆. 真实游戏 state space 巨大不会重复. 担心可设 `topK=2` 引入 R1 stochastic (代价: fantasy -2.5%, score -6%, foul +4%).

### Q: 怎么禁 MCTS 不让前端选?

A: 编辑 `3player.html` 删 dropdown 里 MCTS low/medium/high option, 只保 "纯 MLP".

### Q: 怎么测一个新 ckpt 是不是真比 best 强?

A: 5 seed × 200 games bench-3metric. **不要单 seed promote**:
```bash
for s in 42 100 200 300 400; do
  ./bench-3metric -new NEW -best CURRENT -games 200 -seed $s
done
# 5 seed 平均 fan + score 都 > current 才算真强
```

---

## 11. 紧急回滚

如果新 ckpt 部署后实战反馈差:

```bash
cd ~/agents/boluo-cc/ofc-dev-v3
cp big-models/best.json.bak-<TIMESTAMP> big-models/best.json  # 回滚到备份
kill $(pgrep -f "ofc-dev-v3.*8002"); ./start.sh
```

确保部署前永远备份: `cp big-models/best.json big-models/best.json.bak-$(date +%Y%m%d-%H%M%S)`
