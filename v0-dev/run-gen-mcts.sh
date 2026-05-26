#!/usr/bin/env bash
# Mac 跑 B (MCTS-distilled labels) — 用 gen-mcts-dataset 生成 search-augmented label
#
# 跟 rollout-dataset 区别:
#   rollout: label = rollout mean Q (rollout policy = MLP-greedy, label 反映 NN 影子)
#   mcts:    label = MCTS visit dist + value (label 含 lookahead, 突破 NN 影子)
#
# 时间预估 (Mac 8 核):
#   sims=200 init-n=20: ~1m52s/decision (memory) → 200 game × 13 = 2600 decision × 112s = ~80h
#   sims=100 init-n=10: 估 1/3 = ~25h
#   sims=50  init-n=10: 估 1/4 = ~20h
#
# 推荐 sims=100 + 200 game (~25h overnight)
set -euo pipefail

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
cd "$V0_DIR/server-go"
mkdir -p logs

# build if needed
[ -x "$BIN_DIR/gen-mcts-dataset" ] || go build -o "$BIN_DIR/gen-mcts-dataset" ./cmd/gen-mcts-dataset
[ -x "$BIN_DIR/ofc-train" ] || go build -o "$BIN_DIR/ofc-train" ./cmd/train

LOG="logs/gen-mcts-200g.log"
echo "[chain] $(date) gen-mcts start → $LOG"
"$BIN_DIR/gen-mcts-dataset" \
  -num-games 200 \
  -jokers 2 \
  -mcts-sims 100 \
  -mcts-init-n 10 \
  -mcts-cpuct 1.5 \
  -mcts-leaf-k 3 \
  -workers 1 \
  -phantom-opponents 2 \
  -weights ../ckpts-v2-ema/round-002-acc92.json \
  -out-dir mcts-dataset-200g \
  -foul-cost 20 -fan-bonus-qq 50 -fan-bonus-kk 70 -fan-bonus-aa 200 -fan-bonus-trips 200 \
  > "$LOG" 2>&1

echo "[chain] $(date) gen-mcts done → train start"
"$BIN_DIR/ofc-train" \
  -dataset-dir mcts-dataset-200g \
  -hours 1.5 -round-min 30 \
  -outdim 4 -h1 256 -h2 128 -indim 132 \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.50 \
  -epochs 60 -lr 0.002 \
  -ckpt-dir ckpts-v16-mcts -policy v0-v16-mcts-student \
  > logs/train-v16-mcts.log 2>&1

echo "[chain] $(date) done"
