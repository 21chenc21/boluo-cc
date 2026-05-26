#!/usr/bin/env bash
# 单 ckpt 详细 testcase 报告 — 显示每个 case 的具体摆牌.
#
# 用法:
#   ./run-testcase.sh <ckpt.json> [runs=1]
#
# 例:
#   ./run-testcase.sh server-go/checkpoints/round-016-acc88.json
#   ./run-testcase.sh mac-bundle/round-028-acc89.json 3
#
# 输出:
#   - stdout: live progress
#   - test-reports/<ckpt-name>-<ts>.md: 完整 markdown 报告 (含每个 case 的 头/中/底)

set -e

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_GO="$V0_DIR/server-go"
PORT="${PORT:-18001}"
LEVEL="${LEVEL:-high}"

if [ $# -lt 1 ]; then
    echo "usage: $0 <ckpt.json> [runs=1]"
    echo
    echo "env:"
    echo "  PORT=18001    server port (default 18001)"
    echo "  LEVEL=high    testcase level (low/medium/high)"
    exit 1
fi

CKPT="$1"
RUNS="${2:-1}"

if [ ! -f "$CKPT" ]; then
    echo "❌ ckpt not found: $CKPT"
    exit 1
fi

# 若 ofc-go 不存在, build
BIN_DIR="$V0_DIR/server-go-bin"; mkdir -p "$BIN_DIR"; if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[build] ofc-go not found, building..."
    (cd "$SERVER_GO" && go build -o "$BIN_DIR/ofc-go" ./cmd/server)
fi

# 准备报告
mkdir -p "$V0_DIR/test-reports"
TS=$(date +%Y%m%d-%H%M%S)
CKPT_NAME=$(basename "$CKPT" .json)
REPORT="$V0_DIR/test-reports/${CKPT_NAME}-${TS}.md"

# ckpt metadata
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

# 启 server
LOG="/tmp/v0-testcase-$$-$CKPT_NAME.log"
DB="/tmp/v0-testcase-$$.db"
trap 'pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null; rm -f "$DB" "$LOG"' EXIT

pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true
sleep 0.5
nohup "$BIN_DIR/ofc-go" -addr=":$PORT" -static="$V0_DIR" \
    -weights="$CKPT" -db="$DB" \
    < /dev/null > "$LOG" 2>&1 &
SRV_PID=$!

# 等就绪
for try in 1 2 3 4 5 6 7 8; do
    if curl -s -m 2 "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1; then
        break
    fi
    sleep 0.5
done

if ! curl -s -m 2 "http://127.0.0.1:$PORT/api/health" > /dev/null; then
    echo "❌ server failed to start. log:"
    tail -20 "$LOG"
    exit 1
fi

# 写报告头
{
    echo "# Testcase Report — $CKPT_NAME"
    echo
    echo "- ckpt: \`$CKPT\`"
    echo "- round: $ROUND, accuracy: ${ACC}%, samples: $SAMPLES"
    echo "- arch: h1=$H1 h2=$H2 outDim=$OUTDIM"
    echo "- level: $LEVEL, runs: $RUNS"
    echo "- timestamp: $TS"
    echo
} > "$REPORT"

# 跑 N 次
declare -a PASS_COUNTS=()
for i in $(seq 1 "$RUNS"); do
    echo
    echo "=========================================="
    echo "RUN $i / $RUNS"
    echo "=========================================="
    curl -s "http://127.0.0.1:$PORT/cache/clear" > /dev/null 2>&1 || true

    {
        echo "## Run $i"
        echo
        echo '```'
    } >> "$REPORT"

    # tee: stdout + report
    OUT=$(timeout 600 node "$V0_DIR/test-cases.js" "http://127.0.0.1:$PORT" 2>&1)
    echo "$OUT" | tee -a "$REPORT"
    echo '```' >> "$REPORT"
    echo >> "$REPORT"

    PASS=$(echo "$OUT" | grep "结果" | sed -E 's/.*结果: ([0-9]+)通过.*/\1/')
    PASS_COUNTS+=("$PASS")
done

# 汇总
SORTED=$(printf '%s\n' "${PASS_COUNTS[@]}" | sort -n)
MEDIAN=$(echo "$SORTED" | awk -v n="$RUNS" 'NR==int((n+1)/2)')
MIN=$(echo "$SORTED" | head -1)
MAX=$(echo "$SORTED" | tail -1)

{
    echo "## Summary"
    echo
    echo "| run | passed |"
    echo "|:-:|:-:|"
    for j in $(seq 0 $((RUNS - 1))); do
        echo "| $((j + 1)) | ${PASS_COUNTS[$j]} / 26 |"
    done
    echo "| **median** | **$MEDIAN / 26** |"
    echo "| range | [$MIN, $MAX] |"
    echo
    echo "## Reference baselines (v7_fan testcase, 26 cases)"
    echo
    echo "- v7_fan 1.0.3 production (round-028 + Path Y): 19-20 median"
    echo "- v7_fan 1.0.1 (round-016, no Path Y): 15 median"
    echo
} >> "$REPORT"

echo
echo "=========================================="
echo "✓ DONE  median=$MEDIAN  range=[$MIN, $MAX]"
echo "report: $REPORT"
echo "=========================================="
