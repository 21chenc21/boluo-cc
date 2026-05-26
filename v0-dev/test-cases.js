#!/usr/bin/env node
// test-cases.js — 统一 case-test runner
//
// 支持:
//   - 单 json / 多 json 文件
//   - --runs N 每文件跑 N 次, 末尾给 per-run + median 汇总
//   - 实时流式输出 (每 case 立刻 print, 不缓存)
//   - 默认 cases/all-tests-expanded.json (63 case suite)
//
// 输出:
//   - 单 json + runs=1: 每 case 详细 (R1: AI+exps, RN: initial+AI+exps)
//   - 单 json + runs>1: 每 run 详细, 末尾 per-run 表
//   - 多 json: 每文件 section, 末尾 per-file 表
//
// 用法:
//   node test-cases.js                                # 默认 all-tests-expanded.json
//   node test-cases.js cases/hard.json
//   node test-cases.js cases/all-tests.json cases/hard.json
//   node test-cases.js --url=http://localhost:18001 --runs=5 cases/all-tests-expanded.json
//   node test-cases.js --runs=3 cases/a.json cases/b.json

const fs = require('fs');
const http = require('http');
const path = require('path');

// === CLI 解析 ===
let URL_STR = 'http://localhost:8002';
let RUNS = 1;
const files = [];
for (const arg of process.argv.slice(2)) {
    if (arg.startsWith('--url=')) URL_STR = arg.slice(6);
    else if (arg.startsWith('--runs=')) RUNS = parseInt(arg.slice(7), 10) || 1;
    else if (arg === '--help' || arg === '-h') {
        console.log('usage: node test-cases.js [--url=URL] [--runs=N] [<cases.json> ...]');
        console.log('  默认: cases/all-tests-expanded.json, runs=1, url=http://localhost:8002');
        process.exit(0);
    } else files.push(arg);
}
if (files.length === 0) {
    // 2026-05-22: 默认同时跑 standard testcase + gamecase (真实游戏抽样的 hard outlier), 分开统计
    files.push(path.join(__dirname, 'cases/all-tests-expanded.json'));
    files.push(path.join(__dirname, 'cases/game-cases.json'));
}
const u = new URL(URL_STR);

// === HTTP ===
function postSolve(payload) {
    return new Promise((resolve, reject) => {
        const data = JSON.stringify(payload);
        const req = http.request({
            hostname: u.hostname, port: u.port || 80, path: '/api/solve', method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) },
            timeout: 180000,
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

// === Utils ===
function normCard(c) {
    if (typeof c !== 'string') return c;
    if (c === 'X' || c.startsWith('Xj')) return 'X';
    return c;
}
function sortKey(arr) {
    return (arr || []).map(normCard).sort().join(',');
}
function layoutMatch(ai, exp) {
    return sortKey(ai.top) === sortKey(exp.top)
        && sortKey(ai.middle) === sortKey(exp.middle)
        && sortKey(ai.bottom) === sortKey(exp.bottom);
}
function fmtCard(c) {
    if (c === 'X' || (typeof c === 'string' && c.startsWith('X'))) return '🃏';
    return c;
}
function fmtRow(cards) {
    return (cards || []).map(fmtCard).join(' ');
}
function caseTag(name) {
    // 兼容两种格式:
    //   "9 [R2]: 不弃鬼..." → "9 [R2]:"
    //   "case 6: AA 顶..." → "case 6:"
    const m1 = name.match(/^(\d+\s*\[R\d+\]:)/);
    if (m1) return m1[1];
    const m2 = name.match(/^(case\s*\d+:)/i);
    if (m2) return m2[1];
    return name;
}

// 兼容 expecteds[] (testcase 用) 跟 expected{} (hard.json 用)
function getExpecteds(c) {
    if (Array.isArray(c.expecteds) && c.expecteds.length) return c.expecteds;
    if (c.expected) return [c.expected];
    return [];
}

// flush stdout 实时输出
function emit(line) {
    process.stdout.write(line + '\n');
}

// === 单 case 跑 ===
async function runCase(c) {
    const payload = {
        round: c.round,
        state: c.state,
        dealt: c.dealt,
        discardCount: c.round === 1 ? 0 : 1,
        level: c.level || 'high',
        // 2026-05-22: 默认 Pineapple OFC 2 鬼, case 可 override (例 game-cases 真实游戏抽样 declare jokerCount)
        jokerCount: c.jokerCount ?? 2,
        // 2026-05-22: pureMLP true 测 NN value head 直接能力 (跳 MCTS), false 走 MCTS rollout. case 文件 declare.
        pureMLP: c.pureMLP ?? false,
    };
    const r = await postSolve(payload);
    if (!r.layout) {
        emit(`✗ ${caseTag(c.name)}  ❌ Go 报错: ${r.error || JSON.stringify(r).slice(0, 100)}`);
        return false;
    }
    const ai = {
        top: (r.layout.top || []).map(normCard),
        middle: (r.layout.middle || []).map(normCard),
        bottom: (r.layout.bottom || []).map(normCard),
    };
    let discard = null;
    if (c.round > 1) {
        const placed = new Set([...ai.top, ...ai.middle, ...ai.bottom]);
        for (const d of c.dealt) {
            const nd = normCard(d);
            if (!placed.has(nd)) { discard = nd; break; }
        }
    }
    const exps = getExpecteds(c);

    // 严格 layout match: AI 摆牌必须 == expecteds 之一
    const ok = exps.some(exp => layoutMatch(ai, exp));
    const mark = ok ? '✓' : '✗';
    if (c.round === 1) {
        emit(`${mark} ${caseTag(c.name)}`);
    } else {
        const init = `头[${fmtRow(c.state.top)}] 中[${fmtRow(c.state.middle)}] 底[${fmtRow(c.state.bottom)}]`;
        emit(`${mark} ${caseTag(c.name)} ${init}`);
    }
    const aiLine = `  AI: 头[${fmtRow(ai.top)}] 中[${fmtRow(ai.middle)}] 底[${fmtRow(ai.bottom)}]`
        + (discard ? ` 弃 ${fmtCard(discard)}` : '');
    emit(aiLine);
    exps.forEach((exp, i) => {
        emit(`  exp${i + 1}: 头[${fmtRow(exp.top || [])}] 中[${fmtRow(exp.middle || [])}] 底[${fmtRow(exp.bottom || [])}]`);
    });
    return ok;
}

// 跑 1 个 file 1 次
async function runFile(file, cases) {
    let passed = 0, failed = 0;
    const failTags = [];
    for (const c of cases) {
        try {
            const ok = await runCase(c);
            if (ok) passed++;
            else { failed++; failTags.push(caseTag(c.name)); }
        } catch (e) {
            emit(`✗ ${caseTag(c.name)}  ❌ exception: ${e.message}`);
            failed++;
            failTags.push(caseTag(c.name));
        }
    }
    return { passed, failed, total: passed + failed, failTags };
}

// === main ===
(async () => {
    const multiFile = files.length > 1;
    const multiRun = RUNS > 1;
    const allFileResults = []; // [{file, runs: [{passed,failed,total,failTags}, ...]}]

    for (const f of files) {
        const cases = JSON.parse(fs.readFileSync(f, 'utf8'));
        if (multiFile) emit(`\n========== ${path.basename(f)} (${cases.length} cases) ==========\n`);
        const runs = [];
        for (let r = 0; r < RUNS; r++) {
            if (multiRun) emit(`\n---------- run ${r + 1} / ${RUNS} ----------\n`);
            const res = await runFile(f, cases);
            runs.push(res);
            emit(`\n=== 结果: ${res.passed}通过 / ${res.failed}失败 / ${res.total}总计 ===`);
        }
        allFileResults.push({ file: f, runs });
    }

    // 汇总
    if (multiRun || multiFile) {
        emit('\n========== 汇总 ==========');
        if (multiFile && !multiRun) {
            emit('\n| file | pass | total |');
            emit('|---|:-:|:-:|');
            for (const { file, runs } of allFileResults) {
                emit(`| ${path.basename(file)} | ${runs[0].passed} | ${runs[0].total} |`);
            }
        } else if (multiRun) {
            for (const { file, runs } of allFileResults) {
                const passes = runs.map(r => r.passed);
                const sorted = [...passes].sort((a, b) => a - b);
                const median = sorted[Math.floor(sorted.length / 2)];
                const min = sorted[0], max = sorted[sorted.length - 1];
                emit(`\n[${path.basename(file)}]`);
                emit('| run | pass |');
                emit('|:-:|:-:|');
                runs.forEach((r, i) => emit(`| ${i + 1} | ${r.passed} / ${r.total} |`));
                emit(`| **median** | **${median}** |`);
                emit(`| range | ${min} - ${max} |`);
            }
        }
    }
})();
