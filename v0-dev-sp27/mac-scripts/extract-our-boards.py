import json, sys, collections
# 我方bot种子: my_rounds 精确 dealt+discards, seats[] 重建对手可见牌. 不碰 usedCards 字段.
out=[]; seen=set(); stats=collections.Counter()
for line in open("ofc_hands.jsonl"):
    try: d=json.loads(line)
    except: continue
    mr=d.get("my_rounds") or []
    if len(mr)!=5: stats['非5轮']+=1; continue
    seats=d.get("seats") or []
    others=[]
    for s in seats:
        if s.get("is_me"): continue
        rs=s.get("rounds") or []
        if len(rs)==5: others.append(rs)
    top,mid,bot=[],[],[]; my_dead=[]   # 已见的己方牌(摆+弃)
    ok=True
    for ri,r in enumerate(mr):
        rnd=ri+1
        dealt=r.get("dealt") or []
        if (rnd==1 and len(dealt)!=5) or (rnd>1 and len(dealt)!=3): ok=False; break
        used=list(my_dead)
        for rs in others:
            for prev in rs[:ri]:
                used += (prev.get("top") or [])+(prev.get("mid") or [])+(prev.get("bot") or [])
        allc=used+dealt   # used 已含己方摆+弃(my_dead), 别再加 rows
        # 物理校验: 全局无重复 (鬼牌X可有2张)
        cnt=collections.Counter(allc)
        if any(v>1 for c,v in cnt.items() if c not in ("X","Xj0","Xj1")) or cnt.get("X",0)>2:
            ok=False; break
        entry={"round":rnd,"top":list(top),"middle":list(mid),"bottom":list(bot),
               "dealt":dealt,"used":used}
        k=json.dumps(entry,sort_keys=True)
        if k not in seen: seen.add(k); out.append(entry)
        top+=r.get("top") or []; mid+=r.get("mid") or []; bot+=r.get("bot") or []
        my_dead += (r.get("top") or [])+(r.get("mid") or [])+(r.get("bot") or [])+(r.get("discards") or [])
    if not ok: stats['物理校验失败']+=1
    else: stats['手数ok']+=1

json.dump(out, sys.stdout, ensure_ascii=False)
c=collections.Counter(e["round"] for e in out)
print("\n%s"%dict(stats), file=sys.stderr)
print("我方种子态: %d 条, 轮次 %s"%(len(out),dict(sorted(c.items()))), file=sys.stderr)
