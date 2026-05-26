#!/usr/bin/env node
// JS↔Go quickRollout 单调用 parity (相同 LCG seed)
const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const N = parseInt(process.argv[2] || '20', 10);
const GO_BIN = process.argv[3] || '/tmp/rollout-bench';
const ROOT = path.resolve(__dirname, '..');

vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'solver.js'), 'utf8'), { filename: 'solver.js' });

function makeLCG(seed) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}

const evaluator = new ExpertEvaluator();
const engine = new ExpertRollout(evaluator);
const toCard = (s) => ({ rank: s[0], suit: s[1] });

// 一个 R3 partial state
function makeCase(idx) {
    let s = 100 + idx * 13;
    const rng = () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
    const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
    const SUITS = ['s','h','d','c'];
    const used = new Set();
    const top=[], mid=[], bot=[];
    const totalAlready = 7 + (idx % 3) * 2; // 7/9/11
    while (top.length + mid.length + bot.length < totalAlready) {
        const c = RANKS[Math.floor(rng()*13)] + SUITS[Math.floor(rng()*4)];
        if (used.has(c)) continue;
        used.add(c);
        if (top.length < 3 && rng() < 0.3) top.push(c);
        else if (bot.length < 5 && rng() < 0.6) bot.push(c);
        else if (mid.length < 5) mid.push(c);
        else if (bot.length < 5) bot.push(c);
        else if (top.length < 3) top.push(c);
        else break;
    }
    const placedRound = Math.ceil((totalAlready - 5) / 2) + 1;
    return { top, mid, bot, used: [...used], round: placedRound };
}

function jsRollout(state, currentRound, seed) {
    const lcg = makeLCG(seed);
    Math.random = () => lcg();
    globalThis.ROLLOUT_CONFIG = { r1Mult: 1.0, r1SimpleBlend: 0.3 };
    const gs = new GameState(0);
    gs.round = state.round;
    for (const c of state.top) gs.placeCard(toCard(c), 'top');
    for (const c of state.mid) gs.placeCard(toCard(c), 'middle');
    for (const c of state.bot) gs.placeCard(toCard(c), 'bottom');
    for (const id of state.used) gs.usedCards.add(id);
    return engine.quickRollout(gs, currentRound);
}

function goRollout(state, currentRound, seed) {
    const req = JSON.stringify({
        state: { top: state.top, middle: state.mid, bottom: state.bot, used: state.used, round: state.round },
        currentRound, seed,
    });
    const out = execFileSync(GO_BIN, [], { input: req + '\n', encoding: 'utf8' }).trim();
    return parseFloat(out);
}

console.log(`[parity-rollout] testing ${N} quickRollout calls`);
let mm = 0;
for (let i = 0; i < N; i++) {
    const c = makeCase(i);
    const seed = 1000 + i;
    const js = jsRollout(c, c.round, seed);
    const go = goRollout(c, c.round, seed);
    const diff = Math.abs(js - go);
    if (diff > 0.01) {
        if (mm < 5) {
            console.log(`✗ #${i} round=${c.round} seed=${seed}`);
            console.log(`  state: top=${c.top.join(',')} mid=${c.mid.join(',')} bot=${c.bot.join(',')}`);
            console.log(`  JS=${js.toFixed(4)} GO=${go.toFixed(4)} diff=${diff.toFixed(4)}`);
        }
        mm++;
    }
}
console.log(`\n${N - mm}/${N} matched`);
process.exit(mm > 0 ? 1 : 0);
