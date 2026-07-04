#!/usr/bin/env python3
# 2026-07-04 sp42: 存量数据集离线清零 f129(弃牌rank)/f146(拆connector) — 与代码侧固化清零对齐,
# 免重 gen (label/其他维不动). 用法: python3 mac-scripts/zero-discard-dims.py v3-dataset-i169-sp40-1
import sys, os, gzip, json, glob

DIMS = (129, 146)

def process(fn):
    tmp = fn + '.tmp'
    n = 0
    changed = 0
    with gzip.open(fn, 'rt') as fin, gzip.open(tmp, 'wt') as fout:
        for line in fin:
            d = json.loads(line)
            f = d['features']
            for dim in DIMS:
                if dim < len(f) and f[dim] != 0:
                    f[dim] = 0
                    changed += 1
            fout.write(json.dumps(d) + '\n')
            n += 1
    os.replace(tmp, fn)
    return n, changed

root = sys.argv[1]
files = glob.glob(os.path.join(root, '**', '*.jsonl.gz'), recursive=True)
total = totchg = bad = 0
for i, fn in enumerate(files):
    try:
        n, c = process(fn)
        total += n; totchg += c
    except Exception as e:
        bad += 1
        print(f"  跳过坏文件 {fn}: {e}")
    if (i + 1) % 20 == 0:
        print(f"  {i+1}/{len(files)} files...")
print(f"done: {len(files)} files ({bad} 坏), {total} samples, 清零 {totchg} 个非零值")
