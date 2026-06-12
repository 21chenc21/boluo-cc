#!/usr/bin/env bash
# eval-ckpt.sh — 评测候选 ckpt vs 太子(baseline): testcase + h2h duel → 一份完整对比报告.
# 2026-06-04 加; 2026-06-13 升级: 末尾出完整报告表 (testcase / QQ KK AA 三条 / 范合计 / 分数 / foul / 胜率).
# 判定看 h2h (胜率/平均分/范分档/foul), 不要只信 testcase 通过数.
#
# 用法:
#   ./eval-ckpt.sh <candidate.json> [baseline.json] [games] [seed]
#   ./eval-ckpt.sh v3-train-i147-sp18-sp24/best.json
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

# --- 取整数 helper (从 bench / duel 文本里抠数) ---
pass_of() { echo "$1" | grep -oE '[0-9]+通过' | head -1 | grep -oE '[0-9]+'; }   # bench 通过数
fld()     { echo "$1" | grep -oE "$2=[0-9]+" | head -1 | grep -oE '[0-9]+'; }     # "AA=509" → 509

echo
echo "########## 1) testcase bench (std + gamecase) ##########"
B_STD_RAW=$(DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$BASE" -cases "$STD"  2>/dev/null | tail -1)
B_GAME_RAW=$(DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$BASE" -cases "$GAME" 2>/dev/null | tail -1)
C_STD_RAW=$(DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$CAND" -cases "$STD"  2>/dev/null | tail -1)
C_GAME_RAW=$(DISABLE_MCTS=1 "$BIN/bench-cases" -ckpt "$CAND" -cases "$GAME" 2>/dev/null | tail -1)
echo "[base] std:      $B_STD_RAW"
echo "[base] gamecase: $B_GAME_RAW"
echo "[cand] std:      $C_STD_RAW"
echo "[cand] gamecase: $C_GAME_RAW"

echo
echo "########## 2) h2h duel: ckpt1=base vs ckpt2=cand ($GAMES 同手) ##########"
DUEL=$("$BIN/duel" -ckpt1 "$BASE" -ckpt2 "$CAND" -games "$GAMES" -mcts-sims 0 -jokers 2 -seed "$SEED" -workers 0 2>&1 \
       | grep -vE "^[0-9]{4}/|^\[duel\] running")
echo "$DUEL"

# --- 解析 duel (按行位置: 第1条=base/ckpt1, 第2条=cand/ckpt2) ---
WIN_B=$(echo "$DUEL" | grep -E 'wins:' | sed -n 1p)
WIN_C=$(echo "$DUEL" | grep -E 'wins:' | sed -n 2p)
FAN_B=$(echo "$DUEL" | grep -E 'QQ=' | sed -n 1p)
FAN_C=$(echo "$DUEL" | grep -E 'QQ=' | sed -n 2p)
AVG_LINE=$(echo "$DUEL" | grep -E '^Avg score:')
RATE1=$(echo "$DUEL" | grep -oE 'excl draws: [0-9.]+%' | grep -oE '[0-9.]+' | head -1)
# 各字段
bw=$(echo "$WIN_B" | grep -oE 'wins: [0-9]+' | grep -oE '[0-9]+'); bwp=$(echo "$WIN_B" | grep -oE '[0-9.]+%' | head -1)
cw=$(echo "$WIN_C" | grep -oE 'wins: [0-9]+' | grep -oE '[0-9]+'); cwp=$(echo "$WIN_C" | grep -oE '[0-9.]+%' | head -1)
read -r b_avg c_avg < <(echo "$AVG_LINE" | grep -oE '=[0-9]+\.[0-9]+' | grep -oE '[0-9.]+' | head -2 | tr '\n' ' ')
b_qq=$(fld "$FAN_B" QQ); b_kk=$(fld "$FAN_B" KK); b_aa=$(fld "$FAN_B" AA); b_tr=$(fld "$FAN_B" 三条); b_ft=$(fld "$FAN_B" 范合计); b_fl=$(fld "$FAN_B" foul)
c_qq=$(fld "$FAN_C" QQ); c_kk=$(fld "$FAN_C" KK); c_aa=$(fld "$FAN_C" AA); c_tr=$(fld "$FAN_C" 三条); c_ft=$(fld "$FAN_C" 范合计); c_fl=$(fld "$FAN_C" foul)
b_std=$(pass_of "$B_STD_RAW"); b_gm=$(pass_of "$B_GAME_RAW"); c_std=$(pass_of "$C_STD_RAW"); c_gm=$(pass_of "$C_GAME_RAW")

# --- Δ helper (cand - base, 整数/浮点) ---
d_i() { echo "$(( ${2:-0} - ${1:-0} ))" | sed 's/^\([0-9]\)/+\1/'; }
d_f() { awk -v a="${1:-0}" -v b="${2:-0}" 'BEGIN{printf "%+.2f", b-a}'; }

echo
echo "================== 📊 完整评估报告 =================="
printf "%-14s %-14s %-14s %s\n" "指标" "base(太子)" "cand" "Δ(cand-base)"
echo "----------------------------------------------------------"
printf "%-14s %-14s %-14s %s\n" "testcase std"  "$b_std" "$c_std" "$(d_i $b_std $c_std)"
printf "%-14s %-14s %-14s %s\n" "testcase game" "$b_gm"  "$c_gm"  "$(d_i $b_gm $c_gm)"
echo "------- h2h ($GAMES 局, seed $SEED) -------"
printf "%-14s %-14s %-14s %s\n" "对局胜"     "$bw ($bwp)" "$cw ($cwp)" "$(d_i $bw $cw)"
printf "%-14s %-14s %-14s %s\n" "非平局胜率" "${RATE1}%" "$(awk -v r="$RATE1" 'BEGIN{printf "%.1f", 100-r}')%" "gate 55%"
printf "%-14s %-14s %-14s %s\n" "平均分"     "$b_avg" "$c_avg" "$(d_f $b_avg $c_avg)"
printf "%-14s %-14s %-14s %s\n" "范合计"     "$b_ft"  "$c_ft"  "$(d_i $b_ft $c_ft)"
printf "  %-12s %-14s %-14s %s\n" "QQ"        "$b_qq"  "$c_qq"  "$(d_i $b_qq $c_qq)"
printf "  %-12s %-14s %-14s %s\n" "KK"        "$b_kk"  "$c_kk"  "$(d_i $b_kk $c_kk)"
printf "  %-12s %-14s %-14s %s\n" "AA"        "$b_aa"  "$c_aa"  "$(d_i $b_aa $c_aa)"
printf "  %-12s %-14s %-14s %s\n" "三条"      "$b_tr"  "$c_tr"  "$(d_i $b_tr $c_tr)"
printf "%-14s %-14s %-14s %s\n" "foul"       "$b_fl"  "$c_fl"  "$(d_i $b_fl $c_fl)"
echo "----------------------------------------------------------"
echo "判定: 非平局胜率 ≥55% 才考虑 promote; 范合计↑ + foul↓ + 平均分↑ 综合看."
echo "      testcase 仅辅助 (通过数 ≠ 实战强度; 注意花色脆弱 case)."
