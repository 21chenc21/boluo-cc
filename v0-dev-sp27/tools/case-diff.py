#!/usr/bin/env python3
# case-diff.py <old_bench.txt> <new_bench.txt> [cases.json] — 逐案对比两份 bench-cases 输出的 AI 摆法
# 微调 SOP 固定环节 (2026-07-09 用户定): 每次微调必产 diff 报告 (含牌局初始状态), 差异案呈报 review.
# 用法: python3 tools/case-diff.py old.txt new.txt   (默认读 cases/game-cases.json 回查板面/发牌)
import sys, re, json, os

def parse(path):
    cases = {}
    name, status = None, None
    for line in open(path, encoding='utf-8'):
        m = re.match(r'^([✓✗⚠])\s+(.*)$', line.strip())
        if m:
            status, name = m.group(1), m.group(2).split(':')[0].strip()
            continue
        m = re.match(r'^\s+AI:\s+(.*)$', line.rstrip())
        if m and name:
            cases[name] = (status, m.group(1).strip())
            name = None
    return cases

def load_boards(path):
    if not os.path.exists(path):
        return {}
    boards = {}
    for c in json.load(open(path, encoding='utf-8')):
        nm = c.get('name', '')
        st = c.get('state', {}) or {}
        j = lambda x: ' '.join(x) if x else '—'
        b = f"顶[{j(st.get('top'))}] 中[{j(st.get('middle'))}] 底[{j(st.get('bottom'))}]"
        if st.get('usedCards'):
            b += f" 死牌[{j(st['usedCards'])}]"
        boards[nm] = (f"R{c.get('round','?')}", b, j(c.get('dealt')))
    return boards

def board_of(boards, key):
    for nm, v in boards.items():
        if nm.startswith(key) or key.startswith(nm.split(':')[0].strip()):
            return v
    return None

old, new = parse(sys.argv[1]), parse(sys.argv[2])
boards = load_boards(sys.argv[3] if len(sys.argv) > 3 else 'cases/game-cases.json')

diffs, status_only = [], []
for k in old:
    if k not in new:
        continue
    (os_, oa), (ns_, na) = old[k], new[k]
    if oa != na:
        diffs.append((k, os_, oa, ns_, na))
    elif os_ != ns_:
        status_only.append((k, os_, ns_))
new_cases = [k for k in new if k not in old]

print(f"共同案: {len(set(old) & set(new))} | 摆法差异: {len(diffs)} | 仅状态变: {len(status_only)} | 新增案: {len(new_cases)}\n")
for k, os_, oa, ns_, na in diffs:
    print(f"◆ {k}   [{os_} → {ns_}]")
    bv = board_of(boards, k)
    if bv:
        print(f"  局({bv[0]}): {bv[1]}")
        print(f"  发:  {bv[2]}")
    print(f"  旧: {oa}")
    print(f"  新: {na}\n")
for k, os_, ns_ in status_only:
    print(f"○ 状态变(摆法同): {k}  [{os_} → {ns_}]")
for k in new_cases:
    print(f"+ 新增案: {k}  [{new[k][0]}] {new[k][1]}")
