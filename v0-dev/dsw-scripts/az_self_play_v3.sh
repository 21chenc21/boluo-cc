#!/usr/bin/env bash
# az_self_play_v3.sh — AZ self-play 继续训, big model (v3 起步)
#
# 起点: big-model-v3.json (53/63)
# 每 iter:
#   1. self-play 2000 games (mcts-sims=100) → 200K samples
#   2. PyTorch fine-tune (via wrapper) on samples
#   3. duel new vs best (60 games)
#   4. testcase bench (in-process, ExpertPlace5)
#   5. PROMOTE if testcase ↑ OR fantasy ↑ OR (score↑ AND others=)
#
# 用法: bash dsw-scripts/az_self_play_v3.sh [ITERS]

set -e

ITERS="${1:-10}"

cd /mnt/workspace/v0-dev

BIN_DIR="server-go-bin"
mkdir -p "$BIN_DIR"
[ -x "$BIN_DIR/alphazero-train" ] || (cd server-go && go build -o "../$BIN_DIR/alphazero-train" ./cmd/alphazero-train)

WRAPPER="$(realpath dsw-scripts/ofc-train-pytorch-wrapper.py)"
chmod +x "$WRAPPER"

echo "[az-v3] iters=$ITERS, warm-start=big-model-v3.json"
echo "[az-v3] train-bin = $WRAPPER (PyTorch on GPU)"
echo "[az-v3] starting at $(date)"

"$BIN_DIR/alphazero-train" \
    -iters "$ITERS" \
    -games-per-iter 2000 \
    -mcts-sims 100 \
    -duel-games 60 \
    -warm-start big-model-v3.json \
    -ckpt-dir az-v3 \
    -policy v0-az-v3 \
    -indim 132 \
    -h1 512 -h2 256 -h3 128 \
    -value-rollouts 30 \
    -epochs 15 \
    -lr 0.0001 \
    -train-bin "$WRAPPER" \
    -cases-file cases/all-tests-expanded.json \
    -bench-sims-mult 2 \
    -workers 0

echo "[az-v3] done at $(date)"
