#!/usr/bin/env bash
# fetch-eval-sp27.sh — 拉 sp27 候选 ckpt + 三关评估: 判它有没有超越 sp26(现 prod 基线).
# 见 memory project_retrain_pure_nn_gen / reference_v0_dev_primary_workdir.
#
# 用法:
#   本地 ckpt:  ./fetch-eval-sp27.sh /abs/path/cand.json [games]
#   DSW 拉:     ./fetch-eval-sp27.sh dsw:/mnt/workspace/v0-dev-sp27/<...>/best.json [games]   (经 localhost:2222 反向tunnel)
#   Mac/其它:   ./fetch-eval-sp27.sh chguang@host:/path/best.json [games]
#
# 三关 (全过且 h2h≥55% 非平局 才算超越 sp26 → 可换 prod):
#   关1 litmus 4 veto (纯 value head, DISABLE_SOFT+HARD) — 基础认知不能退
#   关2 std63 + gamecase (prod 模式, 规则on) — 不能比 sp26 差 (sp26: std 58/4w/0f, gc 57/0)
#   关3 h2h vs sp26 (cmd/duel, pureMLP) — 真实力, ≥55% 非平局胜率才是赢
set -uo pipefail
cd "$(dirname "$0")"
export PATH="$PATH:/usr/local/go/bin"

SRC="${1:?usage: ./fetch-eval-sp27.sh <local或remote ckpt> [games]}"
GAMES="${2:-2000}"
SP26=/home/chguang/boluo-cc/v0-dev-sp25featfix/v3-train-i150-sp26-featfix/best.json   # 基线=现prod
DSW_KEY=~/.ssh/dsw_aliyun
CAND=/tmp/sp27-cand.json

echo "########## 0. 拉 ckpt ##########"
if [[ "$SRC" == dsw:* ]]; then
  scp -P 2222 -i "$DSW_KEY" -o StrictHostKeyChecking=no "root@localhost:${SRC#dsw:}" "$CAND" || { echo "❌ DSW scp 失败 (tunnel 死了? 让用户重跑反向tunnel)"; exit 1; }
elif [[ "$SRC" == *:* ]]; then
  scp -o StrictHostKeyChecking=no "$SRC" "$CAND" || { echo "❌ scp 失败"; exit 1; }
else
  CAND="$SRC"
fi
[ -f "$CAND" ] || { echo "❌ ckpt 不存在: $CAND"; exit 1; }
echo "候选 = $CAND (md5 $(md5sum "$CAND"|cut -d' ' -f1))"
echo "基线 sp26 = $SP26"

echo "########## build 工具 (sp27 server-go, 150-d) ##########"
(cd server-go && go build -o /tmp/bench-sp27 ./cmd/bench-cases && go build -o /tmp/duel-sp27 ./cmd/duel) || { echo "❌ build 失败"; exit 1; }
GC=cases/game-cases.json
STD=cases/game-cases.json

echo "########## 关1: litmus 4 veto (纯 value head) ##########"
VETO=$(python3 -c "import json,re;ns=['实战 '+re.match(r'实战 (\d+)',c['name']).group(1)+' ' for c in json.load(open('$GC')) if '一票否决' in c.get('note','') and re.match(r'实战 (\d+)',c['name'])];print('|'.join(ns))")
FAILV=$(DISABLE_SOFT_RULES=1 DISABLE_HARD_RULES=1 DISABLE_MCTS=1 /tmp/bench-sp27 -cases "$GC" -ckpt "$CAND" 2>&1 | grep "✗" | grep -E "$VETO" || true)
if [ -z "$FAILV" ]; then echo "✅ litmus 4 veto 全过"; else echo "❌ litmus 挂 (基础认知退化, 不可用):"; echo "$FAILV"; fi

echo "########## 关2: std63 + gamecase (prod模式 规则on) ##########"
echo -n "[sp27] std: "; DISABLE_MCTS=1 /tmp/bench-sp27 -cases "$STD" -ckpt "$CAND" 2>&1 | grep -oE "[0-9]+通过 / [0-9]+警告 / [0-9]+失败"
echo -n "[sp27] gc:  "; DISABLE_MCTS=1 /tmp/bench-sp27 -cases "$GC"  -ckpt "$CAND" 2>&1 | grep -oE "[0-9]+通过 / [0-9]+失败"
echo "  (sp26 基线: std 58/4w/0f, gc 57/0 — 不能比这差)"

echo "########## 关3: h2h vs sp26 ($GAMES 局, ≥55%非平局才算赢) ##########"
/tmp/duel-sp27 -ckpt1 "$CAND" -ckpt2 "$SP26" -games $((GAMES/4)) -seeds 4 -mcts-sims 0 -jokers 2 -workers 0 2>&1 | tail -12
echo
echo "判定: 关1全过 + 关2不退 + 关3 ckpt1非平局胜率≥55% → 超越 sp26, 可换 prod (deploy 见 v0-dev/deploy-prod.sh, 换 SP26→新ckpt)."
