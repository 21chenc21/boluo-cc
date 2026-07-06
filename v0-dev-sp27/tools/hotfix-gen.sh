#!/usr/bin/env bash
# hotfix-gen.sh <case号> <family|seeds.json> [games=150] — 单靶粮 gen (playbook 二·七 步骤1)
set -euo pipefail
cd "$(dirname "$0")/.."
CASE="${1:?用法: hotfix-gen.sh <case号> <family|seeds.json> [games]}"
FAM="${2:?family 名或 seeds.json}"
GAMES="${3:-150}"
E=$(cat tools/CHAMPION)
OUT="fam-${CASE}-${FAM%.json}"
SEEDARG=(-seed-family-only "$FAM" -seed-family-frac 0.5)
[[ "$FAM" == *.json ]] && SEEDARG=(-seed-states "$FAM" -seed-states-frac 0.6)
( cd server-go && go build -o ../server-go-bin/gen-rollout-dataset ./cmd/gen-rollout-dataset )
grep -ac "makeNamedFamilySeed" server-go-bin/gen-rollout-dataset >/dev/null || { echo "❌ 弹药验证失败"; exit 1; }
echo "gen → $OUT (教师=$E)"
DISABLE_HARD_RULES=1 DISABLE_SOFT_RULES=1 ./server-go-bin/gen-rollout-dataset \
  -weights "$E" -indim 169 -num-games "$GAMES" -rollouts 100 -r1-cap 30 -phantom-opponents 2 \
  -mcts-margin 2.5 -mcts-sims 500 -mcts-topk 5 -traj-explore 0.15 -traj-topk 3 \
  "${SEEDARG[@]}" \
  -foul-cost 6 -fan-bonus-qq 10 -fan-bonus-kk 30 -fan-bonus-aa 100 -fan-bonus-trips 140 \
  -out-dir "$OUT"
