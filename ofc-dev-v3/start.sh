#!/usr/bin/env bash
# start.sh — ofc-dev-v3 production server (V3 features 147-d + sp15 best 57/63)
#
# 用法:
#   ./start.sh             # 前台跑 :8002
#   nohup ./start.sh &     # 后台

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

PORT="${PORT:-8002}"
BIN="$SCRIPT_DIR/server-go-bin/ofc-dev-v3"
WEIGHTS="$SCRIPT_DIR/big-models/best.json"

# Build if 缺
if [ ! -x "$BIN" ]; then
    echo "[start] building ofc-dev-v3..."
    (cd server-go && go build -o "$BIN" ./cmd/server)
fi

# Verify ckpt
if [ ! -f "$WEIGHTS" ]; then
    echo "[start] ERROR: missing $WEIGHTS" >&2
    exit 1
fi

echo "[start] ofc-dev-v3 on :$PORT, weights=big-models/best.json (V3 sp15 57/63), MCTS=on (level 生效), static=."
# 不设 DISABLE_MCTS, 让 level dropdown (low/medium/high/pure_mlp) 正常工作:
#   - pure_mlp: 客户端 pureMLP:true → 跳 MCTS (~40ms)
#   - low/medium/high: r1Mult 缩放 + MCTS sims (慢但更强)
SOLVE_CACHE_SIZE="${SOLVE_CACHE_SIZE:-0}" \
DEFAULT_LEVEL="${DEFAULT_LEVEL:-medium}" \
SOLVE_LOG="${SOLVE_LOG:-on}" \
SOLVE_LOG_RETAIN="${SOLVE_LOG_RETAIN:-100000}" \
exec "$BIN" \
    -addr=":$PORT" \
    -weights="$WEIGHTS" \
    -static="$SCRIPT_DIR" \
    -db="$SCRIPT_DIR/games.db"
