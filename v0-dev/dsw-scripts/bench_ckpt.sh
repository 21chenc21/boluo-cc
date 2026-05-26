#!/usr/bin/env bash
# bench_ckpt.sh — DSW 上跑 bench (run-cases.sh + MCTS_SIMS_MULT=2)
#
# 用法: bench_ckpt.sh CKPT_PATH
# 默认: cases/all-tests-expanded.json

set -e

CKPT="${1:?usage: bench_ckpt.sh CKPT_PATH}"

cd /mnt/workspace/v0-dev

if [ ! -f "$CKPT" ]; then
    echo "❌ ckpt not found: $CKPT" >&2
    exit 1
fi

BIN_DIR="server-go-bin"
mkdir -p "$BIN_DIR"
if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[bench] ofc-go missing, building → $BIN_DIR/ofc-go..."
    (cd server-go && go build -o "../$BIN_DIR/ofc-go" ./cmd/server)
fi

echo "[bench] ckpt: $CKPT"
echo "[bench] starting at $(date)"

MCTS_SIMS_MULT=2 ./run-cases.sh "$CKPT"

echo "[bench] done at $(date)"
