#!/usr/bin/env bash
# Mac — 从 GCP gen farm 拉某 iter 的 dataset 到本地 (累积, 不删旧 iter — 跟原代码一样,
# train 递归读整个 DATASET_ROOT, iter-1..N 全用上, 见 filepath.Walk).
#
# 用法 (Mac 上):  bash mac-scripts/pull-gen-from-gcp.sh <iter>
# env: GCP_HOST(默认 chguang@34.19.194.234)  GCP_KEY(默认 ~/.ssh/gcp-chguang-new)
set -euo pipefail
ITER="${1:?用法: pull-gen-from-gcp.sh <iter>}"
GCP_HOST="${GCP_HOST:-chguang@34.19.194.234}"
GCP_KEY="${GCP_KEY:-$HOME/.ssh/gcp-chguang-new}"
BASE="v3-dataset-i165-sp34-gcp/iter-$ITER"
REMOTE="boluo-cc/v0-dev-sp27/$BASE"
[ -f "$GCP_KEY" ] || { echo "FATAL: GCP key 不在 $GCP_KEY (把 gcp-chguang-new 拷到 Mac ~/.ssh/)"; exit 1; }
mkdir -p "$BASE"
echo "拉 iter-$ITER: $GCP_HOST:~/$REMOTE → $BASE"
rsync -az --info=progress2 -e "ssh -i $GCP_KEY -o StrictHostKeyChecking=no" \
  "$GCP_HOST:$REMOTE/" "$BASE/"
echo "iter-$ITER: $(find "$BASE" -name '*.jsonl.gz'|wc -l) 文件 / $(du -sh "$BASE"|cut -f1)"
echo "累积总数据 (train 会全读): $(find v3-dataset-i165-sp34-gcp -name '*.jsonl.gz'|wc -l) 文件"
