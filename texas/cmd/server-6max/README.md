# server-6max — HTTP NN policy API

## Build

需要 `onnx` build tag(libonnxruntime via server package).

```
go build -tags onnx -o server-6max ./cmd/server-6max
```

## Run

```
./server-6max -model /path/to/hunl6.onnx -port 8080
```

## API

### POST /api/v1/policy

请求 JSON:
```json
{
  "num_players": 6,
  "stack_bbs": 20,
  "bet_sizes": [0.5, 1.0, 2.0],
  "button": 0,
  "hole": ["As", "Ah"],
  "board": ["Kd", "7c", "2h"],
  "history": [
    {"kind": "bet", "size_idx": 1},
    {"kind": "checkcall"}
  ]
}
```

- `num_players`: 2-6
- `bet_sizes`: 各 bet 选项作为 pot fraction(顺序对应 size_idx 0/1/2)
- `button`: dealer button seat (0..N-1)
- `hole`: hero 2 张牌(由 reconstruction 推断 hero seat = 首到行动)
- `board`: 0-5 张牌(server 按需 fill 街转)
- `history`: 已发生 action 序列(actor seat 由 engine rotation 推导)

action kind: `fold` / `checkcall` / `bet` / `allin`. `bet` 配 `size_idx`(0/1/2)。

Response:
```json
{
  "legal_actions": ["fold", "checkcall", "bet0", "bet1", "bet2", "allin"],
  "probs": [0.005, 0.107, 0.073, 0.115, 0.075, 0.625],
  "cur_seat": 3,
  "model": "hunl6-150k.onnx",
  "server_ms": 5
}
```

### GET /healthz

`ok` if model loaded.

## 已验证示例

**AA UTG opening (6-max 20BB)**:
```
curl -s -X POST http://localhost:8080/api/v1/policy -d '{
  "num_players": 6, "stack_bbs": 20,
  "bet_sizes": [0.5, 1.0, 2.0],
  "button": 0, "hole": ["As", "Ah"], "board": [], "history": []
}'
```
返回: allin 62.5% / bet1.0p 11.5% / call 10.7% / bet2.0p 7.5% / bet0.5p 7.3% / fold 0.5%
→ **aggression 88.8%** 符合 AA 在 UTG 6-max 的理论 Nash 期望。

## TODO (production hardening)

- Rate limit / auth
- Request validation (suit/rank parsing 错误处理)
- Structured logging (zap/zerolog)
- Prometheus metrics (latency, error rate)
- Graceful shutdown
- HTTPS / mTLS
