#!/usr/bin/env bash
# GCP gen farm — 只跑某 iter 的 8-shard 纯NN gen (不 train, Mac 负责 train).
# 在 GCP (34.19.194.234) 上跑. rollout policy = 上一 iter Mac train 推回来的 best.json.
#
# 用法 (GCP 上):  bash mac-scripts/gen-iter-gcp.sh <iter> [rollout-ckpt]
#   iter          第几轮 (gen 写到 v3-dataset-i165-sp33-gcp/iter-<iter>/shard*)
#   rollout-ckpt  缺省 = v3-train-i165-sp33-gcp/best.json
# env: GEN_SHARDS(默认8) GAMES(默认1000)
set -euo pipefail
export PATH=$PATH:/usr/local/go/bin
ITER="${1:?用法: gen-iter-gcp.sh <iter> [rollout-ckpt]}"
DATA_VERSION=i165-sp33; RUN=gcp
DATASET_ROOT="v3-dataset-${DATA_VERSION}-${RUN}"
TRAIN_ROOT="v3-train-${DATA_VERSION}-${RUN}"
CKPT="${2:-$TRAIN_ROOT/best.json}"
SHARDS="${GEN_SHARDS:-8}"; GAMES="${GAMES:-1000}"
BIN=server-go-bin; mkdir -p "$BIN"
( cd server-go && go build -o "../$BIN/gen-rollout-dataset" ./cmd/gen-rollout-dataset )
[ -f "$CKPT" ] || { echo "FATAL: rollout ckpt 不存在: $CKPT"; exit 1; }
GEN_OUT="$DATASET_ROOT/iter-$ITER"; mkdir -p "$GEN_OUT"
per=$((GAMES/SHARDS)); rem=$((GAMES%SHARDS)); pids=()
echo "gen iter-$ITER: $SHARDS 片 × ~$per 局 (共 $GAMES), rollout=$CKPT, out=$GEN_OUT"
t0=$(date +%s)
for k in $(seq 0 $((SHARDS-1))); do
  g=$per; [ "$k" -lt "$rem" ] && g=$((per+1))
  DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 "$BIN/gen-rollout-dataset" \
    -num-games "$g" -jokers 2 -rollouts 150 -r1-cap 30 -phantom-opponents 2 -indim 165 \
    -foul-cost 3 -fan-bonus-qq 10 -fan-bonus-kk 30 -fan-bonus-aa 100 -fan-bonus-trips 140 \
    -weights "$CKPT" -out-dir "$GEN_OUT/shard$k" &
  pids+=($!); sleep 1.2   # 错开>1s: gen 无 -seed, 靠时间seed区分, 同秒会生成相同局
done
ec=0; for p in "${pids[@]}"; do wait "$p" || ec=$?; done
echo "gen iter-$ITER 完成 exit=$ec, 用时 $(( $(date +%s)-t0 ))s, 样本 $(find "$GEN_OUT" -name '*.jsonl.gz'|wc -l) 文件 / $(du -sh "$GEN_OUT"|cut -f1)"
[ "$ec" -eq 0 ] && touch "$GEN_OUT/.gen-done" || { echo "gen 非0退出, 不写完成标记"; exit "$ec"; }
