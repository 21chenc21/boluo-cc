#!/usr/bin/env python3
# 2026-07-06 sp46: prod solve_log → 真人板种子 JSON (喂 gen -seed-states).
# 用法:
#   本地有库:  python3 mac-scripts/extract-solve-seeds.py games.db > real-seeds.json
#   prod 远程:  ssh <prod> "sqlite3 ~/path/games.db \"SELECT round, request_json FROM solve_log\"" \
#              | python3 mac-scripts/extract-solve-seeds.py - > real-seeds.json
# 坑 (memory reference_prod_solve_log_mining): json_extract 大小写 / usedCards 可能被截断 → 靠 gen 端校验兜底.
import sys, json, sqlite3

def rows_from_db(path):
    db = sqlite3.connect(path)
    for round_, req in db.execute("SELECT round, request_json FROM solve_log WHERE request_json IS NOT NULL"):
        yield round_, req

def rows_from_stdin():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        # sqlite3 默认 | 分隔: round|{json...}
        i = line.find('|')
        if i < 0:
            continue
        yield line[:i], line[i+1:]

src = sys.argv[1] if len(sys.argv) > 1 else '-'
rows = rows_from_stdin() if src == '-' else rows_from_db(src)

out, seen = [], set()
for round_, req in rows:
    try:
        r = json.loads(req)
        st = r.get('state') or {}
        entry = {
            'round':  int(round_ or r.get('round') or 0),
            'top':    st.get('top') or [],
            'middle': st.get('middle') or [],
            'bottom': st.get('bottom') or [],
            'dealt':  r.get('dealt') or [],
            'used':   st.get('usedCards') or [],
        }
    except Exception:
        continue
    key = json.dumps(entry, sort_keys=True)
    if key in seen:
        continue
    seen.add(key)
    out.append(entry)

json.dump(out, sys.stdout, ensure_ascii=False)
print(f"\n", file=sys.stderr)
print(f"提取 {len(out)} 条去重真人板 (无效条目由 gen 端校验跳过)", file=sys.stderr)
