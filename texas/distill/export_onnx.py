"""
export_onnx.py — convert trained PyTorch policy NN to ONNX for Go inference.

Usage:
    python3 distill/export_onnx.py \
        --in distill/models/leduc-policy.pt \
        --out distill/models/leduc-policy.onnx
"""

import argparse
import os
import sys

try:
    import torch
    import torch.nn as nn
except ImportError as e:
    sys.exit(f"[export] missing dependency: {e}\n        pip3 install -r distill/requirements.txt")

from train import PolicyMLP


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="in_path", default="distill/models/leduc-policy.pt")
    ap.add_argument("--out", default="distill/models/leduc-policy.onnx")
    ap.add_argument("--opset", type=int, default=17)
    args = ap.parse_args()

    print(f"[export] loading {args.in_path}")
    ckpt = torch.load(args.in_path, map_location="cpu", weights_only=False)
    hidden = ckpt.get("hidden", [64, 32])
    feature_dim = ckpt.get("feature_dim", 35)
    num_actions = ckpt.get("num_actions", 3)
    model = PolicyMLP(feature_dim=feature_dim, hidden=tuple(hidden), num_actions=num_actions)
    model.load_state_dict(ckpt["state_dict"])
    model.eval()
    print(f"[export] hidden={hidden} feature_dim={feature_dim} num_actions={num_actions}")

    dummy = torch.zeros(1, feature_dim, dtype=torch.float32)

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    torch.onnx.export(
        model,
        (dummy,),
        args.out,
        input_names=["features"],
        output_names=["logits"],
        dynamic_axes={"features": {0: "batch"}, "logits": {0: "batch"}},
        opset_version=args.opset,
    )
    print(f"[export] saved {args.out}")

    # Optional: verify round-trip via onnxruntime if available.
    try:
        import onnxruntime as ort
        import numpy as np

        sess = ort.InferenceSession(args.out, providers=["CPUExecutionProvider"])
        py_logits = model(dummy).detach().numpy()
        ort_logits = sess.run(["logits"], {"features": dummy.numpy()})[0]
        diff = float(np.abs(py_logits - ort_logits).max())
        print(f"[export] PyTorch vs ONNX max abs diff = {diff:.2e}  (expect < 1e-5)")
        if diff > 1e-4:
            sys.exit(f"[export] FAIL: round-trip diff {diff} too large")
    except ImportError:
        print("[export] onnxruntime not installed, skip round-trip check")


if __name__ == "__main__":
    main()
