# Texas NLHE — deployment

## Components

| Layer | Binary | Build tag | Inputs | Output |
|---|---|---|---|---|
| HUNL push/fold | `cmd/hunl-onnx-validate` | `onnx` | `blueprints/preflop-buckets-K20.json` + `distill/models/hunl-policy.onnx` | NN strategy JSON |
| 6-max NLHE | `cmd/server-6max` | `onnx` | ONNX policy model | HTTP API |

## 6-max HTTP server quickstart

```bash
# Build
go build -tags onnx -o /tmp/server-6max ./cmd/server-6max

# Run (background)
/tmp/server-6max -model distill/models/hunl6-150k.onnx -port 8080 &

# Test
curl -X POST http://localhost:8080/api/v1/policy -H 'Content-Type: application/json' -d '{
  "num_players": 6, "stack_bbs": 20,
  "bet_sizes": [0.5, 1.0, 2.0],
  "button": 0, "hole": ["As", "Ah"], "board": [], "history": []
}'
```

Returns: `legal_actions` + `probs` + `cur_seat` + `model` + `server_ms`.

See [cmd/server-6max/README.md](cmd/server-6max/README.md) for full API.

## Current artifacts

- HUNL push/fold NN: `distill/models/hunl-policy.onnx` (33-d input)
  - 95.7% case-bench PASS (44/46) per Phase B
  - 7.5 µs/query, 134k QPS
- HUNL multi-street NN: `distill/models/hunl-multistreet.pt` → ONNX (288-d input)
  - KL 0.0026 vs σ (Phase 2j)
  - h2h NN-vs-σ +50 ±88 mbb/g (CI 含 0)
- 6-max NN: `/tmp/hunl6-150k.onnx` (288-d input)
  - KL 0.0713 (plateau at indie scale)
  - h2h NN-vs-σ -59 ±256 (AIVAT, CI 含 0)
  - LBR(σ) 1824 mbb/g (perfect-info inflated; true value ~200-600)

## Latency

| Operation | Latency |
|---|---|
| 6-max ONNX forward | ~5 ms (server_ms incl. parse + state replay) |
| HUNL push/fold ONNX | 7.5 µs (134k QPS, no state replay) |
| LBR play (per hand) | ~5 ms (with 15 inner MC samples) |
| h2h play (per hand) | ~50 µs (no inner MC) |

## Production hardening (TODO)

- Auth (API token / mTLS)
- Rate limit
- Request validation (card parsing errors → 400)
- Structured logging
- Prometheus metrics
- Graceful shutdown
- Multi-model routing (HUNL vs 6-max based on `num_players`)
- TLS termination (nginx / Caddy upstream)

## 路线 (post-W6)

1. **真实对战 calibration** — Slumbot HUNL API (公开,无认证),overnight 跑 ≥ 10k hands → AIVAT 之后获 paper-grade 绝对数字
2. **Pluribus 风格 subgame search** — runtime depth-limited search at decision time. 显著提升 σ strength,但 ~2-3 周工作
3. **Range-aware LBR** — 把现 perfect-info LBR 数字 (~1800 mbb/g) 降到真值 ballpark (~200-600). 1-2 周
4. **6-max 实战部署** — 跟真玩家(自己 / 朋友)对战,从行为 log 找 NN 弱点 + 迭代
