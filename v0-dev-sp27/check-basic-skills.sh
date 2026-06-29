#!/usr/bin/env bash
# check-basic-skills.sh — 验证"终极目标": tc 总数 vs 135 + 16 个基本功 case 逐个 ✓/✗
# 2026-06-29 加. 用法: ./check-basic-skills.sh <ckpt.json>
#   纯NN (DISABLE_MCTS + DISABLE_HARD + DISABLE_SOFT), 跟训练 gate 同口径.
set -uo pipefail
cd "$(dirname "$0")"
CKPT="${1:?usage: ./check-basic-skills.sh <ckpt.json>}"
BENCH=server-go/server-go-bin/bench-cases
[ -x "$BENCH" ] || BENCH=server-go-bin/bench-cases

# 16 个基本功 case (实战 N) + 对应的修
declare -a SKILLS=(
  "23:RootA 中底倒置"          "24:RootA 中>底2对倒置"      "51:RootA 顶中kicker倒置+鬼花draw"
  "67:RootA 中小底大未来倒置"   "104:RootB 底葫芦种子"       "117:RootB 底花draw种子"
  "110:f102 鬼配低对杀种子"     "36:鬼放顶范种子"            "50:鬼+A顶AA范"
  "58:鬼+A顶AA范"              "99:Ks顶范种子"              "90:三条rank"
  "124:PairToTrips cap"        "118:顶trips种子"            "38:draw纯花slots"   "64:三条rank"
)

echo "ckpt: $CKPT"
OUT=$(DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 "$BENCH" -ckpt "$CKPT" -cases cases/game-cases.json -workers 0 2>&1)
RESULT=$(echo "$OUT" | grep 结果 | tail -1)
TOTAL=$(echo "$RESULT" | grep -oE "[0-9]+通过" | head -1 | grep -oE "[0-9]+")
echo "=== 总分: $RESULT ==="
if [ "${TOTAL:-0}" -ge 135 ]; then echo "  🎯 突破 135 达成 ($TOTAL)"; else echo "  ⏳ 距 135 还差 $((135 - ${TOTAL:-0})) ($TOTAL)"; fi
echo
echo "=== 16 基本功 (✓过 / ✗未过 / ⚠warn) ==="
pass=0; fail=0
for s in "${SKILLS[@]}"; do
  num="${s%%:*}"; desc="${s#*:}"
  # 匹配 "✓/✗ 实战 NUM [" 或 warn 标记
  line=$(echo "$OUT" | grep -E "实战 ${num} \[" | head -1)
  mark=$(echo "$line" | grep -oE "^[^ ]+" | head -1)
  case "$mark" in
    ✓) st="✓"; pass=$((pass+1)) ;;
    ✗) st="✗"; fail=$((fail+1)) ;;
    *) st="${mark:-?}" ;;
  esac
  printf "  %s 实战%-4s %s\n" "$st" "$num" "$desc"
done
echo
echo "基本功: $pass/16 过, $fail 未过"
[ "$fail" -eq 0 ] && echo "  🎯 基本功全过达成!" || echo "  ⏳ 还有 $fail 个基本功没过"
