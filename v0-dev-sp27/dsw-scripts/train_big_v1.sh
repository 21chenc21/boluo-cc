#!/usr/bin/env bash
# train_big_v1.sh — DSW 上跑 big model (132→512→256→128→4) supervised training
#
# 用累积 self-play samples (round 1 + round 2 of small-model AZ) bootstrap big model.
#
# 用法:
#   /mnt/workspace/v0-dev/dsw-scripts/train_big_v1.sh [OUT_NAME] [EPOCHS]
#   默认: OUT_NAME=big-model-v1.json, EPOCHS=50
#
# DSW 环境: NVIDIA A10, pytorch 2.3.1+cu121
# 预估时间: ~10-15 min (full 2.4M samples × 50 epochs, GPU)

set -e

OUT_NAME="${1:-big-model-v1.json}"
EPOCHS="${2:-50}"

cd /mnt/workspace/v0-dev

# 收集所有 sample dirs
SAMPLE_DIRS=(
    az-prod/iter-001-samples
    az-prod/iter-002-samples
    az-prod/iter-003-samples
    az-prod/iter-004-samples
    az-prod/iter-005-samples
    az-prod/iter-006-samples
    az-prod/iter-007-samples
    az-round2/iter-001-samples
    az-round2/iter-002-samples
    az-round2/iter-003-samples
    az-round2/iter-004-samples
    az-round2/iter-005-samples
)

echo "[train_big] 12 sample dirs, epochs=$EPOCHS, out=$OUT_NAME"
echo "[train_big] starting at $(date)"

python3 -u az/train_pytorch.py \
    --dataset-dirs "${SAMPLE_DIRS[@]}" \
    --out "$OUT_NAME" \
    --in-dim 132 \
    --h1 512 --h2 256 --h3 128 \
    --epochs "$EPOCHS" \
    --batch-size 4096 \
    --lr 0.001 \
    --fan-w 0.40 --foul-w 0.10 --policy-w 0.50 \
    --policy v0-big-v1 \
    --round 1

echo "[train_big] done at $(date)"
echo "[train_big] ckpt: $(realpath $OUT_NAME)"
ls -lh "$OUT_NAME" "${OUT_NAME%.json}-final.json" 2>/dev/null
