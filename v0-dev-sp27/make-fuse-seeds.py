# 2026-07-07 保险丝管辖四案 (16/46/86/63) 精准邻域弹生成器
# 骨架同 case110-seeds: 保留案发结构, 排列 rank/suit, 喂 gen -seed-states
import json, random, itertools

SUITS = "cdhs"
rng = random.Random(1646)

def card(r, s): return r + s

class Board:
    def __init__(self):
        self.seen = set()
    def take(self, r, s=None):
        if s is not None:
            c = card(r, s)
            if c in self.seen: return None
            self.seen.add(c); return c
        ss = [x for x in SUITS if card(r, x) not in self.seen]
        if not ss: return None
        c = card(r, rng.choice(ss)); self.seen.add(c); return c

def uniq(rows):
    out, seen = [], set()
    for r in rows:
        k = json.dumps(r, sort_keys=True)
        if k not in seen:
            seen.add(k); out.append(r)
    return out

# ---------- case16: R3 顶[鬼+宫廷=已锁范] + 发大对诱惑冒顶, 中弱 ----------
# 原案: 顶[Xj0 Qc] 中[9s 2h] 底[TTT] 发[AAA]. 课: 大对进中托底, 别单张上顶抬序要求
def gen16(n=220):
    rows = []
    while len(rows) < n * 2:
        b = Board()
        C = rng.choice("QQK")                      # 顶宫廷 (Q为主)
        P = "A" if C == "K" else rng.choice("KA")  # 发对 rank > C
        tr = rng.choice("9TJ")                     # 底三条 rank
        top = ["Xj0", b.take(C)]
        bot = [b.take(tr) for _ in range(3)]
        mids = rng.sample([r for r in "2345678" if r != tr], 2)
        mid = [b.take(r) for r in mids]
        third = b.take(P) if rng.random() < 0.4 else b.take(rng.choice("23456789"))
        dealt = [b.take(P), b.take(P), third]
        cells = top + mid + bot + dealt
        if None in cells: continue
        rows.append({"round": 3, "top": top, "middle": mid, "bottom": bot,
                     "dealt": dealt, "used": []})
        if len(rows) >= n: break
    return uniq(rows)[:n]

# ---------- case46: R4 顶[鬼+小张=锁低对] + 发同rank诱冒222型三条(中托不住必爆) ----------
# 原案: 顶[Xj0 2c] 中[4s 8c Th] 底[777Q] 发[2s 8s 6c]. 课: 冒顶三条抬序=自杀, 小张下底
def gen46(n=220):
    rows = []
    while len(rows) < n * 2:
        b = Board()
        L = rng.choice("234")                      # 顶锁低对 rank
        tr = rng.choice([r for r in "6789" if r != L])  # 底三条
        kick = rng.choice("JQK")
        top = ["Xj0", b.take(L)]
        bot = [b.take(tr) for _ in range(3)] + [b.take(kick)]
        mids = rng.sample([r for r in "456789T" if r not in (tr, L)], 3)
        mid = [b.take(r) for r in mids]
        dealt = [b.take(L),                        # 诱惑: 冒顶成LLL
                 b.take(rng.choice(mids)),          # 配中成对
                 b.take(rng.choice([r for r in "345689" if r not in (L, tr)]))]
        cells = top + mid + bot + dealt
        if None in cells: continue
        rows.append({"round": 4, "top": top, "middle": mid, "bottom": bot,
                     "dealt": dealt, "used": []})
        if len(rows) >= n: break
    return uniq(rows)[:n]

# ---------- case86: R4 顶AA已锁范 + 发[垃圾 鬼 A] — 鬼+A进中托顶AA, 别弃A/鬼单飞 ----------
# 原案: 顶[Ad Ah] 中[4h 9h Th] 底[8s9s2sKs四花draw] 发[6c Xj0 Ac]. 课: 中必须托住AA
def gen86(n=220):
    rows = []
    while len(rows) < n * 2:
        b = Board()
        T = rng.choice("AAK")                      # 顶大对 (A为主)
        bs = rng.choice(SUITS)                     # 底四花 draw 花色
        top = [b.take(T), b.take(T)]
        br = rng.sample([r for r in "23456789T" if r != T], 3) + ["K" if T != "K" else "Q"]
        bot = [b.take(r, bs) for r in br]
        mids = rng.sample([r for r in "3456789T" if r != T], 3)
        mid = [b.take(r) for r in mids]
        dealt = [b.take(rng.choice([r for r in "234567" if r != T])),
                 "Xj0", b.take(T)]                 # 第三张大对牌 + 鬼
        cells = top + mid + bot + dealt
        if None in cells: continue
        rows.append({"round": 4, "top": top, "middle": mid, "bottom": bot,
                     "dealt": dealt, "used": []})
        if len(rows) >= n: break
    return uniq(rows)[:n]

# ---------- case63: R2 deck-aware — 上位rank死光, 发高对必上顶锁范 ----------
# 原案: 顶[Qd] 中[5c6c] 底[3h9s] 发[Kh Ks 4d] + 4A死. 课: 死牌让高对=可锁最高对
def gen63(n=220):
    rows = []
    while len(rows) < n * 2:
        b = Board()
        f = rng.random()
        if f < 0.5:   H, dead = "K", ["A" + s for s in SUITS]            # KK + 4A死
        elif f < 0.8: H, dead = "A", []                                   # AA 无需死牌
        else:         H, dead = "Q", ["A" + s for s in SUITS] + ["K" + s for s in SUITS]
        for c in dead: b.seen.add(c)
        tc = rng.choice([r for r in "TJQ" if r < H or (H == "A")][:3] or ["T"])
        if H == "Q": tc = rng.choice("TJ")
        if H == "K": tc = rng.choice("TJQ")
        top = [b.take(tc)]
        ms = rng.choice(SUITS)
        m0 = rng.choice("3456")
        mid = [b.take(m0, ms), b.take("23456789"["23456789".index(m0) + 1], ms)]
        bots = rng.sample([r for r in "345789" if r != m0], 2)
        bot = [b.take(r) for r in bots]
        dealt = [b.take(H), b.take(H), b.take(rng.choice("2345678"))]
        cells = top + mid + bot + dealt
        if None in cells: continue
        rows.append({"round": 2, "top": top, "middle": mid, "bottom": bot,
                     "dealt": dealt, "used": dead})
        if len(rows) >= n: break
    return uniq(rows)[:n]

for name, fn in [("16", gen16), ("46", gen46), ("86", gen86), ("63", gen63)]:
    rows = fn()
    with open(f"case{name}-seeds.json", "w") as f:
        json.dump(rows, f, ensure_ascii=False)
    print(f"case{name}-seeds.json: {len(rows)} 变体")
