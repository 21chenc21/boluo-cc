# v7_fan 部署指南

v7_fan 解算服务 = Node frontend (`:8001`) + Go solver (`:9000`) 双层架构。

```
浏览器 ─→ Node :8001 ─→ R1 LRU cache (命中→0ms)
                     ─→ Go :9000  (normal mode, ~6-12x JS 加速)
                     ─→ JS worker (fantasy mode, 复杂 beam search)
                     ─→ SQLite (game 历史)
```

## 一键启动

```bash
cd pineapple-ofc/v7_fan/server
chmod +x deploy.sh
./deploy.sh start
```

输出示例:
```
↻ 编译 Go solver...
✓ 编译完成: ...server-go/ofc-go
↻ 启动 Go solver on :9000...
✓ Go solver up (pid 12345, :9000)
↻ 启动 Node frontend on :8001 (Go backend :9000)...
✓ Node server up (pid 12346, :8001)
🚀 部署成功
   - 浏览器:    http://localhost:8001/3player.html
   - 健康检查:  curl http://localhost:8001/api/health
```

启动后:
- 浏览器: `http://localhost:8001/3player.html` (3 人对战)
- 浏览器: `http://localhost:8001/fantasy.html` (范特西摆牌)
- 浏览器: `http://localhost:8001/index.html` (单机解算)

## 常用命令

```bash
./deploy.sh start        # 启动 (build → run)
./deploy.sh stop         # 停掉 Go + Node
./deploy.sh restart      # 重启
./deploy.sh status       # 看运行 + 健康状态
./deploy.sh smoke        # 跑一个 R1 验证可用
./deploy.sh logs node    # 实时 Node 日志
./deploy.sh logs go      # 实时 Go 日志
./deploy.sh build        # 只编译 Go (不启动)
```

## 配置 (环境变量)

写在 `deploy.sh` 之前 export, 或者一行:
```bash
NODE_PORT=8080 POOL_SIZE=4 ./deploy.sh start
```

| 变量 | 默认 | 含义 |
|---|---|---|
| `GO_PORT` | 9000 | Go service 监听端口 |
| `NODE_PORT` | 8001 | Node frontend 端口 |
| `POOL_SIZE` | 2 | JS worker 数 (fantasy mode 用, 至少 1) |
| `DEFAULT_LEVEL` | medium | high / medium / low |
| `SOLVE_CACHE_SIZE` | 2000 | LRU cache 容量 (0 关闭) |

## 验证 / 监控

### 健康检查
```bash
curl http://localhost:8001/api/health
```
返回示例:
```json
{
  "ok": true,
  "workers": 2,
  "totalSolved": 50,
  "avgElapsedMs": 580,
  "cache": { "size": 12, "hits": 15, "misses": 35, "hitRate": 0.3 },
  "goSolver": {
    "enabled": true,
    "url": "http://localhost:9000",
    "totalSolved": 35,
    "avgElapsedMs": 480
  }
}
```

关键字段:
- `cache.hitRate`: R1 缓存命中率, 期望 > 0.2
- `goSolver.enabled`: Go 加速是否启用
- `avgElapsedMs`: 平均 R1 时间 (含 cache 命中的 0ms)

### 烟测
```bash
./deploy.sh smoke
# 应在 ~1s 内返回 layout
```

## 性能预期 (新档位 HIGH=2.0, MEDIUM=1.0, LOW=0.5)

| 场景 | linux box | M1 Pro 等比 |
|---|---|---|
| R1 LOW (cache miss) | ~1s | ~200ms |
| R1 MEDIUM | ~3s | ~600ms |
| R1 HIGH | ~14s | ~3s |
| 一手 5 round (HIGH) | ~30s | ~6s |
| Cache 命中 | 0ms | 0ms |
| Fantasy 摆牌 (R1) | 1-2s | 0.3-0.5s |

## 档位详情

```js
high:   r1Mult=2.0  // ~7300 rollouts/R1, 顶级策略
medium: r1Mult=1.0  // ~1830 rollouts (旧 HIGH 水准)
low:    r1Mult=0.5  // ~495 rollouts (旧 MEDIUM)
```

testcase 通过率: HIGH=2.0 给 **21/24 (87%)** — 大幅好于旧 HIGH=1.0 (16/24).

随 cache 命中率上升, 整体 avg 会再降 30-50%。

## 异常处理

### Go 挂掉, Node 还在跑
Node 检测到 Go 失败会自动 fallback 到 JS worker pool, 不影响业务 (但慢 6-12x).
日志会有: `[server] Go solver failed, fallback to worker pool: <reason>`

### Fantasy 摆牌报错
Fantasy mode (re-fan / FL beam search) 不走 Go, 直接 JS worker. 如果报错:
1. 检查 POOL_SIZE 是否 ≥ 1
2. 看 Node 日志 `./deploy.sh logs node` 找具体 error

### 端口被占
```bash
NODE_PORT=8080 GO_PORT=9001 ./deploy.sh start
```

### 浏览器 "Failed to fetch"
通常是服务重启时已有 fetch 中断. 刷新页面即可.
检查: `./deploy.sh status` 两个服务都 running.

## 文件清单

```
v7_fan/
├── server-go/                   # Go solver
│   ├── ofc/                     # 计算包 (cards, eval, score, etc.)
│   │   └── trained_weights.json # MLP 权重 (embed)
│   ├── cmd/server/              # HTTP service
│   ├── ofc-go                   # 编译产物 (deploy.sh build 后出现)
│   └── go.mod
└── server/                      # Node frontend
    ├── server.js                # Express + /api/solve
    ├── solver-pool.js           # JS worker 池 (fantasy fallback)
    ├── solve-worker.js          # Worker 加载 game.js + solver.js
    ├── cache.js                 # R1 LRU
    ├── go-solver.js             # HTTP 客户端 → Go
    ├── db.js                    # SQLite (game 历史)
    ├── deploy.sh                # 这个脚本
    └── DEPLOY.md                # 这个文档
```

## 卸载 / 清理

```bash
./deploy.sh stop
rm /tmp/ofc-go.pid /tmp/ofc-node.pid /tmp/ofc-go.log /tmp/ofc-node.log
```

## 远程访问 (可选)

deploy.sh 默认绑 `:8001` (所有接口). 想限本机:
```bash
# 改 server.js 的 app.listen 加 'localhost' 参数 (TODO: 加 BIND_HOST 环境变量)
```

跨机访问 (Mac → Linux 通过 SSH tunnel):
```bash
# Mac 上跑
ssh -L 8001:localhost:8001 chguang@<linux-host>
# 然后 Mac 浏览器访问 http://localhost:8001/3player.html
```

## 性能基线 (用于回归)

10250 个测例 parity 全过 (server-go/parity-*.js):
- evaluate 3/5 + joker: 3750
- score + foul + cap: 1500
- trainedEval: 2000
- simpleEval: 3000

修改 Go 后跑:
```bash
cd server-go
go test ./ofc/ -v
node parity-eval.js 1000
node parity-score.js 1000
node parity-trained.js 500
node parity-simple.js 500
```
任何 mismatch 都是回归。
