#!/usr/bin/env node
// convert-tests.js — 把 test-cases-joker-go.js 里的 63 个 case 转成 JSON.
//
// 输出: cases/all-tests.json (含 name, round, dealt, state, check)
//
// 用法: node convert-tests.js > all-tests.json

const fs = require('fs');
const path = require('path');

// 读 test-cases-joker-go.js 源码
const src = fs.readFileSync(path.join(__dirname, '..', 'test-cases-joker-go.js'), 'utf8');

// 解析 testR1J / testRNJ 调用. 用正则提取参数.
// pattern: testR1J('name', [cards], r => check_expr, opts?);

function cardLitToStr(o) {
    // {rank:'K',suit:'c'} → 'Kc'
    // {rank:'X',suit:'j',jid:0} → 'X' (jid 0 默认)
    // {rank:'X',suit:'j',jid:1} → 'Xj1'
    if (o.rank === 'X') {
        return o.jid && o.jid > 0 ? `Xj${o.jid}` : 'X';
    }
    return o.rank + o.suit;
}

// 把 source 里 [{rank:'K',suit:'c'},...] 反序列化成 ['Kc','Kd',...]
// 用 eval (受控, 都是字面量).
function parseCardsLit(s) {
    // s: "[{rank:'K',suit:'c'},{rank:'K',suit:'d'}]"
    // 用 Function 安全 eval
    const arr = (new Function(`return ${s}`))();
    return arr.map(cardLitToStr);
}

// 用 Function 解析 state object literal
function parseStateLit(s) {
    const obj = (new Function(`return ${s}`))();
    return {
        top: (obj.top || []).map(cardLitToStr),
        middle: (obj.middle || []).map(cardLitToStr),
        bottom: (obj.bottom || []).map(cardLitToStr),
    };
}

// 把谓词函数体 (r => expr) 转成纯表达式
// 输入: "r => cntJoker(r.top) >= 1 && cntRank(r.bottom, 'K') >= 2"
// 输出: "cntJoker(top) >= 1 && cntRank(bottom, 'K') >= 2"
//   - 把 r.X → X (top/middle/bottom/discarded)
//   - 同时支持多行 arrow 函数 (r => { ... return X; })
function predicateToExpr(s) {
    s = s.trim();
    // 单行 arrow
    let m = s.match(/^r\s*=>\s*(.*)$/s);
    if (!m) return null;
    let body = m[1].trim();
    // 如果是 { ... return X; } 形式
    if (body.startsWith('{')) {
        // 简化: 提取 return X
        const ret = body.match(/return\s+([\s\S]+?);?\s*}\s*$/);
        if (!ret) return null;
        body = ret[1].trim();
    }
    // 替换 r.top → top, r.middle → middle, etc
    body = body.replace(/\br\.(top|middle|bottom|discarded)/g, '$1');
    // 移除尾部分号
    body = body.replace(/;\s*$/, '');
    return body;
}

// 切出 testR1J / testRNJ 调用
// 简单分块: split by '\n    await test'
const blocks = src.split(/\n\s*await\s+/);
const cases = [];

for (const blk of blocks) {
    // 跳过头部
    if (!blk.startsWith('testR1J') && !blk.startsWith('testRNJ')) continue;

    const isR1 = blk.startsWith('testR1J');

    // 找到第一个 '(...)' 块结尾 — naive 用 paren count
    let depth = 0, end = -1, started = false;
    for (let i = 0; i < blk.length; i++) {
        const ch = blk[i];
        if (ch === '(') { depth++; started = true; }
        else if (ch === ')') {
            depth--;
            if (started && depth === 0) { end = i; break; }
        }
    }
    if (end < 0) continue;
    const args = blk.slice(blk.indexOf('(') + 1, end).trim();

    try {
        if (isR1) {
            // testR1J('name', cards, r => check, opts?)
            // 参数用 ,\n 分割但注意嵌套
            const parts = splitTopLevel(args, ',');
            if (parts.length < 3) continue;
            const name = (new Function(`return ${parts[0].trim()}`))();
            const cards = parseCardsLit(parts[1].trim());
            const check = predicateToExpr(parts[2].trim());
            const opts = parts[3] ? (new Function(`return ${parts[3].trim()}`))() : null;
            const usedCards = opts && opts.usedCards ? opts.usedCards.map(cardLitToStr) : [];

            cases.push({
                name,
                round: 1,
                dealt: cards,
                state: { top: [], middle: [], bottom: [], usedCards },
                check,
            });
        } else {
            // testRNJ('name', stateBefore, dealt, round, r => check, _numJokers?, discarded?)
            const parts = splitTopLevel(args, ',');
            if (parts.length < 5) continue;
            const name = (new Function(`return ${parts[0].trim()}`))();
            const stateB = parseStateLit(parts[1].trim());
            const dealt = parseCardsLit(parts[2].trim());
            const round = parseInt(parts[3].trim(), 10);
            const check = predicateToExpr(parts[4].trim());
            const discarded = parts[6] ? parseCardsLit(parts[6].trim()) : [];

            // usedCards = state cards + discarded
            const usedCards = [...stateB.top, ...stateB.middle, ...stateB.bottom, ...discarded];

            cases.push({
                name,
                round,
                dealt,
                state: { ...stateB, usedCards },
                check,
            });
        }
    } catch (e) {
        console.error(`parse error in block: ${blk.slice(0, 80)}\n  ${e.message}`);
    }
}

// 顶层逗号 split (跳过括号/字符串内的)
function splitTopLevel(s, delim) {
    const parts = [];
    let depth = 0, inStr = null, cur = '';
    for (let i = 0; i < s.length; i++) {
        const ch = s[i];
        if (inStr) {
            cur += ch;
            if (ch === '\\') { cur += s[++i]; continue; }
            if (ch === inStr) inStr = null;
        } else if (ch === '"' || ch === "'" || ch === '`') {
            inStr = ch; cur += ch;
        } else if (ch === '(' || ch === '[' || ch === '{') {
            depth++; cur += ch;
        } else if (ch === ')' || ch === ']' || ch === '}') {
            depth--; cur += ch;
        } else if (ch === delim && depth === 0) {
            parts.push(cur); cur = '';
        } else {
            cur += ch;
        }
    }
    if (cur.trim()) parts.push(cur);
    return parts;
}

console.log(JSON.stringify(cases, null, 2));
console.error(`\n=== converted ${cases.length} cases ===`);
