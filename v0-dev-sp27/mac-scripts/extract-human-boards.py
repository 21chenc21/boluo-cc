import json, sys, hashlib, collections
ours=set(open("/tmp/our_uids_full.txt").read().split())
RANKS="23456789TJQKA"; SUITS="cdhs"
FULL52=[r+s for r in RANKS for s in SUITS]

games={}; human_meta={}
for line in open("ofc_hands.jsonl"):
    try: d=json.loads(line)
    except: continue
    seats=d.get("seats") or []
    if not seats: continue
    gid0=d.get("game_id") or (str(d.get("room_id"))+"-"+str(d.get("hand_id")))
    # game_id 跨天复用 → 加"真人自己的逐轮牌"哈希锚定同一物理局
    anchor=""
    for s in seats:
        rid=str(s.get("role_id"))
        if not s.get("is_me") and rid not in ours and len(s.get("rounds") or [])==5:
            anchor=rid+"|"+hashlib.md5(json.dumps(s["rounds"],sort_keys=True).encode()).hexdigest()[:12]
            break
    gid=gid0+"#"+anchor
    tbl=games.setdefault(gid,{})
    for s in seats:
        rid=str(s.get("role_id"))
        rs=s.get("rounds") or []
        if len(rs)==5 and rid not in tbl:
            tbl[rid]=rs
        if not s.get("is_me") and rid not in ours:
            human_meta.setdefault(gid,set()).add(rid)

out=[]; seen=set(); bad=0
for gid, humans in human_meta.items():
    tbl=games[gid]
    for rid in sorted(humans):
        rs=tbl.get(rid)
        if not rs: continue
        counts=[len(r.get("top",[]))+len(r.get("mid",[]))+len(r.get("bot",[])) for r in rs]
        if counts!=[5,2,2,2,2]: bad+=1; continue
        top,mid,bot=[],[],[]
        ok=True
        for ri,r in enumerate(rs):
            rnd=ri+1
            placed_now=(r.get("top") or [])+(r.get("mid") or [])+(r.get("bot") or [])
            used=[]
            for prid in sorted(tbl):
                for prev in tbl[prid][:ri]:
                    used += (prev.get("top") or [])+(prev.get("mid") or [])+(prev.get("bot") or [])
            if rnd==1:
                dealt=list(placed_now)
            else:
                known=set(used)|set(placed_now)|set(top)|set(mid)|set(bot)
                pool=sorted(c for c in FULL52 if c not in known)
                if not pool: ok=False; break
                h=int(hashlib.md5((gid+"|"+rid+"|"+str(rnd)).encode()).hexdigest(),16)
                dealt=list(placed_now)+[pool[h%len(pool)]]
            # 物理校验: used 无重复, 且不超 3 人桌上限
            cap=3*(5 if rnd==1 else 5+2*(rnd-2)) if rnd>1 else 0
            if len(used)!=len(set(used)) or (rnd>1 and len(used)>cap): ok=False; break
            entry={"round":rnd,"top":list(top),"middle":list(mid),"bottom":list(bot),
                   "dealt":dealt,"used":used}
            k=json.dumps(entry,sort_keys=True)
            if k not in seen:
                seen.add(k); out.append(entry)
            top+=r.get("top") or []; mid+=r.get("mid") or []; bot+=r.get("bot") or []
        if not ok: bad+=1

json.dump(out, sys.stdout, ensure_ascii=False)
c=collections.Counter(e["round"] for e in out)
lens=collections.Counter(len(e["used"]) for e in out if e["round"]==2)
print("\n真人种子态: %d 条, 轮次 %s, 异常 %d"%(len(out),dict(sorted(c.items())),bad), file=sys.stderr)
print("R2 used 张数分布: %s"%dict(sorted(lens.items())), file=sys.stderr)
