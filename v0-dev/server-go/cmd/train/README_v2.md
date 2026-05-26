# Features V2 训练指南

## 前置

V2 features 已实现在 `ofc/features_v2.go` (128 维), 通过 `BuildFeatures` 在 `inDim == 128` 时自动 dispatch (`trained_eval.go`)。

**训练参数总览**: outDim=4 (value + fan + foul + policy), 推荐 H1=256 H2=128 (V2 信息密度大于 V1 90 维)。

---

## 从远程同步代码到 Mac

**远程**: GCP `34.143.241.113` (linux), 路径 `/home/chguang/boluo-cc/v0-dev/`
**Mac 本地**: `/Users/Chen/agents/boluo-cc/v0-dev/`

### 推: 远程改了 → 拉到 Mac

⚠️ **不用 `--delete`** — 防止把 Mac 上本地训练 ckpt (远程没有) 误删!
⚠️ `ckpts*/` 通配符 — 排所有 ckpts 目录 (ckpts, ckpts-sims400, mac-ckpts 等)

```bash
rsync -avz \
  --exclude='.git' \
  --exclude='node_modules' \
  --exclude='server-go/ofc-go' \
  --exclude='server-go/train' \
  --exclude='server-go/case-train' \
  --exclude='server-go/init-mlp-v2' \
  --exclude='server-go/r1-debug' \
  --exclude='*.log' \
  --exclude='*.db' \
  --exclude='ckpts*/' \
  --exclude='mac-ckpts*/' \
  --exclude='samples/' \
  --exclude='checkpoints/' \
  --exclude='test-reports/' \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/ \
  /Users/Chen/agents/boluo-cc/v0-dev/
```

### 只拉关键改动 (更快)

```bash
rsync -avz \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/ofc/features_v2.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/ofc/features_v2_design.md \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/ofc/hard_rules.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/ofc/trained_eval.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/ofc/expert_place.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/cmd/init-mlp-v2/main.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/cmd/case-train/ \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/cmd/server/main.go \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/server-go/cmd/train/README_v2.md \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/case-test.js \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/case-test.sh \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/cases/all-tests-expanded.json \
  /Users/Chen/agents/boluo-cc/v0-dev/
```
(注意 mac 上目标目录结构要先存在, 必要先 `mkdir -p`)

### 回推: Mac 训完 ckpt → 拉回远程 (review/部署用)

```bash
rsync -avz \
  /Users/Chen/agents/boluo-cc/v0-dev/ckpts/v2-*.json \
  chguang@34.143.241.113:/home/chguang/boluo-cc/v0-dev/ckpts/
```

---

## Build (Mac 上)

⚠️⚠️⚠️ **每次 rsync 完代码必须重 build**! Go 是编译型, source 改了但 binary 不动 = 跑老逻辑。

```bash
cd /Users/Chen/agents/boluo-cc/v0-dev/server-go
go build -o train ./cmd/train
go build -o ofc-go ./cmd/server
go build -o init-mlp-v2 ./cmd/init-mlp-v2
go build -o case-train ./cmd/case-train
cd ..
```

## Rebuild verification (rsync 完务必跑)

```bash
# 看 binary 时间戳跟 source 是否同步
ls -la server-go/train server-go/ofc-go server-go/ofc/features_v2.go
# binary mtime 应 > features_v2.go mtime
```

---

## 训练命令

### Smoke (30 分钟, 验证 pipeline)

```bash
./server-go/train \
  -indim 128 -h1 256 -h2 128 -outdim 4 \
  -hours 0.5 -round-min 30 -sims 50 -workers $(nproc) \
  -ckpt-dir ckpts -policy v2-smoke \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  \
  -fan-bonus-qq 50 -fan-bonus-kk 100 -fan-bonus-aa 200 -fan-bonus-trips 400 \
  -foul-cost 20 -rollout-epsilon 0.10
```

### 全量 (4 小时, 推荐)

```bash
./server-go/train \
  -indim 128 -h1 256 -h2 128 -outdim 4 \
  -hours 4 -round-min 60 -sims 100 -workers $(nproc) \
  -ckpt-dir ckpts -policy v2-full \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  \
  -fan-bonus-qq 50 -fan-bonus-kk 100 -fan-bonus-aa 200 -fan-bonus-trips 400 \
  -foul-cost 20 -rollout-epsilon 0.10
```

### 长跑 (24 小时, AlphaZero 全量)

```bash
nohup ./server-go/train \
  -indim 128 -h1 256 -h2 128 -outdim 4 \
  -hours 24 -round-min 60 -sims 200 -workers $(nproc) \
  -ckpt-dir ckpts -policy v2-az \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  -fan-bonus-qq 50 -fan-bonus-kk 100 -fan-bonus-aa 200 -fan-bonus-trips 400 \
  -foul-cost 20 -rollout-epsilon 0.10 \
  > train-v2-az.log 2>&1 &
```

---

## 核心参数说明

### 网络结构 (一旦定了, ckpt 文件就固定; 后续训练必须用同样维度)

| flag | 默认 | V2 推荐 | 含义 |
|---|---|---|---|
| `-indim` | 90 | **128** | 输入维度 (128 触发 V2 features) |
| `-h1` | 128 | **256** | 隐藏层 1 |
| `-h2` | 64 | **128** | 隐藏层 2 |
| `-outdim` | 1 | **4** | 输出头 (4 = value/fan/foul/policy) |

### 训练时长

| flag | 含义 |
|---|---|
| `-hours` | 总训练小时数 |
| `-round-min` | 单 round 分钟 (1 round = 一次 self-play + 一次 MLP train) |
| `-sims` | 每候选 rollout 次数 (silver label 质量) |
| `-workers` | 并行 game 生成 worker 数 (= $(nproc)) |
| `-max-samples` | MLP 训练 batch 总 sample 上限 (default 500K) |
| `-epochs` | MLP 单 round 训练 epochs (default 80) |

### Loss 权重 (multi-head)

| flag | 默认 | 含义 |
|---|---|---|
| `-fan-w` | 0.15 | fan-head BCE loss 权重 (vs value MSE = 1) |
| `-foul-w` | 0.15 | foul-head BCE loss 权重 |
| `-policy-w` | 0.30 | policy-head BCE loss 权重 (outdim≥4 时) |

V2 推荐 `-fan-w 0.40` (fan signal 重要, 提权)。

### **Label 配置 (核心)** — Silver-label rollout 出的 mcScore 用这套 bonus 计算

```
mcScore = royalty_raw + fanBonus[tier] (if 进 fantasy 且不 foul) - foulCost (if foul)
```

| flag | 默认 | 含义 |
|---|---|---|
| `-fan-bonus-qq` | 50 | QQ fantasy 奖励 |
| `-fan-bonus-kk` | 100 | KK fantasy 奖励 |
| `-fan-bonus-aa` | 200 | AA fantasy 奖励 |
| `-fan-bonus-trips` | 400 | Trips fantasy 奖励 |
| `-foul-cost` | 20 | foul 扣分 |

**调高 fan-bonus** 让 MLP 学到 "fantasy lock 高 EV" 信号更强 (但可能导致激进 foul-risk play)。

### Rollout 探索

| flag | 默认 | 含义 |
|---|---|---|
| `-rollout-epsilon` | 0.1 | rollout 时 ε-greedy 探索率 (0=纯 MLP-greedy) |

V2 推荐保持 **0.10** — 防 silver-label 分布过窄, 训练数据多样性更好。

### Joker / Phantom

| flag | 默认 | 含义 |
|---|---|---|
| `-jokers` | 2 | deck joker 数 (0/2/4) |
| `-phantom-opponents` | 2 | 模拟对手数 (每 game ~Uniform[0, max], 训 deck-aware) |

---

## 训练日志关键行 (看健康度)

正常训练应看到:
1. `[init] fresh NewMLP inDim=128 h1=256 h2=128 outDim=4` — 起步
2. 每 round 结束打印:
   - `samples collected: N` (期望 N ≈ 5K-50K per round)
   - `YMean=X.X YStd=X.X` — 应在合理范围 (YMean ~50-150, YStd ~50-100)
   - `epoch K loss=...` — loss 应在 100 epoch 内降至 < 0.3 (head 0 normalized MSE)
3. 没看到 `nan` / `inf` / loss 爆炸

---

## 验证 (训完后 bench)

```bash
# 列出最新 ckpt
ls -lt ckpts/round-*.json | head -3

# Bench (with hard rules)
SEED=42 ./case-test.sh ckpts/<latest>.json cases/all-tests-expanded.json 2>&1 | grep "^=== 结果"

# Bench (NO hard rules — 看 MLP 真本事)
SEED=42 DISABLE_HARD_RULES=1 ./case-test.sh ckpts/<latest>.json cases/all-tests-expanded.json 2>&1 | grep "^=== 结果"
```

**期望**:
- 30-min smoke: 28-35/63 (没大幅好于 baseline 26/63 也 OK, 验证 pipeline 能跑)
- 4-hr full: 40+/63 (V2 features 真正发挥)
- 24-hr AZ: 50+/63 (目标)

---

## 错误排查

### 1. ckpt inDim mismatch

如果训了一半想换 inDim, ckpt 不兼容。要么删 ckpt 全新训, 要么写 inDim 转换工具 (V1→V2 W1 列零初始化扩展)。

### 2. Loss 爆炸 (>10000)

YMean/YStd 没设。`-weights` 没指 → 看 train.go 是否 fresh init 时初始化 YMean=80 YStd=100 (跟 init-mlp-v2 一致)。

### 3. case-train + V2 给负效果

正常 — case-train 只 134 样本太少, MLP 在 230+ R1 候选时 generalize 不够。需先 self-play 训出 base MLP, 最后再 case-train finetune。

### 4. PolicyBoost 不起作用

推理时设 `POLICY_BOOST=30` env var。仅对 outDim≥4 ckpt 有效 (head 3 存在)。Default 0 = 不用 policy head, 纯 value head 排序。

---

## 完整 pipeline 推荐流程

| Step | 命令 | 时长 |
|---|---|---|
| 1. Smoke | 0.5h train + bench | 30 min |
| 2. 全量 self-play | 4-24h train | 后台 |
| 3. Case finetune | case-train (mix-dataset 用 step 2 输出) | 1h |
| 4. Final bench + ship | bench + deploy | 30 min |
