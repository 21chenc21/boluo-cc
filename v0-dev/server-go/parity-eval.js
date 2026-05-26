#!/usr/bin/env node
// JS↔Go parity: 跑 N 个随机 5-card / 3-card hands, 比较 evaluate3/5 输出
// 用法: node parity-eval.js [n=2000]

const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const N = parseInt(process.argv[2] || '2000', 10);
const GO_BIN = process.argv[3] || '/tmp/eval-bench';

// 加载 JS evaluate3/5
const ROOT = path.resolve(__dirname, '..');
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });

// 生成 N 个手牌
let s = 12345;
const rng = () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function genHand(n, jokers = 0) {
    const used = new Set(), cards = [];
    for (let i = 0; i < jokers; i++) cards.push('X');
    while (cards.length < n) {
        const cid = RANKS[Math.floor(rng() * 13)] + SUITS[Math.floor(rng() * 4)];
        if (used.has(cid)) continue;
        used.add(cid);
        cards.push(cid);
    }
    return cards;
}

const hands5 = [], hands3 = [];
for (let i = 0; i < N; i++) hands5.push(genHand(5));
for (let i = 0; i < N; i++) hands3.push(genHand(3));
// 加 joker hands
const hands5j1 = [], hands5j2 = [], hands5j3 = [], hands5j4 = [];
const hands3j1 = [], hands3j2 = [], hands3j3 = [];
const NJ = Math.max(50, Math.floor(N / 4));
for (let i = 0; i < NJ; i++) hands5j1.push(genHand(5, 1));
for (let i = 0; i < NJ; i++) hands5j2.push(genHand(5, 2));
for (let i = 0; i < NJ; i++) hands5j3.push(genHand(5, 3));
for (let i = 0; i < NJ; i++) hands5j4.push(genHand(5, 4));
for (let i = 0; i < NJ; i++) hands3j1.push(genHand(3, 1));
for (let i = 0; i < NJ; i++) hands3j2.push(genHand(3, 2));
for (let i = 0; i < NJ; i++) hands3j3.push(genHand(3, 3));

// 拼所有 hand 顺序: 5card / 3card / 5j1 / 5j2 / 5j3 / 5j4 / 3j1 / 3j2 / 3j3
const allGroups = [
    { tag: '5-card', hands: hands5, fn: evaluate5 },
    { tag: '3-card', hands: hands3, fn: evaluate3 },
    { tag: '5-card+1j', hands: hands5j1, fn: evaluate5Joker },
    { tag: '5-card+2j', hands: hands5j2, fn: evaluate5Joker },
    { tag: '5-card+3j', hands: hands5j3, fn: evaluate5Joker },
    { tag: '5-card+4j', hands: hands5j4, fn: evaluate5Joker },
    { tag: '3-card+1j', hands: hands3j1, fn: evaluate3Joker },
    { tag: '3-card+2j', hands: hands3j2, fn: evaluate3Joker },
    { tag: '3-card+3j', hands: hands3j3, fn: evaluate3Joker },
];
const allHands = allGroups.flatMap(g => g.hands);
const stdin = allHands.map(h => h.join(' ')).join('\n') + '\n';
const goOut = execFileSync(GO_BIN, [], { input: stdin, encoding: 'utf8' }).trim().split('\n');

console.log(`[parity] testing ${allHands.length} hands across ${allGroups.length} groups`);

let totalMm = 0;
let cursor = 0;
for (const g of allGroups) {
    let mm = 0;
    for (let i = 0; i < g.hands.length; i++) {
        const cards = g.hands[i].map(s => s === 'X' ? { rank: 'X', suit: 'X', joker: true } : { rank: s[0], suit: s[1] });
        const js = g.fn(cards);
        const [t, v] = goOut[cursor + i].split(':').map(Number);
        if (js.type !== t || js.value !== v) {
            if (mm < 3) {
                console.log(`  ✗ ${g.tag} ${g.hands[i].join(' ')}`);
                console.log(`    JS: type=${js.type} value=${js.value}`);
                console.log(`    GO: type=${t} value=${v}`);
            }
            mm++;
        }
    }
    cursor += g.hands.length;
    const status = mm === 0 ? '✓' : '✗';
    console.log(`${status} ${g.tag.padEnd(12)} ${g.hands.length - mm}/${g.hands.length} matched`);
    totalMm += mm;
}
console.log(`\n[parity] total: ${allHands.length - totalMm}/${allHands.length} (${totalMm} mismatch)`);
process.exit(totalMm > 0 ? 1 : 0);
