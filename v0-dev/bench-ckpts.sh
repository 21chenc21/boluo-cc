#!/usr/bin/env bash
# 多 ckpt 汇总 testcase 表 (markdown).
# 不再 swap+rebuild — 直接 ofc-go -weights X.json 加载 (schema-agnostic).
#
# 用法:
#   ./bench-ckpts.sh round-016*.json round-028*.json
#   ./bench-ckpts.sh server-go/checkpoints/round-0{16..28}*.json
#   RUNS=3 ./bench-ckpts.sh ckpts/*.json
#
# 输出:
#   - stdout: live progress
#   - test-reports/bench-<ts>.md: markdown 汇总表

set -e

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_GO="$V0_DIR/server-go"
PORT="${PORT:-18002}"
RUNS="${RUNS:-3}"
LEVEL="${LEVEL:-high}"

if [ $# -eq 0 ]; then
    echo "usage: $0 <ckpt1.json> [ckpt2.json...]"
    echo
    echo "examples:"
    echo "  $0 mac-bundle/round-028-acc89.json"
    echo "  $0 server-go/checkpoints/round-0{10..28}*.json"
    echo "  RUNS=5 $0 ckpts/*.json"
    echo
    echo "env:"
    echo "  RUNS=3       runs per ckpt (default 3, median reported)"
    echo "  LEVEL=high   testcase level"
    echo "  PORT=18002   server port"
    exit 1
fi

# build if needed
BIN_DIR="$V0_DIR/server-go-bin"; mkdir -p "$BIN_DIR"; if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[build] ofc-go not found, building..."
    (cd "$SERVER_GO" && go build -o "$BIN_DIR/ofc-go" ./cmd/server)
fi

mkdir -p "$V0_DIR/test-reports"
TS=$(date +%Y%m%d-%H%M%S)
REPORT="$V0_DIR/test-reports/bench-$TS.md"

trap 'pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null' EXIT

{
    echo "# Checkpoint Bench — $TS"
    echo
    echo "- testcase: \`test-cases.js\` (63 cases)"
    echo "- level: $LEVEL, runs/ckpt: $RUNS, port: $PORT"
    echo
    echo "| ckpt | round | acc | samples | outDim | h1/h2 | runs | **median** | range | fail (median run) |"
    echo "|------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|------|"
} > "$REPORT"

TOTAL=$#
IDX=0

for CKPT in "$@"; do
    IDX=$((IDX + 1))

    if [ ! -f "$CKPT" ]; then
        echo "❌ NOT FOUND: $CKPT"
        echo "| $(basename "$CKPT" .json) | - | - | - | - | - | - | NOT FOUND | - | - |" >> "$REPORT"
        continue
    fi

    NAME=$(basename "$CKPT" .json)
    META=$(python3 <<PY
import json
try:
    with open('$CKPT') as f: d = json.load(f)
    print(f"{d.get('round','?')}|{d.get('accuracy',0)*100:.2f}|{d.get('samplesCount','?')}|{d.get('outDim',1)}|{d.get('h1Dim',d.get('hidden1','?'))}|{d.get('h2Dim',d.get('hidden2','?'))}")
except Exception as e:
    print(f"?|?|?|?|?|?")
PY
)
    ROUND=$(echo "$META" | cut -d'|' -f1)
    ACC=$(echo "$META" | cut -d'|' -f2)
    SAMPLES=$(echo "$META" | cut -d'|' -f3)
    OUTDIM=$(echo "$META" | cut -d'|' -f4)
    H1=$(echo "$META" | cut -d'|' -f5)
    H2=$(echo "$META" | cut -d'|' -f6)

    echo
    echo "============================================================"
    echo "[$IDX/$TOTAL] $NAME  (round=$ROUND, acc=${ACC}%, outDim=$OUTDIM, h1=$H1 h2=$H2)"
    echo "============================================================"

    # 起 server
    DB="/tmp/v0-bench-$$-$NAME.db"
    LOG="/tmp/v0-bench-$$-$NAME.log"
    pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true
    sleep 0.3
    nohup "$BIN_DIR/ofc-go" -addr=":$PORT" -static="$V0_DIR" \
        -weights="$CKPT" -db="$DB" \
        < /dev/null > "$LOG" 2>&1 &

    # 等就绪
    READY=0
    for try in 1 2 3 4 5 6 7 8 10 12; do
        if curl -s -m 2 "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1; then
            READY=1
            break
        fi
        sleep 0.5
    done

    if [ "$READY" -eq 0 ]; then
        echo "  ❌ server failed to start. log:"
        tail -10 "$LOG"
        echo "| $NAME | $ROUND | $ACC% | $SAMPLES | $OUTDIM | $H1/$H2 | - | SERVER FAIL | - | - |" >> "$REPORT"
        rm -f "$DB"
        continue
    fi

    # 跑 N 次
    declare -a RESULTS=()
    declare -a FAIL_LISTS=()
    for i in $(seq 1 "$RUNS"); do
        curl -s "http://127.0.0.1:$PORT/cache/clear" > /dev/null 2>&1 || true
        OUT=$(timeout 600 node "$V0_DIR/test-cases.js" "http://127.0.0.1:$PORT" 2>&1)
        PASS=$(echo "$OUT" | grep "结果" | sed -E 's/.*结果: ([0-9]+)通过.*/\1/')
        # 提取 fail case 短名 (取 "✗ N [Rk]:" 段)
        FAILS=$(echo "$OUT" | grep "^✗" | sed -E 's/^✗ ([0-9]+ \[R[0-9]+\]:).*$/\1/' | tr '\n' ',' | sed 's/,$//')
        RESULTS+=("$PASS")
        FAIL_LISTS+=("$FAILS")
        echo "  run $i: $PASS/26  ${FAILS:+fails: $FAILS}"
    done

    # 算中位
    SORTED=$(printf '%s\n' "${RESULTS[@]}" | sort -n)
    MEDIAN=$(echo "$SORTED" | awk -v n="$RUNS" 'NR==int((n+1)/2)')
    MIN=$(echo "$SORTED" | head -1)
    MAX=$(echo "$SORTED" | tail -1)

    # 找 median run 的 fail list
    MEDIAN_IDX=0
    for j in $(seq 0 $((RUNS - 1))); do
        if [ "${RESULTS[$j]}" = "$MEDIAN" ]; then
            MEDIAN_IDX=$j
            break
        fi
    done
    MEDIAN_FAILS="${FAIL_LISTS[$MEDIAN_IDX]}"

    echo "  → median=$MEDIAN range=[$MIN,$MAX] fail: ${MEDIAN_FAILS:-none}"

    RUNS_STR=""
    for r in "${RESULTS[@]}"; do
        RUNS_STR+=" $r"
    done

    echo "| $NAME | $ROUND | $ACC% | $SAMPLES | $OUTDIM | $H1/$H2 |${RUNS_STR} | **$MEDIAN** | $MIN-$MAX | ${MEDIAN_FAILS:-(all pass)} |" >> "$REPORT"

    # 清理本 ckpt 临时文件
    rm -f "$DB" "$LOG"
    unset RESULTS FAIL_LISTS
done

# 关 server
pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true

# 尾部说明
{
    echo
    echo "## Reference baselines (testcase, 26 cases)"
    echo
    echo "- v7_fan 1.0.3 production (round-028 + Path Y): **19-20 median**"
    echo "- v7_fan 1.0.1 (round-016, no Path Y): 15 median"
    echo "- v7_fan 1.0.0 (acc68, before fix): 11 median"
    echo
    echo "## How to read"
    echo
    echo "- **median ≥ 21**: significant improvement, candidate for hotfix"
    echo "- **median = 20**: matches production"
    echo "- **median 17-19**: regression / underfit"
    echo "- **median ≤ 16**: weights broken or pipeline issue"
    echo
    echo "noise: testcase σ ~3 → 95% CI ±6, single-run differences <6 are noise."
} >> "$REPORT"

echo
echo "============================================================"
echo "✓ DONE"
echo "============================================================"
echo "Report: $REPORT"
echo
tail -25 "$REPORT"
