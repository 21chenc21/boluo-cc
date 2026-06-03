#!/usr/bin/env python3
# 在线 testcase: POST 每个 case 到 localhost:8002/api/solve, 复刻 bench-cases 匹配逻辑.
# 用法: python3 online_testcase.py <cases1.json> [cases2.json ...]
import json, sys, urllib.request

URL = "http://localhost:8002/api/solve"

def normcard(c):
    return "X" if c == "X" or c.startswith("X") else c

def sk(cards):
    return ",".join(sorted(normcard(c) for c in (cards or [])))

def solve(case):
    rnd = case["round"]
    dealt = case["dealt"]
    st = case.get("state", {})
    dc = 0 if rnd == 1 else 1
    if rnd == 99 or case.get("mode") == "fantasy":
        dc = case.get("discardCount", len(dealt) - 13)
    req = {
        "round": rnd,
        "state": {
            "top": st.get("top", []), "middle": st.get("middle", []),
            "bottom": st.get("bottom", []), "usedCards": st.get("usedCards", []),
        },
        "dealt": dealt, "discardCount": dc,
        "jokerCount": case.get("jokerCount", 2),
        "pureMLP": True, "topK": 1,
    }
    if case.get("mode"):
        req["mode"] = case["mode"]
    data = json.dumps(req).encode()
    rq = urllib.request.Request(URL, data=data, headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(rq, timeout=60).read())

def match(layout, exps):
    for e in exps:
        if (sk(layout.get("top")) == sk(e.get("top"))
                and sk(layout.get("middle")) == sk(e.get("middle"))
                and sk(layout.get("bottom")) == sk(e.get("bottom"))):
            return True
    return False

def run(path):
    cases = json.load(open(path))
    p = w = f = 0
    fails = []
    for c in cases:
        try:
            resp = solve(c)
            lay = resp["layout"]
        except Exception as e:
            f += 1; fails.append((c["name"], {"err": str(e)})); continue
        if match(lay, c["expecteds"]):
            p += 1
        elif c.get("warn"):
            w += 1
        else:
            f += 1; fails.append((c["name"], lay))
    print("=== %s: %d通过 / %d警告 / %d失败 / %d总计 ===" % (path, p, w, f, len(cases)))
    for name, lay in fails:
        if "err" in lay:
            print("  ✗ %s  ERR %s" % (name, lay["err"]))
        else:
            print("  ✗ %s" % name)
            print("     AI: 头%s 中%s 底%s" % (lay.get("top"), lay.get("middle"), lay.get("bottom")))

for path in sys.argv[1:]:
    run(path)
