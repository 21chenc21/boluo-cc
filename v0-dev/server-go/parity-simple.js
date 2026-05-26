#!/usr/bin/env node
// JS↔Go parity for simpleEval
const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const N = parseInt(process.argv[2] || '500', 10);
const GO_BIN = process.argv[3] || '/tmp/eval-simple';

const ROOT = path.resolve(__dirname, '..');
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'solver.js'), 'utf8'), { filename: 'solver.js' });
const engine = new ExpertRollout(new ExpertEvaluator());

let s = 7777;
const rng = () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function genState(numJokers = 0) {
    const totalCards = 1 + Math.floor(rng() * 13);
    const used = new Set(), cards = [];
    for (let i = 0; i < numJokers && cards.length < totalCards; i++) cards.push('X');
    while (cards.length < totalCards) {
        const cid = RANKS[Math.floor(rng() * 13)] + SUITS[Math.floor(rng() * 4)];
        if (used.has(cid)) continue;
        used.add(cid);
        cards.push(cid);
    }
    const top = [], middle = [], bottom = [];
    for (const c of cards) {
        const r = rng();
        if (r < 0.25 && top.length < 3) top.push(c);
        else if (r < 0.6 && middle.length < 5) middle.push(c);
        else if (bottom.length < 5) bottom.push(c);
        else if (middle.length < 5) middle.push(c);
        else if (top.length < 3) top.push(c);
    }
    return { top, middle, bottom };
}

const states = [];
for (let i = 0; i < N; i++) states.push(genState(i % 6 === 0 ? 1 : (i % 12 === 0 ? 2 : 0)));

const stdin = states.map(s => `${s.top.join(' ')}|${s.middle.join(' ')}|${s.bottom.join(' ')}`).join('\n') + '\n';
const goOut = execFileSync(GO_BIN, [], { input: stdin, encoding: 'utf8' }).trim().split('\n');

let mm = 0, maxDiff = 0;
for (let i = 0; i < N; i++) {
    const top = states[i].top.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const mid = states[i].middle.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const bot = states[i].bottom.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const stateObj = { top, middle: mid, bottom: bot };
    const js = engine.simpleEval(stateObj);
    const go = parseFloat(goOut[i]);
    const diff = Math.abs(js - go);
    if (diff > maxDiff) maxDiff = diff;
    if (diff > 0.01) {
        if (mm < 5) {
            console.log(`✗ state ${i}: t=[${states[i].top.join(' ')}] m=[${states[i].middle.join(' ')}] b=[${states[i].bottom.join(' ')}]`);
            console.log(`  JS=${js.toFixed(4)} GO=${go.toFixed(4)} diff=${diff.toFixed(4)}`);
        }
        mm++;
    }
}
console.log(`\nmax diff: ${maxDiff.toFixed(4)}`);
console.log(`${N - mm}/${N} matched (threshold 0.01)`);
process.exit(mm > 0 ? 1 : 0);
