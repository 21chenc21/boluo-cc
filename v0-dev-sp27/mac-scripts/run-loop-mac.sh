#!/usr/bin/env bash
# Mac 一条龙 — 跑一次全搞定. 对 iter start..end 每轮自动:
#   ① GCP gen (128核8分片) → ② 拉数据 → ③ Mac train+bench+promote → ④ 推 best.json 回 GCP
# gen 有 .gen-done 标记则跳过 (断点续跑); 每轮 gen 用上一轮 promote 的 best.json 当 rollout policy.
#
# 用法 (Mac /Users/Chen/agents/boluo-cc/v0-dev-sp27):
#   bash mac-scripts/run-loop-mac.sh            # iter 1..5
#   bash mac-scripts/run-loop-mac.sh 3 5        # 从 iter 3 续
# env: GCP_KEY (默认 /Users/Chen/Documents/pem/gcp-chguang-new)
set -euo pipefail
START="${1:-1}"; END="${2:-5}"
export GCP_KEY="${GCP_KEY:-/Users/Chen/Documents/pem/gcp-chguang-new}"
GCP="chguang@34.19.194.234"
GCPDIR="boluo-cc/v0-dev-sp27"
SSH="ssh -i $GCP_KEY -o StrictHostKeyChecking=no"
DSROOT="v3-dataset-i165-sp35-gcp"; TRROOT="v3-train-i165-sp35-gcp"
[ -f "$GCP_KEY" ] || { echo "FATAL: GCP key 不在 $GCP_KEY"; exit 1; }

# 确保 round-002 在本地 (train 首次 bootstrap best.json 用)
mkdir -p v3-train-i165-sp33-1/iter-1
[ -f v3-train-i165-sp33-1/iter-1/round-002-acc95.json ] || \
  rsync -az -e "$SSH" "$GCP:$GCPDIR/v3-train-i165-sp33-1/iter-1/round-002-acc95.json" v3-train-i165-sp33-1/iter-1/

for N in $(seq "$START" "$END"); do
  echo ""; echo "########## ITER $N  ($(date +%H:%M:%S)) ##########"
  # 等 GCP 上现有 gen 跑完 (防并发)
  while [ "$($SSH $GCP 'pgrep -x gen-rollout-dat | wc -l')" -gt 0 ]; do echo "  ...等 GCP 现有 gen 跑完"; sleep 30; done
  # ① gen (有完成标记则复用)
  if [ "$($SSH $GCP "[ -f $GCPDIR/$DSROOT/iter-$N/.gen-done ] && echo y || echo n")" = y ]; then
    echo "[$N] ① gen: GCP 已有完整数据, 跳过"
  else
    echo "[$N] ① GCP gen (清旧+重跑, 阻塞到完成)..."
    $SSH $GCP "cd $GCPDIR && rm -rf $DSROOT/iter-$N && bash mac-scripts/gen-iter-gcp.sh $N"
  fi
  # ② 拉数据
  echo "[$N] ② 拉数据..."; bash mac-scripts/pull-gen-from-gcp.sh "$N"
  # ③ train + bench + promote (1-2h)
  echo "[$N] ③ train + promote..."; bash mac-scripts/train-iter-mac.sh "$N"
  # ④ 推 best.json 回 GCP (供下一轮 gen)
  echo "[$N] ④ 推 best.json 回 GCP..."; rsync -az -e "$SSH" "$TRROOT/best.json" "$GCP:$GCPDIR/$TRROOT/best.json"
  echo "[$N] ✓ 完成"
done
echo ""; echo "===== 全部 iter $START..$END 完成. best = $TRROOT/best.json ====="
