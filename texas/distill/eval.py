"""
eval.py — convert NN output to a cfr.Strategy-compatible JSON, then ship to Go
for exploitability/EV gap evaluation.

Pipeline:
  1. Load PyTorch model + training data (same JSONL as train.py).
  2. For each (infoset, features, legal_mask), compute NN softmax (masked).
  3. Write NN strategy in BlueprintFile format (cfr.SaveBlueprint-compatible).
  4. Go side: cmd/compare-blueprints will load both blueprints and report
     expl + per-infoset KL + GameValue gap.

Usage:
    python3 distill/eval.py \
        --model distill/models/leduc-policy.pt \
        --data distill/data/leduc-train.jsonl \
        --out distill/models/leduc-nn-strategy.json
"""

import argparse
import json
import os
import sys

try:
    import torch
    import torch.nn.functional as F
except ImportError as e:
    sys.exit(f"[eval] missing dependency: {e}\n       pip3 install -r distill/requirements.txt")

from train import PolicyMLP, load_jsonl


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="distill/models/leduc-policy.pt")
    ap.add_argument("--data", default="distill/data/leduc-train.jsonl")
    ap.add_argument("--out", default="distill/models/leduc-nn-strategy.json")
    args = ap.parse_args()

    print(f"[eval] loading model {args.model}")
    ckpt = torch.load(args.model, map_location="cpu", weights_only=False)
    hidden = ckpt.get("hidden", [64, 32])
    feature_dim = ckpt.get("feature_dim", 35)
    model = PolicyMLP(feature_dim=feature_dim, hidden=tuple(hidden))
    model.load_state_dict(ckpt["state_dict"])
    model.eval()
    print(f"[eval] model hidden={hidden} feature_dim={feature_dim}")

    print(f"[eval] loading data {args.data}")
    feats, _probs, masks, labels = load_jsonl(args.data)

    with torch.no_grad():
        logits = model(feats)
        very_neg = torch.full_like(logits, -1e9)
        masked = torch.where(masks > 0, logits, very_neg)
        nn_probs = F.softmax(masked, dim=-1).numpy()

    # Read IDs separately (jsonl).
    ids = []
    with open(args.data) as f:
        for line in f:
            r = json.loads(line)
            ids.append(r["id"])
    assert len(ids) == nn_probs.shape[0]

    # Build BlueprintFile-shaped JSON. Compact probs to legal-only ordering: the
    # cfr.Strategy expects probs to match LegalActions order at each infoset.
    # legal mask order: action enum [Fold=0, CheckCall=1, BetRaise=2]. NN probs
    # are over all 3; we drop entries where legal=0 (illegal).
    strategy_records = []
    for i, infoset_id in enumerate(ids):
        full_probs = nn_probs[i].tolist()
        mask = masks[i].tolist()
        legal_probs = [p for p, m in zip(full_probs, mask) if m > 0]
        # Renormalize (mask removes ~0 contributions but safest to renormalize).
        s = sum(legal_probs)
        if s > 0:
            legal_probs = [p / s for p in legal_probs]
        else:
            legal_probs = [1.0 / len(legal_probs)] * len(legal_probs)
        strategy_records.append({
            "id": int(infoset_id),
            "label": labels[i],
            "probs": legal_probs,
        })

    blueprint = {
        "game": "leduc-holdem",
        "iters": 0,  # not applicable for distilled NN
        "gv_p0": 0.0,  # Go side will recompute
        "exploitability": 0.0,
        "num_infosets": len(strategy_records),
        "strategy": strategy_records,
        "_source": "nn-distill",
    }

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w") as f:
        json.dump(blueprint, f, indent=2)
    print(f"[eval] wrote {len(strategy_records)} infosets → {args.out}")
    print(f"[eval] next: go run ./cmd/compare-blueprints \\")
    print(f"           -tabular blueprints/leduc-vanilla-30k.json \\")
    print(f"           -nn {args.out}")


if __name__ == "__main__":
    main()
