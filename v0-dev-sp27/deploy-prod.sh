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
# ⚠️ kill 用 pkill -x ofc-dev-v3 (按进程名), 不能用 pkill -f 'ofc-dev-v3.*8002':
#    -f 会匹配本 SSH 命令自己的 shell (命令串里含 ofc-dev-v3 + 8002) → 自杀 shell, SSH 掉线.
$SSH $HOST "cd $PD && cp server-go-bin/ofc-dev-v3 server-go-bin/ofc-dev-v3.bak-\$(date +%Y%m%d-%H%M%S) && echo backed-up && pkill -x ofc-dev-v3 || true; sleep 2; echo killed" || { echo "❌ ssh/backup FAIL"; exit 1; }

echo "########## 2. scp 新 binary (此时无运行进程) ##########"
$SCP "$LOCALBIN" "$HOST:$PD/server-go-bin/ofc-dev-v3" || { echo "❌ SCP FAIL"; exit 1; }

echo "########## 3. prod: start (完全 detach 否则 SSH 挂着不返回) ##########"
# ( setsid ./start.sh & ) 子 shell 里后台 + setsid 脱离会话 → SSH 立刻返回, 不被 server fd 挂住.
$SSH $HOST "cd $PD && chmod +x server-go-bin/ofc-dev-v3 && ( setsid ./start.sh >/tmp/ofc-dev-v3-8002.log 2>&1 </dev/null & ) && echo started"
sleep 5
echo "########## 3b. prod: 验 md5 + health (独立 SSH) ##########"
$SSH $HOST "cd $PD && echo -n 'prod md5 = '; md5sum server-go-bin/ofc-dev-v3|cut -d' ' -f1; echo -n 'health: '; curl -s http://localhost:8002/api/health|head -c 90; echo"

echo "########## 4. online_testcase (prod localhost:8002) ##########"
# 2026-06-29: std63 已并入 game-cases.json (183 单文件).
$SCP online_testcase.py cases/game-cases.json "$HOST:/tmp/" >/dev/null
OUT=$($SSH $HOST "cd /tmp && python3 online_testcase.py game-cases.json; rm -f /tmp/online_testcase.py /tmp/game-cases.json")
echo "$OUT"

echo "########## 验收 ##########"
# ⚠️ TODO(2026-06-29 合并后): 验收阈值要按 prod 太子在 game-cases.json(183) 上的实测通过数重新基线.
#   旧阈值是 std63 的 "61通过/2警告/0失败", 已失效. 下次部署: 先跑一遍看 prod 模型在 183 上的 N通过, 再把下面 grep 改成该 N.
if echo "$OUT" | grep -q "game-cases.json:.*0失败"; then
  echo "✅ game-cases 0 失败. (注: 通过数阈值待重新基线, 见上方 TODO)"
else
  echo "⚠️  game-cases 有失败! 检查上面输出. 回滚: ssh prod 'cd $PD && cp server-go-bin/ofc-dev-v3.bak-<最新> server-go-bin/ofc-dev-v3 && pkill -f ofc-dev-v3.*8002 && setsid nohup ./start.sh ...'"
fi
