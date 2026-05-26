#!/usr/bin/env node
// case-test.js — 测 cases.json (而不是 全 63 testcase). 用于 case-train 后 quick 回测.
//
// 用法:
//   node case-test.js cases/hard.json [http://localhost:18001]
//   ./case-test.sh ckpts/X.json cases/hard.json
//
// 检查 AI 摆法是否匹配任一 expected (multi-solution 时 match 任一即 pass).
// 输出: 每 case pass/fail + 总分

const fs = require('fs');
const http = require('http');

const casesPath = process.argv[2];
const URL_STR = process.argv[3] || 'http://localhost:18001';

if (!casesPath) {
    console.error('usage: node case-test.js <cases.json> [server-url]');
    process.exit(1);
}

const u = new URL(URL_STR);
const cases = JSON.parse(fs.readFileSync(casesPath, 'utf8'));

let passed = 0, failed = 0;

function postSolve(payload) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const req = http.request({
            hostname: u.hostname, port: u.port || 80, path: '/api/solve', method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
            timeout: 30000,
        }, (res) => {
            let b = '';
            res.on('data', c => b += c);
            res.on('end', () => {
                try { resolve(JSON.parse(b)); } catch { reject(new Error('bad json: ' + b.slice(0, 100))); }
            });
        });
        req.on('error', reject);
        req.on('timeout', () => { req.destroy(); reject(new Error('timeout')); });
        req.write(data); req.end();
    });
}

// 比较两 layout 是否一致 (top/middle/bottom 各 set 相等)
function layoutMatch(actual, expected) {
    const sortJoin = arr => [...(arr || [])].sort().join(',');
    return sortJoin(actual.top) === sortJoin(expected.top)
        && sortJoin(actual.middle) === sortJoin(expected.middle)
        && sortJoin(actual.bottom) === sortJoin(expected.bottom);
}

function fmtLayout(l) {
    const f = arr => (arr || []).join(' ') || '∅';
    return `头[${f(l.top)}] 中[${f(l.middle)}] 底[${f(l.bottom)}]`;
}

(async () => {
    console.log(`=== 测 ${cases.length} 个 case (server: ${URL_STR}) ===\n`);
    for (const c of cases) {
        const round = c.round || 1;
        const dealt = c.dealt;
        const state = {
            top: c.state.top || [],
            middle: c.state.middle || [],
            bottom: c.state.bottom || [],
            usedCards: c.state.usedCards || [],
        };
        // discardCount: R1=0, R2-R5=1
        const discardCount = round === 1 ? 0 : 1;

        let r;
        try {
            r = await postSolve({
                round, state, dealt, discardCount, level: 'high',
            });
        } catch (e) {
            console.log(`✗ [${c.name}] API 错误: ${e.message}`);
            failed++;
            continue;
        }
        if (!r.layout) {
            console.log(`✗ [${c.name}] 无 layout: ${r.error || JSON.stringify(r).slice(0,100)}`);
            failed++;
            continue;
        }

        // 解析 layout 卡牌 (供 check predicate 用)
        const parseCard = s => {
            if (s === 'X' || s.startsWith('Xj')) {
                return { rank: 'X', suit: 'j', joker: true, jid: s === 'X' ? 0 : parseInt(s.slice(2), 10) };
            }
            return { rank: s[0], suit: s[1] };
        };
        const fullTop = round === 1 ? (r.layout.top||[]) : [...state.top, ...(r.layout.top||[])];
        const fullMid = round === 1 ? (r.layout.middle||[]) : [...state.middle, ...(r.layout.middle||[])];
        const fullBot = round === 1 ? (r.layout.bottom||[]) : [...state.bottom, ...(r.layout.bottom||[])];
        const top = fullTop.map(parseCard);
        const middle = fullMid.map(parseCard);
        const bottom = fullBot.map(parseCard);
        const placedSet = new Set([...(r.layout.top||[]), ...(r.layout.middle||[]), ...(r.layout.bottom||[])]);
        const discRaw = dealt.find(s => !placedSet.has(s));
        const discarded = discRaw ? parseCard(discRaw) : null;

        const cntJoker = cs => cs.filter(c => c.rank === 'X' || c.joker).length;
        const cntRank = (cs, rk) => cs.filter(c => c.rank === rk).length;
        const cntSuit = (cs, st) => cs.filter(c => c.suit === st).length;
        const hasCard = (cs, rk, st) => cs.some(c => c.rank === rk && c.suit === st);

        let ok = false;
        let allExp = [];

        if (c.check) {
            // predicate 模式
            try {
                ok = (new Function('top', 'middle', 'bottom', 'discarded',
                    'cntJoker', 'cntRank', 'cntSuit', 'hasCard',
                    `return ${c.check}`))(top, middle, bottom, discarded, cntJoker, cntRank, cntSuit, hasCard);
            } catch (e) {
                console.log(`✗ [${c.name}] check eval 错: ${e.message}`);
                failed++;
                continue;
            }
        } else {
            if (c.expected) allExp.push(c.expected);
            if (c.expecteds) allExp.push(...c.expecteds);
            if (allExp.length === 0) {
                console.log(`✗ [${c.name}] 无 expected/expecteds/check`);
                failed++;
                continue;
            }
            ok = allExp.some(exp => layoutMatch(r.layout, exp));
        }

        console.log(`${ok ? '✓' : '✗'} [${c.name}]`);
        console.log(`  发: ${dealt.join(' ')}`);
        if (state.top.length || state.middle.length || state.bottom.length) {
            console.log(`  state: 头[${(state.top||[]).join(' ')||'∅'}] 中[${(state.middle||[]).join(' ')||'∅'}] 底[${(state.bottom||[]).join(' ')||'∅'}]`);
        }
        // R1 直接显示 layout, RN 显示 state+AI 拼接的 full layout + 弃牌
        const aiFullLayout = round === 1 ? r.layout : { top: fullTop, middle: fullMid, bottom: fullBot };
        console.log(`  AI: ${fmtLayout(aiFullLayout)}${discarded ? ' 弃 ' + (discarded.rank === 'X' ? 'X' : discarded.rank + discarded.suit) : ''}`);
        if (!ok) {
            if (c.check) {
                console.log(`  check: ${c.check}`);
            } else {
                allExp.forEach((exp, i) => {
                    console.log(`  exp${i+1}: ${fmtLayout(exp)}`);
                });
                if (c.wrongs) {
                    const wrongIdx = c.wrongs.findIndex(w => layoutMatch(r.layout, w));
                    if (wrongIdx >= 0) {
                        console.log(`  ⚠ AI 摆法 = 已知 wrong[${wrongIdx}]`);
                    }
                }
            }
        }
        if (ok) passed++; else failed++;
    }
    console.log(`\n=== 结果: ${passed}通过 / ${failed}失败 / ${cases.length}总计 ===`);
    process.exit(failed > 0 ? 1 : 0);
})();
