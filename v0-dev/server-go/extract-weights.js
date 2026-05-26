#!/usr/bin/env node
// 从 solver.js 抽 inline trainedEval 权重 (56→128→64→1) 到 JSON
const fs = require('fs');
const path = require('path');

const SOLVER = path.resolve(__dirname, '..', 'solver.js');
const src = fs.readFileSync(SOLVER, 'utf8');

function extractArray(name) {
    // const _NAME = [....];  (一行)
    const re = new RegExp(`const ${name}\\s*=\\s*(\\[[\\s\\S]*?\\]);`, 'm');
    const m = src.match(re);
    if (!m) throw new Error(`missing ${name}`);
    // eval (we trust source)
    return Function(`"use strict"; return (${m[1]});`)();
}

const _m  = extractArray('_m');
const _s  = extractArray('_s');
const _w1 = extractArray('_w1');
const _b1 = extractArray('_b1');
const _w2 = extractArray('_w2');
const _b2 = extractArray('_b2');
const _w3 = extractArray('_w3');
const _b3 = extractArray('_b3');

// 校验维度
const IN = _m.length;
const H1 = _w1.length;
const H2 = _w2.length;
console.log(`shape: ${IN} → ${H1} → ${H2} → 1`);
if (_s.length !== IN) throw new Error('s length mismatch');
if (_b1.length !== H1) throw new Error('b1 length mismatch');
if (_w1[0].length !== IN) throw new Error('w1 inner mismatch');
if (_b2.length !== H2) throw new Error('b2 length mismatch');
if (_w2[0].length !== H1) throw new Error('w2 inner mismatch');
if (_w3.length !== 1 || _w3[0].length !== H2) throw new Error('w3 mismatch');
if (_b3.length !== 1) throw new Error('b3 length mismatch');

// yStd/yMean — 从 _fastForward 中提取 (我们之前 patch 用的常量)
const yMatch = src.match(/yStd:\s*(-?[\d.]+),\s*yMean:\s*(-?[\d.]+)/);
if (!yMatch) throw new Error('y-scale not found in _fastForward');
const yStd = parseFloat(yMatch[1]);
const yMean = parseFloat(yMatch[2]);
console.log(`yStd=${yStd}, yMean=${yMean}`);

const out = {
    inDim: IN, h1Dim: H1, h2Dim: H2,
    means: _m, stds: _s,
    w1: _w1, b1: _b1,
    w2: _w2, b2: _b2,
    w3: _w3[0], b3: _b3[0],
    yStd, yMean,
};
const OUT = path.join(__dirname, 'ofc', 'trained_weights.json');
fs.writeFileSync(OUT, JSON.stringify(out));
console.log(`wrote: ${OUT} (${fs.statSync(OUT).size} bytes)`);
