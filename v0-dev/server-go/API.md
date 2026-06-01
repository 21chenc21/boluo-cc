# V3 Solver API

OFC Pineapple AI 解算 HTTP 服务. 单 Go binary `ofc-dev-v3`, 同进程: HTTP + 静态文件 + SQLite + V3 NN inference.

**生产部署**: `34.92.248.175:8002`, V3 NN sp19 iter-3 r1 (md5 `239f9a9b06f11034ecbe33889d456cb8`).

---

## `POST /api/solve` — 主接口

### 请求 body

#### ⭐ 必填

| 字段 | 类型 | 说明 |
|---|---|---|
| `round` | int | 1=R1 摆5 / 2-5=R2-5 弃1摆2 / 99=fantasy 一次摆 14-17 张 |
| `state` | object | 当前棋盘状态 (见下) |
| `state.top` | string[] | 头道已摆牌 |
| `state.middle` | string[] | 中道已摆牌 |
| `state.bottom` | string[] | 底道已摆牌 |
| `state.usedCards` | string[] | 已 placed + 弃 + opp visible 全部 (**不含 dealt 本轮的牌**) |
| `dealt` | string[] | 本轮发牌. R1=5 张, R2-5=3 张, fantasy=14-17 张 |
| `discardCount` | int | R1=0, R2-5=1, fantasy=N-13 |
| `jokerCount` | int | 本局总鬼数 (**0/2/4**). ⚠ 不传按 0, 2 鬼局结果偏! |

#### ⭐ 强烈建议传 (生产关键)

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `pureMLP` | bool | **`false`** | 生产**必传 `true`** (~280ms R1, ExpertPlace3 路径). 不传走 MCTSSearch rollout = 17s + 退步 6-17 case |

#### 可选 — AI 难度 (前端做强弱档)

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `topK` | int | **`1`** | 1=最强 deterministic / 2=中等 R1 top-2 sample / 3=简单 R1 top-3 sample. 只 R1 sample, R2-R5 永远 top-1 |

#### 可选 — 外部追踪 (建议传, 排查问题用)

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `game_id` | string | `""` | 外部 game session id (例 `"112565853-0"`). 进 solve_log |
| `uid` | string | `""` | 用户 id. 排查"用户 X 行为" |
| `seat_number` | int | `0` | 座位号 0/1/2. 排查"座位 Y 求解" |

#### 可选 — 其他

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `mode` | string | `"normal"` | `"normal"` / `"fantasy"` (round=99 时必 fantasy) |
| `level` | string | `"medium"` | MCTS 档位 `"low"/"medium"/"high"`. pureMLP=true 时**忽略** |
| `r1Mult` | float | (level 查表) | 直传 MCTS r1Mult, 覆盖 level. pureMLP=true 时**忽略** |
| `gameId` | string | `null` | 内部 episode UUID (跟 `game_id` 区分) |
| `player` | int | `null` | 内部 player 0/1/2 |

### 生产推荐请求

```json
{
  "round": 1,
  "state": {
    "top": [], "middle": [], "bottom": [],
    "usedCards": []
  },
  "dealt": ["Ks","Kh","5d","9c","2s"],
  "discardCount": 0,
  "jokerCount": 2,

  "pureMLP": true,
  "topK": 1,

  "game_id": "112565853-0",
  "uid": "2637881",
  "seat_number": 0
}
```

### 响应

pureMLP=true (生产路径):
```json
{
  "layout": { "top": ["Ac"], "middle": ["9h"], "bottom": [] },
  "discards": ["3s"],            // R1 时空, R2-5 弃 1 张, fantasy 弃 N-13 张
  "elapsedMs": 283,
  "totalMs": 283,
  "level": "pureMLP",            // 标识跳了 MCTS
  "topK": 2,                     // 回显请求的 topK (=0 时省略)
  "cached": false                // true → LRU 命中
  // r1Mult 字段省略 (pureMLP 不用 MCTS 缩放)
}
```

MCTS path (level=low/medium/high):
```json
{
  "layout": { ... },
  "discards": [...],
  "elapsedMs": 7700,
  "totalMs": 7700,
  "level": "low",                // server 实际用的 level
  "r1Mult": 0.25,                // MCTS r1Mult
  "cached": false
}
```

错误: `400` (bad request) / `500` (fantasy 罕见 fallback fail).

---

## Card 编码

| 字符串 | 含义 |
|---|---|
| `As` `Kh` `Td` `2c` | rank + suit. Suits: `s`♠ `h`♥ `d`♦ `c`♣. Ranks: `2 3 4 5 6 7 8 9 T J Q K A` |
| `X` | 鬼牌. **多鬼场景仍都是 `X`** (server 内部用 `Xj0/Xj1` 持久化, API I/O 层面不区分) |

---

## 其他端点

| 路径 | 方法 | 说明 |
|---|---|---|
| `/api/health` | GET | 健康检查, 返 `{ok, totalSolved, solveLog, cache, levels}` |
| `/api/games` | POST/GET | 保存/列对局 (limit 默认 20, 上限 200) |
| `/api/games/:id` | GET | 取对局详情, 不存在 404 |
| `/cache/clear` | GET | 清 LRU cache |
| `/solve` | POST | Raw (parity test 用, 业务一律走 `/api/solve`) |
| `/*.html` `/*.js` | GET | 静态文件 |

---

## Solve 日志 (`solve_log` 表)

`SOLVE_LOG=on` (start.sh default), 每次 `/api/solve` 进表 (retain=100000).

Schema:
```sql
CREATE TABLE solve_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id       TEXT,           -- 内部 gameId (非外部 game_id)
    player        INTEGER,
    round         INTEGER,
    mode          TEXT,           -- normal / fantasy
    request_json  TEXT NOT NULL,  -- 含外部 game_id / uid / seat_number
    response_json TEXT NOT NULL,
    elapsed_ms    INTEGER,
    created_at    INTEGER NOT NULL
);
```

### 常用查询

按 **uid** 查:
```sql
SELECT id, request_json, response_json, created_at FROM solve_log
WHERE request_json LIKE '%"uid":"2637881"%'
ORDER BY id DESC;
```

按 **外部 game_id** 查:
```sql
SELECT * FROM solve_log WHERE request_json LIKE '%112565853-0%' ORDER BY id DESC;
```

按 **fantasy mode** 查:
```sql
SELECT * FROM solve_log WHERE mode = 'fantasy' ORDER BY id DESC LIMIT 50;
```

---

## 调用示例

### curl

```bash
curl -X POST http://localhost:8002/api/solve \
  -H 'Content-Type: application/json' \
  -d '{
    "round": 1,
    "state": {"top":[],"middle":[],"bottom":[],"usedCards":[]},
    "dealt": ["Ks","Kh","5d","9c","2s"],
    "discardCount": 0,
    "jokerCount": 2,
    "pureMLP": true,
    "topK": 1,
    "game_id": "112565853-0",
    "uid": "2637881",
    "seat_number": 0
  }'
```

### Python

```python
import requests
r = requests.post('http://34.92.248.175:8002/api/solve', json={
    'round': 1,
    'state': {'top': [], 'middle': [], 'bottom': [], 'usedCards': []},
    'dealt': ['Ks','Kh','5d','9c','2s'],
    'discardCount': 0,
    'jokerCount': 2,
    'pureMLP': True,
    'topK': 1,
    'game_id': '112565853-0',
    'uid': '2637881',
    'seat_number': 0,
})
print(r.json())
```

---

## 性能 / 难度选择

| 配置 | 路径 | R1 延迟 | testcase | 推荐 |
|---|---|---|---|---|
| **pureMLP + topK=1** | ExpertPlace3 (NN+硬规则) | **~280ms** | **61/63** | ⭐ 生产唯一 |
| pureMLP + topK=2 | ExpertPlace3 + R1 top-2 sample | ~280ms | 58/63 | 中等难度 (玩家有赢面) |
| pureMLP + topK=3 | ExpertPlace3 + R1 top-3 sample | ~280ms | 54/63 | 简单难度 |
| MCTS low (`level:"low"`) | MCTSSearch rollout | ~7s | (~) | dev 调试 |
| MCTS medium | MCTSSearch rollout | ~17s | 48/63 (-13) | **不推荐 (弱)** |
| MCTS high | MCTSSearch rollout | ~56s | 54/63 (-7) | **不推荐 (弱)** |

> sp19 NN 太强, MCTS rollout (`mcts.go MCTSSearch`) 反而拖后腿. **生产只用 pureMLP (ExpertPlace3)**. MCTSSearch 仅训练 (`cmd/alphazero-train`) 和单元测试用, 不进生产解算路径.

---

## 配置 (Server 启动)

### Flag

| Flag | 默认 | 说明 |
|---|---|---|
| `-addr` | `:8001` | TCP 监听 |
| `-static` | 自动 | 前端文件根目录 |
| `-db` | `games.db` | sqlite. `-db ""` 关 DB |
| `-weights` | (embed) | 覆盖 embed weights 加载指定 V3 ckpt |

### Env

| 变量 | 默认 | 说明 |
|---|---|---|
| `SOLVE_CACHE_SIZE` | 2000 | LRU. 0=关 (生产推荐 5000) |
| `SOLVE_LOG` | off | `off`/`sample`/`on` (start.sh default `on`) |
| `SOLVE_LOG_RETAIN` | 50000 | (start.sh default 100000) |
| `DEFAULT_LEVEL` | medium | 不传 level 时的默认档 |
| `DISABLE_MCTS` | (off) | 全局跳 MCTS (per-request `pureMLP` 同效果) |

---

## 部署 (ofc-dev-v3)

```bash
cd ofc-dev-v3
./start.sh                          # 启 (port 8002)
SOLVE_CACHE_SIZE=5000 ./start.sh    # 生产开 cache
```

切 ckpt:
```bash
cp big-models/best.json big-models/best.json.bak-$(date +%Y%m%d-%H%M%S)
cp /path/to/new.json big-models/best.json
kill $(pgrep -f "ofc-dev-v3.*8002"); ./start.sh
```

### 部署历史

- **sp19 iter-3 r1** ⭐ 当前 (md5 `239f9a9b06f11034ecbe33889d456cb8`) — 实战 +8.5% score vs sp18
- sp18 iter-3 r1 (broken features): 64/66
- sp17 iter-1 r1: 61/66

---

## V3 NN 系统简介

- **Input**: 147-d feature (21 group, `features_v3_design.md`)
- **Network**: 4-head MLP (value / fan-prob / foul-prob / policy)
- **Training**: silver-label MC rollout (100 sims) self-play `-jokers 2`
- **Hard rules**: R1 5 条 + R2-5 3 条 + 软 penalty/bonus. 删除历史:
  - 2026-05-22 `r1RuleLowPair_OnMid` — small pair 强制 mid → partial-foul 漏洞 (case J5599)
  - 2026-05-31 `rnRuleJokerWithA_OnTop` — state.top 已有 A 时强迫 trips foul (case ypk-159252810-11)
  - 2026-05-31 `rnRuleNoDiscardPairMember` — 强迫不弃高对 → R5 cap chain 必 foul (case ypk-180814154-1)
  - 2026-05-31 `rnRuleNoDiscardAce` / `rnRuleNoSplitKeptPair` / `rnRuleKK_OnBot_WithA` — NN 自然学到, 规则冗余或压抑
- **解算路径**:
  - **ExpertPlace3/5** (生产): NN 前向 + 硬规则过滤 + R1 候选 prerank top-1, 唯一进生产 (`pureMLP:true`)
  - **MCTSSearch** (`mcts.go`): NN+rollout, 实测比 ExpertPlace3 弱 (sp19 NN 太强, rollout 拖后腿). 仅训练 / 单元测试用, **不进生产**
