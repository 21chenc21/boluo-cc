#!/usr/bin/env bash
# bench-ckpts-parallel.sh — 多 ckpt 并行 bench (每 ckpt 一个独立 server, 不同 port)
#
# 用法:
#   ./bench-ckpts-parallel.sh ckpt1.json ckpt2.json ckpt3.json
#   RUNS=5 ./bench-ckpts-parallel.sh ...
#
# vs bench-ckpts.sh: 串行 N×30min → 并行 30min (相同时间出 N 倍数据)
set -e

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_GO="$V0_DIR/server-go"
RUNS="${RUNS:-5}"
LEVEL="${LEVEL:-high}"
BASE_PORT="${BASE_PORT:-19000}"

if [ $# -eq 0 ]; then
    echo "usage: $0 <ckpt1.json> [ckpt2.json...]"
    echo
    echo "env:"
    echo "  RUNS=5         runs per ckpt"
    echo "  LEVEL=high     testcase level"
    echo "  BASE_PORT=19000  starting port (each ckpt uses BASE_PORT+i)"
    exit 1
fi

BIN_DIR="$V0_DIR/server-go-bin"; mkdir -p "$BIN_DIR"; if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[build] ofc-go not found, building..."
    (cd "$SERVER_GO" && go build -o "$BIN_DIR/ofc-go" ./cmd/server)
fi

mkdir -p "$V0_DIR/test-reports"
TS=$(date +%Y%m%d-%H%M%S)
REPORT="$V0_DIR/test-reports/bench-parallel-$TS.md"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"; for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; pkill -f "ofc-go.*-addr.*:1900" 2>/dev/null || true' EXIT

{
    echo "# Parallel Checkpoint Bench — $TS"
    echo
    echo "- testcase: \`test-cases.js\` (63 cases)"
    echo "- level: $LEVEL, runs/ckpt: $RUNS"
    echo
    echo "| ckpt | round | acc | runs | **median** | range | detail | fail (median run) |"
    echo "|------|:-:|:-:|:-:|:-:|:-:|:-:|------|"
} > "$REPORT"

run_one_ckpt() {
    local CKPT="$1"
    local PORT="$2"
    local IDX="$3"
    local NAME=$(basename "$CKPT" .json)
    local OUT_DIR="$WORK_DIR/$IDX"
    mkdir -p "$OUT_DIR"

    if [ ! -f "$CKPT" ]; then
        echo "NOT FOUND" > "$OUT_DIR/result"
        return
    fi

    local META=$(python3 <<PY
import json
try:
    with open('$CKPT') as f: d = json.load(f)
    print(f"{d.get('round','?')}|{d.get('accuracy',0)*100:.2f}")
except: print("?|?")
PY
    )
    local ROUND=$(echo "$META" | cut -d'|' -f1)
    local ACC=$(echo "$META" | cut -d'|' -f2)

    # start server
    local SLOG="$OUT_DIR/server.log"
    local DB="$OUT_DIR/games.db"
    pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true
    sleep 0.3
    "$BIN_DIR/ofc-go" -addr=":$PORT" -static="$V0_DIR" \
        -weights="$CKPT" -db="$DB" \
        > "$SLOG" 2>&1 &
    local SRV_PID=$!

    # wait ready
    local READY=0
    for try in 1 2 3 4 5 6 7 8 10 12 15; do
        if curl -s -m 2 "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1; then
            READY=1
            break
        fi
        sleep 0.6
    done
    if [ $READY -ne 1 ]; then
        echo "SERVER FAIL" > "$OUT_DIR/result"
        kill "$SRV_PID" 2>/dev/null || true
        return
    fi

    # 每 ckpt 一份详细摆法报告
    local DETAIL_REPORT="$V0_DIR/test-reports/${NAME}-parallel-${TS}.md"
    {
        echo "# Detail Report — $NAME"
        echo
        echo "- ckpt: \`$CKPT\`"
        echo "- round: $ROUND, accuracy: ${ACC}%"
        echo "- level: $LEVEL, runs: $RUNS, port: $PORT"
        echo "- timestamp: $TS"
        echo
    } > "$DETAIL_REPORT"

    # N runs
    local RESULTS=()
    local FAIL_LISTS=()
    for i in $(seq 1 "$RUNS"); do
        curl -s "http://127.0.0.1:$PORT/cache/clear" > /dev/null 2>&1 || true
        local OUT=$(timeout 600 node "$V0_DIR/test-cases.js" "http://127.0.0.1:$PORT" 2>&1)
        local PASS=$(echo "$OUT" | grep "结果" | sed -E 's/.*结果: ([0-9]+)通过.*/\1/')
        local FAILS=$(echo "$OUT" | grep "^✗" | tr '\n' ',' | sed 's/,$//')
        RESULTS+=("$PASS")
        FAIL_LISTS+=("$FAILS")
        {
            echo "## Run $i (pass: $PASS / 63)"
            echo
            echo '```'
            echo "$OUT"
            echo '```'
            echo
        } >> "$DETAIL_REPORT"
    done

    kill "$SRV_PID" 2>/dev/null || true
    pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true

    # median
    local SORTED=$(printf '%s\n' "${RESULTS[@]}" | sort -n)
    local MEDIAN=$(echo "$SORTED" | awk -v n="$RUNS" 'NR==int((n+1)/2)')
    local MIN=$(echo "$SORTED" | head -1)
    local MAX=$(echo "$SORTED" | tail -1)
    local MEDIAN_IDX=0
    for j in $(seq 0 $((RUNS - 1))); do
        if [ "${RESULTS[$j]}" = "$MEDIAN" ]; then MEDIAN_IDX=$j; break; fi
    done
    local MEDIAN_FAILS="${FAIL_LISTS[$MEDIAN_IDX]}"
    local RUNS_STR=""
    for r in "${RESULTS[@]}"; do RUNS_STR+=" $r"; done

    # 详细报告底部加 summary
    {
        echo "## Summary"
        echo
        echo "| run | pass |"
        echo "|:-:|:-:|"
        for j in $(seq 0 $((RUNS - 1))); do
            echo "| $((j + 1)) | ${RESULTS[$j]} / 63 |"
        done
        echo "| **median** | **$MEDIAN / 63** |"
        echo "| range | $MIN - $MAX |"
    } >> "$DETAIL_REPORT"

    echo "$NAME|$ROUND|$ACC|$RUNS_STR|$MEDIAN|$MIN-$MAX|${MEDIAN_FAILS:-(all pass)}|$DETAIL_REPORT" > "$OUT_DIR/result"
}

# parallel launch
echo "Launching $# ckpts in parallel..."
PIDS=()
IDX=0
for CKPT in "$@"; do
    PORT=$((BASE_PORT + IDX))
    run_one_ckpt "$CKPT" "$PORT" "$IDX" > "$WORK_DIR/$IDX.stdout" 2>&1 &
    PIDS+=($!)
    echo "  ckpt[$IDX] $(basename "$CKPT") → port $PORT (PID $!)"
    IDX=$((IDX + 1))
done

# wait
echo "Waiting for all ckpts to complete..."
for p in "${PIDS[@]}"; do
    wait "$p" || true
done

# collect results
IDX=0
for CKPT in "$@"; do
    if [ -f "$WORK_DIR/$IDX/result" ]; then
        local_r=$(cat "$WORK_DIR/$IDX/result")
        if [ "$local_r" = "NOT FOUND" ] || [ "$local_r" = "SERVER FAIL" ]; then
            echo "| $(basename "$CKPT" .json) | - | - | - | $local_r | - | - |" >> "$REPORT"
        else
            NAME=$(echo "$local_r" | cut -d'|' -f1)
            ROUND=$(echo "$local_r" | cut -d'|' -f2)
            ACC=$(echo "$local_r" | cut -d'|' -f3)
            RUNS_STR=$(echo "$local_r" | cut -d'|' -f4)
            MEDIAN=$(echo "$local_r" | cut -d'|' -f5)
            RANGE=$(echo "$local_r" | cut -d'|' -f6)
            FAILS=$(echo "$local_r" | cut -d'|' -f7)
            DETAIL=$(echo "$local_r" | cut -d'|' -f8)
            echo "| $NAME | $ROUND | $ACC% |$RUNS_STR | **$MEDIAN** | $RANGE | [详细]($(basename "$DETAIL")) | $FAILS |" >> "$REPORT"
        fi
    fi
    IDX=$((IDX + 1))
done

echo
echo "============================================================"
echo "✓ DONE"
echo "============================================================"
echo "Report: $REPORT"
echo
cat "$REPORT"
