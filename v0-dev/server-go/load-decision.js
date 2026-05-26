#!/usr/bin/env node
// 单决策并发压测: N 用户同时各发 1 次 /api/solve, 看延迟分布
// 用法:
//   node load-decision.js [concurrency=30] [r1Mult=1.0]
// 例:
//   node load-decision.js 30 1.0    # 高档默认 (旧 high)
//   node load-decision.js 30 2.0    # 更强
//   node load-decision.js 50 0.5    # 中档, 高并发
//   node load-decision.js 100 0.25  # 低档, 极高并发
//
// 也支持 level 字符串: 'low' / 'medium' / 'high'

const http = require('http');

const CONCURRENCY = parseInt(process.argv[2] || '30', 10);
const LEVEL_OR_MULT = process.argv[3] || '1.0';

const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function makeRng(seed) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}

function genR1Dealt(seed) {
    const rng = makeRng(seed);
    const used = new Set(), cards = [];
    while (cards.length < 5) {
        const c = RANKS[Math.floor(rng()*13)] + SUITS[Math.floor(rng()*4)];
        if (used.has(c)) continue;
        used.add(c);
        cards.push(c);
    }
    return cards;
}

function post(payload, timeoutMs = 180000) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const t0 = process.hrtime.bigint();
        const req = http.request({
            hostname: 'localhost', port: 8001, path: '/api/solve', method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
            timeout: timeoutMs,
        }, (res) => {
            let b = '';
            res.on('data', c => b += c);
            res.on('end', () => {
                const ms = Number(process.hrtime.bigint() - t0) / 1e6;
                try { resolve({ ms, body: JSON.parse(b) }); }
                catch (e) { reject(new Error(b)); }
            });
        });
        req.on('error', reject);
        req.on('timeout', () => { req.destroy(); reject(new Error('timeout')); });
        req.write(data);
        req.end();
    });
}

function buildLevelOrMult(s) {
    const num = parseFloat(s);
    if (!isNaN(num) && isFinite(num) && num > 0) return { r1Mult: num };
    return { level: s };
}

async function oneDecision(userId) {
    const dealt = genR1Dealt(7777 + userId);
    const levelArgs = buildLevelOrMult(LEVEL_OR_MULT);
    return post({
        round: 1,
        state: { top: [], middle: [], bottom: [], usedCards: [`Xnoise_u${userId}`] },
        dealt,
        discardCount: 0,
        ...levelArgs,
    });
}

function pct(arr, p) {
    if (arr.length === 0) return 0;
    const sorted = [...arr].sort((a, b) => a - b);
    const idx = Math.floor(sorted.length * p);
    return sorted[Math.min(idx, sorted.length - 1)];
}

(async () => {
    console.log(`load-decision: ${CONCURRENCY} 用户同时 1 次 R1 决策 @ ${LEVEL_OR_MULT}`);

    const t0 = Date.now();
    const tasks = [];
    for (let u = 0; u < CONCURRENCY; u++) tasks.push(oneDecision(u).catch(e => ({ error: e.message })));
    const results = await Promise.all(tasks);
    const totalSec = (Date.now() - t0) / 1000;

    const ok = results.filter(r => !r.error);
    const err = results.filter(r => r.error);

    console.log();
    console.log(`总耗时 (墙钟): ${totalSec.toFixed(1)}s`);
    console.log(`成功: ${ok.length}/${CONCURRENCY}, 失败: ${err.length}`);
    if (err.length) console.log(`错误: ${err.slice(0, 3).map(r => r.error).join('; ')}`);

    if (ok.length > 0) {
        const ms = ok.map(r => r.ms);
        console.log();
        console.log(`单次决策延迟 (含排队):`);
        console.log(`  p50 = ${pct(ms, 0.5).toFixed(0)}ms`);
        console.log(`  p95 = ${pct(ms, 0.95).toFixed(0)}ms`);
        console.log(`  p99 = ${pct(ms, 0.99).toFixed(0)}ms`);
        console.log(`  max = ${Math.max(...ms).toFixed(0)}ms`);
        console.log(`  avg = ${(ms.reduce((s,x)=>s+x, 0) / ms.length).toFixed(0)}ms`);
        console.log();
        console.log(`Go 端实际计算耗时 (无排队):`);
        const goMs = ok.map(r => r.body.elapsedMs).filter(x => x);
        if (goMs.length > 0) {
            console.log(`  p50 = ${pct(goMs, 0.5).toFixed(0)}ms`);
            console.log(`  avg = ${(goMs.reduce((s,x)=>s+x, 0) / goMs.length).toFixed(0)}ms`);
        }
        console.log();
        console.log(`吞吐: ${(ok.length / totalSec).toFixed(2)} 决策/秒`);
    }
})();
