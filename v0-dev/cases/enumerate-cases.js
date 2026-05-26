#!/usr/bin/env node
// enumerate-cases.js — 把 all-tests.json 里 check predicate 展开成具体 expecteds/wrongs/状态.
//
// 输入: all-tests.json (含 check field) + r004 bench 输出 (每 case 的 AI layout)
// 输出: all-tests-expanded.json
//
// 算法:
//   1. 对每 case 枚举所有合法摆法 (R1: 3^5 路径×slot 约束; RN: 3 discard × 3^2 路径×slot 约束)
//   2. 用 check predicate 过滤, 满足的 → expecteds
//   3. R004 实际摆法 → wrongs (如果 R004 在该 case 上 fail)
//   4. 如果 expecteds 为空, status = '待确认'
//   5. 输出 hand.json-like 格式

const fs = require('fs');
const path = require('path');

const allTestsPath = process.argv[2] || path.join(__dirname, 'all-tests.json');
const r004OutputPath = process.argv[3] || ''; // optional — R004 bench output 文件
const outPath = process.argv[4] || path.join(__dirname, 'all-tests-expanded.json');

const parseCard = s => {
    if (s === 'X' || s.startsWith('Xj')) {
        // 归一化: 所有 joker 输出都用 'X' (jid 只用于内部 deduplication, 不进 layout str)
        return { rank: 'X', suit: 'j', joker: true, jid: s === 'X' ? 0 : parseInt(s.slice(2), 10), str: 'X' };
    }
    return { rank: s[0], suit: s[1], str: s };
};

// dedup key for layout
const layoutKey = l => `T:${[...l.top].sort().join(',')}|M:${[...l.middle].sort().join(',')}|B:${[...l.bottom].sort().join(',')}`;

const cntJoker = cs => cs.filter(c => c.rank === 'X' || c.joker).length;
const cntRank = (cs, rk) => cs.filter(c => c.rank === rk).length;
const cntSuit = (cs, st) => cs.filter(c => c.suit === st).length;
const hasCard = (cs, rk, st) => cs.some(c => c.rank === rk && c.suit === st);

// 用 check predicate 评估一个 (full_top, full_mid, full_bot, discarded) 状态
function evalCheck(checkStr, top, middle, bottom, discarded) {
    try {
        return (new Function('top', 'middle', 'bottom', 'discarded',
            'cntJoker', 'cntRank', 'cntSuit', 'hasCard',
            `return ${checkStr}`))(top, middle, bottom, discarded, cntJoker, cntRank, cntSuit, hasCard);
    } catch (e) {
        return false;
    }
}

// 枚举一个 case 的所有合法摆法 (返回 LayoutSpec[] — 跟 hand.json 同 schema)
function enumerateValidPlacements(c) {
    const round = c.round || 1;
    const dealt = c.dealt.map(parseCard);
    const state = c.state || { top: [], middle: [], bottom: [] };
    const stateTopCnt = (state.top || []).length;
    const stateMidCnt = (state.middle || []).length;
    const stateBotCnt = (state.bottom || []).length;

    const topRemain = 3 - stateTopCnt;
    const midRemain = 5 - stateMidCnt;
    const botRemain = 5 - stateBotCnt;

    const stateTop = (state.top || []).map(parseCard);
    const stateMid = (state.middle || []).map(parseCard);
    const stateBot = (state.bottom || []).map(parseCard);

    const out = [];

    if (round === 1) {
        // 5 dealt → 5 placements. 枚举 3^5 = 243.
        // placements[i] ∈ {0,1,2} = top/mid/bot
        for (let mask = 0; mask < 243; mask++) {
            const p = [
                Math.floor(mask / 81) % 3,
                Math.floor(mask / 27) % 3,
                Math.floor(mask / 9) % 3,
                Math.floor(mask / 3) % 3,
                mask % 3,
            ];
            const cntT = p.filter(x => x === 0).length;
            const cntM = p.filter(x => x === 1).length;
            const cntB = p.filter(x => x === 2).length;
            if (cntT > topRemain || cntM > midRemain || cntB > botRemain) continue;
            const newTop = [...stateTop], newMid = [...stateMid], newBot = [...stateBot];
            const placedTop = [], placedMid = [], placedBot = [];
            for (let i = 0; i < 5; i++) {
                if (p[i] === 0) { newTop.push(dealt[i]); placedTop.push(dealt[i].str); }
                else if (p[i] === 1) { newMid.push(dealt[i]); placedMid.push(dealt[i].str); }
                else { newBot.push(dealt[i]); placedBot.push(dealt[i].str); }
            }
            if (evalCheck(c.check, newTop, newMid, newBot, null)) {
                const lay = { top: placedTop, middle: placedMid, bottom: placedBot };
                if (!out.some(o => layoutKey(o) === layoutKey(lay))) out.push(lay);
            }
        }
    } else {
        // R2-R5: 3 dealt, 1 discard, 2 placed. 枚举 3 (discard) × 3^2 (placements for kept).
        for (let discardIdx = 0; discardIdx < 3; discardIdx++) {
            const kept = dealt.filter((_, i) => i !== discardIdx);
            const discarded = dealt[discardIdx];
            for (let mask = 0; mask < 9; mask++) {
                const p = [Math.floor(mask / 3) % 3, mask % 3];
                const cntT = p.filter(x => x === 0).length;
                const cntM = p.filter(x => x === 1).length;
                const cntB = p.filter(x => x === 2).length;
                if (cntT > topRemain || cntM > midRemain || cntB > botRemain) continue;
                const newTop = [...stateTop], newMid = [...stateMid], newBot = [...stateBot];
                const placedTop = [], placedMid = [], placedBot = [];
                for (let i = 0; i < 2; i++) {
                    if (p[i] === 0) { newTop.push(kept[i]); placedTop.push(kept[i].str); }
                    else if (p[i] === 1) { newMid.push(kept[i]); placedMid.push(kept[i].str); }
                    else { newBot.push(kept[i]); placedBot.push(kept[i].str); }
                }
                if (evalCheck(c.check, newTop, newMid, newBot, discarded)) {
                    const lay = { top: placedTop, middle: placedMid, bottom: placedBot };
                    if (!out.some(o => layoutKey(o) === layoutKey(lay))) out.push(lay);
                }
            }
        }
    }
    return out;
}

// 从 bench 输出解析每 case 的 AI 摆法 (R1 直接, RN 是 state+AI)
function parseR004Output(outputText) {
    const map = new Map(); // case-name (前缀 "N [") → { top, middle, bottom, discarded? }
    const lines = outputText.split('\n');
    for (let i = 0; i < lines.length; i++) {
        const m = lines[i].match(/^[✗✓] \[(\d+) \[/);
        if (!m) continue;
        const caseNumStr = m[1];
        // 找接下来的 "AI:" 行
        for (let j = i + 1; j < Math.min(i + 6, lines.length); j++) {
            const aiMatch = lines[j].match(/^  AI: 头\[([^\]]*)\] 中\[([^\]]*)\] 底\[([^\]]*)\](?: 弃 (\S+))?/);
            if (aiMatch) {
                const parseRow = s => s === '∅' || !s ? [] : s.split(/\s+/).filter(x => x);
                map.set(caseNumStr, {
                    top: parseRow(aiMatch[1]),
                    middle: parseRow(aiMatch[2]),
                    bottom: parseRow(aiMatch[3]),
                    discarded: aiMatch[4] || null,
                });
                break;
            }
        }
    }
    return map;
}

// 提取 case-test.js 风格 R2-R5 layout 的 "新增部分" (减去 state)
function extractIncrementalLayout(round, fullLayout, state) {
    if (round === 1) {
        return { top: fullLayout.top, middle: fullLayout.middle, bottom: fullLayout.bottom };
    }
    const stateTopStrs = new Set((state.top || []).slice());
    const stateMidStrs = new Set((state.middle || []).slice());
    const stateBotStrs = new Set((state.bottom || []).slice());
    return {
        top: fullLayout.top.filter(s => !stateTopStrs.has(s)),
        middle: fullLayout.middle.filter(s => !stateMidStrs.has(s)),
        bottom: fullLayout.bottom.filter(s => !stateBotStrs.has(s)),
    };
}

// === 主流程 ===
const cases = JSON.parse(fs.readFileSync(allTestsPath, 'utf8'));
const r004Map = r004OutputPath && fs.existsSync(r004OutputPath)
    ? parseR004Output(fs.readFileSync(r004OutputPath, 'utf8'))
    : new Map();

let pendingCount = 0;
let withWrongCount = 0;
const expanded = cases.map(c => {
    const expecteds = c.check ? enumerateValidPlacements(c) : [];
    const result = {
        name: c.name,
        round: c.round || 1,
        dealt: c.dealt,
        state: c.state,
    };
    // 保留原始 check 字段供未来 ref
    if (c.check) result.check = c.check;

    if (expecteds.length > 0) {
        result.expecteds = expecteds;
    } else {
        result.status = '待确认';
        pendingCount++;
    }

    // 加 wrong: R004 实际摆法 (如果 fail)
    const caseNumMatch = c.name.match(/^(\d+) /);
    if (caseNumMatch) {
        const ai = r004Map.get(caseNumMatch[1]);
        if (ai) {
            // 看 R004 摆法是否在 expecteds 里, 不在就当 wrong
            const inExp = expecteds.some(e => {
                const sj = arr => [...(arr || [])].sort().join(',');
                // R004 AI is full layout (state + incremental for RN). 我们需要 incremental.
                const aiInc = extractIncrementalLayout(c.round || 1, ai, c.state || {});
                return sj(aiInc.top) === sj(e.top)
                    && sj(aiInc.middle) === sj(e.middle)
                    && sj(aiInc.bottom) === sj(e.bottom);
            });
            if (!inExp) {
                const aiInc = extractIncrementalLayout(c.round || 1, ai, c.state || {});
                result.wrongs = [aiInc];
                withWrongCount++;
            }
        }
    }

    return result;
});

fs.writeFileSync(outPath, JSON.stringify(expanded, null, 2));
console.log(`✓ Written ${expanded.length} cases to ${outPath}`);
console.log(`  - 待确认 (predicate 枚不出): ${pendingCount}`);
console.log(`  - 含 R004 wrong: ${withWrongCount}`);
const withExp = expanded.filter(c => c.expecteds).length;
console.log(`  - 有 expecteds: ${withExp}`);
const expCounts = expanded.filter(c => c.expecteds).map(c => c.expecteds.length);
if (expCounts.length) {
    console.log(`  - expecteds 数量分布: min=${Math.min(...expCounts)} max=${Math.max(...expCounts)} avg=${(expCounts.reduce((a,b)=>a+b,0)/expCounts.length).toFixed(1)}`);
}
