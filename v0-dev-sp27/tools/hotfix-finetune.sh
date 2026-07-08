#!/usr/bin/env bash
# hotfix-finetune.sh <fam粮dir> <case号> [epochs=3] [init_ckpt] — 太子成年微调 (playbook 二·七 步骤2)
# 链式治疗: 第4参传上一发ckpt (如 v3-train-hf-fuse4d2/round-001-acc94.json), 省略=tools/CHAMPION
set -euo pipefail
cd "$(dirname "$0")/.."
FAMDIR="${1:?用法: hotfix-finetune.sh <fam粮dir> <case号> [epochs]}"
CASE="${2:?case号}"
EPOCHS="${3:-3}"
E="${4:-$(cat tools/CHAMPION)}"
OUT="v3-train-hf-${CASE}"
( cd server-go && go build -o ../server-go-bin/train ./cmd/train )
mkdir -p "$OUT"
./server-go-bin/train \
  -dataset-dir "$FAMDIR" -dataset-keep-warm-start \
  -hours 0.2 -round-min 12 \
  -outdim 4 -h1 512 -h2 256 -h3 128 -indim 169 \
  -epochs "$EPOCHS" -lr 0.001 -warm-lr-mult 0.025 -y-recompute \
  -fan-bonus-qq 10 -fan-bonus-kk 30 -fan-bonus-aa 100 -fan-bonus-trips 140 \
  -foul-cost 6 -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  -init-from-ckpt "$E" \
  -ckpt-dir "$OUT" -policy "hf-${CASE}"
echo "→ $OUT/round-001-*.json  (下一步: tools/bench-verify.sh 它)"
