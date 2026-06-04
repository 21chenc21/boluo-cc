#!/usr/bin/env bash
# eval-ckpt.sh — 评测一个候选 ckpt vs 太子(baseline): testcase bench + 2000 同手 h2h duel.
# 2026-06-04 加. 判定看 h2h(胜率/平均分/范分档/foul), 不要只信 testcase 通过数.
#
# 用法:
#   ./eval-ckpt.sh <candidate.json> [baseline.json] [games] [seed]
#   ./eval-ckpt.sh v3-train-i147-sp18-sp21/iter-2/round-001-acc93.json
#   ./eval-ckpt.sh cand.json base.json 2000 42
#
# 3 玩家对局另用: server-go-bin/duel -ckpt1 A -ckpt2 B -ckpt3 C -games N -seeds M -mcts-sims 0 -workers 0
set -uo pipefail
cd "$(dirname "$0")"

CAND="${1:?usage: eval-ckpt.sh <candidate.json> [baseline.json] [games] [seed]}"
BASE="${2:-/home/chguang/boluo-cc/ofc-dev-v3/big-models/best.json}"
GAMES="${3:-2000}"
SEED="${4:-42}"
BIN=server-go-bin
STD=cases/all-tests-expanded.json
GAME=cases/game-cases.json

echo "候选 cand = $CAND"
echo "基准 base = $BASE"
echo "games=$GAMES  seed=$SEED  (pureMLP, 多核)"

echo
echo "########## 1) testcase bench (std63 + gamecase) ##########"
for L in base cand; do
  ck=$([ "$L" = base ] && echo "$BASE" || echo "$CAND")
  printf "[%-4s] std63:    " "$L"; DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$ck" -cases "$STD"  2>/dev/null | tail -1
  printf "[%-4s] gamecase: " "$L"; DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$ck" -cases "$GAME" 2>/dev/null | tail -1
done

echo
echo "########## 2) h2h duel: ckpt1=base vs ckpt2=cand ($GAMES 同手) ##########"
"$BIN/duel" -ckpt1 "$BASE" -ckpt2 "$CAND" -games "$GAMES" -mcts-sims 0 -jokers 2 -seed "$SEED" -workers 0 \
  2>&1 | grep -vE "^[0-9]{4}/|^\[duel\] running"

echo
echo "判定: h2h 非平局胜率 ≥55% 才考虑 promote; 平均分/范合计/foul 综合看. testcase 仅辅助 (通过数 ≠ 实战强度)."
