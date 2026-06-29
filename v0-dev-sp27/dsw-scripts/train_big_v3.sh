#!/usr/bin/env bash
# train_big_v3.sh — conservative fine-tune (避免 v2 漂离 baseline)
#
# v2 训 50 epoch lr=0.0005, val_loss 0.50→0.24 大幅过拟合 → bench -2
# v3 试 conservative: 短 epoch + 低 lr, 大模型保留 baseline 政策 + 微调

set -e

OUT_NAME="${1:-big-model-v3.json}"
EPOCHS="${2:-15}"
LR="${3:-0.0001}"

cd /mnt/workspace/v0-dev

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

echo "[v3] conservative fine-tune from big-model-warmstart"
echo "[v3] epochs=$EPOCHS lr=$LR out=$OUT_NAME"
echo "[v3] starting at $(date)"

python3 -u az/train_pytorch.py \
    --dataset-dirs "${SAMPLE_DIRS[@]}" \
    --out "$OUT_NAME" \
    --warm-ckpt big-model-warmstart.json \
    --in-dim 132 \
    --h1 512 --h2 256 --h3 128 \
    --epochs "$EPOCHS" \
    --batch-size 4096 \
    --lr "$LR" \
    --fan-w 0.40 --foul-w 0.10 --policy-w 0.50 \
    --policy v0-big-v3 \
    --round 1

echo "[v3] done at $(date)"
