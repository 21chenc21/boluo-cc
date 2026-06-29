#!/usr/bin/env bash
# train_big_v2.sh — big model v2: warm-start from baseline expansion
#
# 起点: big-model-warmstart.json (从 round-001-acc89 数学等价扩展, bench = 51/63)
# 目标: fine-tune 大模型 capacity 学到比 baseline 更强的 representation
#
# 用法: bash dsw-scripts/train_big_v2.sh [OUT] [EPOCHS] [LR]

set -e

OUT_NAME="${1:-big-model-v2.json}"
EPOCHS="${2:-50}"
LR="${3:-0.0005}"   # warm-start 用低 LR 避免覆盖 baseline 知识

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

if [ ! -f big-model-warmstart.json ]; then
    echo "[train_big_v2] big-model-warmstart.json missing — run expand_ckpt_to_big.py first" >&2
    exit 1
fi

echo "[train_big_v2] epochs=$EPOCHS, lr=$LR, out=$OUT_NAME"
echo "[train_big_v2] warm-start from big-model-warmstart.json (bench=51/63 baseline)"
echo "[train_big_v2] starting at $(date)"

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
    --policy v0-big-v2 \
    --round 1

echo "[train_big_v2] done at $(date)"
ls -lh "$OUT_NAME" "${OUT_NAME%.json}-final.json" 2>/dev/null
