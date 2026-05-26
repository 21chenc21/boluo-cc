#!/bin/bash
# oracle-pipeline.sh — Oracle silver-label dataset 生成 → MLP 训练 → bench 一气呵成
# 用法:
#   ./oracle-pipeline.sh                # 默认 600 games K=2 (~8h Mac 8-core)
#   GAMES=1000 K=2 ./oracle-pipeline.sh # 1000 games K=2 (~12h)
#   GAMES=300 K=4 ./oracle-pipeline.sh  # 300 games K=4 (~8h, 多 future 平均)
#
# 输出:
#   oracle-dataset-v1/round{1..5}/shard-*.jsonl.gz  数据集
#   ckpts-oracle-v1/round-001-acc*.json             新 ckpt
#   bench 结果打在 stdout 末尾
#
# 后台跑:
#   nohup ./oracle-pipeline.sh > oracle-pipeline.log 2>&1 &
#   tail -f oracle-pipeline.log

set -e

# 可调 (用 env 覆盖)
GAMES="${GAMES:-600}"
K="${K:-2}"
R1_CAP="${R1_CAP:-50}"
WORKERS="${WORKERS:-8}"
INDIM="${INDIM:-132}"
EPOCHS="${EPOCHS:-80}"
LR="${LR:-0.001}"
BASE_CKPT="${BASE_CKPT:-ckpts-v2-ema/round-001-acc89.json}"
DATASET_DIR="${DATASET_DIR:-oracle-dataset-v1}"
CKPT_DIR="${CKPT_DIR:-ckpts-oracle-v1}"
CASES="${CASES:-cases/all-tests-expanded.json}"

# Fan bonus knobs (oracle label 用)
FAN_QQ="${FAN_QQ:-50}"
FAN_KK="${FAN_KK:-70}"
FAN_AA="${FAN_AA:-200}"
FAN_TRIPS="${FAN_TRIPS:-200}"
FOUL_COST="${FOUL_COST:-20}"

echo "================================================"
echo "Oracle Pipeline"
echo "  games=$GAMES K=$K r1-cap=$R1_CAP workers=$WORKERS"
echo "  indim=$INDIM epochs=$EPOCHS lr=$LR"
echo "  fan: QQ=$FAN_QQ KK=$FAN_KK AA=$FAN_AA trips=$FAN_TRIPS  foul=$FOUL_COST"
echo "  base ckpt: $BASE_CKPT"
echo "  dataset:   $DATASET_DIR"
echo "  out ckpt:  $CKPT_DIR"
echo "================================================"
echo

echo "[$(date)] === Phase 1: gen oracle dataset ==="
./server-go/gen-oracle-dataset \
  -num-games "$GAMES" \
  -workers "$WORKERS" \
  -r1-multi-k "$K" \
  -r1-cap "$R1_CAP" \
  -indim "$INDIM" \
  -weights "$BASE_CKPT" \
  -fan-bonus-qq "$FAN_QQ" \
  -fan-bonus-kk "$FAN_KK" \
  -fan-bonus-aa "$FAN_AA" \
  -fan-bonus-trips "$FAN_TRIPS" \
  -foul-cost "$FOUL_COST" \
  -out-dir "$DATASET_DIR"

echo
echo "[$(date)] === Phase 2: train MLP ==="
./server-go/train \
  -indim "$INDIM" -h1 256 -h2 128 -outdim 4 \
  -dataset-dir "$DATASET_DIR" \
  -epochs "$EPOCHS" -lr "$LR" \
  -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  -ckpt-dir "$CKPT_DIR" -policy oracle-v1 \
  -init-from-ckpt "$BASE_CKPT"

echo
echo "[$(date)] === Phase 3: bench new ckpt ==="
LATEST=$(ls -t "$CKPT_DIR"/round-*.json 2>/dev/null | head -1)
if [ -z "$LATEST" ]; then
    echo "❌ 没找到训出的 ckpt"
    exit 1
fi
echo "Bench: $LATEST"
SEED=42 ./case-test.sh "$LATEST" "$CASES" 2>&1 | tail -5

echo
echo "[$(date)] === DONE ==="
