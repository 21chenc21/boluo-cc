#!/usr/bin/env python3
"""
ofc-train-pytorch-wrapper.py — 模拟 ofc-train (Go) CLI, 内部调 train_pytorch.py.

alphazero-train 调 -train-bin <path>, 该 binary 必须接受:
  -dataset-dir DIR
  -outdim NUM, -h1 NUM, -h2 NUM, -h3 NUM (新)
  -indim NUM
  -fan-w FLOAT, -foul-w FLOAT, -policy-w FLOAT
  -epochs NUM, -lr FLOAT
  -ckpt-dir DIR     (save round-001-accXX.json here)
  -policy STRING
  -init-from-ckpt PATH (optional)
  -hours FLOAT, -round-min FLOAT (ignored, ofc-train internal scheduling)

我们把这些转成 train_pytorch.py 的参数, 跑训练, 保存 ckpt 到 ckpt-dir 里.
alphazero-train 之后 findLatestCkpt(ckpt-dir) 取最新 round-NNN-accXX.json.

用法 (alphazero-train 自己调, 用户一般不直接调):
  ofc-train-pytorch-wrapper.py -dataset-dir DIR -h3 128 ...
"""
import argparse
import os
import subprocess
import sys
import time
from pathlib import Path


def main():
    p = argparse.ArgumentParser()
    p.add_argument('-dataset-dir', dest='dataset_dir', required=True)
    p.add_argument('-outdim', type=int, default=4)
    p.add_argument('-h1', type=int, default=128)
    p.add_argument('-h2', type=int, default=64)
    p.add_argument('-h3', type=int, default=0)
    p.add_argument('-indim', type=int, default=132)
    p.add_argument('-fan-w', dest='fan_w', type=float, default=0.40)
    p.add_argument('-foul-w', dest='foul_w', type=float, default=0.10)
    p.add_argument('-policy-w', dest='policy_w', type=float, default=0.50)
    p.add_argument('-epochs', type=int, default=15)
    p.add_argument('-lr', type=float, default=0.0001)
    p.add_argument('-ckpt-dir', dest='ckpt_dir', required=True)
    p.add_argument('-policy', default='v0-pytorch')
    p.add_argument('-init-from-ckpt', dest='init_from_ckpt', default='')
    p.add_argument('-hours', type=float, default=0.5)        # ignored
    p.add_argument('-round-min', dest='round_min', type=float, default=60)  # ignored
    args = p.parse_args()

    # 输出 ckpt path (alphazero-train 期望 round-NNN-accXX.json 模式)
    os.makedirs(args.ckpt_dir, exist_ok=True)
    out_path = os.path.join(args.ckpt_dir, 'round-001-acc00.json')

    # 训练脚本路径
    script_dir = Path(__file__).resolve().parent.parent
    train_script = script_dir / 'az' / 'train_pytorch.py'

    cmd = [
        'python3', '-u', str(train_script),
        '--dataset-dirs', args.dataset_dir,
        '--out', out_path,
        '--in-dim', str(args.indim),
        '--h1', str(args.h1),
        '--h2', str(args.h2),
        '--h3', str(args.h3),
        '--epochs', str(args.epochs),
        '--batch-size', '4096',
        '--lr', str(args.lr),
        '--fan-w', str(args.fan_w),
        '--foul-w', str(args.foul_w),
        '--policy-w', str(args.policy_w),
        '--policy', args.policy,
        '--round', '1',
    ]
    if args.init_from_ckpt:
        cmd += ['--warm-ckpt', args.init_from_ckpt]

    print(f'[wrapper] starting at {time.strftime("%Y-%m-%d %H:%M:%S")}', flush=True)
    print(f'[wrapper] cmd: {" ".join(cmd)}', flush=True)
    print(f'[wrapper] output → {out_path}', flush=True)

    rc = subprocess.call(cmd)
    if rc != 0:
        print(f'[wrapper] train_pytorch.py failed rc={rc}', file=sys.stderr)
        sys.exit(rc)

    if not os.path.exists(out_path):
        # train_pytorch 保存 best by val, 偶尔可能没新文件 (val 没创新低)
        # 这时用 -final.json
        final_path = out_path.replace('.json', '-final.json')
        if os.path.exists(final_path):
            os.rename(final_path, out_path)
        else:
            print(f'[wrapper] no ckpt produced at {out_path}', file=sys.stderr)
            sys.exit(1)
    else:
        # 也保留 final
        final_path = out_path.replace('.json', '-final.json')
        # 不需要重命名 final, alphazero-train 用 findLatestCkpt 找 mtime 最新的
        # 如果 final 比 best 新, 它会取 final. 这里 OK.
        pass

    print(f'[wrapper] done at {time.strftime("%Y-%m-%d %H:%M:%S")}, ckpt: {out_path}', flush=True)


if __name__ == '__main__':
    main()
