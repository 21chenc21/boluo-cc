#!/usr/bin/env bash
# deploy-prod.sh v2 — 一键推 prod (34.92.248.175:8002).
# 用法:
#   ./deploy-prod.sh                        # 纯 binary 部署 (权重不动)
#   ./deploy-prod.sh <weights.json>         # binary + 权重原子部署 (自动改 prod start.sh 指向)
# 流程: build → [权重 scp + start.sh 指向] → 备份/kill/scp/start → health → online_testcase(基线对照) → 审计抽查
set -uo pipefail
cd "$(dirname "$0")"
export PATH="$PATH:/usr/local/go/bin"

KEY=/home/chguang/boluo-cc/gcp-chguang-new/gcp-chguang-new
HOST=chguang@34.92.248.175
PD=boluo-cc/ofc-dev-v3
LOCALBIN=/home/chguang/boluo-cc/ofc-dev-v3/server-go-bin/ofc-dev-v3
SSH="ssh -i $KEY -o StrictHostKeyChecking=no"
SCP="scp -i $KEY -o StrictHostKeyChecking=no"
# 已知基线败单 (E + 全栈, 全部已定性: 化石/刀刃/活靶排队). 新增败案才报警.
BASELINE_OK="实战 16|实战 23|实战 67|实战 73|实战 104|实战 105|实战 110|63 \[R2\]"

WEIGHTS_LOCAL="${1:-}"

echo "===== 0. build ====="
(cd server-go && go build -o "$LOCALBIN" ./cmd/server) || { echo "❌ BUILD FAIL"; exit 1; }
MD5=$(md5sum "$LOCALBIN" | cut -d' ' -f1); echo "binary md5 = $MD5"

if [ -n "$WEIGHTS_LOCAL" ]; then
  [ -f "$WEIGHTS_LOCAL" ] || { echo "❌ 权重不存在: $WEIGHTS_LOCAL"; exit 1; }
  WMD5=$(md5sum "$WEIGHTS_LOCAL" | cut -d' ' -f1)
  WNAME="w-$(date +%Y%m%d-%H%M)-${WMD5:0:8}.json"
  echo "===== 0b. 推权重 big-models/$WNAME (md5 $WMD5) ====="
  $SCP "$WEIGHTS_LOCAL" "$HOST:$PD/big-models/$WNAME" || { echo "❌ 权重 SCP FAIL"; exit 1; }
  $SSH $HOST "cd $PD && cp start.sh start.sh.bak-\$(date +%Y%m%d-%H%M%S) && sed -i 's|WEIGHTS=\"\$SCRIPT_DIR/big-models/[^\"]*\"|WEIGHTS=\"\$SCRIPT_DIR/big-models/$WNAME\"|' start.sh && grep WEIGHTS= start.sh | head -1"
fi

echo "===== 1. 备份 + kill (避 ETXTBSY; pkill -x 防自杀) ====="
$SSH $HOST "cd $PD && cp server-go-bin/ofc-dev-v3 server-go-bin/ofc-dev-v3.bak-\$(date +%Y%m%d-%H%M%S) && pkill -x ofc-dev-v3 || true; sleep 2; echo killed" || { echo "❌ ssh FAIL"; exit 1; }

echo "===== 2. scp binary ====="
$SCP "$LOCALBIN" "$HOST:$PD/server-go-bin/ofc-dev-v3" || { echo "❌ SCP FAIL"; exit 1; }

echo "===== 3. start (setsid 全脱管) ====="
$SSH $HOST "cd $PD && chmod +x server-go-bin/ofc-dev-v3 && ( setsid ./start.sh >/tmp/ofc-dev-v3-8002.log 2>&1 </dev/null & ) && echo started"
sleep 5
$SSH $HOST "cd $PD && echo -n 'prod binary md5 = '; md5sum server-go-bin/ofc-dev-v3|cut -d' ' -f1; echo -n 'health: '; curl -s http://localhost:8002/api/health|head -c 60; echo; grep -E 'loaded weights|DISABLE_SOFT|OFC_SEARCH_WORKERS' /tmp/ofc-dev-v3-8002.log | tail -3"

echo "===== 4. online_testcase (基线对照) ====="
$SCP online_testcase.py cases/game-cases.json "$HOST:/tmp/" >/dev/null
OUT=$($SSH $HOST "cd /tmp && python3 online_testcase.py game-cases.json; rm -f /tmp/online_testcase.py /tmp/game-cases.json")
echo "$OUT" | grep "==="
NEWFAILS=$(echo "$OUT" | grep "✗" | grep -vE "$BASELINE_OK" || true)
if [ -z "$NEWFAILS" ]; then
  echo "✅ 无基线外新败案. 部署通过."
else
  echo "⚠️ 基线外新败案 (检查!):"
  echo "$NEWFAILS"
  echo "回滚: ssh prod 'cd $PD && cp server-go-bin/ofc-dev-v3.bak-<最新> server-go-bin/ofc-dev-v3 && [还原start.sh.bak] && pkill -x ofc-dev-v3 && setsid ./start.sh &'"
fi

echo "===== 5. 审计抽查 ====="
$SSH $HOST "grep -c 'serve-search' /tmp/ofc-dev-v3-8002.log || true"
