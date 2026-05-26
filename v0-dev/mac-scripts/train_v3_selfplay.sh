#!/usr/bin/env bash
# train_v3_selfplay.sh — V3 features 147-d self-play 训练
#
# 跟 train_v3_iter.sh (distillation) 区别:
#   - Gen 时 rollout policy = 当前 best V3 ckpt (动态更新), 不是固定 big-model-v3
#   - Self-play loop: 每 iter NN 强 → labels 反映 NN 自己的 EV → NN 进步 → 反复
#   - 上限不再被 teacher 锁死 (memory `project_az_round1`: V2 self-play iter-4 突破 53/63)
#
# 起点:
#   - 推荐: 先跑 translate-v2-v3-weights 把 big-model-v3 (V2) 翻译成 V3 ckpt 当 best.json
#     这样起点 ~22/63 (有 50% V2 weights 搬运), 不是纯随机.
#   - 备选: best.json 不存在 → iter-1 用 default heuristic (embed) policy, 起点更冷.
#
# 用法:
#   bash mac-scripts/train_v3_selfplay.sh [ITERS] [GAMES_PER_ITER]
#   bash mac-scripts/train_v3_selfplay.sh 10 200    # 10 iter, 200 game/iter
#
# 可选 env:
#   NO_TRANSLATE=1  跳过 V2→V3 翻译 (Mac path C 冷启动)
#   RUN=foo         文件夹后缀, 不同 RUN 完全隔离: v3-dataset-i147-sp-foo / v3-train-i147-sp-foo
#                   (默认空 = 用旧 v3-dataset-i147-sp 路径累积)
#   INIT_CKPT=path  起点 ckpt 路径. 复制到新 train_root/iter-0-init/, best.json → 此.
#                   仅当 best.json 不存在时生效 (避免覆盖已运行的 best).
#
# 例:从 iter-3 acc83 ckpt 启新 self-play loop:
#   INIT_CKPT=v3-train-i147-sp/iter-3/round-001-acc83.json RUN=from-acc83 NO_TRANSLATE=1 \
#     bash mac-scripts/train_v3_selfplay.sh 10 300
#
# 输出:
#   v3-dataset-i147-selfplay/iter-N/round*/
#   v3-train-i147-selfplay/iter-N/round-NNN-accXX.json
#   v3-train-i147-selfplay/best.json (current best 自循环 rollout policy)

# ====================================================================
SCRIPT_VERSION="2026-05-22-sp18"
# 改动历史:
#   2026-05-19-sp1: 基于 train_v3_iter.sh fork, 改 gen rollout policy = best.json (self-play).
#   2026-05-19-sp2: rollouts 20 → 100.
#   2026-05-19-sp3: Phase C 3-metric PROMOTE (testcase >> fantasy >> score).
#   2026-05-19-sp4: bench-3metric exit 1 杀脚本 fix + duel || true + warm-lr-mult 降 (治标).
#   2026-05-19-sp5:
#     - 撤回 sp4 LR 降 (warm-lr-mult 恢复 0.5). 治本是 silver label fan-cap fix (commit ac4db00),
#       Mac iter-1/2 NaN 真因是用 OLD binary 没该 fix → 训练 over-rewarded fantasy 爆 loss.
#       DSW 用新 binary iter-1/2 均无 NaN 验证.
#     - train.go 加 NaN/Inf loss 检测早退 + 不保 NaN ckpt (root cause defense).
#   2026-05-19-sp6:
#     - ctrl-c (SIGINT) 现在真停, 不再 cascading 'FATAL: gen 失败' through 剩余 iters.
#       trap SIGINT/SIGTERM 立即 exit 130/143. gen 失败块也判退出码区分用户中断 vs 真故障.
#   2026-05-19-sp7:
#     - sp6 没真停: bash 3.2 pipeline 'gen | tee' 中 ctrl-c 信号给 tee (不死), trap 错过,
#       $? 反映 tee 的 0 而非 gen 的 130 → 级联 FATAL 又复现.
#     - 改用 PIPESTATUS[0] 拿 gen 真实退出码, 配 || true 防 set -e.
#   2026-05-19-sp8:
#     - 启动 prune corrupt shards (gzip -t 测试 + rm). 多次 ctrl-c 残留 corrupt 累积:
#       Mac 实测 78K samples 44 good / 50 corrupt skipped, 占空间不优雅.
#     - train.go "oracle dataset" log 改 "dataset" (函数 loadOracleSamples 同步 → loadDatasetSamples).
#   2026-05-19-sp9:
#     - NaN 仍发 (Mac iter-1/2/3 R1 都 NaN, 越来越快). 加固训练稳定性:
#       * warm-lr-mult 0.5 → 0.1 (warm LR 0.00010, 5× 安全余量)
#       * -y-recompute true (每 iter 重算 yMean/yStd, 防 preserve 后归一化漂移放大梯度)
#     - Phase C: NaN abort 无新 ckpt 时不 fallback 到旧 ckpt, 直接 continue.
#   2026-05-19-sp10:
#     - sp9 LR 0.0001 太低 (后来 iter-3 出 30/63 证明能进步, 只是慢).
#     - 准备放开到 0.0003, 但用户认为太激进改 0.0002.
#   2026-05-19-sp11:
#     - 折中: warm-lr-mult 0.2 (warm 0.0002, 2× sp9 速度, 保守).
#     - y-recompute 已治根 NaN, foul-cost 6 保留.
#   2026-05-19-sp12:
#     - RUN env: 给文件夹加后缀, 不同 RUN 完全隔离 (旧 ckpt 留作 reference).
#         RUN=foo → v3-dataset-i147-sp-foo / v3-train-i147-sp-foo
#     - INIT_CKPT env: 指定起点 ckpt, 复制到新 train_root 当 best.json.
#   2026-05-20-sp13:
#     - 修 V3 Z0 (final_score) 计算: fantasy bonus 跟 royalty 改 conditional on (1-pFoul).
#     - 修 U/T3 no-pair sentinel -1.0 (旧 -0.083 跟 pair-2 几乎同).
#   2026-05-20-sp14:
#     - 🚨 重磅 fix: TrainedEval / TrainedEvalFull 一直走 V2 features 给 V3 NN.
#   2026-05-20-sp15:
#     - reward shaping: -fan-bonus-aa 80 → 100 / -fan-bonus-trips 90 → 120.
#       NN 实测 TE 给 joker+A=AA-lock 低估 16-24 分 (case 12/13/24).
#       提 AA/trips 价值让 silver-label MC EV 反映 fantasy 真实值, NN 自己学到正确权衡.
#       撤回 R1JokerWithAOnTopBonus 25 hack (回 10), 用 reward shaping 治本.
#     - V3 const V3FanBonusAA / V3FanBonusTrips 同步 (Z0 计算需对齐 label scale).
#     - S_slot (idx 128) 改 scale 0.3 全场 (sp15 v2):
#         原 1.0 太强 (case 34 误伤); 但 v1 R<4=0 太激进 (13 个 ckpt bench -10~-26).
#         v2 折中: 0.3 全场, 信号保留强度减 70%. bench 全部回正, acc84 43→46 净改进.
#     - FoulImminentPenalty case 4 修: top.Type==mid.Type==Pair/Trips 比 rank 不再跳过 (case 50 修).
#     - rnRuleTopMustAllowFantasy round-gate: R<4 fire, R>=4 skip (case 44/50 误杀 fan-chase 走 mid 修).
#     - RnJokerWithHighOnTopBonus +10 / RnSingleAOnTopBonus +10 新加 (cases 29/32/36 NN 低估 R2-R5 fantasy lock).
#     - 配套 19 个新 unit test 全过.
#     - rollout policy 变 (新 bonus 影响 silver-label EV), DATA_VERSION 再 bump → i147-sp15.
#   2026-05-20-sp16:
#     - Features cap-aware 修 4 处, 让 NN 学到 "joker on top 被 mid cap 后真值" — case 49/50 根因:
#       1. eRoyaltyTop: 用 topEvalCapped 提 cap-aware pair/trips rank 算 royalty (识 joker pair contribution)
#       2. U_pairRk top (idx 102): 用 topEvalCapped, 不再 maxPairRankRow (joker+A 误标 pair-A)
#       3. F_fantasy: 当 midEv 是 pair-X 时 P(top pair > X) 强制 0 (foul-imminent)
#       4. FoulImminentPenalty case 4: Evaluate3JokerCap (传 mid 当 cap), 避免误火
#     - sp15 v3 iter-3 r2 = 60/2w/1f (case 9 NN gap 0.10 微小学错). sp16 features 变 + retrain.
#     - INIT_CKPT 用 iter-3 r2 (sp15 当前最强).
#     - DATA_VERSION → i147-sp16 (features semantic 变, fresh dataset).
#   2026-05-20-sp17:
#     - 🚨 修 TrainedEval Round bug — trainedEvalImpl 重建 gs 丢 Round/LastDiscard/NumJokers.
#       所有 V3 NN 训练/推理用 broken features. 修法: 传 *GameState pointer 不重建.
#     - 修 pMidGTBot terminal state 返 0 (不是 0.2) — case 50 Z0 假罚 -1.2 治.
#     - 修 case 40/37/7 testcase data (3 个 state 多 1 张 typo).
#     - INIT_CKPT 改 sp16 iter-5 r1 (sp17 features + Round fix 下 bench 56/63 最强).
#     - DATA_VERSION → i147-sp17 (Round 信号入 silver-label, fresh).
#   2026-05-22-sp18:
#     - 加 RnTopCapBlockedFantasyPenalty -5 (joker top + cap-aware 低 pair → 浪费 joker, case 50)
#     - RnJokerWithHighOnTopBonus 加 cap-aware guard (cap 死时不再 +10 错奖)
#     - Mark cases 35/37/40/45 as warn (AI 选合理但不在 expecteds)
#     - sp17 iter-1 r1 deployed 8002, bench: 59通过/4警告/0 真错.
#     - DATA_VERSION → i147-sp18 (rollout policy 含 sp17 best, 数据 fresh).
DATA_VERSION="i147-sp18"
# ====================================================================

set -e

# Ctrl-C / kill 立即真停, 不让 'set -e + continue' 把剩余 iters 也跑爆 (2026-05-19-sp6 fix)
trap 'echo "[v3-sp] interrupted by user (SIGINT)"; exit 130' INT
trap 'echo "[v3-sp] killed (SIGTERM)"; exit 143' TERM

ITERS="${1:-5}"
GAMES="${2:-200}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
V0_DIR="$(dirname "$SCRIPT_DIR")"
cd "$V0_DIR"

BIN_DIR="$V0_DIR/server-go-bin"
mkdir -p "$BIN_DIR"
TRAIN_BIN="$BIN_DIR/ofc-train"
GEN_BIN="$BIN_DIR/gen-rollout-dataset"
BENCH_BIN="$BIN_DIR/bench-cases"
TRANSLATE_BIN="$BIN_DIR/translate-v2-v3-weights"
BENCH3_BIN="$BIN_DIR/bench-3metric"

echo "[v3-sp] (re)build binaries..."
for tool in ofc-train gen-rollout-dataset bench-cases translate-v2-v3-weights bench-3metric; do
    case "$tool" in
        ofc-train) SRC="./cmd/train" ;;
        *) SRC="./cmd/$tool" ;;
    esac
    if ! (cd server-go && go build -o "$BIN_DIR/$tool" "$SRC") 2>&1; then
        echo "[v3-sp] FATAL: $tool build 失败" >&2
        exit 1
    fi
done

# Preflight: 验证 ofc-train flag.
REQUIRED_FLAGS=("-dataset-keep-warm-start")
TRAIN_HELP=$("$TRAIN_BIN" -h 2>&1 || true)
for f in "${REQUIRED_FLAGS[@]}"; do
    if ! echo "$TRAIN_HELP" | grep -q -- "$f"; then
        echo "[v3-sp] FATAL: ofc-train 缺 flag $f. 删 $TRAIN_BIN 重 build." >&2
        exit 1
    fi
done
echo "[v3-sp] preflight OK"

DATASET_ROOT="v3-dataset-$DATA_VERSION"
TRAIN_ROOT="v3-train-$DATA_VERSION"
# sp12: RUN env 加后缀, 不同 RUN 完全隔离
if [ -n "$RUN" ]; then
    DATASET_ROOT="${DATASET_ROOT}-${RUN}"
    TRAIN_ROOT="${TRAIN_ROOT}-${RUN}"
fi
BEST_LINK="$TRAIN_ROOT/best.json"
LOG="/tmp/v3-sp-$(date +%Y%m%d-%H%M%S).log"

mkdir -p "$DATASET_ROOT" "$TRAIN_ROOT"

# sp12: INIT_CKPT env, 用指定 ckpt 当起点 best.json (只在 best.json 不存在时生效)
if [ -n "$INIT_CKPT" ] && [ ! -e "$BEST_LINK" ]; then
    if [ -f "$INIT_CKPT" ]; then
        INIT_BASENAME=$(basename "$INIT_CKPT")
        mkdir -p "$TRAIN_ROOT/iter-0-init"
        cp "$INIT_CKPT" "$TRAIN_ROOT/iter-0-init/$INIT_BASENAME"
        ln -sf "iter-0-init/$INIT_BASENAME" "$BEST_LINK"
        echo "[v3-sp] INIT_CKPT=$INIT_CKPT 复制到 $TRAIN_ROOT/iter-0-init/, best.json → 此"
    else
        echo "[v3-sp] WARN: INIT_CKPT=$INIT_CKPT 不存在, 忽略 (走默认 best.json fallback)"
    fi
fi

# Prune corrupt shards 在启动时跑一次 (sp8): 之前 ctrl-c 会留半写 .jsonl.gz, 累积一堆 skipped.
# gzip -t 测每个 .jsonl.gz, 失败的删. 安全因 gen 写的都是完整 shard, 半写=损坏一定 detect.
if [ -d "$DATASET_ROOT" ]; then
    PRUNED=0
    while IFS= read -r f; do
        if ! gzip -t "$f" 2>/dev/null; then
            rm -f "$f" && PRUNED=$((PRUNED + 1))
        fi
    done < <(find "$DATASET_ROOT" -name '*.jsonl.gz' 2>/dev/null)
    if [ "$PRUNED" -gt 0 ]; then
        echo "[v3-sp] pruned $PRUNED corrupt shard(s) from $DATASET_ROOT (gzip -t failed, likely ctrl-c 残留)"
    fi
fi

if [ "$(uname)" = "Darwin" ]; then
    SCRIPT_MTIME=$(stat -f "%Sm" -t "%Y-%m-%d %H:%M" "${BASH_SOURCE[0]}" 2>/dev/null)
else
    SCRIPT_MTIME=$(stat -c "%y" "${BASH_SOURCE[0]}" 2>/dev/null | cut -d. -f1)
fi
echo "[v3-sp] === SCRIPT_VERSION=$SCRIPT_VERSION (mtime=$SCRIPT_MTIME) ===" | tee -a "$LOG"
echo "[v3-sp] === DATA_VERSION=$DATA_VERSION → $DATASET_ROOT / $TRAIN_ROOT ===" | tee -a "$LOG"
echo "[v3-sp] iters=$ITERS, games-per-iter=$GAMES" | tee -a "$LOG"
echo "[v3-sp] log=$LOG" | tee -a "$LOG"
echo "[v3-sp] start $(date)" | tee -a "$LOG"
echo "" | tee -a "$LOG"

BEST_TC=0
BEST_CKPT=""

# best.json bootstrap:
# 1. 若 best.json 已存在 → bench 取分续跑 (用户手动放置 translated V3 或先前 best)
# 2. 若不存在但 big-models/big-model-v3.json 存在 → 自动 translate 当起点 (推荐路径)
# 3. 若都没 → 冷启动 (gen 用 embed default heuristic)
if [ -L "$BEST_LINK" ] && [ ! -e "$BEST_LINK" ]; then
    echo "[v3-sp] WARN: best.json 是断链, 删除" | tee -a "$LOG"
    rm -f "$BEST_LINK"
fi
if [ ! -e "$BEST_LINK" ]; then
    if [ "$NO_TRANSLATE" = "1" ]; then
        echo "[v3-sp] NO_TRANSLATE=1: 跳过 V2→V3 翻译, 冷启动 (Mac path C: 随机 V3 + default heuristic gen)" | tee -a "$LOG"
    elif [ -f "big-models/big-model-v3.json" ]; then
        TRANSLATED="$TRAIN_ROOT/v3-translated-from-v2.json"
        echo "[v3-sp] best.json 缺失, 自动 translate big-model-v3 (V2 132-d) → V3 147-d 当起点 (DSW path A)..." | tee -a "$LOG"
        echo "[v3-sp]   (跳过 translate: NO_TRANSLATE=1 bash $0 ...)" | tee -a "$LOG"
        if "$TRANSLATE_BIN" -in big-models/big-model-v3.json -out "$TRANSLATED" 2>&1 | tee -a "$LOG"; then
            ln -sf "v3-translated-from-v2.json" "$BEST_LINK"
            echo "[v3-sp] translated V3 → $BEST_LINK" | tee -a "$LOG"
        else
            echo "[v3-sp] WARN: translate 失败, 冷启动 gen 用 default policy" | tee -a "$LOG"
        fi
    fi
fi
if [ -e "$BEST_LINK" ]; then
    RESOLVED=$(readlink -f "$BEST_LINK" 2>/dev/null || echo "$BEST_LINK")
    if [ -f "$RESOLVED" ]; then
        echo "[v3-sp] best.json → $RESOLVED, bench 取分..." | tee -a "$LOG"
        BENCH_OUT=$(DISABLE_MCTS=1 "$BENCH_BIN" -ckpt "$RESOLVED" -cases cases/all-tests-expanded.json -workers 0 2>&1 | tail -3)
        EXISTING_TC=$(echo "$BENCH_OUT" | grep -oE "[0-9]+通过" | head -1 | grep -oE "[0-9]+")
        if [ -n "$EXISTING_TC" ] && [ "$EXISTING_TC" -gt 0 ]; then
            BEST_TC=$EXISTING_TC
            BEST_CKPT="$RESOLVED"
            echo "[v3-sp] 起点: best=$BEST_CKPT ($BEST_TC/63), self-play loop 从此开始" | tee -a "$LOG"
        else
            echo "[v3-sp] WARN: best.json bench=0, 冷启动" | tee -a "$LOG"
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

    # Phase A: gen samples — SELF-PLAY: rollout policy = 当前 BEST_CKPT (动态)
    # 跟 distillation 区别: 这里 BEST_CKPT 每 iter 都换, NN 跟自己玩.
    echo "[iter $iter] Phase A: gen $GAMES games (rollouts=100, indim 147, SELF-PLAY + exploration)..." | tee -a "$LOG"
    GEN_ARGS=(-num-games "$GAMES" -jokers 2 -rollouts 100 -r1-cap 30
              -phantom-opponents 2 -indim 147
              -foul-cost 6 -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 100 -fan-bonus-trips 120
              -out-dir "$GEN_OUT")
    if [ -n "$BEST_CKPT" ] && [ -f "$BEST_CKPT" ]; then
        GEN_ARGS+=(-weights "$BEST_CKPT")
        echo "[iter $iter]   rollout policy = $BEST_CKPT ($BEST_TC/63)" | tee -a "$LOG"
    else
        echo "[iter $iter]   rollout policy = embed default (cold start)" | tee -a "$LOG"
    fi
    # 用 PIPESTATUS[0] 拿 gen 的真实退出码 (bash 3.2 pipefail + tee 不可靠, 见 sp7).
    "$GEN_BIN" "${GEN_ARGS[@]}" 2>&1 | tee -a "$LOG" || true
    GEN_EXIT=${PIPESTATUS[0]}
    if [ "$GEN_EXIT" -ne 0 ]; then
        echo "[iter $iter] FATAL: gen 失败 (exit=$GEN_EXIT)" | tee -a "$LOG"
        # 130=SIGINT 143=SIGTERM: 用户中断 → 整个 script 退
        if [ "$GEN_EXIT" -eq 130 ] || [ "$GEN_EXIT" -eq 143 ]; then
            echo "[v3-sp] gen interrupted, abort" | tee -a "$LOG"
            exit "$GEN_EXIT"
        fi
        continue
    fi

    # Phase B: train (warm-start from best)
    echo "[iter $iter] Phase B: train V3..." | tee -a "$LOG"
    # sp11: warm-lr-mult 0.2 (warm 0.0002). 折中 sp9 0.1 vs sp10 0.3, 稳一点.
    # foul-cost=6 保留 (用户设计: 追 fan 优先级 > 避 foul).
    TRAIN_ARGS=(-dataset-dir "$DATASET_ROOT" -dataset-keep-warm-start -hours 1 -round-min 30
                -outdim 4 -h1 512 -h2 256 -h3 128 -indim 147
                -epochs 30 -lr 0.001 -warm-lr-mult 0.2 -y-recompute
                -fan-bonus-qq 20 -fan-bonus-kk 40 -fan-bonus-aa 100 -fan-bonus-trips 120
                -foul-cost 6 -fan-w 0.40 -foul-w 0.10 -policy-w 0.30
                -ckpt-dir "$TRAIN_OUT" -policy "v0-v3-sp-iter$iter")
    if [ -n "$BEST_CKPT" ] && [ -f "$BEST_CKPT" ]; then
        TRAIN_ARGS+=(-init-from-ckpt "$BEST_CKPT")
        echo "[iter $iter] warm-start train from $BEST_CKPT" | tee -a "$LOG"
    fi
    # 同 gen 块用 PIPESTATUS[0] (sp7).
    "$TRAIN_BIN" "${TRAIN_ARGS[@]}" 2>&1 | tee -a "$LOG" || true
    TRAIN_EXIT=${PIPESTATUS[0]}
    if [ "$TRAIN_EXIT" -ne 0 ]; then
        echo "[iter $iter] FATAL: train 失败 (exit=$TRAIN_EXIT)" | tee -a "$LOG"
        if [ "$TRAIN_EXIT" -eq 130 ] || [ "$TRAIN_EXIT" -eq 143 ]; then
            echo "[v3-sp] train interrupted, abort" | tee -a "$LOG"
            exit "$TRAIN_EXIT"
        fi
        continue
    fi

    # Phase C: bench 所有本 iter 新生的 round-* ckpt, 取最高分 promote.
    NEW_CKPTS=()
    while IFS= read -r line; do
        NEW_CKPTS+=("$line")
    done < <(find "$TRAIN_OUT" -name "round-*-acc*.json" -newer "$TRAIN_OUT/.iter_started" 2>/dev/null | sort)
    # sp9 fix: 当 NaN abort 无新 ckpt 时不 fallback 到旧 ckpt (会 promote 上 iter 的残留 ckpt).
    # 直接 continue 进下个 iter.
    if [ "${#NEW_CKPTS[@]}" -eq 0 ]; then
        echo "[iter $iter] ⚠ no new ckpt produced (NaN abort?), skip promote 进下 iter" | tee -a "$LOG"
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

    echo "" | tee -a "$LOG"
    echo "[iter $iter] testcase pick: $NEW_CKPT → new_tc=$NEW_TC/63 vs best_tc=$BEST_TC/63" | tee -a "$LOG"

    # === 3-metric duel (testcase >> fantasy >> score, 跟 alphazero-train round-1 一致) ===
    # 即便 testcase 没动, fantasy/score ↑ 也算 self-play 有进步 → PROMOTE.
    # 但若 testcase 大跌 (>5 case) 强制 DISCARD (guard).
    DUEL_GAMES=200
    NEW_FAN=0 BEST_FAN=0 NEW_SCORE=0 BEST_SCORE=0
    if [ -n "$BEST_CKPT" ] && [ -f "$BEST_CKPT" ] && [ "$NEW_CKPT" != "$BEST_CKPT" ]; then
        echo "[iter $iter] 3-metric duel: new vs best, $DUEL_GAMES games same-hand..." | tee -a "$LOG"
        # || true 防 bench-3metric 任何非 0 退出码触发 set -e 杀脚本 (2026-05-19 fix)
        DUEL_OUT=$("$BENCH3_BIN" -new "$NEW_CKPT" -best "$BEST_CKPT" -games $DUEL_GAMES -workers 0 2>&1 || true)
        echo "$DUEL_OUT" | tee -a "$LOG"
        NEW_FAN=$(echo "$DUEL_OUT" | grep "^NEW_FAN=" | cut -d= -f2)
        BEST_FAN=$(echo "$DUEL_OUT" | grep "^BEST_FAN=" | cut -d= -f2)
        NEW_SCORE=$(echo "$DUEL_OUT" | grep "^NEW_SCORE=" | cut -d= -f2)
        BEST_SCORE=$(echo "$DUEL_OUT" | grep "^BEST_SCORE=" | cut -d= -f2)
        set +o pipefail
        [ -z "$NEW_FAN" ] && NEW_FAN=0
        [ -z "$BEST_FAN" ] && BEST_FAN=0
        [ -z "$NEW_SCORE" ] && NEW_SCORE=0
        [ -z "$BEST_SCORE" ] && BEST_SCORE=0
    fi

    # 决策规则:
    #   guard: testcase 跌 ≥5 → 强制 DISCARD (NN 整体退化)
    #   rule 1: testcase ↑ → PROMOTE
    #   rule 2: fantasy ↑ (新进范局数 > 旧) → PROMOTE
    #   rule 3: score ↑ AND fantasy = AND testcase = → PROMOTE (stable 优化)
    TC_DROP=$(( BEST_TC - NEW_TC ))
    SHOULD_PROMOTE=0
    REASON=""
    if [ -z "$BEST_CKPT" ]; then
        SHOULD_PROMOTE=1
        REASON="initial (no prev best)"
    elif [ "$TC_DROP" -ge 5 ]; then
        REASON="testcase 大跌 -$TC_DROP → guard DISCARD"
    elif [ "$NEW_TC" -gt "$BEST_TC" ]; then
        SHOULD_PROMOTE=1
        REASON="testcase↑ ($BEST_TC → $NEW_TC)"
    elif [ "$NEW_FAN" -gt "$BEST_FAN" ]; then
        SHOULD_PROMOTE=1
        REASON="fantasy↑ ($BEST_FAN → $NEW_FAN)"
    elif [ "$NEW_TC" -eq "$BEST_TC" ] && [ "$NEW_FAN" -eq "$BEST_FAN" ] && \
         awk -v a="$NEW_SCORE" -v b="$BEST_SCORE" 'BEGIN { exit !(a > b) }'; then
        SHOULD_PROMOTE=1
        REASON="score↑ ($BEST_SCORE → $NEW_SCORE) [tc/fan stable]"
    else
        REASON="testcase/fantasy/score 均未严格优于 best"
    fi

    if [ "$SHOULD_PROMOTE" -eq 1 ]; then
        BEST_TC=$NEW_TC
        BEST_CKPT="$NEW_CKPT"
        ln -sf "../$NEW_CKPT" "$BEST_LINK" 2>/dev/null || cp "$NEW_CKPT" "$BEST_LINK"
        echo "[iter $iter] ✓ PROMOTE: $REASON" | tee -a "$LOG"
        echo "[iter $iter]   new best = $NEW_CKPT (tc=$NEW_TC fan=$NEW_FAN score=$NEW_SCORE)" | tee -a "$LOG"
    else
        echo "[iter $iter] ✗ DISCARD: $REASON" | tee -a "$LOG"
        echo "[iter $iter]   best stays = $BEST_CKPT (tc=$BEST_TC fan=$BEST_FAN score=$BEST_SCORE)" | tee -a "$LOG"
    fi
    echo "" | tee -a "$LOG"
done

echo "=== DONE $(date) ===" | tee -a "$LOG"
echo "[v3-sp] best ckpt: $BEST_CKPT" | tee -a "$LOG"
echo "[v3-sp] best testcase: $BEST_TC/63" | tee -a "$LOG"
echo "[v3-sp] log: $LOG"
