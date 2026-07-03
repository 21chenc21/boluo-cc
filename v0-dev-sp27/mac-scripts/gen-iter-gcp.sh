#!/usr/bin/env bash
# GCP gen farm — 8-shard 纯NN gen + 聚合进度. 不 train (Mac 负责 train).
# 用法 (GCP 上):  bash mac-scripts/gen-iter-gcp.sh <iter> [rollout-ckpt]
#   rollout-ckpt 缺省: 有 best.json 用它, 否则用 sp36 太子 iter-4 r2 (sp37 首轮).
# env: GEN_SHARDS(默认8) GAMES(默认1000)
set -uo pipefail
export PATH=$PATH:/usr/local/go/bin
ITER="${1:?用法: gen-iter-gcp.sh <iter> [rollout-ckpt]}"
DATA_VERSION=i168-sp37; RUN=gcp
DATASET_ROOT="v3-dataset-${DATA_VERSION}-${RUN}"
TRAIN_ROOT="v3-train-${DATA_VERSION}-${RUN}"
CKPT="${2:-}"
if [ -z "$CKPT" ]; then
  if [ -f "$TRAIN_ROOT/best.json" ]; then CKPT="$TRAIN_ROOT/best.json"
  else CKPT="v3-train-i165-sp36-1/iter-4/round-002-acc94.json"; fi
fi
SHARDS="${GEN_SHARDS:-8}"; GAMES="${GAMES:-1000}"
BIN=server-go-bin; mkdir -p "$BIN"
( cd server-go && go build -o "../$BIN/gen-rollout-dataset" ./cmd/gen-rollout-dataset ) || { echo "FATAL: build 失败"; exit 1; }
[ -f "$CKPT" ] || { echo "FATAL: rollout ckpt 不存在: $CKPT"; exit 1; }
GEN_OUT="$DATASET_ROOT/iter-$ITER"; rm -rf "$GEN_OUT"; mkdir -p "$GEN_OUT"
per=$((GAMES/SHARDS)); rem=$((GAMES%SHARDS)); pids=()
echo "gen iter-$ITER: $SHARDS 片 × ~$per 局 (共 $GAMES), rollout=$CKPT, out=$GEN_OUT"
t0=$(date +%s)
for k in $(seq 0 $((SHARDS-1))); do
  g=$per; [ "$k" -lt "$rem" ] && g=$((per+1))
  DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 "$BIN/gen-rollout-dataset" \
    -num-games "$g" -jokers 2 -rollouts 100 -r1-cap 30 -phantom-opponents 2 -indim 168 \
    -foul-cost 3 -fan-bonus-qq 10 -fan-bonus-kk 30 -fan-bonus-aa 100 -fan-bonus-trips 140 \
    -weights "$CKPT" -out-dir "$GEN_OUT/shard$k" > "$GEN_OUT/shard$k.log" 2>&1 &
  pids+=($!); sleep 1.2   # 错开>1s: gen 无 -seed, 靠时间seed区分
done
# 聚合进度 (每 20s 汇总 8 片的 game N/M)
while :; do
  alive=0; for p in "${pids[@]}"; do kill -0 "$p" 2>/dev/null && alive=$((alive+1)); done
  done_g=0
  for k in $(seq 0 $((SHARDS-1))); do
    n=$(grep -oE "game [0-9]+/" "$GEN_OUT/shard$k.log" 2>/dev/null | tail -1 | grep -oE "^[0-9]+|[0-9]+" | tail -1)
    done_g=$((done_g + ${n:-0}))
  done
  el=$(( $(date +%s) - t0 ))
  rate=$(awk "BEGIN{if($el>0)printf \"%.1f\",$done_g*60/$el; else print 0}")
  eta=$(awk "BEGIN{if($done_g>0)printf \"%.0f\",($GAMES-$done_g)*$el/$done_g/60; else print \"?\"}")
  echo "[gen iter-$ITER] $done_g/$GAMES games | ${rate} g/min | ETA ${eta} min | ${alive}/$SHARDS 片活 | $(date +%H:%M:%S)"
  [ "$alive" -eq 0 ] && break
  sleep 20
done
ec=0; for p in "${pids[@]}"; do wait "$p" || ec=$?; done
echo "gen iter-$ITER 完成 exit=$ec, 用时 $(( $(date +%s)-t0 ))s, 样本 $(find "$GEN_OUT" -name '*.jsonl.gz'|wc -l) 文件 / $(du -sh "$GEN_OUT" 2>/dev/null|cut -f1)"
[ "$ec" -eq 0 ] && touch "$GEN_OUT/.gen-done" || { echo "gen 非0退出, 不写完成标记"; exit "$ec"; }
