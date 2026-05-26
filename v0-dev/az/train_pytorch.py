#!/usr/bin/env python3
"""
PyTorch trainer for OFC student NN — Go-compatible.

Arch (跟 Go ofc-train 一致):
  Input 132 V2 features → normalize (means/stds) → Linear(132,256)+ReLU → Linear(256,128)+ReLU → Linear(128,4)
  Outputs:
    out[0]: value (unnormalized: out[0]*yStd + yMean)
    out[1]: fan_logit (sigmoid → prob)
    out[2]: foul_logit (sigmoid → prob)
    out[3]: policy_logit (per-candidate; cross-cand softmax)

Sample (jsonl.gz):
  features (132 floats)
  mcScore (raw rollout/MCTS mean Q)
  fanRate, foulRate (in [0,1])
  policyTarget (one-hot 0/1 or probability)
  round (1-5)

输出: Go-compatible JSON ckpt (跟 ofc-train 写的 schema 一致)
"""
import argparse
import gzip
import json
import os
import sys
import time
from pathlib import Path

import numpy as np
import torch
import torch.nn as nn
import torch.optim as optim
from torch.utils.data import DataLoader, Dataset


class MLPHeads(nn.Module):
    """
    支持 2-hidden (h3=0) 或 3-hidden (h3>0).

    2-hidden: in_dim → h1 → h2 → out_dim
    3-hidden: in_dim → h1 → h2 → h3 → out_dim
    """
    def __init__(self, in_dim=132, h1=256, h2=128, h3=0, out_dim=4):
        super().__init__()
        self.h3_dim = h3
        self.fc1 = nn.Linear(in_dim, h1)
        self.fc2 = nn.Linear(h1, h2)
        if h3 > 0:
            self.fc3 = nn.Linear(h2, h3)
            self.fc_out = nn.Linear(h3, out_dim)
        else:
            self.fc3 = nn.Linear(h2, out_dim)
            self.fc_out = None
        self.relu = nn.ReLU()

    def forward(self, xn):
        h1 = self.relu(self.fc1(xn))
        h2 = self.relu(self.fc2(h1))
        if self.h3_dim > 0:
            h3 = self.relu(self.fc3(h2))
            return self.fc_out(h3)
        return self.fc3(h2)


def load_jsonl_gz(path):
    """Load all samples from a single .jsonl.gz file."""
    samples = []
    with gzip.open(path, 'rt') as f:
        for line in f:
            line = line.strip()
            if line:
                samples.append(json.loads(line))
    return samples


def load_dataset(dirs):
    """Walk dirs, load all *.jsonl.gz. Return (features, value_raw, fan, foul, policy, round)."""
    all_samples = []
    for d in dirs:
        d = Path(d)
        for p in d.rglob('*.jsonl.gz'):
            ss = load_jsonl_gz(p)
            all_samples.extend(ss)
            print(f'  loaded {len(ss)} from {p.name}')
    print(f'total: {len(all_samples)} samples')
    return all_samples


class OFCDataset(Dataset):
    def __init__(self, samples, y_mean, y_std, means, stds):
        # Pre-extract & normalize as tensors
        feats = np.array([s['features'] for s in samples], dtype=np.float32)
        feats = (feats - means) / np.where(stds > 0, stds, 1.0)
        self.X = torch.from_numpy(feats)

        # Value target: normalize
        vy = np.array([s.get('mcScore', 0.0) for s in samples], dtype=np.float32)
        self.Y_value = torch.from_numpy((vy - y_mean) / y_std)
        self.Y_fan = torch.from_numpy(np.array([s.get('fanRate', 0.0) for s in samples], dtype=np.float32))
        self.Y_foul = torch.from_numpy(np.array([s.get('foulRate', 0.0) for s in samples], dtype=np.float32))
        self.Y_policy = torch.from_numpy(np.array([s.get('policyTarget', 0.0) for s in samples], dtype=np.float32))

    def __len__(self): return len(self.X)
    def __getitem__(self, i):
        return self.X[i], self.Y_value[i], self.Y_fan[i], self.Y_foul[i], self.Y_policy[i]


def save_go_ckpt(model, means, stds, y_mean, y_std, out_path, meta):
    """Save PyTorch weights as Go-compatible JSON ckpt.

    Schema:
      2-hidden: inDim, h1Dim, h2Dim, outDim, w1/b1, w2/b2, w3/b3 (= output)
      3-hidden: + h3Dim, w3/b3 = middle, w4/b4 = output
    """
    state = model.state_dict()
    W1 = state['fc1.weight'].cpu().numpy()
    B1 = state['fc1.bias'].cpu().numpy()
    W2 = state['fc2.weight'].cpu().numpy()
    B2 = state['fc2.bias'].cpu().numpy()
    W3 = state['fc3.weight'].cpu().numpy()
    B3 = state['fc3.bias'].cpu().numpy()

    h3_dim = getattr(model, 'h3_dim', 0)
    ckpt = {
        'inDim': W1.shape[1],
        'h1Dim': W1.shape[0],
        'h2Dim': W2.shape[0],
        'w1': W1.tolist(),
        'b1': B1.tolist(),
        'w2': W2.tolist(),
        'b2': B2.tolist(),
        'w3': W3.tolist(),
        'b3': B3.tolist(),
        'means': means.tolist(),
        'stds': stds.tolist(),
        'yMean': float(y_mean),
        'yStd': float(y_std),
        **meta,
    }
    if h3_dim > 0:
        W4 = state['fc_out.weight'].cpu().numpy()
        B4 = state['fc_out.bias'].cpu().numpy()
        ckpt['h3Dim'] = h3_dim
        ckpt['w4'] = W4.tolist()
        ckpt['b4'] = B4.tolist()
        ckpt['outDim'] = W4.shape[0]
    else:
        ckpt['outDim'] = W3.shape[0]

    with open(out_path, 'w') as f:
        json.dump(ckpt, f)
    print(f'saved → {out_path} (arch: {W1.shape[1]}'
          f'→{W1.shape[0]}→{W2.shape[0]}'
          + (f'→{h3_dim}' if h3_dim > 0 else '')
          + f'→{ckpt["outDim"]})')


def compute_norm_stats(samples, n_features=132):
    """Compute per-feature means/stds + y mean/std on a sample."""
    feats = np.array([s['features'] for s in samples], dtype=np.float32)
    means = feats.mean(axis=0)
    stds = feats.std(axis=0)

    vy = np.array([s.get('mcScore', 0.0) for s in samples], dtype=np.float32)
    return means, stds, float(vy.mean()), float(vy.std() + 1e-6)


def train(args):
    device = torch.device('cuda' if torch.cuda.is_available() else 'cpu')
    print(f'device: {device}, torch: {torch.__version__}')

    samples = load_dataset(args.dataset_dirs)
    if args.max_samples and len(samples) > args.max_samples:
        rng = np.random.default_rng(42)
        idx = rng.choice(len(samples), args.max_samples, replace=False)
        samples = [samples[i] for i in idx]
        print(f'subsampled to {len(samples)}')

    # Compute normalization stats
    if args.warm_ckpt:
        # Reuse stats from warm ckpt
        wc = json.load(open(args.warm_ckpt))
        means = np.array(wc['means'], dtype=np.float32)
        stds = np.array(wc['stds'], dtype=np.float32)
        y_mean = wc['yMean']
        y_std = wc['yStd']
        print(f'warm stats from {args.warm_ckpt}: yMean={y_mean:.3f} yStd={y_std:.3f}')
    else:
        means, stds, y_mean, y_std = compute_norm_stats(samples)
        print(f'fresh stats: yMean={y_mean:.3f} yStd={y_std:.3f}')

    ds = OFCDataset(samples, y_mean, y_std, means, stds)
    n_total = len(ds)
    n_train = int(n_total * 0.95)
    n_val = n_total - n_train

    # GPU 优化: 整 dataset 一次性扔显存, 训练用 manual indexing (跳过 DataLoader 的 per-sample
    # GPU indexing 开销). 2.4M × 132 floats × 4B ≈ 1.26GB, A10 24GB 足.
    use_gpu_dataset = device.type == 'cuda'
    if use_gpu_dataset:
        print(f'[gpu-dataset] moving full dataset to {device}...', flush=True)
        ds.X = ds.X.to(device)
        ds.Y_value = ds.Y_value.to(device)
        ds.Y_fan = ds.Y_fan.to(device)
        ds.Y_foul = ds.Y_foul.to(device)
        ds.Y_policy = ds.Y_policy.to(device)
        print(f'[gpu-dataset] done, GPU mem ~{ds.X.element_size() * ds.X.nelement() / 1e9:.2f}GB', flush=True)
        # 固定 train/val split
        g = torch.Generator(device='cpu').manual_seed(42)
        perm0 = torch.randperm(n_total, generator=g)
        train_idx = perm0[:n_train].to(device)
        val_idx = perm0[n_train:].to(device)
        train_dl = None
        val_dl = None
    else:
        train_ds, val_ds = torch.utils.data.random_split(ds, [n_train, n_val],
                                                         generator=torch.Generator().manual_seed(42))
        train_dl = DataLoader(train_ds, batch_size=args.batch_size, shuffle=True, num_workers=2, pin_memory=True)
        val_dl = DataLoader(val_ds, batch_size=args.batch_size, shuffle=False, num_workers=2, pin_memory=True)
        train_idx = val_idx = None

    model = MLPHeads(in_dim=args.in_dim, h1=args.h1, h2=args.h2, h3=args.h3, out_dim=4).to(device)
    if args.warm_ckpt:
        wc = json.load(open(args.warm_ckpt))
        wc_h3 = int(wc.get('h3Dim', 0))
        if wc_h3 != args.h3:
            print(f'WARN: warm ckpt h3={wc_h3} != target h3={args.h3}, '
                  'skipping warm-start weights (arch mismatch). Fresh init used.')
        else:
            with torch.no_grad():
                model.fc1.weight.copy_(torch.tensor(wc['w1']))
                model.fc1.bias.copy_(torch.tensor(wc['b1']))
                model.fc2.weight.copy_(torch.tensor(wc['w2']))
                model.fc2.bias.copy_(torch.tensor(wc['b2']))
                model.fc3.weight.copy_(torch.tensor(wc['w3']))
                model.fc3.bias.copy_(torch.tensor(wc['b3']))
                if args.h3 > 0:
                    model.fc_out.weight.copy_(torch.tensor(wc['w4']))
                    model.fc_out.bias.copy_(torch.tensor(wc['b4']))
            print(f'warm-started from {args.warm_ckpt}')

    optimizer = optim.Adam(model.parameters(), lr=args.lr)
    bce = nn.BCEWithLogitsLoss()
    mse = nn.MSELoss()

    print(f'train={n_train}, val={n_val}, batch={args.batch_size}, lr={args.lr}, epochs={args.epochs}', flush=True)
    print(f'loss weights: value=1.0 fan={args.fan_w} foul={args.foul_w} policy={args.policy_w}', flush=True)
    print(f'mode: {"GPU manual-indexing" if use_gpu_dataset else "CPU DataLoader"}', flush=True)

    def train_one_epoch_gpu():
        # Manual GPU batch indexing — avoid DataLoader per-sample GPU index overhead
        perm = train_idx[torch.randperm(n_train, device=device)]
        sum_loss = sum_v = sum_fan = sum_foul = sum_pol = 0.0
        n_batch = 0
        for b_start in range(0, n_train, args.batch_size):
            idx = perm[b_start:b_start + args.batch_size]
            X = ds.X[idx]
            Yv = ds.Y_value[idx]
            Yfan = ds.Y_fan[idx]
            Yfoul = ds.Y_foul[idx]
            Ypol = ds.Y_policy[idx]

            out = model(X)
            lv = mse(out[:, 0], Yv)
            lfan = bce(out[:, 1], Yfan)
            lfoul = bce(out[:, 2], Yfoul)
            lpol = bce(out[:, 3], Ypol)
            loss = lv + args.fan_w * lfan + args.foul_w * lfoul + args.policy_w * lpol

            optimizer.zero_grad()
            loss.backward()
            optimizer.step()

            sum_loss += loss.item()
            sum_v += lv.item()
            sum_fan += lfan.item()
            sum_foul += lfoul.item()
            sum_pol += lpol.item()
            n_batch += 1
        return sum_loss, sum_v, sum_fan, sum_foul, sum_pol, n_batch

    def val_gpu():
        sum_loss = 0.0
        n_batch = 0
        with torch.no_grad():
            for b_start in range(0, n_val, args.batch_size):
                idx = val_idx[b_start:b_start + args.batch_size]
                X = ds.X[idx]
                Yv = ds.Y_value[idx]
                Yfan = ds.Y_fan[idx]
                Yfoul = ds.Y_foul[idx]
                Ypol = ds.Y_policy[idx]
                out = model(X)
                lv = mse(out[:, 0], Yv)
                lfan = bce(out[:, 1], Yfan)
                lfoul = bce(out[:, 2], Yfoul)
                lpol = bce(out[:, 3], Ypol)
                sum_loss += (lv + args.fan_w * lfan + args.foul_w * lfoul + args.policy_w * lpol).item()
                n_batch += 1
        return sum_loss, n_batch

    best_val = float('inf')
    for epoch in range(args.epochs):
        model.train()
        t0 = time.time()
        if use_gpu_dataset:
            sum_loss, sum_v, sum_fan, sum_foul, sum_pol, n_batch = train_one_epoch_gpu()
        else:
            sum_loss = sum_v = sum_fan = sum_foul = sum_pol = 0
            n_batch = 0
            for X, Yv, Yfan, Yfoul, Ypol in train_dl:
                X = X.to(device, non_blocking=True)
                Yv = Yv.to(device, non_blocking=True)
                Yfan = Yfan.to(device, non_blocking=True)
                Yfoul = Yfoul.to(device, non_blocking=True)
                Ypol = Ypol.to(device, non_blocking=True)
                out = model(X)
                lv = mse(out[:, 0], Yv)
                lfan = bce(out[:, 1], Yfan)
                lfoul = bce(out[:, 2], Yfoul)
                lpol = bce(out[:, 3], Ypol)
                loss = lv + args.fan_w * lfan + args.foul_w * lfoul + args.policy_w * lpol
                optimizer.zero_grad()
                loss.backward()
                optimizer.step()
                sum_loss += loss.item()
                sum_v += lv.item()
                sum_fan += lfan.item()
                sum_foul += lfoul.item()
                sum_pol += lpol.item()
                n_batch += 1

        # Val
        model.eval()
        if use_gpu_dataset:
            val_loss, val_n = val_gpu()
        else:
            val_loss = 0
            val_n = 0
            with torch.no_grad():
                for X, Yv, Yfan, Yfoul, Ypol in val_dl:
                    X = X.to(device, non_blocking=True)
                    Yv = Yv.to(device, non_blocking=True)
                    Yfan = Yfan.to(device, non_blocking=True)
                    Yfoul = Yfoul.to(device, non_blocking=True)
                    Ypol = Ypol.to(device, non_blocking=True)
                    out = model(X)
                    lv = mse(out[:, 0], Yv)
                    lfan = bce(out[:, 1], Yfan)
                    lfoul = bce(out[:, 2], Yfoul)
                    lpol = bce(out[:, 3], Ypol)
                    val_loss += (lv + args.fan_w * lfan + args.foul_w * lfoul + args.policy_w * lpol).item()
                    val_n += 1

        dt = time.time() - t0
        print(f'epoch {epoch+1}/{args.epochs}  train_loss={sum_loss/n_batch:.4f} '
              f'(v={sum_v/n_batch:.4f} fan={sum_fan/n_batch:.4f} foul={sum_foul/n_batch:.4f} pol={sum_pol/n_batch:.4f}) '
              f'val_loss={val_loss/val_n:.4f}  {dt:.1f}s', flush=True)

        if val_loss / val_n < best_val:
            best_val = val_loss / val_n
            meta = {
                'accuracy': 0.0,
                'gamesPlayed': -1,
                'samplesCount': len(samples),
                'policyVersion': args.policy,
                'round': args.round,
                'timestamp': time.strftime('%Y-%m-%dT%H:%M:%S%z'),
            }
            save_go_ckpt(model, means, stds, y_mean, y_std, args.out, meta)

    # Final save with last weights too (in case overfit at end)
    final_path = args.out.replace('.json', '-final.json')
    meta = {
        'accuracy': 0.0,
        'gamesPlayed': -1,
        'samplesCount': len(samples),
        'policyVersion': args.policy,
        'round': args.round,
        'timestamp': time.strftime('%Y-%m-%dT%H:%M:%S%z'),
    }
    save_go_ckpt(model, means, stds, y_mean, y_std, final_path, meta)


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--dataset-dirs', nargs='+', required=True)
    p.add_argument('--out', required=True, help='output Go-compatible JSON ckpt path')
    p.add_argument('--warm-ckpt', default='', help='warm-start from existing Go ckpt (matches arch)')
    p.add_argument('--in-dim', type=int, default=132)
    p.add_argument('--h1', type=int, default=512, help='hidden 1 (default 512 for big model)')
    p.add_argument('--h2', type=int, default=256, help='hidden 2 (default 256 for big model)')
    p.add_argument('--h3', type=int, default=128, help='hidden 3 (default 128 for 3-hidden; 0 = 2-hidden legacy)')
    p.add_argument('--epochs', type=int, default=60)
    p.add_argument('--batch-size', type=int, default=512)
    p.add_argument('--lr', type=float, default=2e-3)
    p.add_argument('--fan-w', type=float, default=0.40)
    p.add_argument('--foul-w', type=float, default=0.10)
    p.add_argument('--policy-w', type=float, default=0.50)
    p.add_argument('--max-samples', type=int, default=0)
    p.add_argument('--policy', default='v0-pytorch')
    p.add_argument('--round', type=int, default=1)
    args = p.parse_args()
    train(args)
