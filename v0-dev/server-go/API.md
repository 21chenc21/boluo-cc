# V3 Solver API

OFC Pineapple AI 解算服务 — 单 Go binary (`ofc-go`), 同进程: HTTP API + 静态文件 + SQLite DB + LRU cache + V3 NN inference.

**当前状态** (2026-05-23): V3 NN (147-d features), **sp19 iter-3 r1 太子** ckpt (fixed-features-trained) 部署在 `ofc-dev-v3:8002`. V2/v7_fan 已退役.

**部署 ckpt**: `big-models/best.json` md5 = `239f9a9b06f11034ecbe33889d456cb8` (sp19 iter-3 r1).

## Base URL

```
http://localhost:8002/        # ofc-dev-v3 生产 (V3 NN, 147-d)
```

API endpoints 在 `/api/*`, 静态文件 (3player.html 等) 在 `/`.

## 端点一览

| 路径 | 方法 | 用途 |
|------|------|------|
| `/api/solve` | POST | 主解算 (game-friendly) |
| `/solve` | POST | 同上但用 raw `rolloutConfig` (parity test) |
| `/api/health` | GET | 健康 / 监控 |
| `/health` | GET | 同上 (兼容) |
| `/api/games` | POST/GET | 保存/列对局 |
| `/api/games/:id` | GET | 取对局详情 |
| `/cache/clear` | GET | 清 LRU cache |
| `/*.html`, `/*.js`, `/*.css` | GET | 静态文件 (前端) |

---

## `POST /api/solve` — 主解算

请求 body:

```json
{
  "round": 1,                       // 必填. 1=R1 摆5, 2-5=R2-5 弃1摆2, 99=fantasy
  "state": {                        // 必填
    "top": ["Ks", "Kh"],
    "middle": ["5d"],
    "bottom": [],
    "usedCards": ["Js", "Qc"]
  },
  "dealt": ["Ac", "9h", "3s"],      // 必填. R1=5 张, R2-5=3 张, fantasy=14-17 张
  "discardCount": 1,                // R1=0, R2-5=1, fantasy=N-13

  // === AI 类型 (MCTS 档位, 三选一; 生产唯一推荐 pureMLP) ===
  "level": "low",                   // 'high'/'medium'/'low' MCTS 档位 (实测都退步, 不推荐生产)
  "r1Mult": 2.5,                    // 或直接传数值 (覆盖 level)
  "pureMLP": true,                  // ⭐ true → 跳 MCTS, 仅 NN value-head (~250-370ms). 生产唯一推荐.

  // === AI 难度 (top-K sample, 2026-05-23 新增) ===
  "topK": 1,                        // 1=最强 top-1 deterministic (default) / 2=中等 R1 top-2 sample / 3=简单 R1 top-3 sample. R2-R5 永远 top-1 保 endgame quality.

  // === 鬼牌局 ===
  "jokerCount": 2,                  // 本局总鬼数. 0=无鬼 / 2=标准 Pineapple / 4=双副. 默认 0!

  // === 可选 ===
  "mode": "normal",                 // "normal" (默认) | "fantasy"
  "gameId": "abc",                  // 写 solve_log 关联
  "player": 0                       // 0/1/2
}
```

> **⚠ jokerCount 必传**: 默认 0, 但 Pineapple OFC 标准是 2 鬼局. 0/2/4 鬼局 NN feature 计算不同, 不传则按 0 算 → 2 鬼局推理结果偏差大. 2026-05-22 修复跨 fork 漏迁移 bug.

> **⚠ pureMLP 用途**: 跳 MCTS (R1 8s → 280ms), 测 NN 直接能力. 生产推荐 `pureMLP=true` (~280ms/R1). `level` 在 pureMLP 模式下是占位.

> **⚠ topK = AI 难度**: 1=最强 (deterministic top-1, 实测 5 seed × 200 games 平均 fantasy 63.2 / score 4884 / foul 58.2), 2=中等 (R1 top-2 sample, fantasy 61.6 / score 4593 / foul 60.4), 3=简单 (R1 top-3 sample). 只 R1 用 sample, R2-R5 永远 top-1 (实测全 round sample 灾难性 fantasy -42 / score -75%). 默认 1.

> **AI 类型 (level) vs AI 难度 (topK) 区分**:
> - **AI 类型** = MCTS 档位 (pureMLP / low / medium / high). 实测 MCTS 都退步 6-17 case, **生产只用 pureMLP**.
> - **AI 难度** = NN top-K sample. 给前端做强弱档. topK=1 最强, topK=2/3 给玩家"赢面".

响应:

```json
{
  "layout": {
    "top": ["Ac"],
    "middle": ["9h"],
    "bottom": []
  },
  "discards": ["3s"],
  "elapsedMs": 1234,                // Go 解算耗时 (含 cache lookup)
  "totalMs": 1240,                  // 端到端
  "level": "high",                  // "custom" = 直传 r1Mult
  "r1Mult": 2.0,
  "cached": false                   // true → LRU 命中, elapsedMs=0
}
```

错误响应:
- `400 dealt required` / `400 bad request: ...`
- `500 fantasy: no layout (Go all phases failed)` — 极端 case

---

## `POST /solve` — Raw 解算 (parity test)

直接接 Go 内部形状 (含 `rolloutConfig`):

```json
{
  "state": { "top": [], "middle": [], "bottom": [], "usedCards": [], "round": 1 },
  "dealt": ["Ks","Kh","5d","9c","2s"],
  "discardCount": 0,
  "mode": "normal",
  "jokerCount": 2,
  "rolloutConfig": { "r1Mult": 0.5 }
}
```

响应外层包 `ok`:

```json
{
  "ok": true,
  "layout": { "top": [...], "middle": [...], "bottom": [...] },
  "discards": [...],
  "elapsedMs": 800,
  "cached": false
}
```

`/solve` 给 `parity-*.js` 测试 + 老 client; 业务调用一律 `/api/solve`.

---

## `GET /api/health`

```json
{
  "ok": true,
  "totalSolved": 142,
  "avgElapsedMs": 850,
  "levels": {
    "high":   { "r1Mult": 1.0 },
    "medium": { "r1Mult": 0.5 },
    "low":    { "r1Mult": 0.25 }
  },
  "defaultLevel": "medium",
  "cache": {
    "enabled": true,
    "size": 47, "max": 5000,
    "hits": 28, "misses": 114,
    "hitRate": 0.197
  },
  "solveLog": { "mode": "off", "rate": 0.1, "retain": 50000, "count": 0 }
}
```

cache 关 (`SOLVE_CACHE_SIZE=0`) 时 `cache: { enabled: false }`.

---

## `POST /api/games` — 保存对局

```json
{
  "id": "abc123",                   // 可选, 不传自动生成 16-hex
  "jokerCount": 2,
  "players": [...],
  "rounds": [...],
  "scores": [...],
  "meta": {...}
}
```

响应: `{ "id": "abc123", "ok": true }`.

## `GET /api/games?limit=20` — 列对局

```json
{
  "games": [
    { "id": "...", "created_at": 1777618839323, "joker_count": 2, "scores": {...} }
  ]
}
```

`limit` 默认 20, 上限 200.

## `GET /api/games/:id` — 详情

返回完整对局, 不存在 `404 {"error":"not found"}`.

---

## `GET /cache/clear` — 清 LRU

```json
{ "ok": true }
```

或 `{ "ok": false, "error": "cache disabled" }` 如果 `SOLVE_CACHE_SIZE=0`.

---

## 性能 / 模式选择 (2026-05-23 实测)

| 模式 | 设置 | R1 耗时 | testcase 63 | 备注 |
|---|---|---|---|---|
| **pureMLP** ⭐ | `pureMLP:true` | ~275ms | **61/2w/0f** | **生产唯一选择**, NN value-head top-1 |
| MCTS low | `level:"low"` | ~7.7s | (未实测此档) | r1Mult=0.25 |
| MCTS medium | `level:"medium"` | ~17s | **48/4w/11f (-13)** | r1Mult=0.5, **MCTS 反而退步 13 case** |
| MCTS high | `level:"high"` | ~56s | **54/2w/7f (-7)** | r1Mult=1.0 |
| MCTS 2x | `r1Mult=2.0` | ~110s | 55/2w/6f (-6) | sims 加多分数收敛但**仍 < pureMLP** |

> **重要**: sp19 NN 太强, MCTS rollout (ExpertRollout policy) **反而拖后腿**. MCTS sims 越多越接近 pureMLP, 但永远追不上. 生产**只用 pureMLP**.
>
> sp19 iter-3 r1 + fixed features bench: standard 61/2w/0f + gamecase 3/3 = **64/66 testcase**. 实战 200 局对打 fantasy 63, foul 58, avg score 5097 (比 sp18 太子 +8.5%).
>
> MCTS 保留仅供: (1) 调试看 NN 错估, (2) NN ckpt 退化时兜底, (3) 训练 self-play silver-label gen.

---

## Cache key (2026-05-22 加 jk)

```json
{ "t": sortedTop, "m": sortedMid, "b": sortedBot, "u": sortedUsed,
  "r": round, "d": sortedDealt, "dc": discardCount, "mo": mode,
  "rc": { "r1Mult": ... }, "jk": jokerCount }
```

0/2/4 鬼局击中不同 cache. 命中 → `cached: true`, `elapsedMs: 0`.

---

## Mode 字段

| mode | 说明 |
|---|---|
| `normal` | R1-R5 标准摆牌, 三阶段 MCTS + V3 NN (147-d features) prerank |
| `fantasy` | FL 范特西摆牌 (14-17 张 1 次摆完). reFan/nonReFan 锚直枚举 + beam search + V3 NN eval |

---

## Card 编码

| 字符串 | 含义 |
|---|---|
| `As` | 黑桃 A |
| `Kh` | 红桃 K |
| `Td` | 方块 T (10) |
| `2c` | 梅花 2 |
| `X` | 鬼牌 (joker, 单一) |
| `Xj0`, `Xj1` | 多张鬼牌时, jid 区分 |

Suits: `s`=♠, `h`=♥, `d`=♦, `c`=♣
Ranks: `2 3 4 5 6 7 8 9 T J Q K A`

---

## 配置 — 命令行 flag

```bash
ofc-go -addr=:8002 -static=. -db=games.db -weights=big-models/best.json
```

| Flag | 默认 | 说明 |
|---|---|---|
| `-addr` | `:8001` | TCP 监听 |
| `-unix` | (空) | Unix socket 路径 (覆盖 -addr) |
| `-static` | 自动 | 前端文件根目录 |
| `-db` | `games.db` | sqlite 文件路径. `-db ""` 关 DB (`/api/games*` 返回 503) |
| `-weights` | (embedded) | 覆盖 embedded weights, 加载指定 V3 ckpt JSON |

## 配置 — 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `SOLVE_CACHE_SIZE` | 2000 | LRU 容量, 0=关 (调试期推荐 0) |
| `DEFAULT_LEVEL` | medium | 不传 level/r1Mult 时的默认档 |
| `HIGH_MULT` | 1.0 | 覆盖 high 档 r1Mult |
| `MEDIUM_MULT` | 0.5 | 覆盖 medium 档 r1Mult |
| `LOW_MULT` | 0.25 | 覆盖 low 档 r1Mult |
| `DISABLE_MCTS` | (off) | 设非空 → 全局跳 MCTS, ExpertPlace5/3 直接 prerank top-1 (per-request `pureMLP` 同效果) |
| `DISABLE_HARD_RULES` | (off) | 设非空 → 跳过所有硬规则 filter (调试用) |
| `POLICY_BOOST` | 0 | head3 policy logit 加权到 prerank score (默认 0=不用 policy head) |
| `MCTS_SIMS_MULT` | 1.0 | MCTS sims 倍率 (越大越精越慢) |
| `MCTS_PRERANK_W` | 0 | stage1 = w * prerank + (1-w) * rollout_mean. 1=纯 prerank skip rollout |
| `SOLVE_LOG` | off | `off` / `sample` / `on` |
| `SOLVE_LOG_RATE` | 0.1 | sample 模式采样率 |
| `SOLVE_LOG_RETAIN` | 50000 | solve_log 表保留最近 N 条 |

> ofc-dev-v3 `start.sh` 当前设 `SOLVE_CACHE_SIZE=0` (调试期), 生产前改回 5000.

---

## 调用示例

### curl

```bash
# pureMLP (生产推荐, ~250ms R1)
curl -X POST http://localhost:8002/api/solve \
  -H 'Content-Type: application/json' \
  -d '{"round":1,"state":{"top":[],"middle":[],"bottom":[],"usedCards":[]},"dealt":["Ks","Kh","5d","9c","2s"],"discardCount":0,"jokerCount":2,"pureMLP":true,"level":"low"}'

# MCTS medium (生产强化, ~5s R1)
curl -X POST http://localhost:8002/api/solve \
  -H 'Content-Type: application/json' \
  -d '{"round":1,"state":{"top":[],"middle":[],"bottom":[],"usedCards":[]},"dealt":["Ks","Kh","5d","9c","2s"],"discardCount":0,"jokerCount":2,"level":"medium"}'

# fantasy
curl -X POST http://localhost:8002/api/solve \
  -H 'Content-Type: application/json' \
  -d '{"round":99,"state":{"top":[],"middle":[],"bottom":[],"usedCards":[]},"dealt":["Ks","Kh","Kd","As","Ah","Ad","Ac","2s","3h","4d","5c","6s","7h","8d"],"discardCount":1,"mode":"fantasy","jokerCount":2}'

# 健康
curl http://localhost:8002/api/health | jq
```

### Node.js

```js
const http = require('http');
const data = JSON.stringify({
  round: 1,
  state: { top: [], middle: [], bottom: [], usedCards: [] },
  dealt: ['Ks','Kh','5d','9c','2s'],
  discardCount: 0,
  jokerCount: 2,
  pureMLP: true,
  level: 'low',
});
http.request({
  hostname: 'localhost', port: 8002, path: '/api/solve', method: 'POST',
  headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
}, (res) => {
  let body = ''; res.on('data', c => body += c);
  res.on('end', () => console.log(JSON.parse(body)));
}).end(data);
```

### Python

```python
import requests
r = requests.post('http://localhost:8002/api/solve', json={
    'round': 1,
    'state': {'top': [], 'middle': [], 'bottom': [], 'usedCards': []},
    'dealt': ['Ks','Kh','5d','9c','2s'],
    'discardCount': 0,
    'jokerCount': 2,
    'pureMLP': True,
    'level': 'low',
})
print(r.json())
```

---

## 错误处理

| 状态码 | 触发 | 推荐动作 |
|---|---|---|
| 400 | dealt/state 缺、bad card 字符串 | 检查请求 |
| 404 | `/api/games/:id` 不存在 | 检查 id |
| 405 | wrong method (`/api/solve` 非 POST) | 改 POST |
| 500 | fantasy 三阶段全 fail (理论不发生) | 上报 |
| 503 | `/api/games*` 但 DB 没启用 (`-db ""`) | 启用 DB |

无内置 timeout — 客户端按模式自己设. 推荐:
- pureMLP: 5s
- MCTS low: 10s
- MCTS medium: 30s
- MCTS high: 60s
- fantasy: 10s

---

## 频率 / 并发

无内置限流. 实测 (Linux 4 核, level=medium):
- 单请求: ~1s (Mac M1 ~200ms)
- 4 并发: ~1s 各
- 8 并发: ~2s 各 (CPU 饱和)
- pureMLP 模式 R1 ~300ms 各, 数千 QPS

加 NGINX 限流 / Cloudflare 防 DDoS 视部署需要.

---

## 部署 (ofc-dev-v3)

```bash
cd ofc-dev-v3
./start.sh                # 启 (默认 :8002, weights=big-models/best.json)
SOLVE_CACHE_SIZE=5000 ./start.sh    # 生产 cache 开 (调试期 ./start.sh 默认 0)
```

要换 ckpt:
```bash
cp v3-train-i147-spXX-XXX/iter-N/round-001-accXX.json big-models/best.json
# kill + 重启 server
kill $(pgrep -f "ofc-dev-v3.*8002"); ./start.sh
```

**部署历史**:
- sp17 iter-1 r1 (broken-features): 59/63 standard + 2/3 gamecase = 61/66
- sp18 iter-3 r1 (broken-features): 61/63 standard + 3/3 gamecase = 64/66
- **sp19 iter-3 r1 (fixed-features, 当前)**: 61/63 standard + 3/3 gamecase = 64/66, 实战 +8.5% score

---

## V3 NN 系统简介

- **Input**: 147-d feature (21 group, V3 design doc 在 `features_v3_design.md`)
- **Network**: 4-head MLP (value / fan-prob / foul-prob / policy)
- **Training**: silver-label MC rollout (100 sims) self-play, `-jokers 2`
- **Hard rules**: `hard_rules.go` (R1 5 条 + R2-5 8 条) + 软 penalty/bonus. R1 `r1RuleLowPair_OnMid` 2026-05-22 删 (multiple small pairs 强制 partial-foul bug).
- **MCTS**: 3-stage (s1c=30 / s2c=8 / s3c=3) + V3 NN prerank
- **Pure MLP mode**: 跳 MCTS, 仅 prerank top-1 (生产快路径)

详情见 `MEMORY.md` 索引.
