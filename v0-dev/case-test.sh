#!/usr/bin/env bash
# case-test.sh — 用 ckpt 跑 cases.json (不是全 63 testcase). 专项回测用.
#
# 用法:
#   ./case-test.sh <ckpt.json> [cases.json=cases/hard.json]
#
# 例:
#   ./case-test.sh ckpts/round-007.json
#   ./case-test.sh ckpts/round-007-fine.json cases/hard.json
#   ./case-test.sh ckpts/v0-dev-r1.json cases/my-cases.json
#
# 输出: 每 case pass/fail + 总分

set -e

V0_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER_GO="$V0_DIR/server-go"
PORT="${PORT:-18001}"

CKPT="${1:-}"
CASES="${2:-cases/hard.json}"

if [ -z "$CKPT" ]; then
    echo "usage: $0 <ckpt.json> [cases.json=cases/hard.json]"
    exit 1
fi

if [ ! -f "$CKPT" ]; then
    echo "❌ ckpt not found: $CKPT"
    exit 1
fi
if [ ! -f "$CASES" ]; then
    echo "❌ cases not found: $CASES"
    exit 1
fi

# 若 ofc-go 不存在, build
BIN_DIR="$V0_DIR/server-go-bin"; mkdir -p "$BIN_DIR"; if [ ! -x "$BIN_DIR/ofc-go" ]; then
    echo "[build] ofc-go not found, building..."
    (cd "$SERVER_GO" && go build -o "$BIN_DIR/ofc-go" ./cmd/server)
fi

# 启 server
LOG="/tmp/case-test-$$-$(basename "$CKPT" .json).log"
DB="/tmp/case-test-$$.db"
trap 'pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null; rm -f "$DB" "$LOG"' EXIT

pkill -f "ofc-go.*-addr.*:$PORT" 2>/dev/null || true
sleep 0.5
nohup env SEED="${SEED:-42}" "$BIN_DIR/ofc-go" -addr=":$PORT" -static="$V0_DIR" \
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

echo "ckpt: $CKPT"
echo "cases: $CASES"
echo "==========================================="
node "$V0_DIR/case-test.js" "$CASES" "http://127.0.0.1:$PORT"
echo "==========================================="
