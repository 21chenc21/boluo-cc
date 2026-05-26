#!/usr/bin/env bash
# train_v3_iter.sh — V3 features 迭代训练
#
# 每 iter:
#   1. gen 500 games (用 best V3 ckpt 当 rollout policy; iter 1 用 embed default)
#   2. train 在累计 dataset 上 (warm-start from best 如果存在)
#   3. bench DISABLE_MCTS=1
#   4. 比 best 提升 → PROMOTE; 不提升 → DISCARD
#
# 用法:
#   bash mac-scripts/train_v3_iter.sh [ITERS] [GAMES_PER_ITER]
#   bash mac-scripts/train_v3_iter.sh 5 500    # 5 iter, 500 game/iter ~25-30h
#   bash mac-scripts/train_v3_iter.sh 3 300    # 3 iter, 300 game/iter ~10h
#
# 输出:
#   v3-dataset/iter-N/round*/                 第 N iter 生成的 samples
#   v3-train/iter-N/round-NNN-accXX.json      第 N iter 训练 ckpt
#   v3-train/best.json                        当前最强 ckpt (symlink)
#   /tmp/v3-iter-<ts>.log                     全 log

# ====================================================================
# SCRIPT_VERSION — 任何脚本/代码改动 bump, 启动打印, 用户 rsync 判断同步.
SCRIPT_VERSION="2026-05-19g"
#
# DATA_VERSION — 仅在数据布局变 (inDim / feature schema / label scale) bump.
#                文件夹后缀: v3-dataset-${DATA_VERSION} / v3-train-${DATA_VERSION}.
#                不同 DATA_VERSION 完全隔离, 不可能串数据. 软改动 (skip+warn, log清理)
#                保持同 DATA_VERSION, 复用已有训练.
DATA_VERSION="i147"
#
# 改动历史:
#   2026-05-19a: 加 SCRIPT_VERSION 打印; 加 preflight (-dataset-keep-warm-start); 加
#                best.json 自愈; bench 所有 round-* 取 max promote; gen 删 verbose log;
#                train.go 加 -dataset-keep-warm-start flag 保 round-2 warm-start.
#   2026-05-19b: V3 features 131 → 147 (Tier 1+2+3: L cross-row + LR locked/draws +
#                N/N2 discard 真实信号). [DATA_VERSION i131 → i147]
#   2026-05-19c: train.go loadOracleSamples 改 skip+warn corrupt shard 不再 fatal.
#                [SCRIPT 改, DATA_VERSION 不变 i147]
#   2026-05-19d: 文件夹后缀化 (v3-dataset-i147 / v3-train-i147), 弃 .indim marker.
#                [SCRIPT 改, DATA_VERSION 不变 i147]
#   2026-05-19e: 修 Phase C mapfile 不兼容 Mac 默认 bash 3.2, 换 while read 实现.
#                [SCRIPT 改, DATA_VERSION 不变 i147]
#   2026-05-19f: 修 SCRIPT_MTIME 在 Linux 上乱码 (stat -f 是 fs info 不是 mtime).
#                改 uname 判断 Darwin / 其它.  [SCRIPT 改, DATA_VERSION 不变 i147]
# ====================================================================

set -e

ITERS="${1:-5}"
GAMES="${2:-500}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
V0_DIR="$(dirname "$SCRIPT_DIR")"
cd "$V0_DIR"

BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
TRAIN_BIN="$BIN_DIR/ofc-train"
GEN_BIN="$BIN_DIR/gen-rollout-dataset"
BENCH_BIN="$BIN_DIR/bench-cases"

# Build/rebuild — 总是跑 go build (incremental, 没改动则秒返回).
# 这样新拉源码自动 pick up, 不会用过期 binary 跑几小时浪费.
echo "[v3-iter] (re)build binaries..."
for tool in ofc-train gen-rollout-dataset bench-cases case-train; do
    case "$tool" in
        ofc-train) SRC="./cmd/train" ;;
        *) SRC="./cmd/$tool" ;;
    esac
    if ! (cd server-go && go build -o "$BIN_DIR/$tool" "$SRC") 2>&1; then
        echo "[v3-iter] FATAL: $tool build 失败" >&2
        exit 1
    fi
done

# Preflight: 验证 ofc-train 支持本脚本需要的 flag (防 binary 跟脚本不同步).
# 这里只检 -dataset-keep-warm-start (2026-05-19 加的, rollout-dataset 必需).
REQUIRED_FLAGS=("-dataset-keep-warm-start")
TRAIN_HELP=$("$TRAIN_BIN" -h 2>&1 || true)
for f in "${REQUIRED_FLAGS[@]}"; do
    if ! echo "$TRAIN_HELP" | grep -q -- "$f"; then
        echo "[v3-iter] FATAL: ofc-train 缺 flag $f. 删 $TRAIN_BIN 重 build, 或源码没 sync 干净." >&2
        exit 1
    fi
done
echo "[v3-iter] preflight OK: ofc-train 支持 ${REQUIRED_FLAGS[*]}"

DATASET_ROOT="v3-dataset-$DATA_VERSION"
TRAIN_ROOT="v3-train-$DATA_VERSION"
BEST_LINK="$TRAIN_ROOT/best.json"
LOG="/tmp/v3-iter-$(date +%Y%m%d-%H%M%S).log"

mkdir -p "$DATASET_ROOT" "$TRAIN_ROOT"

# Prune corrupt shards 启动时跑一次 (2026-05-19g): ctrl-c 残留半写 .jsonl.gz 累积.
if [ -d "$DATASET_ROOT" ]; then
    PRUNED=0
    while IFS= read -r f; do
        if ! gzip -t "$f" 2>/dev/null; then
            rm -f "$f" && PRUNED=$((PRUNED + 1))
        fi
    done < <(find "$DATASET_ROOT" -name '*.jsonl.gz' 2>/dev/null)
    if [ "$PRUNED" -gt 0 ]; then
        echo "[v3-iter] pruned $PRUNED corrupt shard(s) from $DATASET_ROOT"
    fi
fi

if [ "$(uname)" = "Darwin" ]; then
    SCRIPT_MTIME=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M" "${BASH_SOURCE[0]}" 2>/dev/null)
else
    SCRIPT_MTIME=$(stat -c "%y" "${BASH_SOURCE[0]}" 2>/dev/null | cut -d. -f1)
fi
echo "[v3-iter] === SCRIPT_VERSION=$SCRIPT_VERSION (mtime=$SCRIPT_MTIME) ===" | tee -a "$LOG"
echo "[v3-iter] === DATA_VERSION=$DATA_VERSION → $DATASET_ROOT / $TRAIN_ROOT ===" | tee -a "$LOG"
echo "[v3-iter] iters=$ITERS, games-per-iter=$GAMES" | tee -a "$LOG"
echo "[v3-iter] log=$LOG" | tee -a "$LOG"
echo "[v3-iter] start $(date)" | tee -a "$LOG"
echo "" | tee -a "$LOG"

BEST_TC=0
BEST_CKPT=""

# best.json 处理:
#   场景 1: 软链或文件存在 + target 有效 → bench 取分, 续跑
#   场景 2: 链子断了 (rsync 丢了 target) 或 0 字节 → 删掉, 尝试从 iter-*/round-*.json 找最新的当 fallback
#   场景 3: 啥也没有 → 从零 (iter-1 fresh)
if [ -L "$BEST_LINK" ] && [ ! -e "$BEST_LINK" ]; then
    echo "[v3-iter] WARN: best.json 是断链 (rsync 没带 -l 保符号链?), 删除并 fallback" | tee -a "$LOG"
    rm -f "$BEST_LINK"
fi
if [ ! -e "$BEST_LINK" ]; then
    # Fallback: 找现有 iter-*/round-*.json 里最新的
    LATEST_CKPT=$(ls -t "$TRAIN_ROOT"/iter-*/round-*-acc*.json 2>/dev/null | head -1)
    if [ -n "$LATEST_CKPT" ]; then
        REL_PATH=$(echo "$LATEST_CKPT" | sed "s|^$TRAIN_ROOT/||")
        echo "[v3-iter] best.json 缺失, 自动链到最新 ckpt: $REL_PATH" | tee -a "$LOG"
        ln -sf "$REL_PATH" "$BEST_LINK"
    fi
fi
if [ -e "$BEST_LINK" ]; then
    RESOLVED=$(readlink -f "$BEST_LINK" 2>/dev/null || echo "$BEST_LINK")
    if [ -f "$RESOLVED" ]; then
        echo "[v3-iter] 检测到现有 best.json → $RESOLVED, bench 取分..." | tee -a "$LOG"
        BENCH_OUT=$(DISABLE_MCTS=1 "$BENCH_BIN" -ckpt "$RESOLVED" -cases cases/all-tests-expanded.json -workers 0 2>&1 | tail -3)
        EXISTING_TC=$(echo "$BENCH_OUT" | grep -oE "[0-9]+通过" | head -1 | grep -oE "[0-9]+")
        if [ -n "$EXISTING_TC" ] && [ "$EXISTING_TC" -gt 0 ]; then
            BEST_TC=$EXISTING_TC
            BEST_CKPT="$RESOLVED"
            echo "[v3-iter] 续跑: best=$BEST_CKPT ($BEST_TC/63), 后续 iter 从此 warm-start" | tee -a "$LOG"
        else
            echo "[v3-iter] WARN: best.json bench 失败或 0 分, 忽略, 从零开始" | tee -a "$LOG"
        fi
    fi
fi

for ((iter=1; iter<=ITERS; iter++)); do
    ITER_TS=$(date +%H:%M:%S)
    echo "=== ITER $iter / $ITERS ($ITER_TS) ===" | tee -a "$LOG"

    GEN_OUT="$DATASET_ROOT/iter-$iter"
    TRAIN_OUT="$TRAIN_ROOT/iter-$iter"
    mkdir -p "$GEN_OUT" "$TRAIN_OUT"
    touch "$TRAIN_OUT/.iter_started"

    # Phase A: gen samples
    # 2026-05-18 D-mode 设计 (smoke 实测验证):
    #   rollout policy = big-model-v3 (V2 132-d, 47/63 真 baseline NN)
    #   rollouts = 20 (而非 500), 因 big NN 慢 22×, 减 25× rollouts 抵消
    #   trade-off: label SE 增 5× (σ/√20≈6.7), 换强 baseline 47/63 quality 的 labels
    #   速度: Mac 8-core ~13 g/min, 500 games ~37 min, 5 iter ~3.5h ✓
    BASELINE_POLICY="big-models/big-model-v3.json"
    echo "[iter $iter] Phase A: gen $GAMES games (rollouts=20, indim 147, policy=big-model-v3 47/63, D-mode)..." | tee -a "$LOG"
    GEN_ARGS=(-num-games "$GAMES" -jokers 2 -rollouts 20 -r1-cap 30
              -phantom-opponents 2 -indim 147
              -foul-cost 6 -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 80 -fan-bonus-trips 90
              -out-dir "$GEN_OUT"
              -weights "$BASELINE_POLICY")
    set -o pipefail
    if ! "$GEN_BIN" "${GEN_ARGS[@]}" 2>&1 | tee -a "$LOG"; then
        echo "[iter $iter] FATAL: gen 失败" | tee -a "$LOG"
        set +o pipefail
        continue
    fi
    set +o pipefail

    # Phase B: train on ALL accumulated iter-* dirs
    echo "[iter $iter] Phase B: train V3 (warm from best if exists)..." | tee -a "$LOG"
    # rollout-dataset 模式: -dataset-keep-warm-start 保 round-2 接 round-1 不被 fresh 砸 (2026-05-19 fix)
    TRAIN_ARGS=(-dataset-dir "$DATASET_ROOT" -dataset-keep-warm-start -hours 1 -round-min 30
                -outdim 4 -h1 512 -h2 256 -h3 128 -indim 147
                -epochs 30 -lr 0.001 -warm-lr-mult 0.5
                -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 80 -fan-bonus-trips 90
                -foul-cost 6 -fan-w 0.40 -foul-w 0.10 -policy-w 0.30
                -ckpt-dir "$TRAIN_OUT" -policy "v0-v3-iter$iter")
    if [ -n "$BEST_CKPT" ] && [ -f "$BEST_CKPT" ]; then
        TRAIN_ARGS+=(-init-from-ckpt "$BEST_CKPT")
        echo "[iter $iter] warm-start train from $BEST_CKPT" | tee -a "$LOG"
    fi
    # train + capture exit code (pipefail 防 tee 屏蔽)
    set -o pipefail
    if ! "$TRAIN_BIN" "${TRAIN_ARGS[@]}" 2>&1 | tee -a "$LOG"; then
        echo "[iter $iter] FATAL: train 失败 (check log above for unexpected EOF / corrupted shard)" | tee -a "$LOG"
        set +o pipefail
        continue
    fi
    set +o pipefail

    # Phase C: bench 所有本 iter 新生的 round-* ckpt, 取最高分 promote
    # (2026-05-19 fix: 旧版只 bench 字典序最大的 round-002, 错失 round-001 warm-start 那个;
    #  实测 iter-2 r1=26 vs r2=24, iter-4 r1=27 vs r2=20 — round-1 经常更高)
    # 注: 用 while read 而非 mapfile, 兼容 Mac 默认 bash 3.2 (mapfile 是 bash 4+).
    NEW_CKPTS=()
    while IFS= read -r line; do
        NEW_CKPTS+=("$line")
    done < <(find "$TRAIN_OUT" -name "round-*-acc*.json" -newer "$TRAIN_OUT/.iter_started" 2>/dev/null | sort)
    if [ "${#NEW_CKPTS[@]}" -eq 0 ]; then
        while IFS= read -r line; do
            NEW_CKPTS+=("$line")
        done < <(ls -t "$TRAIN_OUT"/round-*-acc*.json 2>/dev/null | head -2)
    fi
    if [ "${#NEW_CKPTS[@]}" -eq 0 ]; then
        echo "[iter $iter] ERROR: no ckpt produced" | tee -a "$LOG"
        continue
    fi

    NEW_TC=0
    NEW_CKPT=""
    for ck in "${NEW_CKPTS[@]}"; do
        echo "[iter $iter] Phase C: bench $ck" | tee -a "$LOG"
        BENCH_OUT=$(DISABLE_MCTS=1 "$BENCH_BIN" -ckpt "$ck" -cases cases/all-tests-expanded.json -workers 0 2>&1 | tail -3)
        echo "$BENCH_OUT" | tee -a "$LOG"
        TC=$(echo "$BENCH_OUT" | grep -oE "[0-9]+通过" | head -1 | grep -oE "[0-9]+")
        [ -z "$TC" ] && TC=0
        echo "[iter $iter]   $ck → $TC/63" | tee -a "$LOG"
        if [ "$TC" -gt "$NEW_TC" ]; then
            NEW_TC=$TC
            NEW_CKPT="$ck"
        fi
    done

    # Compare to best
    echo "" | tee -a "$LOG"
    echo "[iter $iter] best of iter: $NEW_CKPT → $NEW_TC/63, prev best=$BEST_TC/63" | tee -a "$LOG"
    if [ "$NEW_TC" -gt "$BEST_TC" ]; then
        BEST_TC=$NEW_TC
        BEST_CKPT="$NEW_CKPT"
        ln -sf "../$NEW_CKPT" "$BEST_LINK" 2>/dev/null || cp "$NEW_CKPT" "$BEST_LINK"
        echo "[iter $iter] ✓ PROMOTE new → best ($NEW_TC/63)" | tee -a "$LOG"
    else
        echo "[iter $iter] ✗ DISCARD (no improvement; best stays $BEST_TC/63)" | tee -a "$LOG"
    fi
    echo "" | tee -a "$LOG"
done

echo "=== DONE $(date) ===" | tee -a "$LOG"
echo "[v3-iter] best ckpt: $BEST_CKPT" | tee -a "$LOG"
echo "[v3-iter] best testcase: $BEST_TC/63" | tee -a "$LOG"
echo "[v3-iter] log: $LOG"
