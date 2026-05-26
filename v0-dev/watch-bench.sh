#!/usr/bin/env bash
# watch-bench.sh — 监控指定 ckpts 目录, 有新 round-NNN-accXX.json 自动跑 case-test
#
# 用法:
#   ./watch-bench.sh                                  # 默认 ckpts/, 60s 轮询
#   ./watch-bench.sh ckpts-v2-fresh                   # 监控 ckpts-v2-fresh/
#   ./watch-bench.sh ckpts-v2-fresh 30                # + 30s 轮询
#   ./watch-bench.sh ckpts-v2-fresh 60 cases/my.json  # + 用别的 cases 文件
#
#   # 启动时把已存在的 ckpt 按 round 顺序全跑一遍 (启动后再监控新 ckpt):
#   RUN_EXISTING=1 ./watch-bench.sh ckpts-v2-fresh
#
# 输出格式 (fail 行):
#   ✗ [N case-name]
#     init :[top][mid][bot]              ← R1 时 [][][] (无 state)
#     AI-ERR:[top][mid][bot]             ← R1 = 完整摆法; RN = state+placement 合并后
#                                          (RN 含 "弃 X" 后缀)
#
# 完整 log 存: test-reports-<ckpts-dir>/case-bench-<round>-<ts>.log
# Seen tracking: 仅在内存 (本次 watch-bench 进程生命周期), 进程退出后丢

set -e

DIR="${1:-ckpts}"
INTERVAL="${2:-60}"
CASES="${3:-cases/all-tests-expanded.json}"
SEED="${SEED:-42}"
REPORT_DIR="test-reports-$(basename "$DIR")"

if [ ! -d "$DIR" ]; then
    echo "❌ dir not found: $DIR"
    exit 1
fi
if [ ! -f "$CASES" ]; then
    echo "❌ cases not found: $CASES"
    exit 1
fi

mkdir -p "$REPORT_DIR"

echo "[watch-bench] dir=$DIR interval=${INTERVAL}s cases=$CASES SEED=$SEED"
echo "[watch-bench] report dir: $REPORT_DIR"
echo "[watch-bench] seen tracking: in-memory only (lost on exit)"
if [ -n "${RUN_EXISTING:-}" ]; then
    echo "[watch-bench] RUN_EXISTING=1 → 启动会按顺序跑所有现存 ckpt"
else
    echo "[watch-bench] 默认跳过现存 ckpt (set RUN_EXISTING=1 to bench existing)"
fi
echo "[watch-bench] press Ctrl-C to stop"
echo

# In-memory seen tracking (bash 3.x 兼容, 不用 declare -A 关联数组)
# 用 "|path|path|..." 字符串拼接, 包裹 | 防 prefix 误判
SEEN_STR="|"
mark_seen() { SEEN_STR="${SEEN_STR}$1|"; }
is_seen() { case "$SEEN_STR" in *"|$1|"*) return 0;; *) return 1;; esac; }

# 如果 RUN_EXISTING 未设, 把现存 ckpt 标 seen (启动时不跑)
if [ -z "${RUN_EXISTING:-}" ]; then
    PRE_SEEN=0
    for f in "$DIR"/round-*-acc*.json; do
        [ -f "$f" ] || continue
        case "$f" in *baseline*) continue ;; esac
        mark_seen "$f"
        PRE_SEEN=$((PRE_SEEN + 1))
    done
    if [ "$PRE_SEEN" -gt 0 ]; then
        echo "[watch-bench] marked $PRE_SEEN existing ckpt as seen (不重 bench)"
        echo
    fi
fi

# 单个 ckpt bench 函数
run_bench() {
    local f="$1"
    local ts
    ts=$(date +%Y%m%d-%H%M%S)
    local basename_f
    basename_f=$(basename "$f" .json)
    local report_file="$REPORT_DIR/case-bench-${basename_f}-${ts}.log"

    echo "════════════════════════════════════════════"
    echo "[$(date +%H:%M:%S)] 🆕 CKPT: $f"
    echo "[$(date +%H:%M:%S)] SEED=$SEED ./case-test.sh $f $CASES"
    echo "════════════════════════════════════════════"

    # 实时显示: fail 行 + init/AI 摆法 (∅ → 空, 头/中/底 prefix 去掉)
    SEED=$SEED ./case-test.sh "$f" "$CASES" 2>&1 | tee "$report_file" \
        | awk '
            /^=== 结果/ { print; next }
            /^✗ \[/ {
                in_fail = 1
                state_seen = 0
                print $0
                next
            }
            /^✓ \[/ { in_fail = 0; next }

            # state line (RN only)
            in_fail && /^  state:/ {
                line = $0
                gsub(/∅/, "", line)
                sub(/^  state: */, "  init :", line)
                gsub(/头\[ */, "[", line)
                gsub(/\] 中\[ */, "][", line)
                gsub(/\] 底\[ */, "][", line)
                print line
                state_seen = 1
                next
            }

            # AI line (always)
            in_fail && /^  AI:/ {
                if (!state_seen) {
                    print "  init :[][][]"   # R1 synthetic
                }
                line = $0
                gsub(/∅/, "", line)
                sub(/^  AI: */, "  AI-ERR:", line)
                # 切 "弃 X" 后缀
                disc = ""
                if (match(line, / 弃 [^ ]+$/)) {
                    disc = substr(line, RSTART, RLENGTH)
                    line = substr(line, 1, RSTART - 1)
                }
                gsub(/头\[ */, "[", line)
                gsub(/\] 中\[ */, "][", line)
                gsub(/\] 底\[ */, "][", line)
                print line disc
                in_fail = 0   # done, skip exp lines
                next
            }
        '
    echo "  📄 full log: $report_file"
    echo
}

# 如果 RUN_EXISTING 设了, 启动先把现存 ckpt 按 round 顺序跑一遍
if [ -n "${RUN_EXISTING:-}" ]; then
    EXISTING=$(ls "$DIR"/round-*-acc*.json 2>/dev/null | sort)
    if [ -n "$EXISTING" ]; then
        echo "[watch-bench] 启动: 按顺序 bench 所有现存 ckpt..."
        echo
        for f in $EXISTING; do
            case "$f" in *baseline*) continue ;; esac
            is_seen "$f" && continue
            run_bench "$f"
            mark_seen "$f"
        done
        echo "[watch-bench] 启动 bench 完成, 开始监控新 ckpt..."
        echo
    fi
fi

# 主循环: 监控新 ckpt
while true; do
    for f in $(ls "$DIR"/round-*-acc*.json 2>/dev/null | sort); do
        [ -f "$f" ] || continue
        case "$f" in *baseline*) continue ;; esac
        is_seen "$f" && continue

        run_bench "$f"
        mark_seen "$f"
    done
    sleep "$INTERVAL"
done
