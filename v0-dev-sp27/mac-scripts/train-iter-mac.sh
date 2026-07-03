#!/usr/bin/env bash
# Mac — train 某 iter (读累积 dataset, 原样复刻 train_v3_purenn.sh 的 Phase B/C),
# 然后 bench 新 ckpt → 失败(✗)最少的 promote 成 best.json.
# 完事把 best.json 推回 GCP 供下一 iter gen (见结尾提示).
#
# 用法 (Mac 上):  bash mac-scripts/train-iter-mac.sh <iter>
# env: INIT_CKPT  首次 best.json 不存在时的 warm-start 起点
#      (默认 v3-train-i165-sp36-1/iter-4/round-002-acc94.json)
set -euo pipefail
ITER="${1:?用法: train-iter-mac.sh <iter>}"
DATA_VERSION=i168-sp37; RUN=gcp
DATASET_ROOT="v3-dataset-${DATA_VERSION}-${RUN}"
TRAIN_ROOT="v3-train-${DATA_VERSION}-${RUN}"
TRAIN_OUT="$TRAIN_ROOT/iter-$ITER"
BEST="$TRAIN_ROOT/best.json"
BIN=server-go-bin
mkdir -p "$TRAIN_OUT" "$BIN" "$TRAIN_ROOT"
( cd server-go && go build -o "../$BIN/ofc-train" ./cmd/train && go build -o "../$BIN/bench-cases" ./cmd/bench-cases )

# 首次 bootstrap best.json = round-002 (warm-start 起点); 用真文件不用 symlink
if [ ! -f "$BEST" ]; then
  INIT="${INIT_CKPT:-v3-train-i165-sp36-1/iter-4/round-002-acc94.json}"
  [ -f "$INIT" ] || { echo "FATAL: INIT_CKPT 不存在: $INIT"; exit 1; }
  cp "$INIT" "$BEST"; echo "首次 bootstrap: best.json ← $INIT"
fi

bench_fail() {  # echo "<fail> <line>"
  local ck="$1" line
  line=$(DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 \
    "$BIN/bench-cases" -ckpt "$ck" -cases cases/game-cases.json -workers 0 2>&1 | grep 结果 | tail -1)
  local f; f=$(echo "$line" | grep -oE "[0-9]+失败" | grep -oE "[0-9]+"); [ -z "$f" ] && f=999
  echo "$f|$line"
}

touch "$TRAIN_OUT/.iter_started"
echo "=== train iter-$ITER (warm-start $BEST, 读 $DATASET_ROOT 全部累积) ==="
"$BIN/ofc-train" -dataset-dir "$DATASET_ROOT" -dataset-keep-warm-start -hours 1 -round-min 30 \
  -outdim 4 -h1 512 -h2 256 -h3 128 -indim 168 \
  -epochs 30 -lr 0.001 -warm-lr-mult 0.2 -y-recompute \
  -fan-bonus-qq 10 -fan-bonus-kk 30 -fan-bonus-aa 100 -fan-bonus-trips 140 \
  -foul-cost 3 -fan-w 0.40 -foul-w 0.10 -policy-w 0.30 \
  -ckpt-dir "$TRAIN_OUT" -policy "v0-v3-sp-iter$ITER" -init-from-ckpt "$BEST"

echo "=== bench 本 iter 新 ckpt (纯NN DISABLE_HARD+SOFT, 挑失败最少) ==="
best_fail=999; best_ck=""
while IFS= read -r ck; do
  r=$(bench_fail "$ck"); f=${r%%|*}; line=${r#*|}
  echo "  $ck → $line"
  if [ "$f" -lt "$best_fail" ]; then best_fail=$f; best_ck=$ck; fi
done < <(find "$TRAIN_OUT" -name "round-*-acc*.json" -newer "$TRAIN_OUT/.iter_started" | sort)
[ -z "$best_ck" ] && { echo "⚠ 没产出新 ckpt (NaN?), 不动 best.json"; exit 1; }

r=$(bench_fail "$BEST"); old_fail=${r%%|*}
echo "=== iter-$ITER 最优 $best_ck (失败 $best_fail)  vs  旧 best (失败 $old_fail) ==="
if [ "$best_fail" -lt "$old_fail" ]; then
  rm -f "$BEST"; cp "$best_ck" "$BEST"   # rm 先防 symlink 跟随覆盖 init
  echo "✅ PROMOTE: best.json ← $best_ck (失败 $old_fail→$best_fail)"
else
  echo "⏸ DISCARD (新 $best_fail ≥ 旧 $old_fail); best.json 不变 (仍 = 旧). 注: iter-$ITER 数据仍会进下轮训练"
fi
echo ""
echo ">>> 下一步: 推 best.json 回 GCP 供 iter-$((ITER+1)) gen:"
echo "    rsync -az -e \"ssh -i \$GCP_KEY\" $BEST chguang@35.203.6.88:~/boluo-cc/v0-dev-sp27/$BEST"
