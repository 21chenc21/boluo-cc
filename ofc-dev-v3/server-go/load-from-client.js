#!/usr/bin/env node
// 客户端并发压测脚本 — 在你 Mac 上跑, 远程测 API
// 用法:
//   node load-from-client.js <url> [concurrency=10] [level_or_r1Mult=1.0]
// 例 (数值优先, 直接传 r1Mult):
//   node load-from-client.js http://10.148.0.7:8001 30 1.0    # 旧 high 等价
//   node load-from-client.js http://10.148.0.7:8001 50 0.5    # 中档高并发
//   node load-from-client.js http://10.148.0.7:8001 100 0.25  # 低档极并发
//   node load-from-client.js http://10.148.0.7:8001 5 3.0     # 极强少并发
// 也支持 level 字符串: 'low' / 'medium' / 'high'

const http = require('http');
const https = require('https');
const { URL } = require('url');

const URL_STR = process.argv[2];
const CONCURRENCY = parseInt(process.argv[3] || '10', 10);
const LEVEL_OR_MULT = process.argv[4] || '1.0';

if (!URL_STR) {
    console.error('用法: node load-from-client.js <url> [concurrency=10] [r1Mult=1.0]');
    console.error('  r1Mult 可以是: 0.25 0.5 0.7 1.0 1.5 2.0 3.0 (任意正数)');
    console.error('              或: low / medium / high (用预设档位)');
    console.error('示例: node load-from-client.js http://10.148.0.7:8001 30 1.0');
    process.exit(1);
}

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

function post(path, payload, timeoutMs = 300000) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const t0 = process.hrtime.bigint();
        let connectMs = 0, firstByteMs = 0;
        const req = lib.request({
            hostname: u.hostname, port: u.port || (isHttps ? 443 : 80),
            path, method: 'POST',
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

function buildLevelOrMult(s) {
    const num = parseFloat(s);
    if (!isNaN(num) && isFinite(num) && num > 0) return { r1Mult: num };
    return { level: s };
}

async function oneDecision(userId) {
    const levelArgs = buildLevelOrMult(LEVEL_OR_MULT);
    return post('/api/solve', {
        round: 1,
        state: { top: [], middle: [], bottom: [], usedCards: [`Xclient_${Date.now()}_u${userId}`] },
        dealt: genR1(7777 + userId * 31),
        discardCount: 0,
        ...levelArgs,
    });
}

function pct(arr, p) {
    if (arr.length === 0) return 0;
    const sorted = [...arr].sort((a, b) => a - b);
    return sorted[Math.min(Math.floor(sorted.length * p), sorted.length - 1)];
}

(async () => {
    console.log(`load-from-client: ${URL_STR}`);
    console.log(`  ${CONCURRENCY} 并发用户, 每人 1 次 R1 决策, 配置=${LEVEL_OR_MULT}`);
    console.log();

    // 先测一次 health 看连通性
    const health = await post('/api/health', {}).catch(e => null);
    if (!health) {
        // 重试 GET
        await new Promise((res, rej) => {
            const req = lib.request({
                hostname: u.hostname, port: u.port || (isHttps ? 443 : 80),
                path: '/api/health', method: 'GET',
            }, (r) => {
                let b = ''; r.on('data', c => b+=c);
                r.on('end', () => { try{res(JSON.parse(b))}catch{res(null)} });
            });
            req.on('error', rej); req.end();
        }).then(h => h && console.log(`✓ 服务在线: cache size=${h.cache?.size}, hitRate=${h.cache?.hitRate}`)).catch(() => {});
    }

    // warm up: 1 个请求测网络往返
    const warm = await oneDecision(99999).catch(e => ({ error: e.message }));
    if (warm.error) {
        console.error('warmup 失败:', warm.error);
        process.exit(1);
    }
    console.log(`Warmup R1: 网络连接=${warm.connectMs.toFixed(0)}ms, 首字节=${warm.firstByteMs.toFixed(0)}ms, 总=${warm.totalMs.toFixed(0)}ms`);
    console.log(`         Go 计算 elapsedMs=${warm.body.elapsedMs}ms (cache: ${warm.body.cached})`);
    console.log();

    // 并发测试
    console.log(`=== 开始 ${CONCURRENCY} 并发 ===`);
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
    if (err.length) console.log(`错误样本: ${err.slice(0, 3).map(r => r.error).join(' | ')}`);

    if (ok.length > 0) {
        const totals = ok.map(r => r.totalMs);
        const goMs = ok.map(r => r.body.elapsedMs).filter(x => x > 0);
        const conn = ok.map(r => r.connectMs).filter(x => x > 0);

        console.log();
        console.log(`端到端延迟 (含网络 + Node + Go + 排队):`);
        console.log(`  p50 = ${pct(totals, 0.5).toFixed(0)}ms`);
        console.log(`  p95 = ${pct(totals, 0.95).toFixed(0)}ms`);
        console.log(`  p99 = ${pct(totals, 0.99).toFixed(0)}ms`);
        console.log(`  max = ${Math.max(...totals).toFixed(0)}ms`);
        console.log(`  avg = ${(totals.reduce((s,x)=>s+x,0)/totals.length).toFixed(0)}ms`);

        if (goMs.length > 0) {
            console.log();
            console.log(`Go 端纯计算耗时 (无网络):`);
            console.log(`  p50 = ${pct(goMs, 0.5).toFixed(0)}ms`);
            console.log(`  avg = ${(goMs.reduce((s,x)=>s+x,0)/goMs.length).toFixed(0)}ms`);
            console.log();
            console.log(`网络 + Node 开销 (端到端 - Go): avg ${(totals.reduce((s,x)=>s+x,0)/totals.length - goMs.reduce((s,x)=>s+x,0)/goMs.length).toFixed(0)}ms`);
        }
        if (conn.length > 0) {
            console.log(`TCP 连接时间: avg ${(conn.reduce((s,x)=>s+x,0)/conn.length).toFixed(0)}ms`);
        }

        console.log();
        console.log(`吞吐: ${(ok.length / wallSec).toFixed(2)} 决策/秒`);
    }
})();
