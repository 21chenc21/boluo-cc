#!/usr/bin/env bash
# deploy-prod.sh — build v0-dev binary + 部署到 prod (34.92.248.175:8002) + online_testcase 验证.
# 2026-06-05 加. 流程见 memory feedback_no_unrequested_prod_push:
#   build → 先 kill 再 scp (避 ETXTBSY) → start → online_testcase (std63 必 61/2w/0f).
# ⚠️ 这脚本真推生产, 只在确定要部署时跑.
#
# 用法: ./deploy-prod.sh           (用当前太子 best.json, 纯代码部署)
set -uo pipefail
cd "$(dirname "$0")"
export PATH="$PATH:/usr/local/go/bin"

KEY=/home/chguang/boluo-cc/gcp-chguang-new/gcp-chguang-new
HOST=chguang@34.92.248.175
PD=boluo-cc/ofc-dev-v3   # prod dir (相对 home)
LOCALBIN=/home/chguang/boluo-cc/ofc-dev-v3/server-go-bin/ofc-dev-v3
SSH="ssh -i $KEY -o StrictHostKeyChecking=no"
SCP="scp -i $KEY -o StrictHostKeyChecking=no"

echo "########## 0. build binary (v0-dev/server-go) ##########"
(cd server-go && go build -o "$LOCALBIN" ./cmd/server) || { echo "❌ BUILD FAIL"; exit 1; }
MD5=$(md5sum "$LOCALBIN" | cut -d' ' -f1); echo "local binary md5 = $MD5"

echo "########## 1. prod: backup + kill 旧 proc (避 ETXTBSY) ##########"
$SSH $HOST "cd $PD && cp server-go-bin/ofc-dev-v3 server-go-bin/ofc-dev-v3.bak-\$(date +%Y%m%d-%H%M%S) && echo backed-up && pkill -f 'ofc-dev-v3.*8002'; sleep 2; echo killed" || { echo "❌ ssh/backup FAIL"; exit 1; }

echo "########## 2. scp 新 binary (此时无运行进程) ##########"
$SCP "$LOCALBIN" "$HOST:$PD/server-go-bin/ofc-dev-v3" || { echo "❌ SCP FAIL"; exit 1; }

echo "########## 3. prod: 验 md5 + start + health ##########"
$SSH $HOST "cd $PD && echo -n 'prod md5 = '; md5sum server-go-bin/ofc-dev-v3 | cut -d' ' -f1; chmod +x server-go-bin/ofc-dev-v3 && setsid nohup ./start.sh >/tmp/ofc-dev-v3-8002.log 2>&1 </dev/null & sleep 5; echo -n 'health: '; curl -s http://localhost:8002/api/health | head -c 90; echo"

echo "########## 4. online_testcase (prod localhost:8002) ##########"
$SCP online_testcase.py cases/all-tests-expanded.json cases/game-cases.json "$HOST:/tmp/" >/dev/null
OUT=$($SSH $HOST "cd /tmp && python3 online_testcase.py all-tests-expanded.json game-cases.json; rm -f /tmp/online_testcase.py /tmp/all-tests-expanded.json /tmp/game-cases.json")
echo "$OUT"

echo "########## 验收 ##########"
if echo "$OUT" | grep -q "all-tests-expanded.json: 61通过 / 2警告 / 0失败"; then
  echo "✅ std63 达标 (61/2w/0f). 部署成功."
else
  echo "⚠️  std63 未达标! 检查上面输出. 回滚: ssh prod 'cd $PD && cp server-go-bin/ofc-dev-v3.bak-<最新> server-go-bin/ofc-dev-v3 && pkill -f ofc-dev-v3.*8002 && setsid nohup ./start.sh ...'"
fi
