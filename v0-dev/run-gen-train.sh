#!/usr/bin/env bash
# Mac chain: gen-rollout-dataset 1500 games → train student NN
# 预计 gen ~8h + train ~1.5h = ~9.5h
#
# 用法:
#   ./run-gen-train.sh                       # 前台跑, 另开 terminal tail -f 日志
#   nohup ./run-gen-train.sh >/dev/null &    # 后台跑 (防 SSH 断开)
set -euo pipefail

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
cd "$V0_DIR/server-go"
mkdir -p logs

# build if needed
[ -x "$BIN_DIR/gen-rollout-dataset" ] || go build -o "$BIN_DIR/gen-rollout-dataset" ./cmd/gen-rollout-dataset
[ -x "$BIN_DIR/ofc-train" ] || go build -o "$BIN_DIR/ofc-train" ./cmd/train

GEN_LOG="logs/gen-rollout-1500g.log"
TRAIN_LOG="logs/train-v15-rollout.log"
DATASET_DIR="rollout-dataset-1500g"
CKPT_DIR="ckpts-v15-rollout"

echo "[chain] $(date) gen start → server-go/$GEN_LOG"
"$BIN_DIR/gen-rollout-dataset" \
  -num-games 1500 \
  -jokers 2 \
  -rollouts 500 \
  -r1-cap 30 \
  -phantom-opponents 2 \
  -out-dir "$DATASET_DIR" \
  -foul-cost 20 \
  -fan-bonus-qq 50 -fan-bonus-kk 70 -fan-bonus-aa 200 -fan-bonus-trips 200 \
  > "$GEN_LOG" 2>&1

echo "[chain] $(date) gen done → train start → server-go/$TRAIN_LOG"
"$BIN_DIR/ofc-train" \
  -dataset-dir "$DATASET_DIR" \
  -hours 1.5 \
  -round-min 30 \
  -outdim 4 \
  -h1 256 -h2 128 \
  -indim 132 \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  -epochs 40 \
  -lr 0.002 \
  -ckpt-dir "$CKPT_DIR" \
  -policy v0-v15-rollout-student \
  > "$TRAIN_LOG" 2>&1

echo "[chain] $(date) train done. ckpt → server-go/$CKPT_DIR/"
