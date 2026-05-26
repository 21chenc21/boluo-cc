#!/usr/bin/env python3
"""
expand_ckpt_to_big.py — 把 2-hidden 小 ckpt 数学等价扩展成 3-hidden 大 ckpt.

原理: 新 NN 起初计算 == 旧 NN 输出.

老 MLP: x → h1=ReLU(W1 x + b1) → h2=ReLU(W2 h1 + b2) → out = W3 h2 + b3
       (维度 [in, old_h1, old_h2, out])

新 MLP: x → h1'=ReLU(W1' x + b1') → h2'=ReLU(W2' h1' + b2')
       → h3'=ReLU(W3' h2' + b3') → out' = W4' h3' + b4'
       (维度 [in, new_h1, new_h2, new_h3, out])

约束:
  new_h1 >= old_h1     (h1 多余 dim 设为 zero)
  new_h2 >= old_h2     (h2 多余 dim 设为 zero)
  new_h3 == old_h2     (h3 中间层做 identity copy of h2)

数学映射:
  W1' = [[old_W1],
         [0 ]]           # [new_h1, in]
  b1' = [old_b1, 0]      # [new_h1]
  W2' = [[old_W2, 0],
         [0,      0]]    # [new_h2, new_h1]
  b2' = [old_b2, 0]      # [new_h2]
  W3' = [[I_old_h2, 0],
         [0,        0]]  # [new_h3, new_h2]  - identity in top-left block
  b3' = 0                # [new_h3] — h3=h2 (h2 已经 >=0 通过 ReLU)
  W4' = [old_W3, 0]      # [out, new_h3] — top old_h2 cols = old W3
  b4' = old_b3           # [out]

意义: 起步 expanded NN 数学等价于 baseline. Bench 应该 == baseline 数字.
然后 fine-tune 在 cumulative samples 上, 大模型多出来的 capacity 用来吸收新信号.

用法:
  python3 expand_ckpt_to_big.py \\
    --small-ckpt ckpts-v2-ema/round-001-acc89.json \\
    --out big-model-warmstart.json \\
    --new-h1 512 --new-h2 256 --new-h3 128
"""
import argparse
import json
import numpy as np


def expand(small_ckpt_path, new_h1, new_h2, new_h3):
    with open(small_ckpt_path) as f:
        sc = json.load(f)

    s_W1 = np.array(sc['w1'], dtype=np.float32)  # [old_h1, in]
    s_W2 = np.array(sc['w2'], dtype=np.float32)  # [old_h2, old_h1]
    s_W3 = np.array(sc['w3'], dtype=np.float32)  # [out, old_h2]
    s_B1 = np.array(sc['b1'], dtype=np.float32)
    s_B2 = np.array(sc['b2'], dtype=np.float32)
    s_B3 = np.array(sc['b3'], dtype=np.float32)

    in_dim = s_W1.shape[1]
    old_h1, old_h2, out_dim = s_W1.shape[0], s_W2.shape[0], s_W3.shape[0]

    assert s_W2.shape == (old_h2, old_h1), f'W2 shape mismatch {s_W2.shape}'
    assert s_W3.shape == (out_dim, old_h2), f'W3 shape mismatch {s_W3.shape}'

    if not (new_h1 >= old_h1):
        raise ValueError(f'new_h1 {new_h1} must >= old_h1 {old_h1}')
    if not (new_h2 >= old_h2):
        raise ValueError(f'new_h2 {new_h2} must >= old_h2 {old_h2}')
    if not (new_h3 == old_h2):
        raise ValueError(f'new_h3 {new_h3} must == old_h2 {old_h2} for identity middle layer')

    # Init new weights (zero)
    W1 = np.zeros((new_h1, in_dim), dtype=np.float32)
    B1 = np.zeros(new_h1, dtype=np.float32)
    W2 = np.zeros((new_h2, new_h1), dtype=np.float32)
    B2 = np.zeros(new_h2, dtype=np.float32)
    W3 = np.zeros((new_h3, new_h2), dtype=np.float32)
    B3 = np.zeros(new_h3, dtype=np.float32)
    W4 = np.zeros((out_dim, new_h3), dtype=np.float32)
    B4 = np.zeros(out_dim, dtype=np.float32)

    # Copy old → new
    W1[:old_h1, :] = s_W1
    B1[:old_h1] = s_B1
    W2[:old_h2, :old_h1] = s_W2
    B2[:old_h2] = s_B2
    # W3: identity on top-left block (since new_h3 == old_h2)
    for i in range(old_h2):
        W3[i, i] = 1.0
    # B3 = 0 (h3 == h2, h2 已通过 ReLU 非负, ReLU(1*h2+0) = h2)
    # W4 = old W3 (output)
    W4[:, :old_h2] = s_W3
    B4[:] = s_B3

    # Build new ckpt (preserve metadata, means/stds, yMean/yStd)
    new = {
        'inDim': in_dim,
        'h1Dim': new_h1,
        'h2Dim': new_h2,
        'h3Dim': new_h3,
        'outDim': out_dim,
        'w1': W1.tolist(),
        'b1': B1.tolist(),
        'w2': W2.tolist(),
        'b2': B2.tolist(),
        'w3': W3.tolist(),
        'b3': B3.tolist(),
        'w4': W4.tolist(),
        'b4': B4.tolist(),
        'means': sc['means'],
        'stds': sc['stds'],
        'yMean': sc['yMean'],
        'yStd': sc['yStd'],
    }
    # Preserve any other metadata fields
    for k in sc:
        if k not in new and not k.startswith('w') and not k.startswith('b'):
            new[k] = sc[k]
    new['arch_note'] = (f'expanded from {old_h1}->{old_h2}->{out_dim} '
                        f'(2-hidden) to {new_h1}->{new_h2}->{new_h3}->{out_dim} (3-hidden), '
                        f'math-equivalent init')
    return new, (in_dim, old_h1, old_h2, out_dim)


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--small-ckpt', required=True)
    p.add_argument('--out', required=True)
    p.add_argument('--new-h1', type=int, default=512)
    p.add_argument('--new-h2', type=int, default=256)
    p.add_argument('--new-h3', type=int, default=128)
    args = p.parse_args()

    new, (in_dim, old_h1, old_h2, out_dim) = expand(
        args.small_ckpt, args.new_h1, args.new_h2, args.new_h3)

    with open(args.out, 'w') as f:
        json.dump(new, f)

    print(f'expanded {args.small_ckpt}')
    print(f'  old arch: {in_dim} → {old_h1} → {old_h2} → {out_dim} (2-hidden)')
    print(f'  new arch: {in_dim} → {args.new_h1} → {args.new_h2} → {args.new_h3} → {out_dim} (3-hidden)')
    print(f'  saved → {args.out}')


if __name__ == '__main__':
    main()
