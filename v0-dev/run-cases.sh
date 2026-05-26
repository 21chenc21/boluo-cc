#!/usr/bin/env bash
# run-cases.sh — 统一测试入口
#
# 模式:
#   单 ckpt: 启 ofc-go + 实时流式输出每 case
#   多 ckpt: 并行启 N 个 ofc-go (port 19000+i), 并行跑, 末尾汇总表 + 每 ckpt detail report
#
# 用法:
#   ./run-cases.sh <ckpt.json>
#   ./run-cases.sh <ckpt.json> --runs 5
#   ./run-cases.sh <ckpt.json> --runs 5 -- cases/hard.json
#   ./run-cases.sh <ckpt1> <ckpt2> <ckpt3>                    # 多 ckpt 并行
#   ./run-cases.sh <ckpt1> <ckpt2> --runs 5
#   ./run-cases.sh server-go/ckpts/*.json --runs 5
#   ./run-cases.sh <ckpt1> <ckpt2> -- cases/all.json cases/hard.json --runs 3
#
# 默认 cases: cases/all-tests-expanded.json
# 默认 runs: 1
# 默认 base port: 19000 (单 ckpt) / 19000+i (多 ckpt)
set -e

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_GO="$V0_DIR/server-go"
BASE_PORT="${PORT:-19000}"

# === arg 解析 ===
CKPTS=()
CASES=()
RUNS=1
DASHDASH=0
while [ $# -gt 0 ]; do
    case "$1" in
        --runs) RUNS="$2"; shift 2;;
        --runs=*) RUNS="${1#--runs=}"; shift;;
        --) DASHDASH=1; shift;;
        -h|--help)
            echo "usage: $0 <ckpt.json> [more-ckpt.json ...] [--runs N] [-- <cases.json> ...]"
            echo "  单 ckpt → 实时流式输出"
            echo "  多 ckpt → 并行模式, 末尾汇总 + per-ckpt detail report"
            echo "  默认 cases: cases/all-tests-expanded.json"
            echo "  默认 runs: 1, base port: 19000"
            exit 0;;
        --*) echo "unknown flag: $1" >&2; exit 1;;
        *)
            if [ $DASHDASH -eq 0 ]; then
                CKPTS+=("$1")
            else
                CASES+=("$1")
            fi
            shift;;
    esac
done

if [ ${#CKPTS[@]} -eq 0 ]; then
    echo "❌ no ckpt specified. usage: $0 <ckpt.json> [more...] [--runs N] [-- cases.json ...]" >&2
    exit 1
fi
for c in "${CKPTS[@]}"; do
    if [ ! -f "$c" ]; then
        echo "❌ ckpt not found: $c" >&2
        exit 1
    fi
done

# build ofc-go if needed
BIN_DIR="$V0_DIR/server-go-bin"; mkdir -p "$BIN_DIR"; if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[build] ofc-go missing, building..."
    (cd "$SERVER_GO" && go build -o "$BIN_DIR/ofc-go" ./cmd/server)
fi

# === server start helper ===
start_server() {
    local CKPT="$1" PORT="$2" SLOG="$3" DB="$4"
    pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true
    sleep 0.3
    "$BIN_DIR/ofc-go" -addr=":$PORT" -static="$V0_DIR" \
        -weights="$CKPT" -db="$DB" > "$SLOG" 2>&1 &
    echo $!
}
wait_ready() {
    local PORT="$1"
    for try in 1 2 3 4 5 6 7 8 10 12 15 20; do
        if curl -s -m 2 "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1; then
            return 0
        fi
        sleep 0.5
    done
    return 1
}

# === 单 ckpt 模式: 实时流式 ===
TS=$(date +%Y%m%d-%H%M%S)
if [ ${#CKPTS[@]} -eq 1 ]; then
    CKPT="${CKPTS[0]}"
    PORT="$BASE_PORT"
    SLOG=$(mktemp /tmp/ofc-rc-XXX.log)
    DB=$(mktemp /tmp/ofc-rc-XXX.db)
    trap 'pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true; rm -f "$SLOG" "$DB"' EXIT INT TERM
    SRV_PID=$(start_server "$CKPT" "$PORT" "$SLOG" "$DB")
    if ! wait_ready "$PORT"; then
        echo "❌ server failed to start. log:" >&2; tail -20 "$SLOG" >&2; exit 1
    fi
    CKPT_NAME=$(basename "$CKPT" .json)
    CKPT_DIR=$(dirname "$CKPT")
    REPORT="$CKPT_DIR/${CKPT_NAME}-test-${TS}.md"
    {
        echo "# Test Report — $CKPT_NAME"
        echo
        echo "- ckpt: \`$CKPT\`"
        echo "- runs: $RUNS, cases: ${CASES[*]:-cases/all-tests-expanded.json + cases/game-cases.json (default, 分开统计)}"
        echo "- timestamp: $TS"
        echo
        echo '```'
    } > "$REPORT"
    echo "[run-cases] ckpt: $CKPT_NAME, port: $PORT, runs: $RUNS"
    echo "[run-cases] cases: ${CASES[*]:-cases/all-tests-expanded.json + cases/game-cases.json (default, 分开统计)}"
    echo "[run-cases] report: $REPORT"
    echo
    # tee 到报告 + 终端
    node "$V0_DIR/test-cases.js" --url="http://127.0.0.1:$PORT" --runs="$RUNS" "${CASES[@]}" 2>&1 | tee -a "$REPORT"
    echo '```' >> "$REPORT"
    echo
    echo "[run-cases] report saved: $REPORT"
    exit 0
fi

# === 多 ckpt 模式: 并行 ===
# summary 放第一个 ckpt 的目录 (多 ckpt 通常同 dir)
SUMMARY_DIR=$(dirname "${CKPTS[0]}")
SUMMARY="$SUMMARY_DIR/run-cases-summary-${TS}.md"
WORK_DIR=$(mktemp -d)
PIDS=()
SERVER_PIDS=()
trap 'for p in "${PIDS[@]:-}" "${SERVER_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; pkill -f "ofc-go.*-addr.*:19[0-9][0-9][0-9]" 2>/dev/null || true; rm -rf "$WORK_DIR"' EXIT INT TERM

echo "[run-cases] 并行 ${#CKPTS[@]} ckpts × $RUNS runs, base port: $BASE_PORT"
echo "[run-cases] cases: ${CASES[*]:-cases/all-tests-expanded.json + cases/game-cases.json (default, 分开统计)}"
echo "[run-cases] summary report: $SUMMARY"
echo

{
    echo "# Multi-ckpt Bench — $TS"
    echo
    echo "- runs/ckpt: $RUNS"
    echo "- cases: ${CASES[*]:-cases/all-tests-expanded.json + cases/game-cases.json (default, 分开统计)}"
    echo
    echo "| ckpt | round | acc | runs | **median** | range | detail |"
    echo "|------|:-:|:-:|:-:|:-:|:-:|:-:|"
} > "$SUMMARY"

run_one() {
    local CKPT="$1" PORT="$2" IDX="$3"
    local NAME=$(basename "$CKPT" .json)
    local OD="$WORK_DIR/$IDX"
    mkdir -p "$OD"
    local META=$(python3 <<PY 2>/dev/null
import json
try:
    with open('$CKPT') as f: d = json.load(f)
    print(f"{d.get('round','?')}|{d.get('accuracy',0)*100:.2f}")
except: print("?|?")
PY
)
    local ROUND=$(echo "$META" | cut -d'|' -f1)
    local ACC=$(echo "$META" | cut -d'|' -f2)
    local SLOG="$OD/server.log"
    local DB="$OD/games.db"
    local PID=$(start_server "$CKPT" "$PORT" "$SLOG" "$DB")
    SERVER_PIDS+=("$PID")
    if ! wait_ready "$PORT"; then
        echo "❌ $NAME server failed on port $PORT" >&2
        echo "SERVER_FAIL|$NAME|$ROUND|$ACC" > "$OD/result"
        kill "$PID" 2>/dev/null || true
        return
    fi
    local CKPT_DIR_=$(dirname "$CKPT")
    local DETAIL="$CKPT_DIR_/${NAME}-test-${TS}.md"
    local RAW="$OD/raw.txt"
    {
        echo "# Detail — $NAME (port $PORT)"
        echo
        echo "- ckpt: \`$CKPT\`, round: $ROUND, accuracy: ${ACC}%"
        echo "- runs: $RUNS, cases: ${CASES[*]:-cases/all-tests-expanded.json}"
        echo
        echo '```'
    } > "$DETAIL"
    # 同时实时输出到 stdout (加 [name] 前缀) + 完整无前缀写 detail report
    node "$V0_DIR/test-cases.js" --url="http://127.0.0.1:$PORT" --runs="$RUNS" "${CASES[@]}" 2>&1 \
        | tee "$RAW" \
        | awk -v p="[$NAME]" '{print p" "$0; fflush()}'
    cat "$RAW" >> "$DETAIL"
    echo '```' >> "$DETAIL"

    # 解析每 run pass (从 raw 抓, detail 含 markdown 包装)
    local RESULTS=()
    while IFS= read -r line; do
        RESULTS+=("$line")
    done < <(grep -E "结果: [0-9]+通过" "$RAW" | sed -E 's/.*结果: ([0-9]+)通过.*/\1/')
    local R_STR=""
    for r in "${RESULTS[@]}"; do R_STR+=" $r"; done
    local SORTED=$(printf '%s\n' "${RESULTS[@]}" | sort -n)
    local MEDIAN=$(echo "$SORTED" | awk -v n="${#RESULTS[@]}" 'NR==int((n+1)/2)')
    local MIN=$(echo "$SORTED" | head -1)
    local MAX=$(echo "$SORTED" | tail -1)
    echo "OK|$NAME|$ROUND|$ACC|$R_STR|$MEDIAN|$MIN-$MAX|$DETAIL" > "$OD/result"
    kill "$PID" 2>/dev/null || true
    echo "[run-cases] ckpt[$IDX] $NAME done: median=$MEDIAN range=$MIN-$MAX → $DETAIL"
}

# spawn
for i in "${!CKPTS[@]}"; do
    PORT=$((BASE_PORT + i))
    run_one "${CKPTS[$i]}" "$PORT" "$i" &
    PIDS+=($!)
done

# wait all
for p in "${PIDS[@]}"; do
    wait "$p" || true
done

# write summary table
for i in "${!CKPTS[@]}"; do
    RES_FILE="$WORK_DIR/$i/result"
    if [ ! -f "$RES_FILE" ]; then
        echo "| $(basename "${CKPTS[$i]}" .json) | - | - | - | MISSING | - | - |" >> "$SUMMARY"
        continue
    fi
    R=$(cat "$RES_FILE")
    STATUS=$(echo "$R" | cut -d'|' -f1)
    if [ "$STATUS" = "SERVER_FAIL" ]; then
        NAME=$(echo "$R" | cut -d'|' -f2)
        echo "| $NAME | - | - | - | FAIL | - | - |" >> "$SUMMARY"
    else
        NAME=$(echo "$R" | cut -d'|' -f2)
        ROUND=$(echo "$R" | cut -d'|' -f3)
        ACC=$(echo "$R" | cut -d'|' -f4)
        R_STR=$(echo "$R" | cut -d'|' -f5)
        MEDIAN=$(echo "$R" | cut -d'|' -f6)
        RANGE=$(echo "$R" | cut -d'|' -f7)
        DETAIL=$(echo "$R" | cut -d'|' -f8)
        echo "| $NAME | $ROUND | $ACC% |$R_STR | **$MEDIAN** | $RANGE | [详细]($(basename "$DETAIL")) |" >> "$SUMMARY"
    fi
done

echo
echo "============================================================"
echo "✓ DONE — summary: $SUMMARY"
echo "============================================================"
echo
cat "$SUMMARY"
