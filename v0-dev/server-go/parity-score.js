#!/usr/bin/env node
// JS↔Go parity for scoreHand: 跑 N 个随机完整 board (3+5+5), 比较输出
// 用法: node parity-score.js [n=1000]
const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const N = parseInt(process.argv[2] || '1000', 10);
const GO_BIN = process.argv[3] || '/tmp/score-bench';

const ROOT = path.resolve(__dirname, '..');
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });

let s = 5678;
const rng = () => { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; return s / 4294967296; };
const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function genBoard(numJokers = 0) {
    const used = new Set(), cards = [];
    for (let i = 0; i < numJokers; i++) cards.push('X');
    while (cards.length < 13) {
        const cid = RANKS[Math.floor(rng() * 13)] + SUITS[Math.floor(rng() * 4)];
        if (used.has(cid)) continue;
        used.add(cid);
        cards.push(cid);
    }
    // 随机分 3+5+5 (不强制非 foul, parity 测试要覆盖 foul 局面)
    return {
        top: cards.slice(0, 3),
        mid: cards.slice(3, 8),
        bot: cards.slice(8, 13),
    };
}

const groups = [
    { tag: '0j', n: N, j: 0 },
    { tag: '2j', n: Math.max(50, Math.floor(N / 4)), j: 2 },
    { tag: '4j', n: Math.max(50, Math.floor(N / 4)), j: 4 },
];

const allBoards = [];
for (const g of groups) {
    for (let i = 0; i < g.n; i++) allBoards.push({ ...genBoard(g.j), grp: g.tag });
}

const stdin = allBoards.map(b => `${b.top.join(' ')}|${b.mid.join(' ')}|${b.bot.join(' ')}`).join('\n') + '\n';
const goOut = execFileSync(GO_BIN, [], { input: stdin, encoding: 'utf8' }).trim().split('\n');

let cursor = 0, totalMm = 0;
for (const g of groups) {
    let mm = 0;
    for (let i = 0; i < g.n; i++) {
        const b = allBoards[cursor + i];
        const top = b.top.map(s => s === 'X' ? { rank:'X', suit:'X', joker:true } : { rank:s[0], suit:s[1] });
        const mid = b.mid.map(s => s === 'X' ? { rank:'X', suit:'X', joker:true } : { rank:s[0], suit:s[1] });
        const bot = b.bot.map(s => s === 'X' ? { rank:'X', suit:'X', joker:true } : { rank:s[0], suit:s[1] });
        const js = scoreHand(top, mid, bot);
        const [foulG, scoreG, royG, fanG] = goOut[cursor + i].split(':').map(Number);

        const jsFoul = js.foul ? 1 : 0;
        const jsFan = js.fantasy ? 1 : 0;

        // foul 优先比对; foul 时 score=-20 双方都对
        if (jsFoul !== foulG) {
            if (mm < 3) {
                console.log(`✗ ${g.tag} foul mismatch: JS=${jsFoul}(score=${js.score}) GO=${foulG}(score=${scoreG})`);
                console.log(`  top: ${b.top.join(' ')}`);
                console.log(`  mid: ${b.mid.join(' ')}`);
                console.log(`  bot: ${b.bot.join(' ')}`);
            }
            mm++; continue;
        }
        // 不 foul 时, royalty 必须对得上
        if (!jsFoul && (js.royalties !== royG || jsFan !== fanG)) {
            if (mm < 3) {
                console.log(`✗ ${g.tag} royalty/fan mismatch: JS=R${js.royalties}/F${jsFan} GO=R${royG}/F${fanG}`);
                console.log(`  top: ${b.top.join(' ')}`);
                console.log(`  mid: ${b.mid.join(' ')}`);
                console.log(`  bot: ${b.bot.join(' ')}`);
            }
            mm++;
        }
    }
    cursor += g.n;
    const status = mm === 0 ? '✓' : '✗';
    console.log(`${status} ${g.tag.padEnd(4)} ${g.n - mm}/${g.n} matched`);
    totalMm += mm;
}
console.log(`\n[parity] total: ${allBoards.length - totalMm}/${allBoards.length} (${totalMm} mismatch)`);
process.exit(totalMm > 0 ? 1 : 0);
