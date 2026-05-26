#!/usr/bin/env bash
# az_puremlp.sh — Mac AZ self-play 训练, 目标推纯 MLP testcase 47→55+
#
# 设计:
#   - warm v3 (47/63 pure MLP baseline)
#   - MC25 self-play (MCTS 25 sims, 快 — 反正 MCTS 会偏, 不追求精确)
#   - 1000 games/iter (~2-3h Mac 8-core)
#   - rollout-epsilon 0 (不加 ε-greedy 漂移 — MC 本身就偏移, 再加噪音意义不大)
#   - PROMOTE 按纯 MLP testcase (DISABLE_MCTS=1, 对齐生产部署)
#   - testcase-drop-limit 3 (跌超 3 → 强制 DISCARD, 防 mode collapse)
#   - lr 0.0005, warm-lr-mult 1.0 (实际 LR = 0.0005, 不打折)
#
# 用法:
#   bash mac-scripts/az_puremlp.sh           # 默认 10 iter
#   bash mac-scripts/az_puremlp.sh 5         # 5 iter
#   bash mac-scripts/az_puremlp.sh 10 az-v5  # 自定义 ckpt-dir
#
# 输出:
#   ./<CKPT_DIR>/iter-NNN.json   每 iter promoted ckpt
#   ./<CKPT_DIR>/iter-NNN-train/ 训练中间文件
#
# Mac M-chip 单 iter 估时 (1000 games × MC25): ~2-3h. 16h 估 5-7 iter.

set -e

ITERS="${1:-10}"
CKPT_DIR="${2:-az-v4-puremlp}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
V0_DIR="$(dirname "$SCRIPT_DIR")"
cd "$V0_DIR"

BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
TRAIN_BIN="$BIN_DIR/ofc-train"
AZ_BIN="$BIN_DIR/alphazero-train"
BENCH_BIN="$BIN_DIR/bench-cases"

if [ ! -x "$TRAIN_BIN" ] || [ ! -x "$AZ_BIN" ] || [ ! -x "$BENCH_BIN" ]; then
    echo "[az-puremlp] 二进制不存在, 自动 build → $BIN_DIR/"
    (cd server-go && \
        go build -o "$TRAIN_BIN" ./cmd/train && \
        go build -o "$AZ_BIN" ./cmd/alphazero-train && \
        go build -o "$BENCH_BIN" ./cmd/bench-cases)
    echo "[az-puremlp] build done"
fi

WARM_START="big-models/big-model-v3.json"
if [ ! -f "$WARM_START" ]; then
    echo "[az-puremlp] ERROR: $WARM_START not found"
    exit 1
fi

LOG="/tmp/az-v4-puremlp-$(date +%Y%m%d-%H%M%S).log"

echo "[az-puremlp] iters=$ITERS, warm=$WARM_START, ckpt-dir=$CKPT_DIR"
echo "[az-puremlp] MC25 (25 sims), 1000 games/iter, rollout-eps=0, drop-limit=3"
echo "[az-puremlp] DISABLE_MCTS=1 → PROMOTE 看纯 MLP testcase"
echo "[az-puremlp] log = $LOG"
echo "[az-puremlp] start at $(date)"
echo ""

DISABLE_MCTS=1 "$AZ_BIN" \
    -iters "$ITERS" \
    -games-per-iter 1000 \
    -mcts-sims 25 \
    -duel-games 100 \
    -warm-start "$WARM_START" \
    -ckpt-dir "$CKPT_DIR" \
    -policy v0-az-v4-puremlp \
    -indim 132 \
    -h1 512 -h2 256 -h3 128 \
    -value-rollouts 30 \
    -epochs 25 \
    -lr 0.0001 \
    -warm-lr-mult 1.0 \
    -rollout-epsilon 0 \
    -testcase-drop-limit 1 \
    -train-bin "$TRAIN_BIN" \
    -cases-file cases/all-tests-expanded.json \
    -bench-sims-mult 2 \
    -workers 0 2>&1 | tee "$LOG"

echo ""
echo "[az-puremlp] done at $(date)"
echo "[az-puremlp] log = $LOG"
echo ""
echo "[az-puremlp] 查看每 iter 的纯 MLP testcase 趋势:"
echo "  grep 'testcase:' $LOG"
echo ""
echo "[az-puremlp] 单独 bench 任一 iter:"
echo "  DISABLE_MCTS=1 ./server-go-bin/bench-cases -ckpt $CKPT_DIR/iter-NNN.json -cases cases/all-tests-expanded.json -workers 0"
