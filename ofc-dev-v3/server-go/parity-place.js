#!/usr/bin/env node
// Go expertPlace5/3 vs JS expertPlace5/3 严格 parity (相同 LCG seed)
//
// 用法: node parity-place.js [n=20]
const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const N = parseInt(process.argv[2] || '20', 10);
const GO_BIN = process.argv[3] || '/tmp/place-bench';
const ROOT = path.resolve(__dirname, '..');

// 加载 JS 实现
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'solver.js'), 'utf8'), { filename: 'solver.js' });

// LCG 与 Go 端一致
function makeLCG(seed) {
    let state = seed >>> 0;
    return () => {
        state = (state * 1664525 + 1013904223) >>> 0;
        return state / 4294967296;
    };
}

// patch Math.random
const _origRandom = Math.random;
let lcg = makeLCG(0);
Math.random = () => lcg();

const evaluator = new ExpertEvaluator();
const engine = new ExpertRollout(evaluator);

const toCard = (s) => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] };
const cardId = (c) => c.rank === 'X' ? 'X' : c.rank + c.suit;

function jsPlace(round, stateData, dealt, seed, level) {
    const cfg = level === 'low' ? { r1Mult:0.25, r1SimpleBlend:0.5 }
              : level === 'medium' ? { r1Mult:0.5, r1SimpleBlend:0.4 }
              : { r1Mult:1.0, r1SimpleBlend:0.3 };
    globalThis.ROLLOUT_CONFIG = cfg;

    const state = new GameState(0);
    state.round = round;
    for (const s of stateData.top) state.placeCard(toCard(s), 'top');
    for (const s of stateData.middle) state.placeCard(toCard(s), 'middle');
    for (const s of stateData.bottom) state.placeCard(toCard(s), 'bottom');
    for (const id of stateData.used) state.usedCards.add(id);

    const dCards = dealt.map(toCard);
    // 重置 LCG seed
    lcg = makeLCG(seed);
    const beforeTop = [...state.top], beforeMid = [...state.middle], beforeBot = [...state.bottom];
    if (round === 1 || dCards.length === 5) engine.expertPlace5(state, dCards);
    else engine.expertPlace3(state, dCards);

    const beforeIds = (arr) => arr.map(cardId);
    const beforeSet = (arr) => { const s = new Map(); for (const id of beforeIds(arr)) s.set(id, (s.get(id)||0)+1); return s; };
    const diff = (b, a) => {
        const seen = beforeSet(b);
        const out = [];
        for (const c of a) {
            const id = cardId(c);
            if (seen.get(id) > 0) seen.set(id, seen.get(id)-1);
            else out.push(c);
        }
        return out.map(cardId);
    };
    const addedTop = diff(beforeTop, state.top);
    const addedMid = diff(beforeMid, state.middle);
    const addedBot = diff(beforeBot, state.bottom);
    const placed = new Set([...addedTop, ...addedMid, ...addedBot]);
    const discards = dealt.filter(s => !placed.has(s));
    return { top: addedTop, middle: addedMid, bottom: addedBot, discards };
}

function goPlace(round, stateData, dealt, seed, level) {
    const req = JSON.stringify({
        round, state: stateData, dealt, seed, level,
    });
    const out = execFileSync(GO_BIN, [], { input: req + '\n', encoding: 'utf8', maxBuffer: 1024*1024 }).trim();
    return JSON.parse(out);
}

// 测试用例: 简单 R1 + R2 场景
function genCase(idx) {
    let s = 100 + idx * 17;
    const rng = () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
    const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
    const SUITS = ['s','h','d','c'];
    const isR1 = (idx % 3) === 0;  // 1/3 R1, 2/3 RN
    const used = new Set();
    const dealt = [];
    while (dealt.length < (isR1 ? 5 : 3)) {
        const c = RANKS[Math.floor(rng()*13)] + SUITS[Math.floor(rng()*4)];
        if (used.has(c)) continue;
        used.add(c);
        dealt.push(c);
    }
    if (isR1) {
        return {
            round: 1,
            state: { top:[], middle:[], bottom:[], used:[] },
            dealt, seed: 1000 + idx, level: ['low','medium','high'][idx % 3],
        };
    }
    // RN: 给 state 已有几张
    const top = [], mid = [], bot = [];
    const totalAlready = 5 + (idx % 4) * 2;  // 5/7/9/11
    while (top.length + mid.length + bot.length < totalAlready) {
        const c = RANKS[Math.floor(rng()*13)] + SUITS[Math.floor(rng()*4)];
        if (used.has(c)) continue;
        used.add(c);
        // 分到三行
        if (top.length < 3 && rng() < 0.3) top.push(c);
        else if (bot.length < 5 && rng() < 0.6) bot.push(c);
        else if (mid.length < 5) mid.push(c);
        else if (bot.length < 5) bot.push(c);
        else if (top.length < 3) top.push(c);
        else break;
    }
    const placedRound = Math.ceil((totalAlready - 5) / 2) + 1;
    return {
        round: placedRound + 1,
        state: { top, middle: mid, bottom: bot, used: [...used] },
        dealt, seed: 1000 + idx, level: ['low','medium','high'][idx % 3],
    };
}

console.log(`[parity-place] testing ${N} placements (Go vs JS, same LCG seed)`);
let mm = 0;
const mismatches = [];
for (let i = 0; i < N; i++) {
    const c = genCase(i);
    const js = jsPlace(c.round, c.state, c.dealt, c.seed, c.level);
    const go = goPlace(c.round, c.state, c.dealt, c.seed, c.level);
    // 把 null 当 [] 处理 (Go 端 nil slice marshals 为 null)
    const norm = (x) => x || [];
    js.top = norm(js.top); js.middle = norm(js.middle); js.bottom = norm(js.bottom); js.discards = norm(js.discards);
    go.top = norm(go.top); go.middle = norm(go.middle); go.bottom = norm(go.bottom); go.discards = norm(go.discards);
    const same = JSON.stringify(js.top.slice().sort()) === JSON.stringify(go.top.slice().sort())
              && JSON.stringify(js.middle.slice().sort()) === JSON.stringify(go.middle.slice().sort())
              && JSON.stringify(js.bottom.slice().sort()) === JSON.stringify(go.bottom.slice().sort())
              && JSON.stringify(js.discards.slice().sort()) === JSON.stringify(go.discards.slice().sort());
    if (!same) {
        mm++;
        if (mismatches.length < 5) mismatches.push({ idx: i, c, js, go });
    } else if (i < 3) {
        console.log(`  ✓ #${i} R${c.round} ${c.level} dealt=${c.dealt.join(' ')}`);
        console.log(`    placement: t=${js.top.join(',')} m=${js.middle.join(',')} b=${js.bottom.join(',')} d=${js.discards.join(',')}`);
    }
}

if (mismatches.length > 0) {
    console.log('\n=== first mismatches ===');
    for (const m of mismatches) {
        console.log(`✗ #${m.idx} R${m.c.round} ${m.c.level} dealt=${m.c.dealt.join(' ')}`);
        console.log(`  state: top=${m.c.state.top.join(',')} mid=${m.c.state.middle.join(',')} bot=${m.c.state.bottom.join(',')}`);
        console.log(`  JS: t=${m.js.top.join(',')} m=${m.js.middle.join(',')} b=${m.js.bottom.join(',')} d=${m.js.discards.join(',')}`);
        console.log(`  GO: t=${m.go.top.join(',')} m=${m.go.middle.join(',')} b=${m.go.bottom.join(',')} d=${m.go.discards.join(',')}`);
    }
}

console.log(`\n[parity-place] ${N - mm}/${N} matched (${mm} mismatch)`);
process.exit(mm > 0 ? 1 : 0);
