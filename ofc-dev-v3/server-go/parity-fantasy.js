#!/usr/bin/env node
// FL Phase 1 parity: JS expertPlaceFantasy vs Go ExpertPlaceFantasy (经 HTTP /solve)
// 注: Phase 1 只覆盖 reFan + 非 reFan 锚直枚举. JS 走 _expertPlaceFantasyImpl beam search 路径
// 时, Go 会返回 502 — 这种 case 我们记 'go-skip' 而不是不一致.
//
// 用法: node parity-fantasy.js [n=50] [goUrl=http://localhost:9000]
//
// 注意: 启动 Go service: cd ../server-go && ./ofc-go &

const fs = require('fs'), path = require('path'), vm = require('vm'), http = require('http');

const N = parseInt(process.argv[2] || '50', 10);
const GO_URL = process.argv[3] || 'http://localhost:9000';

const ROOT = path.resolve(__dirname, '..');
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'solver.js'), 'utf8'), { filename: 'solver.js' });

const evaluator = new ExpertEvaluator();
const engine = new ExpertRollout(evaluator);

function makeLCG(seed) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}

const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['s','h','d','c'];

function genDealt(rng, total) {
    const used = new Set();
    const out = [];
    while (out.length < total) {
        const r = RANKS[Math.floor(rng() * 13)];
        const s = SUITS[Math.floor(rng() * 4)];
        const id = r + s;
        if (used.has(id)) continue;
        used.add(id);
        out.push({ rank: r, suit: s });
    }
    return out;
}

function toCardStr(c) {
    if (!c) return '';
    if (c.rank === 'X' || c.joker) return 'X';
    return c.rank + c.suit;
}

function jsFantasy(dealt, discardCount) {
    const t0 = Date.now();
    const r = engine.expertPlaceFantasy(new GameState(0), dealt, discardCount, 10, 0);
    const t = Date.now() - t0;
    if (!r || !r.layout) return { ok: false, t };
    return {
        ok: true,
        layout: {
            top: r.layout.top.map(toCardStr).sort(),
            middle: r.layout.middle.map(toCardStr).sort(),
            bottom: r.layout.bottom.map(toCardStr).sort(),
        },
        t,
    };
}

function goFantasy(dealt, discardCount) {
    return new Promise((resolve) => {
        const t0 = Date.now();
        const payload = JSON.stringify({
            state: { top: [], middle: [], bottom: [], usedCards: [], round: 99 },
            dealt: dealt.map(toCardStr),
            discardCount,
            mode: 'fantasy',
        });
        const u = new URL(GO_URL + '/solve');
        const req = http.request({
            hostname: u.hostname, port: u.port, path: u.pathname, method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload) },
            timeout: 60000,
        }, (res) => {
            let b = '';
            res.on('data', c => b += c);
            res.on('end', () => {
                const t = Date.now() - t0;
                try {
                    const j = JSON.parse(b);
                    if (res.statusCode === 502 || !j.ok) {
                        resolve({ ok: false, status: res.statusCode, error: j.error, t });
                        return;
                    }
                    resolve({
                        ok: true,
                        layout: {
                            top: (j.layout.top || []).slice().sort(),
                            middle: (j.layout.middle || []).slice().sort(),
                            bottom: (j.layout.bottom || []).slice().sort(),
                        },
                        t,
                    });
                } catch (e) {
                    resolve({ ok: false, error: 'parse: ' + b.slice(0, 100), t });
                }
            });
        });
        req.on('error', e => resolve({ ok: false, error: e.message, t: Date.now() - t0 }));
        req.write(payload);
        req.end();
    });
}

function eqLayout(a, b) {
    if (!a || !b) return false;
    const ja = JSON.stringify(a), jb = JSON.stringify(b);
    return ja === jb;
}

function evalRoyalty(layout, src) {
    const top = layout.top.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const mid = layout.middle.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const bot = layout.bottom.map(s => s === 'X' ? { rank:'X', suit:'j', joker:true, jid:0 } : { rank:s[0], suit:s[1] });
    const sc = scoreHand(top, mid, bot);
    return { royalty: sc.royalties, foul: sc.foul };
}

(async () => {
    console.log(`FL Phase 1 parity: ${N} hands  Go=${GO_URL}`);
    console.log();

    const rng = makeLCG(123);
    let exact = 0, royaltyOk = 0, goSkipped = 0, jsFail = 0, mismatch = 0;
    const mismatchSamples = [];

    for (let i = 0; i < N; i++) {
        const discardCount = (i % 4) + 1;  // 1,2,3,4
        const dealt = genDealt(rng, 13 + discardCount);

        const j = jsFantasy(dealt, discardCount);
        if (!j.ok) { jsFail++; continue; }

        const g = await goFantasy(dealt, discardCount);
        if (!g.ok) {
            goSkipped++;
            continue;
        }

        if (eqLayout(j.layout, g.layout)) {
            exact++;
        } else {
            // 不完全一致, 但比 royalty
            const jR = evalRoyalty(j.layout);
            const gR = evalRoyalty(g.layout);
            if (jR.royalty === gR.royalty && !jR.foul && !gR.foul) {
                royaltyOk++;
            } else {
                mismatch++;
                if (mismatchSamples.length < 5) {
                    mismatchSamples.push({
                        i, discardCount,
                        dealt: dealt.map(toCardStr),
                        js: j.layout, jsR: jR.royalty,
                        go: g.layout, goR: gR.royalty,
                    });
                }
            }
        }

        if ((i + 1) % 10 === 0) {
            process.stdout.write(`.`);
        }
    }
    console.log();
    console.log();
    console.log(`=== 结果 ===`);
    console.log(`完全一致 (布局):  ${exact}/${N}`);
    console.log(`royalty 一致:     ${royaltyOk}/${N}`);
    console.log(`go-skip (502):    ${goSkipped}/${N}  ← Phase 1 没覆盖, JS 走 beam search`);
    console.log(`JS 失败:          ${jsFail}/${N}`);
    console.log(`真实 mismatch:    ${mismatch}/${N}`);
    if (mismatchSamples.length > 0) {
        console.log();
        console.log(`=== mismatch 示例 ===`);
        for (const s of mismatchSamples) {
            console.log(`\n#${s.i} discard=${s.discardCount}  dealt=${s.dealt.join(',')}`);
            console.log(`  JS  R=${s.jsR}  T=${s.js.top.join(',')}  M=${s.js.middle.join(',')}  B=${s.js.bottom.join(',')}`);
            console.log(`  Go  R=${s.goR}  T=${s.go.top.join(',')}  M=${s.go.middle.join(',')}  B=${s.go.bottom.join(',')}`);
        }
    }
    const cover = (exact + royaltyOk) / (N - goSkipped - jsFail);
    console.log();
    console.log(`Phase 1 覆盖率: ${(N - goSkipped - jsFail)}/${N} = ${((N - goSkipped - jsFail) / N * 100).toFixed(1)}%`);
    console.log(`Go 的解 royalty 与 JS 一致率: ${(cover * 100).toFixed(1)}%`);
})();
