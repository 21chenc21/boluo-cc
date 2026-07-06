#!/usr/bin/env bash
# replay-prod.sh — 拿 uid + game_id 从 prod solve_log 取记录, 复现 prod vs 本地决策, diff
#
# 用法:
#   ./replay-prod.sh <uid> <game_id> [seat]
#
# 例:
#   ./replay-prod.sh 13320060 ypk-98042186-5
#   ./replay-prod.sh 13320060 ypk-98042186-5 1
#
# 流程:
#   1. SSH prod 查 solve_log (uid + game_id [+ seat] 全 round)
#   2. 启动 local server (port 18002, 本地 binary + 太子 ckpt)
#   3. 用 prod 原 request 打 local, 对比 prod vs local 的 layout/discards
#   4. 不一致的 round 高亮, 提示是否需要 trace 找 bug
#
# 假设:
#   - SSH key: /home/chguang/boluo-cc/chguang-gcp/googlecloud
#   - prod db: ~/boluo-cc/ofc-dev-v3/games.db
#   - local binary: /home/chguang/boluo-cc/ofc-dev-v3/server-go-bin/ofc-dev-v3
#   - jq 安装

set -e

if [ $# -lt 2 ]; then
    echo "usage: $0 <uid> <game_id> [seat]" >&2
    exit 1
fi

UID_ARG="$1"
GAME_ID="$2"
SEAT="${3:-}"

SSH_KEY=/home/chguang/boluo-cc/chguang-gcp/googlecloud
PROD_HOST=chguang@34.92.248.175
LOCAL_PORT=18002
LOCAL_BIN=/home/chguang/boluo-cc/ofc-dev-v3/server-go-bin/ofc-dev-v3
LOCAL_WEIGHTS=/home/chguang/boluo-cc/ofc-dev-v3/big-models/best.json

# === 1. SSH 查 prod db (用 heredoc 避免 quote 地狱) ===
SEAT_FILTER=""
if [ -n "$SEAT" ]; then
    SEAT_FILTER='AND request_json LIKE '"'"'%"seat_number":'"$SEAT"'%'"'"
fi

echo "[replay-prod] query: uid=$UID_ARG game_id=$GAME_ID${SEAT:+ seat=$SEAT}"
TSV=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
    "$PROD_HOST" sqlite3 '~/boluo-cc/ofc-dev-v3/games.db' <<EOF
SELECT id, round, request_json, response_json FROM solve_log
WHERE request_json LIKE '%"uid":"$UID_ARG"%'
  AND request_json LIKE '%"game_id":"$GAME_ID"%'
  $SEAT_FILTER
ORDER BY id;
EOF
)
if [ -z "$TSV" ]; then
    echo "[replay-prod] 无记录" >&2
    exit 1
fi
ROW_CNT=$(echo "$TSV" | wc -l)
echo "[replay-prod] 找到 $ROW_CNT 条记录"

# === 2. 启动 local server ===
LOCAL_MD5=$(md5sum "$LOCAL_BIN" | awk '{print $1}')
PROD_MD5=$(ssh -i "$SSH_KEY" "$PROD_HOST" "md5sum ~/boluo-cc/ofc-dev-v3/server-go-bin/ofc-dev-v3" 2>/dev/null | awk '{print $1}')
echo "[replay-prod] local binary md5: $LOCAL_MD5"
echo "[replay-prod] prod  binary md5: $PROD_MD5"
if [ "$LOCAL_MD5" = "$PROD_MD5" ]; then
    echo "[replay-prod] ⚠ binary 一致, local 决策应该 = prod (无 bug). 若 diff 出现, 检查 cache/state 传递"
else
    echo "[replay-prod] binary 不同 → 这是 local 代码 vs prod 二进制的对比, 用于看修复后行为"
fi

# Kill any existing on this port + start fresh
pkill -f "ofc-dev-v3.*:$LOCAL_PORT" 2>/dev/null || true
sleep 1
"$LOCAL_BIN" -addr=":$LOCAL_PORT" -weights="$LOCAL_WEIGHTS" \
    -static=/home/chguang/boluo-cc/ofc-dev-v3 -db="" > /tmp/replay-local.log 2>&1 &
LOCAL_PID=$!
trap "kill $LOCAL_PID 2>/dev/null" EXIT
sleep 2

if ! curl -s -m 3 "http://localhost:$LOCAL_PORT/api/health" > /dev/null; then
    echo "[replay-prod] 本地 server 启动失败, see /tmp/replay-local.log" >&2
    exit 1
fi

# === 3. Replay 每 round ===
echo
echo "=== Replay $ROW_CNT 轮 ==="

DIFF_COUNT=0
MATCH_COUNT=0
DIFF_ROUNDS=()

while IFS='|' read -r ID ROUND REQ_JSON RESP_JSON; do
    PROD_LAYOUT=$(echo "$RESP_JSON" | jq -c '.layout' 2>/dev/null || echo "?")
    PROD_DISC=$(echo "$RESP_JSON" | jq -c '.discards' 2>/dev/null || echo "?")
    DEALT=$(echo "$REQ_JSON" | jq -c '.dealt')
    PRE_TOP=$(echo "$REQ_JSON" | jq -c '.state.top')
    PRE_MID=$(echo "$REQ_JSON" | jq -c '.state.middle')
    PRE_BOT=$(echo "$REQ_JSON" | jq -c '.state.bottom')

    LOCAL_RESP=$(curl -s -m 30 -X POST "http://localhost:$LOCAL_PORT/api/solve" \
        -H "Content-Type: application/json" -d "$REQ_JSON")
    LOCAL_LAYOUT=$(echo "$LOCAL_RESP" | jq -c '.layout' 2>/dev/null || echo "?")
    LOCAL_DISC=$(echo "$LOCAL_RESP" | jq -c '.discards' 2>/dev/null || echo "?")

    SAME="✓"
    if [ "$PROD_LAYOUT" != "$LOCAL_LAYOUT" ] || [ "$PROD_DISC" != "$LOCAL_DISC" ]; then
        SAME="✗"
        DIFF_COUNT=$((DIFF_COUNT + 1))
        DIFF_ROUNDS+=("R$ROUND (id $ID)")
    else
        MATCH_COUNT=$((MATCH_COUNT + 1))
    fi

    echo
    echo "--- R$ROUND (solve_log id=$ID) $SAME ---"
    echo "  dealt: $DEALT"
    echo "  state: top=$PRE_TOP mid=$PRE_MID bot=$PRE_BOT"
    echo "  PROD:  layout=$PROD_LAYOUT  discards=$PROD_DISC"
    echo "  LOCAL: layout=$LOCAL_LAYOUT discards=$LOCAL_DISC"
done <<< "$TSV"

echo
echo "=== 汇总 ==="
echo "  匹配: $MATCH_COUNT"
echo "  差异: $DIFF_COUNT"
if [ $DIFF_COUNT -gt 0 ]; then
    echo "  差异 round: ${DIFF_ROUNDS[*]}"
    echo
    echo "下一步: cmd/rn-trace-nn (R2-R5) 或 cmd/r1-trace-nn (R1) 看 NN 真选 + 软规则影响"
fi
