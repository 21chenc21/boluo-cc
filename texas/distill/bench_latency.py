"""
bench_latency.py — measure ONNX inference latency on single CPU.

POC criterion #4: < 10 ms / query.

Tests batch sizes 1, 16, 64, 256 to characterize:
- Single-query latency (1 user)
- Throughput at moderate batch (server side)
- Max throughput

Usage:
    python3 distill/bench_latency.py --model distill/models/leduc-policy.onnx --warmup 100 --trials 1000
"""

import argparse
import sys
import time

try:
    import numpy as np
    import onnxruntime as ort
except ImportError as e:
    sys.exit(f"[bench] missing dependency: {e}\n         pip install -r distill/requirements.txt")

FEATURE_DIM = 35


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="distill/models/leduc-policy.onnx")
    ap.add_argument("--warmup", type=int, default=100)
    ap.add_argument("--trials", type=int, default=2000)
    args = ap.parse_args()

    # CPU only — POC constraint: single-machine CPU deploy.
    sess = ort.InferenceSession(
        args.model,
        sess_options=ort.SessionOptions(),
        providers=["CPUExecutionProvider"],
    )

    print(f"[bench] model: {args.model}")
    print(f"[bench] providers: {sess.get_providers()}")
    print()

    for batch in [1, 16, 64, 256]:
        x = np.random.rand(batch, FEATURE_DIM).astype(np.float32)

        # Warmup
        for _ in range(args.warmup):
            sess.run(None, {"features": x})

        # Measure
        t0 = time.perf_counter()
        for _ in range(args.trials):
            sess.run(None, {"features": x})
        elapsed = time.perf_counter() - t0

        per_call_ms = (elapsed / args.trials) * 1000
        per_sample_us = per_call_ms / batch * 1000
        throughput = batch * args.trials / elapsed

        print(f"  batch={batch:4d}  {per_call_ms:7.3f} ms/call  {per_sample_us:6.2f} µs/sample  {throughput:9.0f} qps")

    print()
    print(f"POC #4 (single-query latency < 10 ms): see batch=1 row above")


if __name__ == "__main__":
    main()
