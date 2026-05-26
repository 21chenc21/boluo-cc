#!/usr/bin/env bash
# train_v3_supervised.sh — V3 features 全流程 train + bench
#
# 因 V2 samples (132-d) 跟 V3 (131-d) 不兼容, 必须重新 gen V3 samples.
#
# 流程:
#   1. gen-rollout-dataset 生 V3 samples (indim 131, 500 games × 500 rollouts)
#   2. train fresh V3 NN (indim 131, 30 epoch, lr 0.001)
#   3. bench 纯 MLP (DISABLE_MCTS=1)
#
# 用法:
#   bash mac-scripts/train_v3_supervised.sh [GAMES]
#   bash mac-scripts/train_v3_supervised.sh 500  # 默认
#
# 时间 (Mac M-chip 8 core):
#   gen: 500 games × 500 rollouts ≈ 4-6h
#   train: 30 epoch on ~75K samples ≈ 15 min
#   bench: 5s
#   总: ~5-7h
#
# 输出:
#   v3-dataset/round{1..5}/shard-*.jsonl.gz   生成的 V3 samples
#   v3-train/round-NNN-accXX.json             训练 ckpt
#   /tmp/v3-pipeline-<ts>.log                  全 log

set -e

GAMES="${1:-500}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
V0_DIR="$(dirname "$SCRIPT_DIR")"
cd "$V0_DIR"

BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
TRAIN_BIN="$BIN_DIR/ofc-train"
GEN_BIN="$BIN_DIR/gen-rollout-dataset"
BENCH_BIN="$BIN_DIR/bench-cases"

# Build if 缺
for tool in ofc-train gen-rollout-dataset bench-cases; do
    if [ ! -x "$BIN_DIR/$tool" ]; then
        echo "[v3-pipeline] build $tool..."
        case "$tool" in
            ofc-train) (cd server-go && go build -o "$BIN_DIR/$tool" ./cmd/train) ;;
            *) (cd server-go && go build -o "$BIN_DIR/$tool" "./cmd/$tool") ;;
        esac
    fi
done

DATASET_DIR="v3-dataset"
TRAIN_DIR="v3-train"
LOG="/tmp/v3-pipeline-$(date +%Y%m%d-%H%M%S).log"

echo "[v3-pipeline] games=$GAMES, indim=131, fresh V3"
echo "[v3-pipeline] log=$LOG"
echo "[v3-pipeline] start $(date)"

# Phase A: gen-rollout-dataset
if [ ! -d "$DATASET_DIR/round5" ] || [ -z "$(ls $DATASET_DIR/round5 2>/dev/null)" ]; then
    echo "[v3-pipeline] Phase A: gen V3 samples ($GAMES games)..."
    "$GEN_BIN" \
        -num-games "$GAMES" \
        -jokers 2 \
        -rollouts 500 \
        -r1-cap 30 \
        -phantom-opponents 2 \
        -out-dir "$DATASET_DIR" \
        -indim 131 \
        -foul-cost 6 \
        -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 80 -fan-bonus-trips 90 \
        2>&1 | tee -a "$LOG"
    echo "[v3-pipeline] Phase A done $(date)"
else
    echo "[v3-pipeline] Phase A skipped (dataset exists)"
fi

# Phase B: train V3 NN
echo "[v3-pipeline] Phase B: train V3 fresh init..."
mkdir -p "$TRAIN_DIR"
"$TRAIN_BIN" \
    -dataset-dir "$DATASET_DIR" \
    -hours 1 -round-min 30 \
    -outdim 4 -h1 512 -h2 256 -h3 128 \
    -indim 131 \
    -epochs 30 -lr 0.001 \
    -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 80 -fan-bonus-trips 90 \
    -foul-cost 6 \
    -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
    -ckpt-dir "$TRAIN_DIR" \
    -policy v0-v3-fresh \
    2>&1 | tee -a "$LOG"

echo "[v3-pipeline] Phase B done $(date)"

# Phase C: bench
LATEST=$(ls -t "$TRAIN_DIR"/round-*-acc*.json 2>/dev/null | head -1)
if [ -z "$LATEST" ]; then
    echo "[v3-pipeline] ERROR: no ckpt" | tee -a "$LOG"
    exit 1
fi

echo "[v3-pipeline] Phase C: bench 纯 MLP $LATEST"
DISABLE_MCTS=1 "$BENCH_BIN" -ckpt "$LATEST" -cases cases/all-tests-expanded.json -workers 0 \
    2>&1 | tee -a "$LOG"

echo ""
echo "[v3-pipeline] FINAL ckpt: $LATEST"
echo "[v3-pipeline] log: $LOG"
echo "[v3-pipeline] done $(date)"
