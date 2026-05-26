#!/usr/bin/env node
// 并发压测: 模拟 N 个用户同时打牌
// 用法:
//   node load-test.js [concurrency=20] [hands=5] [level_or_r1Mult=0.5]
// 例:
//   node load-test.js 20 1 1.0     # r1Mult=1.0 (旧 high)
//   node load-test.js 50 1 0.5     # r1Mult=0.5 (中档, 高并发)
//   node load-test.js 30 2 medium  # 走预设 medium 档
//
// 每个 user 跑 N 手 5 round 完整流程, 度量延迟分布

const http = require('http');

const CONCURRENCY = parseInt(process.argv[2] || '20', 10);
const HANDS_PER_USER = parseInt(process.argv[3] || '5', 10);
const LEVEL_OR_MULT = process.argv[4] || '0.5';

const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function makeRng(seed) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}

function makeDeck(numJokers) {
    const out = [];
    for (const su of ['c','d','h','s']) {
        for (const r of RANKS) out.push(r + su);
    }
    for (let i = 0; i < numJokers; i++) out.push('X');
    return out;
}

function post(payload) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const t0 = process.hrtime.bigint();
        const req = http.request({
            hostname: 'localhost', port: 8001, path: '/api/solve', method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
            timeout: 90000,
        }, (res) => {
            let b = '';
            res.on('data', c => b += c);
            res.on('end', () => {
                const ms = Number(process.hrtime.bigint() - t0) / 1e6;
                try {
                    resolve({ ms, body: JSON.parse(b) });
                } catch (e) { reject(new Error(b)); }
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

async function runOneHand(userId, handIdx) {
    const seed = userId * 1000 + handIdx;
    const rng = makeRng(seed);
    const numJokers = [0, 0, 2, 4][Math.floor(rng() * 4)];
    const deck = makeDeck(numJokers);
    for (let i = deck.length - 1; i > 0; i--) {
        const j = Math.floor(rng() * (i + 1));
        [deck[i], deck[j]] = [deck[j], deck[i]];
    }
    const state = { top: [], middle: [], bottom: [], usedCards: [`Xnoise_u${userId}_h${handIdx}`] };
    let idx = 0;
    const roundMs = [];
    let totalMs = 0;
    let cacheHits = 0;
    const levelArgs = buildLevelOrMult(LEVEL_OR_MULT);
    for (let r = 1; r <= 5; r++) {
        const n = r === 1 ? 5 : 3;
        const dealt = deck.slice(idx, idx + n);
        idx += n;
        const { ms, body } = await post({
            round: r,
            state: { ...state, round: r },
            dealt,
            discardCount: r === 1 ? 0 : 1,
            ...levelArgs,
        });
        if (body.error) throw new Error(`R${r}: ${body.error}`);
        for (const c of body.layout.top) state.top.push(c);
        for (const c of body.layout.middle) state.middle.push(c);
        for (const c of body.layout.bottom) state.bottom.push(c);
        if (body.discards) for (const d of body.discards) state.usedCards.push(d);
        roundMs.push(ms);
        totalMs += ms;
        if (body.cached) cacheHits++;
    }
    return { roundMs, totalMs, cacheHits, top: state.top, mid: state.middle, bot: state.bottom };
}

function pct(arr, p) {
    if (arr.length === 0) return 0;
    const sorted = [...arr].sort((a, b) => a - b);
    const idx = Math.floor(sorted.length * p);
    return sorted[Math.min(idx, sorted.length - 1)];
}

async function userTask(userId) {
    const results = [];
    for (let h = 0; h < HANDS_PER_USER; h++) {
        try {
            const r = await runOneHand(userId, h);
            results.push(r);
        } catch (e) {
            results.push({ error: e.message });
        }
    }
    return results;
}

(async () => {
    console.log(`load-test: ${CONCURRENCY} users × ${HANDS_PER_USER} hands @ ${LEVEL_OR_MULT}`);
    console.log(`API: http://localhost:8001/api/solve`);
    console.log();

    const t0 = Date.now();
    const tasks = [];
    for (let u = 0; u < CONCURRENCY; u++) {
        tasks.push(userTask(u));
    }
    const allResults = await Promise.all(tasks);
    const totalSec = (Date.now() - t0) / 1000;

    // 收集 stats
    const allRoundMs = [];
    const allHandMs = [];
    let totalRounds = 0, totalHands = 0, totalErr = 0, totalCacheHits = 0;
    for (const userResults of allResults) {
        for (const r of userResults) {
            if (r.error) { totalErr++; continue; }
            allRoundMs.push(...r.roundMs);
            allHandMs.push(r.totalMs);
            totalRounds += r.roundMs.length;
            totalHands++;
            totalCacheHits += r.cacheHits;
        }
    }

    console.log(`=== 结果 ===`);
    console.log(`总耗时:    ${totalSec.toFixed(1)}s`);
    console.log(`完成 hands: ${totalHands}/${CONCURRENCY * HANDS_PER_USER}, ${totalErr} 错误`);
    console.log(`总 rounds:  ${totalRounds}`);
    console.log(`Cache 命中: ${totalCacheHits}/${totalRounds} (${(totalCacheHits/totalRounds*100).toFixed(1)}%)`);
    console.log(`QPS:        ${(totalRounds / totalSec).toFixed(1)} round/s, ${(totalHands / totalSec * 60).toFixed(1)} hand/min`);
    console.log();
    console.log(`单 round 延迟 (ms):`);
    console.log(`  p50=${pct(allRoundMs, 0.5).toFixed(0)}  p95=${pct(allRoundMs, 0.95).toFixed(0)}  p99=${pct(allRoundMs, 0.99).toFixed(0)}  max=${Math.max(...allRoundMs).toFixed(0)}`);
    console.log(`  avg=${(allRoundMs.reduce((s, x) => s + x, 0) / allRoundMs.length).toFixed(0)}`);
    console.log();
    console.log(`一手 (5 round) 总时 (ms):`);
    console.log(`  p50=${pct(allHandMs, 0.5).toFixed(0)}  p95=${pct(allHandMs, 0.95).toFixed(0)}  p99=${pct(allHandMs, 0.99).toFixed(0)}  max=${Math.max(...allHandMs).toFixed(0)}`);
    console.log(`  avg=${(allHandMs.reduce((s, x) => s + x, 0) / allHandMs.length).toFixed(0)}`);
    console.log();

    // 服务器健康检查
    try {
        const health = await new Promise((resolve, reject) => {
            const req = http.request({
                hostname: 'localhost', port: 8001, path: '/api/health', method: 'GET',
            }, (res) => {
                let b = ''; res.on('data', c => b += c);
                res.on('end', () => resolve(JSON.parse(b)));
            });
            req.on('error', reject);
            req.end();
        });
        console.log(`Server cache: ${health.cache.hits} hits / ${health.cache.misses} miss = ${(health.cache.hitRate*100).toFixed(1)}%`);
        console.log(`Go avgElapsedMs: ${health.goSolver.avgElapsedMs}ms (totalSolved=${health.goSolver.totalSolved})`);
    } catch (e) { console.warn('health failed:', e.message); }
})();
