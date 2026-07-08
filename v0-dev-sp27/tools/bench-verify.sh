#!/usr/bin/env bash
# bench-verify.sh <ckpt> — 裸 + 全栈×2 + 名单稳定性 (playbook 步骤3/验收三关之一二)
set -euo pipefail
cd "$(dirname "$0")/.."
CK="${1:?用法: bench-verify.sh <ckpt>}"
( cd server-go && go build -o ../server-go-bin/bench-cases ./cmd/bench-cases )
T=$(mktemp -d)
DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 ./server-go-bin/bench-cases -ckpt "$CK" > "$T/naked.txt" 2>&1
echo "裸:  $(grep 结果 "$T/naked.txt" | grep -oE '[0-9]+失败') → $(grep '^✗' "$T/naked.txt" | sed 's/:.*//' | tr '\n' ' ')"
for r in 1 2; do
  DISABLE_MCTS=1 DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 \
  OFC_KEEP_FILTERS=1 OFC_SERVE_SEARCH=2.5 OFC_SEARCH_WORKERS=6 OFC_SEARCH_CAP=240 OFC_SEARCH_SLOTS=8 \
  ./server-go-bin/bench-cases -ckpt "$CK" -workers 1 > "$T/full$r.txt" 2>&1
  echo "栈$r: $(grep 结果 "$T/full$r.txt" | grep -oE '[0-9]+失败') → $(grep '^✗' "$T/full$r.txt" | sed 's/:.*//' | tr '\n' ' ')"
done
diff <(grep '^✗' "$T/full1.txt" | sed 's/:.*//') <(grep '^✗' "$T/full2.txt" | sed 's/:.*//') >/dev/null \
  && echo "✅ 全栈名单稳定" || echo "⚠️ 全栈名单漂移 (门槛沿案存在)"
rm -rf "$T"
