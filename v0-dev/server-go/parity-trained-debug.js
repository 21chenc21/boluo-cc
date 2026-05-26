#!/usr/bin/env node
// 找一个 mismatch state, 打印 JS f 和 Go f 对比
const { execFileSync } = require('child_process');
const fs = require('fs'), path = require('path'), vm = require('vm');

const ROOT = path.resolve(__dirname, '..');
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'game.js'), 'utf8'), { filename: 'game.js' });
vm.runInThisContext(fs.readFileSync(path.join(ROOT, 'solver.js'), 'utf8'), { filename: 'solver.js' });

// 复制 trainedEval 内的 feature build 逻辑, 加 instrumentation
function jsTrainedEvalFeatures(state) {
    const { top, middle, bottom } = state;

    const getType = (cards) => {
        if (cards.length === 0) return 0;
        if (cards.length === 5) return evaluate5(cards).type;
        if (cards.length === 3) return evaluate3(cards).type;
        const jokerCnt = cards.filter(c => c.rank === 'X').length;
        const counts = {};
        for (const c of cards) if (c.rank !== 'X') counts[c.rank] = (counts[c.rank] || 0) + 1;
        const vals = Object.values(counts);
        const mx = vals.length > 0 ? Math.max(...vals) : 0;
        const pairs = vals.filter(v => v >= 2).length;
        const eff = mx + jokerCnt;
        if (eff >= 4) return 7;
        if (eff >= 3 && pairs >= 2) return 6;
        if (eff >= 3 && jokerCnt >= 1 && vals.length >= 2) return 6;
        if (eff >= 3) return 3;
        if (pairs >= 2 || (pairs === 1 && jokerCnt >= 1)) return 2;
        if (eff >= 2) return 1;
        return 0;
    };
    const getPairRank = (cards) => {
        const jokerCnt = cards.filter(c => c.rank === 'X').length;
        const counts = {};
        for (const c of cards) if (c.rank !== 'X') counts[c.rank] = (counts[c.rank] || 0) + 1;
        let mx = -1;
        for (const [r, cnt] of Object.entries(counts)) { if (cnt >= 2) mx = Math.max(mx, parseInt(r)); }
        if (jokerCnt >= 1 && mx < 0) {
            const realRanks = Object.keys(counts).map(Number);
            if (realRanks.length > 0) mx = Math.max(...realRanks);
            else mx = 12;
        }
        return mx;
    };
    const rankIndex = (r) => r === 'X' ? 12 : '23456789TJQKA'.indexOf(r);
    const getMaxSuit = (cards) => {
        if (cards.length < 2) return 0;
        const sc = {};
        for (const c of cards) sc[c.suit] = (sc[c.suit] || 0) + 1;
        return Math.max(...Object.values(sc));
    };
    const getBestRun = (cards) => {
        if (cards.length < 2) return 0;
        const ri = [...new Set(cards.map(c => rankIndex(c.rank)))].sort((a,b) => a-b);
        let best = 1, run = 1;
        for (let i = 1; i < ri.length; i++) {
            if (ri[i]-ri[i-1] <= 2) { run++; best = Math.max(best, run); } else run = 1;
        }
        return best;
    };

    // dummy mapping: cards have rank as char/'X', suit as 's'/'h'/'d'/'c'/'j'
    const tCards = top, mCards = middle, bCards = bottom;
    const botType = getType(bCards), midType = getType(mCards), topType = getType(tCards);
    // rankIndex for joker is 12. for real rank, parseInt of digit or string?
    // counts uses rank char as key, so cant convert directly. Use rankIndex map:
    const rk = (r) => r === 'X' ? 12 : '23456789TJQKA'.indexOf(r);
    // Re-do getPairRank using rk
    const getPairRankFix = (cards) => {
        const jokerCnt = cards.filter(c => c.rank === 'X').length;
        const counts = {};
        for (const c of cards) if (c.rank !== 'X') counts[c.rank] = (counts[c.rank] || 0) + 1;
        let mx = -1;
        for (const [r, cnt] of Object.entries(counts)) { if (cnt >= 2) mx = Math.max(mx, rk(r)); }
        if (jokerCnt >= 1 && mx < 0) {
            const realRanks = Object.keys(counts).map(rk);
            if (realRanks.length > 0) mx = Math.max(...realRanks);
            else mx = 12;
        }
        return mx;
    };
    const botPR = getPairRankFix(bCards), midPR = getPairRankFix(mCards), topPR = getPairRankFix(tCards);
    const topMax = tCards.length > 0 ? Math.max(...tCards.map(c => rk(c.rank))) : 0;
    const topTrips = tCards.length === 3 && topType >= 3;
    const chasing = topPR >= 10 || topTrips;
    const placed = tCards.length + mCards.length + bCards.length;

    const _rn = placed <= 5 ? 1 : Math.ceil((placed - 5) / 2) + 1;
    const bMaxS = getMaxSuit(bCards), mMaxS = getMaxSuit(mCards);
    const bRn = getBestRun(bCards), mRn = getBestRun(mCards);
    const mHP = midType >= 1, midHasPair = midType >= 1;

    let _dS = 0, _dF = 0;
    if (tCards.length === 3 && mCards.length === 5) {
        const tE = evaluate3(tCards), mE = evaluate5(mCards);
        if (mE.type > tE.type) _dS = 1;
        else if (mE.type < tE.type) _dF = 1;
        else if (mE.type === tE.type && tE.type === 1) {
            if (midPR > topPR) _dS = 1;
            else if (midPR < topPR) _dF = 1;
        }
    }
    if (mCards.length === 5 && bCards.length === 5) {
        if (evaluate5(mCards).value > evaluate5(bCards).value) _dF = 1;
        else _dS = Math.max(_dS, 1);
    }

    const f = [
        botType > midType ? 1 : 0, midType > topType ? 1 : 0,
        midType > botType ? 1 : 0, topType > midType ? 1 : 0,
        (chasing && midPR >= 0 && topPR >= 0 && midPR >= topPR) ? 1 : 0,
        botType >= 2 ? 1 : 0, chasing ? 1 : 0,
        topPR === 10 ? 1 : 0, topPR === 11 ? 1 : 0, (topPR >= 12 || topTrips) ? 1 : 0,
        botType - midType, midType - topType,
        (botPR >= 0 && midPR >= 0) ? botPR - midPR : 0,
        (midPR >= 0 && topPR >= 0) ? midPR - topPR : 0,
        tCards.length, tCards.length === 1 ? 1 : 0,
        (tCards.length === 2 && topType < 1) ? 1 : 0,
        (topType === 0 && topMax >= 10) ? 1 : 0, tCards.length === 0 ? 1 : 0,
        _rn,
        Math.abs(tCards.length-1)+Math.abs(mCards.length-_rn*5/3)/2+Math.abs(bCards.length-_rn*5/3)/2,
        1, botType, midType, 5-bCards.length,
        bMaxS >= 4 ? 1 : 0, (bMaxS >= 3 && bMaxS < 4) ? 1 : 0,
        bRn >= 4 ? 1 : 0, mMaxS >= 4 ? 1 : 0, (mMaxS >= 3 && mMaxS < 4) ? 1 : 0,
        (chasing && mHP) ? 1 : 0,
        (topPR === 10 && mHP) ? 1 : 0, (topPR === 11 && mHP) ? 1 : 0,
        ((topPR >= 12 || topTrips) && midHasPair) ? 1 : 0,
        (chasing && botType >= 2) ? 1 : 0,
        (chasing && midType === 0 && mCards.length >= 3) ? 1 : 0,
        _dS, _dF,
        bMaxS * bMaxS / 25, mMaxS * mMaxS / 25,
        bRn / 5, mRn / 5,
        (botPR + 1) / 13, (midPR + 1) / 13, (topPR + 1) / 13,
        (botType >= 1 && midType >= 1) ? 1 : 0,
        (botPR >= 0 && midPR >= 0 && midPR > botPR) ? 1 : 0,
        botPR >= 8 ? 1 : 0, midPR >= 8 ? 1 : 0,
        (botPR >= 0 && midPR >= 0 && botPR > midPR) ? 1 : 0,
        (tCards.filter(c => c.rank === 'X').length + (topPR >= 0 ? 1 : 0)) / 2,
        (mCards.filter(c => c.rank === 'X').length + (midPR >= 0 ? 1 : 0)) / 2,
        (bCards.filter(c => c.rank === 'X').length + (botPR >= 0 ? 1 : 0)) / 2,
        Math.min(1, (bMaxS + bCards.filter(c => c.rank === 'X').length) / 5),
        Math.min(1, (mMaxS + mCards.filter(c => c.rank === 'X').length) / 5),
        (tCards.filter(c => c.rank === 'X').length > 0 && topMax >= 10) ? 1 : 0,
    ];
    return f;
}

// 取一个 mismatch case
const top = [{rank:'X',suit:'j',joker:true,jid:0}, {rank:'X',suit:'j',joker:true,jid:1}];
const mid = [];
const bot = [];

const jsF = jsTrainedEvalFeatures({ top, middle: mid, bottom: bot });

// Go output
const stdin = `X X||\n`;
const goOut = JSON.parse(execFileSync('/tmp/eval-trained-debug', [], { input: stdin, encoding: 'utf8' }).trim());

const goF = goOut.f;
console.log('idx | JS | GO | diff');
for (let i = 0; i < jsF.length; i++) {
    const d = Math.abs(jsF[i] - goF[i]);
    if (d > 1e-3) {
        console.log(`${i.toString().padStart(2)} | ${jsF[i].toFixed(4).padStart(8)} | ${goF[i].toFixed(4).padStart(8)} | ${d.toFixed(4)}`);
    }
}
console.log('\nJS score=', engine.trainedEval({ top, middle: mid, bottom: bot }).toFixed(4));
console.log('GO score=', goOut.score.toFixed(4));
