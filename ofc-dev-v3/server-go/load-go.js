#!/usr/bin/env node
// 直接打 Go service (:9000) — 跳过 Node + Cache, 纯测 Go 性能
// 用法:
//   node load-go.js [concurrency=30] [r1Mult=1.0] [url=http://localhost:9000]
// 例:
//   node load-go.js 30 1.0
//   node load-go.js 50 0.5 http://10.148.0.7:9000
//   node load-go.js 100 0.25

const http = require('http');
const https = require('https');
const { URL } = require('url');

const CONCURRENCY = parseInt(process.argv[2] || '30', 10);
const R1_MULT = parseFloat(process.argv[3] || '1.0');
const URL_STR = process.argv[4] || 'http://localhost:9000';

const u = new URL(URL_STR);
const isHttps = u.protocol === 'https:';
const lib = isHttps ? https : http;

const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function makeRng(seed) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}
function genR1(seed) {
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

function postSolve(payload, timeoutMs = 600000) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const t0 = process.hrtime.bigint();
        let connectMs = 0, firstByteMs = 0;
        const req = lib.request({
            hostname: u.hostname, port: u.port || (isHttps ? 443 : 80),
            path: '/solve',                          // Go 端点 /solve, 不是 /api/solve
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
            timeout: timeoutMs,
        }, (res) => {
            let b = '';
            res.on('data', c => {
                if (firstByteMs === 0) firstByteMs = Number(process.hrtime.bigint() - t0) / 1e6;
                b += c;
            });
            res.on('end', () => {
                const totalMs = Number(process.hrtime.bigint() - t0) / 1e6;
                try { resolve({ totalMs, firstByteMs, connectMs, body: JSON.parse(b), status: res.statusCode }); }
                catch (e) { reject(new Error('bad json: ' + b.slice(0, 200))); }
            });
        });
        req.on('socket', (sock) => {
            sock.on('connect', () => { connectMs = Number(process.hrtime.bigint() - t0) / 1e6; });
        });
        req.on('error', reject);
        req.on('timeout', () => { req.destroy(); reject(new Error('timeout')); });
        req.write(data);
        req.end();
    });
}

async function oneDecision(userId) {
    return postSolve({
        state: {
            top: [], middle: [], bottom: [],
            usedCards: [`Xclient_${Date.now()}_u${userId}`],  // noise 防服务端任何 cache (Go 无 cache 也无所谓)
            round: 1,
        },
        dealt: genR1(7777 + userId * 31),
        discardCount: 0,
        mode: 'normal',
        jokerCount: 0,
        rolloutConfig: {
            r1Mult: R1_MULT,
            r1SimpleBlend: R1_MULT >= 1.5 ? 0.3 : R1_MULT >= 0.8 ? 0.3 : 0.4,
        },
    });
}

function pct(arr, p) {
    if (arr.length === 0) return 0;
    const sorted = [...arr].sort((a, b) => a - b);
    return sorted[Math.min(Math.floor(sorted.length * p), sorted.length - 1)];
}

(async () => {
    console.log(`load-go: ${URL_STR}/solve  ${CONCURRENCY} 并发 r1Mult=${R1_MULT}`);
    console.log();

    // 健康检查
    await new Promise((res, rej) => {
        const req = lib.request({
            hostname: u.hostname, port: u.port || (isHttps ? 443 : 80),
            path: '/health', method: 'GET',
        }, (r) => {
            let b = ''; r.on('data', c => b+=c);
            r.on('end', () => { try{res(JSON.parse(b))}catch{res(null)} });
        });
        req.on('error', rej); req.end();
    }).then(h => h && console.log(`✓ Go service: totalSolved=${h.totalSolved}, avg=${h.avgElapsedMs}ms`)).catch(e => {
        console.error('Go service 不通:', e.message);
        process.exit(1);
    });

    // warmup 1 个看连通
    const warm = await oneDecision(99999).catch(e => ({ error: e.message }));
    if (warm.error) {
        console.error('warmup 失败:', warm.error);
        process.exit(1);
    }
    console.log(`Warmup R1: 连接=${warm.connectMs.toFixed(0)}ms, 首字节=${warm.firstByteMs.toFixed(0)}ms, 总=${warm.totalMs.toFixed(0)}ms (Go elapsedMs=${warm.body.elapsedMs})`);
    console.log();

    // 并发
    console.log(`=== ${CONCURRENCY} 并发 ===`);
    const t0 = Date.now();
    const tasks = [];
    for (let u_ = 0; u_ < CONCURRENCY; u_++) {
        tasks.push(oneDecision(u_).catch(e => ({ error: e.message })));
    }
    const results = await Promise.all(tasks);
    const wallSec = (Date.now() - t0) / 1000;

    const ok = results.filter(r => !r.error);
    const err = results.filter(r => r.error);

    console.log();
    console.log(`总墙钟: ${wallSec.toFixed(1)}s`);
    console.log(`成功: ${ok.length}/${CONCURRENCY}, 失败: ${err.length}`);
    if (err.length) console.log(`错误: ${err.slice(0, 3).map(r => r.error).join(' | ')}`);

    if (ok.length > 0) {
        const totals = ok.map(r => r.totalMs);
        const goMs = ok.map(r => r.body.elapsedMs).filter(x => x > 0);
        console.log();
        console.log(`端到端 (含网络):`);
        console.log(`  p50 = ${pct(totals, 0.5).toFixed(0)}ms`);
        console.log(`  p95 = ${pct(totals, 0.95).toFixed(0)}ms`);
        console.log(`  p99 = ${pct(totals, 0.99).toFixed(0)}ms`);
        console.log(`  max = ${Math.max(...totals).toFixed(0)}ms`);
        console.log(`  avg = ${(totals.reduce((s,x)=>s+x,0)/totals.length).toFixed(0)}ms`);
        if (goMs.length > 0) {
            console.log();
            console.log(`Go 计算:`);
            console.log(`  p50 = ${pct(goMs, 0.5).toFixed(0)}ms  avg = ${(goMs.reduce((s,x)=>s+x,0)/goMs.length).toFixed(0)}ms`);
            console.log(`网络开销 (端到端 - Go): avg ${(totals.reduce((s,x)=>s+x,0)/totals.length - goMs.reduce((s,x)=>s+x,0)/goMs.length).toFixed(0)}ms`);
        }
        console.log();
        console.log(`吞吐: ${(ok.length / wallSec).toFixed(2)} 决策/秒`);
    }
})();
