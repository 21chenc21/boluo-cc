#!/usr/bin/env node
// 跑 test-cases-joker.js 同样的 case, 但通过 Go HTTP API (Path X 生效).
// 用法: node test-cases-joker-go.js [url=http://localhost:8002]
//
// 比 testcase-joker.js 多: 直接打 Go binary, 测 Path X 改动.
// vs JS testcase: 同 case 同 check, 但行为是 Go 端 v8-fan 的.

const http = require('http');

const URL_STR = process.argv[2] || 'http://localhost:8002';
const u = new URL(URL_STR);

let passed = 0, failed = 0;

function fmtCard(c) {
    if (c.rank === 'X' || c.joker) return '🃏';
    return c.rank + c.suit;
}
function fmtCards(cs) { return cs.map(fmtCard).join(' '); }

function cardToStr(c) {
    if (typeof c === 'string') return c;
    if (c.rank === 'X' || c.joker) {
        if (c.jid !== undefined && c.jid > 0) return 'Xj' + c.jid;
        return 'X';
    }
    return c.rank + c.suit;
}

function strToCard(s) {
    if (s === 'X' || s.startsWith('Xj')) {
        const jid = s === 'X' ? 0 : parseInt(s.slice(2), 10);
        return { rank: 'X', suit: 'j', joker: true, jid };
    }
    return { rank: s[0], suit: s[1] };
}

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

async function testR1J(name, cards, checkFn, opts = {}) {
    const dealt = cards.map(cardToStr);
    const usedCards = (opts.usedCards || []).map(cardToStr);
    const r = await postSolve({
        round: 1,
        state: { top: [], middle: [], bottom: [], usedCards },
        dealt,
        discardCount: 0,
        level: 'high',
    });
    if (!r.layout) {
        console.log(`✗ [${name}]  ❌ Go 报错: ${r.error || JSON.stringify(r).slice(0,100)}`);
        failed++;
        return;
    }
    const top = (r.layout.top || []).map(strToCard);
    const middle = (r.layout.middle || []).map(strToCard);
    const bottom = (r.layout.bottom || []).map(strToCard);
    const result = { top, middle, bottom };
    const ok = checkFn(result);
    console.log(`${ok ? '✓' : '✗'} [${name}]`);
    if (opts.usedCards && opts.usedCards.length > 0) {
        console.log(`  桌面已用: ${fmtCards(opts.usedCards)}`);
    }
    console.log(`  发: ${fmtCards(cards)}`);
    console.log(`  AI: 头[${fmtCards(top)}] 中[${fmtCards(middle)}] 底[${fmtCards(bottom)}]`);
    if (ok) passed++; else failed++;
}

async function testRNJ(name, stateBefore, dealt, round, checkFn, _numJokers = 4, discarded = []) {
    const stateOut = {
        top: stateBefore.top.map(cardToStr),
        middle: stateBefore.middle.map(cardToStr),
        bottom: stateBefore.bottom.map(cardToStr),
        usedCards: [
            ...stateBefore.top.map(cardToStr),
            ...stateBefore.middle.map(cardToStr),
            ...stateBefore.bottom.map(cardToStr),
            ...discarded.map(cardToStr),
        ],
    };
    const dealtStr = dealt.map(cardToStr);
    const r = await postSolve({
        round, state: stateOut, dealt: dealtStr,
        discardCount: 1, level: 'high',
    });
    if (!r.layout) {
        console.log(`✗ [${name}]  ❌ Go 报错: ${r.error || JSON.stringify(r).slice(0,100)}`);
        failed++;
        return;
    }
    const top = [...stateBefore.top, ...(r.layout.top || []).map(strToCard)];
    const middle = [...stateBefore.middle, ...(r.layout.middle || []).map(strToCard)];
    const bottom = [...stateBefore.bottom, ...(r.layout.bottom || []).map(strToCard)];
    const placedRaw = [...(r.layout.top || []), ...(r.layout.middle || []), ...(r.layout.bottom || [])];
    const placedSet = new Set(placedRaw);
    const discRaw = dealtStr.find(s => !placedSet.has(s));
    const disc = discRaw ? strToCard(discRaw) : null;
    const result = { top, middle, bottom, discarded: disc };
    const ok = checkFn(result);
    console.log(`${ok ? '✓' : '✗'} [${name}]`);
    console.log(`  发: ${fmtCards(dealt)} → 弃${disc ? fmtCard(disc) : '∅'}`);
    console.log(`  AI: 头[${fmtCards(top)}] 中[${fmtCards(middle)}] 底[${fmtCards(bottom)}]`);
    if (ok) passed++; else failed++;
}

const cntJoker = cs => cs.filter(c => c.rank === 'X' || c.joker).length;
const cntRank = (cs, r) => cs.filter(c => c.rank === r).length;
const cntSuit = (cs, s) => cs.filter(c => c.suit === s).length;
const hasCard = (cs, r, s) => cs.some(c => c.rank === r && c.suit === s);

(async () => {
    console.log('=== 鬼牌专项测试 (Go API, level=high) ===\n');

    await testR1J('1 [R1]: 鬼孤身在顶, KK锁底',
        [{rank:'X',suit:'j',jid:0},{rank:'K',suit:'c'},{rank:'K',suit:'d'},{rank:'3',suit:'h'},{rank:'7',suit:'s'}],
        r => cntJoker(r.top) >= 1 && cntRank(r.bottom, 'K') >= 2
    );
    await testR1J('2 [R1]: 鬼孤身在顶等高牌',
        [{rank:'X',suit:'j',jid:0},{rank:'Q',suit:'c'},{rank:'2',suit:'d'},{rank:'5',suit:'h'},{rank:'8',suit:'s'}],
        r => cntJoker(r.top) >= 1
    );
    await testR1J('3 [R1]: AA顶进范, 鬼放底/中保灵活性',
        [{rank:'X',suit:'j',jid:0},{rank:'A',suit:'c'},{rank:'A',suit:'d'},{rank:'3',suit:'h'},{rank:'7',suit:'s'}],
        r => cntRank(r.top, 'A') >= 2 && cntJoker(r.top) === 0
    );
    await testR1J('4 [R1]: 1鬼放顶+另1鬼配3♣ 凑SF',
        [{rank:'X',suit:'j',jid:0},{rank:'X',suit:'j',jid:1},{rank:'T',suit:'c'},{rank:'J',suit:'c'},{rank:'Q',suit:'c'}],
        r => cntJoker(r.top) >= 1 && cntJoker([...r.middle, ...r.bottom]) >= 1
    );
    await testRNJ('5 [R3]: 鬼优先放顶/中, 不补底flush',
        { top: [], middle: [{rank:'6',suit:'d'}], bottom: [{rank:'T',suit:'c'},{rank:'2',suit:'c'},{rank:'5',suit:'c'},{rank:'9',suit:'c'}] },
        [{rank:'X',suit:'j',jid:0},{rank:'K',suit:'h'},{rank:'7',suit:'s'}],
        3,
        r => cntJoker(r.top) >= 1 || cntJoker(r.middle) >= 1
    );
    // 用户严格: AA 上顶 (实对锁 fantasy) + KK 底 + 鬼中 (鬼 mid/bot 灵活)
    // 不允许 joker+A 顶 (浪费一张 A, AA 顶才最优)
    await testR1J('6 [R1]: 鬼+KK+AA 双对最优配置',
        [{rank:'X',suit:'j',jid:0},{rank:'K',suit:'c'},{rank:'K',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'}],
        r => cntRank(r.top, 'A') >= 2 && cntRank(r.bottom, 'K') >= 2 && cntJoker(r.top) === 0
    );
    await testRNJ('7 [R3]: 鬼上顶配9 等高牌进范',
        { top: [{rank:'9',suit:'h'}], middle: [{rank:'2',suit:'c'},{rank:'2',suit:'h'},{rank:'7',suit:'d'}], bottom: [{rank:'K',suit:'s'},{rank:'K',suit:'c'},{rank:'4',suit:'h'},{rank:'5',suit:'s'}] },
        [{rank:'X',suit:'j',jid:0},{rank:'Q',suit:'d'},{rank:'3',suit:'c'}],
        3,
        r => cntJoker(r.top) >= 1
    );
    await testR1J('8 [R1]: 双鬼分顶底 等大牌',
        [{rank:'X',suit:'j',jid:0},{rank:'X',suit:'j',jid:1},{rank:'2',suit:'c'},{rank:'3',suit:'d'},{rank:'4',suit:'h'}],
        r => cntJoker(r.top) >= 1 && (cntJoker(r.middle) >= 1 || cntJoker(r.bottom) >= 1)
    );
    await testRNJ('9 [R2]: 不弃鬼 + 鬼优先放顶',
        { top: [{rank:'3',suit:'h'}], middle: [{rank:'2',suit:'s'},{rank:'7',suit:'d'}], bottom: [{rank:'4',suit:'c'},{rank:'6',suit:'h'}] },
        [{rank:'X',suit:'j',jid:0},{rank:'7',suit:'h'},{rank:'2',suit:'c'}],
        2,
        r => !(r.discarded && (r.discarded.rank === 'X' || r.discarded.joker)) && cntJoker(r.top) >= 1
    );
    await testR1J('10 [R1]: 鬼配A上顶, TJ♦底保同花',
        [{rank:'X',suit:'j',jid:0},{rank:'T',suit:'d'},{rank:'J',suit:'d'},{rank:'A',suit:'s'},{rank:'7',suit:'c'}],
        r => {
            const topJokerA = cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1;
            const tjSameRow = [r.top, r.middle, r.bottom].some(row =>
                hasCard(row, 'T', 'd') && hasCard(row, 'J', 'd')
            );
            return topJokerA && tjSameRow;
        }
    );

    console.log('\n=== UR 系列 (用户实战 R1 弱点) ===\n');

    await testR1J('11 [R1]: A+joker 应一起上顶 (4d 5h Ah As X)',
        [{rank:'4',suit:'d'},{rank:'5',suit:'h'},{rank:'A',suit:'h'},{rank:'A',suit:'s'},{rank:'X',suit:'j',jid:0}],
        r => cntRank(r.top, 'A') >= 1 && (cntRank(r.top, 'A') >= 2 || cntJoker(r.top) >= 1)
    );
    await testR1J('12 [R1]: A+joker 应一起上顶 (As 4c 8h X 5h)',
        [{rank:'A',suit:'s'},{rank:'4',suit:'c'},{rank:'8',suit:'h'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'}],
        r => cntRank(r.top, 'A') >= 1 && cntJoker(r.top) >= 1
    );
    await testR1J('13 [R1]: A+joker 应一起上顶 (9s 2c X 5h Ac)',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'A',suit:'c'}],
        r => cntRank(r.top, 'A') >= 1 && cntJoker(r.top) >= 1
    );
    await testR1J('14 [R1]: JsQs 同色高散应底道 (9c As Qs Js 7h)',
        [{rank:'9',suit:'c'},{rank:'A',suit:'s'},{rank:'Q',suit:'s'},{rank:'J',suit:'s'},{rank:'7',suit:'h'}],
        r => hasCard(r.bottom, 'J', 's') && hasCard(r.bottom, 'Q', 's')
    );
    // UR5: 用户放宽 — 至少一张高牌 (Ts/Kh) 在底即可, 另一张 top/middle 都行.
    // 因为 Kh top 单顶 / Ts middle 配 3c 都是合理 R1 摆法.
    await testR1J('15 [R1]: TsKh 至少一张应底道 (4h Ts Kh 3c Qh)',
        [{rank:'4',suit:'h'},{rank:'T',suit:'s'},{rank:'K',suit:'h'},{rank:'3',suit:'c'},{rank:'Q',suit:'h'}],
        r => hasCard(r.bottom, 'T', 's') || hasCard(r.bottom, 'K', 'h')
    );
    // UR6: 用户放宽 — 至少一张高牌 (9c/Qh) 在底, 另一张可分到 top.
    await testR1J('16 [R1]: 9cQh 至少一张应底道 (8c 2d Qh 9c Kc)',
        [{rank:'8',suit:'c'},{rank:'2',suit:'d'},{rank:'Q',suit:'h'},{rank:'9',suit:'c'},{rank:'K',suit:'c'}],
        r => hasCard(r.bottom, '9', 'c') || hasCard(r.bottom, 'Q', 'h')
    );
    await testR1J('17 [R1]: TT 应保留底道 (Td Th 3h 9s Ks)',
        [{rank:'T',suit:'d'},{rank:'T',suit:'h'},{rank:'3',suit:'h'},{rank:'9',suit:'s'},{rank:'K',suit:'s'}],
        r => cntRank(r.bottom, 'T') >= 2 && cntRank(r.bottom, '3') === 0
    );
    await testR1J('18 [R1]: Qd+TT 应同底 (Qd Tc 4s Td 6d)',
        [{rank:'Q',suit:'d'},{rank:'T',suit:'c'},{rank:'4',suit:'s'},{rank:'T',suit:'d'},{rank:'6',suit:'d'}],
        r => cntRank(r.bottom, 'T') >= 2 && hasCard(r.bottom, 'Q', 'd')
    );
    // 用户: 应追底花 (4 ♠ 全底) + A 上顶 (chase AA fantasy)
    await testR1J('19 [R1]: 4 张 ♠ 应全底 + A 顶 (2s 5s 3s Js Ac)',
        [{rank:'2',suit:'s'},{rank:'5',suit:'s'},{rank:'3',suit:'s'},{rank:'J',suit:'s'},{rank:'A',suit:'c'}],
        r => cntSuit(r.bottom, 's') >= 4 && hasCard(r.top, 'A', 'c')
    );
    await testR1J('20 [R1]: 7h Ts 8s 顺面应集中底 (3c Td 8s 7h 4d)',
        [{rank:'3',suit:'c'},{rank:'T',suit:'d'},{rank:'8',suit:'s'},{rank:'7',suit:'h'},{rank:'4',suit:'d'}],
        r => hasCard(r.bottom, '7', 'h') && hasCard(r.bottom, '8', 's') && hasCard(r.bottom, 'T', 'd')
    );
    // UR11: 用户放宽 — 9h (较高那张) 在底即可, 7d 可中可底.
    await testR1J('21 [R1]: 9h 应底道 (4d 6c 9h Ac 7d)',
        [{rank:'4',suit:'d'},{rank:'6',suit:'c'},{rank:'9',suit:'h'},{rank:'A',suit:'c'},{rank:'7',suit:'d'}],
        r => hasCard(r.bottom, '9', 'h')
    );
    // UR12: 用户放宽 — TT 在底 + 3 不上底, 不强制 Ks 位置 (Ks 顶/底/中都行).
    await testR1J('22 [R1]: TT 应底, 3 不上底 (Td Th 3h 9s Ks)',
        [{rank:'T',suit:'d'},{rank:'T',suit:'h'},{rank:'3',suit:'h'},{rank:'9',suit:'s'},{rank:'K',suit:'s'}],
        r => cntRank(r.bottom, 'T') >= 2 && cntRank(r.bottom, '3') === 0
    );
    await testRNJ('23 [R2]: R1 33+T 中→R2 拿 Q 不应替换 33',
        { top: [], middle: [{rank:'3',suit:'d'},{rank:'T',suit:'d'},{rank:'3',suit:'h'}], bottom: [{rank:'J',suit:'s'},{rank:'8',suit:'s'}] },
        [{rank:'4',suit:'d'},{rank:'Q',suit:'c'},{rank:'6',suit:'s'}],
        2,
        r => cntRank(r.middle, '3') >= 2,
        2
    );
    // 用户固定答案: A鬼顶 (lock AA via wild) + 23 中 + K 底
    await testR1J('24 [R1]: A鬼顶 + 23 中 + K 底 (X 3h Ks As 2d, 固定)',
        [{rank:'X',suit:'j',jid:0},{rank:'3',suit:'h'},{rank:'K',suit:'s'},{rank:'A',suit:'s'},{rank:'2',suit:'d'}],
        r => cntJoker(r.top) >= 1 && hasCard(r.top, 'A', 's')
            && hasCard(r.middle, '2', 'd') && hasCard(r.middle, '3', 'h')
            && hasCard(r.bottom, 'K', 's')
    );
    // UR15: 用户放宽 — 33 在中 (保对) + 顶不带 3 (不破对). Td/8s/Js 分布灵活.
    await testR1J('25 [R1]: 33 在中保对, 顶无 3 (3d Js 8s Td 3h)',
        [{rank:'3',suit:'d'},{rank:'J',suit:'s'},{rank:'8',suit:'s'},{rank:'T',suit:'d'},{rank:'3',suit:'h'}],
        r => cntRank(r.middle, '3') >= 2 && cntRank(r.top, '3') === 0
    );
    await testR1J('26 [R1]: 233 应分中底, 不全堆中 (2c Th 3c 5c 3h)',
        [{rank:'2',suit:'c'},{rank:'T',suit:'h'},{rank:'3',suit:'c'},{rank:'5',suit:'c'},{rank:'3',suit:'h'}],
        r => cntRank(r.middle, '3') >= 2 && hasCard(r.bottom, 'T', 'h')
    );

    // UR17: 用户报 — dealt 2s 4d Jh 3c Ac. 模型给 T=Ac M=4d 3c B=2s Jh.
    // 期望 (用户): J 在底 (高 rank 集中) — 当前 check 验证底含 Jh + 顶 Ac 单顶.
    // 也验证 2 张低散 (2/3/4) 应在中而非分散.
    await testR1J('27 [R1]: AcJh + 2/3/4 低散 (2s 4d Jh 3c Ac)',
        [{rank:'2',suit:'s'},{rank:'4',suit:'d'},{rank:'J',suit:'h'},{rank:'3',suit:'c'},{rank:'A',suit:'c'}],
        r => hasCard(r.top, 'A', 'c') && hasCard(r.bottom, 'J', 'h') && cntRank(r.middle, '2') + cntRank(r.middle, '3') + cntRank(r.middle, '4') >= 2
    );

    console.log('\n=== R2-R5 系列 (joker / fan / flush / foul 决策) ===\n');

    // RN1 [R2-flush]: R2 加同色 → 底 4-card flush draw
    // R1 完: top[], mid[3c 4c], bot[Th 5h 7h] (3 ♥)
    // R2 拿 9♥ → 应放底凑 4 同色; 弃 K♣ 或 6♦
    await testRNJ('28 [R2]: 加 9♥ 凑底 4 同色',
        { top: [], middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'}],
          bottom: [{rank:'T',suit:'h'},{rank:'5',suit:'h'},{rank:'7',suit:'h'}] },
        [{rank:'9',suit:'h'},{rank:'K',suit:'c'},{rank:'6',suit:'d'}],
        2,
        r => cntSuit(r.bottom, 'h') >= 4
    );

    // RN2 删除 — 数据有歧义

    // RN3 [R2-keep-pair]: 不破已有的 mid 33
    // R1 完: top[Kh], mid[3d 3h], bot[8c 5d]
    // R2 拿 A → 上顶/底, 不应碰 mid
    await testRNJ('29 [R2]: 不破中 33 (拿 A 别压中)',
        { top: [{rank:'K',suit:'h'}], middle: [{rank:'3',suit:'d'},{rank:'3',suit:'h'}],
          bottom: [{rank:'8',suit:'c'},{rank:'5',suit:'d'}] },
        [{rank:'2',suit:'s'},{rank:'A',suit:'c'},{rank:'7',suit:'d'}],
        2,
        r => cntRank(r.middle, '3') >= 2  // 33 仍在中
    );

    // RN4 [R3-no-discard-joker]: R3 dealt 含 joker, 永远不该弃
    // R1+R2 完: top[Qc], mid[3d 4d 5d], bot[2c 7s 9s]
    await testRNJ('30 [R3]: 不弃 joker',
        { top: [{rank:'Q',suit:'c'}],
          middle: [{rank:'3',suit:'d'},{rank:'4',suit:'d'},{rank:'5',suit:'d'}],
          bottom: [{rank:'2',suit:'c'},{rank:'7',suit:'s'},{rank:'9',suit:'s'}] },
        [{rank:'X',suit:'j',jid:0},{rank:'8',suit:'h'},{rank:'A',suit:'h'}],
        3,
        r => !(r.discarded && (r.discarded.rank === 'X' || r.discarded.joker))
    );

    // RN5 / RN6 删除 — mid flush 在 OFC 几乎必 foul (bot 难达 ≥ flush)

    // ===== R2 系列 (RN7-RN13, 4 more) =====

    // RN7 [R2-straight]: 9♣ 上底凑 4-conn open-ended straight, 比 K 范 EV 高
    // R1: top[], mid[2c 3c], bot[6h 7s 8d] (3 connectors)
    // 概率算账: 保 9c 上底 → 底顺 ~67% × 4 = +2.7;
    //          弃 9c → 底顺 ~25% × 4 = +1. 保 9c 净 +1.7 EV.
    // K 范在两个场景中都 ~14%, 不影响选择.
    await testRNJ('31 [R2]: 9♣ 上底凑 open-ended straight (高 EV)',
        { top: [], middle: [{rank:'2',suit:'c'},{rank:'3',suit:'c'}],
          bottom: [{rank:'6',suit:'h'},{rank:'7',suit:'s'},{rank:'8',suit:'d'}] },
        [{rank:'9',suit:'c'},{rank:'K',suit:'h'},{rank:'2',suit:'s'}],
        2,
        r => hasCard(r.bottom, '9', 'c')  // 9c 必上底
    );

    // RN8 [R2-jjj]: 双鬼 dealt → 至少 1 个上 top 追范
    // R1: top[Kc], mid[5d 6d], bot[2s 7c]
    // R2 拿 X X 8h → 1 joker 上顶 (top 已有 K, 加 joker → 等高 pair lock fan)
    await testRNJ('32 [R2]: 双鬼 ≥1 上顶',
        { top: [{rank:'K',suit:'c'}], middle: [{rank:'5',suit:'d'},{rank:'6',suit:'d'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'c'}] },
        [{rank:'X',suit:'j',jid:0},{rank:'X',suit:'j',jid:1},{rank:'8',suit:'h'}],
        2,
        r => cntJoker(r.top) >= 1
    );

    // RN9 [R2-KK-bot]: KK 必须上底 anchor (top Qd 已经在那, 加 K 形成 QKK 顶 = foul-prone)
    // R1: top[Qd], mid[5c 6c], bot[3h 9s]
    await testRNJ('33 [R2]: KK 上底 (anchor)',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.bottom, 'K') >= 2
    );

    // RN10 [R2-suit-cluster]: R2 拿 2 张同色 + 已有同色 → 集中
    // R1: top[Ac], mid[3d 4s], bot[5h 6h] (2 ♥)
    // R2 拿 7h 8h Kc → 2 ♥ 都应底, 凑 4 ♥ flush draw
    await testRNJ('34 [R2]: 2 ♥ 集中底凑 flush',
        { top: [{rank:'A',suit:'c'}], middle: [{rank:'3',suit:'d'},{rank:'4',suit:'s'}],
          bottom: [{rank:'5',suit:'h'},{rank:'6',suit:'h'}] },
        [{rank:'7',suit:'h'},{rank:'8',suit:'h'},{rank:'K',suit:'c'}],
        2,
        r => cntSuit(r.bottom, 'h') >= 4
    );

    // RN11 [R2-no-mid-trips]: 5d 不上中 (弃 OR 上 top/bot 都行)
    await testRNJ('35 [R2]: 5 不上中 (mid trips foul)',
        { top: [{rank:'A',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'5',suit:'h'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'c'}] },
        [{rank:'5',suit:'d'},{rank:'9',suit:'c'},{rank:'3',suit:'h'}],
        2,
        r => cntRank(r.middle, '5') < 3
    );

    // RN12 [R2-fan-A]: top 单 joker, R2 拿 A → A+joker 顶 (类 UR1-3 R2 版)
    // R1: top[joker], mid[2c 3c], bot[7d 9s]
    await testRNJ('36 [R2]: A 配顶 joker 锁 AA',
        { top: [{rank:'X',suit:'j',jid:0}], middle: [{rank:'2',suit:'c'},{rank:'3',suit:'c'}],
          bottom: [{rank:'7',suit:'d'},{rank:'9',suit:'s'}] },
        [{rank:'A',suit:'s'},{rank:'8',suit:'h'},{rank:'4',suit:'d'}],
        2,
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1
    );

    // RN13 删除 — 弃低牌 outs 看具体场景, 不强制 specific 牌

    // ===== R3 系列 (RN14-RN21, 8 more) =====

    // RN14 [R3-no-mid-straight]: mid straight 必 foul (bot 难 ≥ straight), 弃 9d
    // R1+R2: top[Ah], mid[5c 6h 7d 8s] (4 conn), bot[2c Jh 3d]
    await testRNJ('37 [R3]: 弃 9♦ 不凑 mid straight',
        { top: [{rank:'A',suit:'h'}],
          middle: [{rank:'5',suit:'c'},{rank:'6',suit:'h'},{rank:'7',suit:'d'},{rank:'8',suit:'s'}],
          bottom: [{rank:'2',suit:'c'},{rank:'J',suit:'h'},{rank:'3',suit:'d'}] },
        [{rank:'9',suit:'d'},{rank:'K',suit:'c'},{rank:'2',suit:'h'}],
        3,
        r => r.discarded && r.discarded.rank === '9'
    );

    // RN15 [R3-no-mid-trips]: 9 不上中 (弃 OR 上顶/底都行, 只要不破 mid pair-9 → trips)
    await testRNJ('38 [R3]: 9 不上中 (mid trips foul)',
        { top: [{rank:'K',suit:'c'}],
          middle: [{rank:'9',suit:'d'},{rank:'9',suit:'h'},{rank:'3',suit:'s'}],
          bottom: [{rank:'2',suit:'c'},{rank:'5',suit:'h'},{rank:'J',suit:'s'}] },
        [{rank:'9',suit:'c'},{rank:'4',suit:'d'},{rank:'7',suit:'s'}],
        3,
        r => cntRank(r.middle, '9') < 3
    );

    // RN16 [R3-mono]: 6 不上中 (mid 取最小连两 4-5 / 4 / 5, 6 该 bot 或 discard)
    await testRNJ('39 [R3]: 6 不上中 (低牌取小)',
        { top: [{rank:'A',suit:'h'},{rank:'Q',suit:'h'}],
          middle: [{rank:'2',suit:'c'},{rank:'3',suit:'d'}],
          bottom: [{rank:'8',suit:'h'},{rank:'J',suit:'s'},{rank:'9',suit:'c'}] },
        [{rank:'4',suit:'s'},{rank:'5',suit:'d'},{rank:'6',suit:'c'}],
        3,
        r => cntRank(r.middle, '6') === 0
    );

    // RN17 [R3-no-mid-flush]: mid flush 必 foul, 弃 8d
    // R1+R2: top[Ac], mid[3d 4d 5d 6d] (4 ♦), bot[2c 7s 9h]
    await testRNJ('40 [R3]: 弃 8♦ 不凑 mid flush',
        { top: [{rank:'A',suit:'c'}],
          middle: [{rank:'3',suit:'d'},{rank:'4',suit:'d'},{rank:'5',suit:'d'},{rank:'6',suit:'d'}],
          bottom: [{rank:'2',suit:'c'},{rank:'7',suit:'s'},{rank:'9',suit:'h'}] },
        [{rank:'8',suit:'d'},{rank:'K',suit:'c'},{rank:'2',suit:'h'}],
        3,
        r => r.discarded && r.discarded.rank === '8' && r.discarded.suit === 'd'
    );

    // top 双 joker + 高牌, K 或 Q 任一上顶 都行 (joker 当 K/Q → 高对). 2c 必上底 (低不浪费)
    await testRNJ('41 [R3]: 2c 必上底 (K/Q 顶或底 都可)',
        { top: [{rank:'X',suit:'j',jid:0},{rank:'X',suit:'j',jid:1}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'d'}] },
        [{rank:'K',suit:'h'},{rank:'Q',suit:'h'},{rank:'2',suit:'c'}],
        3,
        r => hasCard(r.bottom, '2', 'c')
    );

    // RN19 删除 — 用户: 弃 K 没问题 (this case 没 signal)

    // RN20 [R3-discard-Q-4-mid]: 弃 Q (锁顶 KK 后已无意义), 4s 上中
    await testRNJ('42 [R3]: 弃 Q, 4 上中',
        { top: [{rank:'K',suit:'c'}],
          middle: [{rank:'3',suit:'d'},{rank:'3',suit:'h'},{rank:'7',suit:'s'}],
          bottom: [{rank:'2',suit:'c'},{rank:'8',suit:'s'},{rank:'9',suit:'c'}] },
        [{rank:'A',suit:'h'},{rank:'Q',suit:'d'},{rank:'4',suit:'s'}],
        3,
        r => r.discarded && r.discarded.rank === 'Q' && hasCard(r.middle, '4', 's')
    );

    // RN21 [R3-fan-locked]: top joker+A 已锁 AA, K dealt → K 不该上顶
    // R1+R2: top[joker, As], mid[3c 4c], bot[5h 6h 7h]
    await testRNJ('43 [R3]: K 不浪费在顶 (joker+A 已锁)',
        { top: [{rank:'X',suit:'j',jid:0},{rank:'A',suit:'s'}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'}],
          bottom: [{rank:'5',suit:'h'},{rank:'6',suit:'h'},{rank:'7',suit:'h'}] },
        [{rank:'K',suit:'h'},{rank:'2',suit:'d'},{rank:'9',suit:'s'}],
        3,
        r => cntRank(r.top, 'K') === 0  // K 不上顶
    );

    // ===== R4 系列 (RN22-RN30, 9 more) =====

    // RN22 [R4-straight-bot]: bot 4-card 连号 → R4 完成
    // R1+R2+R3: top[Ah Kc], mid[3c 4c 9c Tc], bot[5h 6s 7d 8c]
    // R4 拿 9h → bot 5-9 straight
    await testRNJ('44 [R4]: 9♥ 完成底 straight',
        { top: [{rank:'A',suit:'h'},{rank:'K',suit:'c'}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'9',suit:'c'},{rank:'T',suit:'c'}],
          bottom: [{rank:'5',suit:'h'},{rank:'6',suit:'s'},{rank:'7',suit:'d'},{rank:'8',suit:'c'}] },
        [{rank:'9',suit:'h'},{rank:'2',suit:'s'},{rank:'2',suit:'d'}],
        4,
        r => hasCard(r.bottom, '9', 'h')
    );

    // RN23 删除 — top 锁 KK + mid 4 张 HC, foul 已不可救; 设计错

    // RN24 删除 — state 必爆 (top KKK > mid HC)

    // RN25 删除 — 数据有误

    // R4 不能进范, 包不 foul. 2c 上顶 (锁低 kicker 防 K 升级 top), K 上顶或底 都可
    await testRNJ('45 [R4]: 2c 上顶 (K 顶/底 都可)',
        { top: [{rank:'Q',suit:'h'}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'T',suit:'h'},{rank:'J',suit:'h'},{rank:'K',suit:'s'},{rank:'A',suit:'c'}] },
        [{rank:'K',suit:'h'},{rank:'9',suit:'d'},{rank:'2',suit:'c'}],
        4,
        r => hasCard(r.top, '2', 'c')
    );

    // RN27 删除

    // RN28 [R4-keep-A]: dealt 含 A, 不该弃 A
    // R1+R2+R3: top[Kh Qh], mid[3c 4c 5c 6c], bot[2s 7d 8s 9d]
    await testRNJ('46 [R4]: A 不弃',
        { top: [{rank:'K',suit:'h'},{rank:'Q',suit:'h'}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'d'},{rank:'8',suit:'s'},{rank:'9',suit:'d'}] },
        [{rank:'A',suit:'s'},{rank:'2',suit:'d'},{rank:'3',suit:'d'}],
        4,
        r => !(r.discarded && r.discarded.rank === 'A')
    );

    // RN29 [R4-no-jam-bot]: bot 已 KK pair, 不应破
    // R1+R2+R3: top[Ah Qh], mid[3c 4c 5c 6c], bot[Kc Kd 7s 8d]
    await testRNJ('47 [R4]: 不破底 KK',
        { top: [{rank:'A',suit:'h'},{rank:'Q',suit:'h'}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'K',suit:'c'},{rank:'K',suit:'d'},{rank:'7',suit:'s'},{rank:'8',suit:'d'}] },
        [{rank:'5',suit:'h'},{rank:'2',suit:'h'},{rank:'9',suit:'h'}],
        4,
        r => cntRank(r.bottom, 'K') >= 2
    );

    // RN30 删除

    // ===== R5 系列 (RN31-RN40, 10) =====

    // RN31 / RN32 / RN33 删除 — state 必爆 (foul-locked), 设计错误

    // RN34 [R5-bot-flush]: bot 4 同色 + R5 final 加 ♥ 完成 bot flush (mid 弱, 不 foul)
    // top 2 + mid 4 + bot 4 + R5 1 = 11+1=12. R5 后 top 2+mid 5+bot 5=12 错. 应 11.
    // 调整: top 2 + mid 5 (full) + bot 4 = 11 ✓
    await testRNJ('48 [R5]: Q♥ 完成底 flush',
        { top: [{rank:'A',suit:'c'},{rank:'K',suit:'c'}],
          middle: [{rank:'2',suit:'d'},{rank:'3',suit:'d'},{rank:'5',suit:'d'},{rank:'6',suit:'s'},{rank:'4',suit:'c'}],
          bottom: [{rank:'7',suit:'h'},{rank:'8',suit:'h'},{rank:'9',suit:'h'},{rank:'J',suit:'h'}] },
        [{rank:'Q',suit:'h'},{rank:'4',suit:'s'},{rank:'2',suit:'s'}],
        5,
        r => cntSuit(r.bottom, 'h') >= 5
    );

    // RN35 [R5-top-8h]: 8h 上顶 (joker 当 8 → 88 pair, 不 foul; joker 当 K 锁 KK 必 foul)
    await testRNJ('49 [R5]: 8♥ 上顶 (joker 降为 8, 不 foul)',
        { top: [{rank:'X',suit:'j',jid:0},{rank:'K',suit:'h'}],
          middle: [{rank:'Q',suit:'c'},{rank:'Q',suit:'h'},{rank:'J',suit:'h'},{rank:'9',suit:'d'}],
          bottom: [{rank:'3',suit:'s'},{rank:'4',suit:'h'},{rank:'5',suit:'s'},{rank:'6',suit:'h'},{rank:'7',suit:'d'}] },
        [{rank:'K',suit:'c'},{rank:'8',suit:'h'},{rank:'2',suit:'c'}],
        5,
        r => hasCard(r.top, '8', 'h')
    );

    // RN36 删除

    // RN37 [R5-7h-top-8s-bot]: 7♥ 顶 + 8♠ 底 (bot 9-T-J-Q + 8s = Q-high straight). 弃 As (锁 AA 必 foul)
    await testRNJ('50 [R5]: 7♥ 顶, 8♠ 底凑 straight',
        { top: [{rank:'X',suit:'j',jid:0},{rank:'2',suit:'c'}],
          middle: [{rank:'K',suit:'h'},{rank:'K',suit:'d'},{rank:'3',suit:'h'},{rank:'4',suit:'s'},{rank:'5',suit:'h'}],
          bottom: [{rank:'9',suit:'d'},{rank:'T',suit:'h'},{rank:'J',suit:'c'},{rank:'Q',suit:'d'}] },
        [{rank:'A',suit:'s'},{rank:'7',suit:'h'},{rank:'8',suit:'s'}],
        5,
        r => hasCard(r.top, '7', 'h') && hasCard(r.bottom, '8', 's')
    );

    // RN38 / RN39 删除 — state 必爆 (foul-locked)

    // ===== Deck-aware 系列 (DA1-DA8): 桌面已用牌影响策略 =====
    console.log('\n=== Deck-aware 系列 (桌面已用 A/K 数量影响 Fantasy 选择) ===\n');

    // UR3 base hand: 9s 2c X 5h Ac. 0 A used → 应 AA 锁顶 (常规 UR3)
    await testR1J('51 [R1]: UR3 hand, 0 A used → AA 锁顶 (常规)',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'A',suit:'c'}],
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1,
        { usedCards: [] }
    );

    // 同手, 桌面已 2 A used → 仍 AA 锁顶 (Ac 是该 A, joker 当 A → AA. 没 AAA 升级机会但 Fantasy 仍锁)
    await testR1J('52 [R1]: UR3 hand, 2 A used → 仍 AA 锁顶 (无 AAA 升级)',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'A',suit:'c'}],
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1,
        { usedCards: [{rank:'A',suit:'d'},{rank:'A',suit:'h'}] }
    );

    // 同手, 桌面已 3 A used → 这 Ac 是 deck 唯一 A. 仍 AA (joker 当 A) 锁 Fantasy
    await testR1J('53 [R1]: UR3 hand, 3 A used → 仍 AA 锁顶 (Ac 是最后 1 A)',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'A',suit:'c'}],
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1,
        { usedCards: [{rank:'A',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'}] }
    );

    // dealt 没 A, 桌面 4 A all used → 后续 R2-R5 不可能拿到 A.
    // joker 单顶 仍可 Fantasy (joker = K/Q if drawn). joker 不该浪费在 mid/bot.
    await testR1J('54 [R1]: 4 A used + dealt 无 A → joker 顶等 K/Q 进范',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'8',suit:'c'}],
        r => cntJoker(r.top) >= 1,
        { usedCards: [{rank:'A',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'},{rank:'A',suit:'c'}] }
    );

    // dealt 含 K, 桌面 4 A used + 3 K used (only Kh 是该手 K).
    // joker+K 顶锁 KK pair Fantasy (joker 当 K → KK +8 royalty + Fantasy) 是正确摆.
    // 即使 deck 无 K 升级 trips, joker 仍可降级为低 rank 避 foul. 双向保险.
    // check: joker 必上顶 (K 上不上顶都行).
    await testR1J('55 [R1]: 4A+3K used, dealt K → joker 必上顶 (KK 锁 Fantasy)',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'K',suit:'h'}],
        r => cntJoker(r.top) >= 1,
        { usedCards: [
            {rank:'A',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'},{rank:'A',suit:'c'},
            {rank:'K',suit:'c'},{rank:'K',suit:'d'},{rank:'K',suit:'s'}
        ] }
    );

    // 同手 dealt 含 K + joker, 桌面 0 A used (4 A 全在 deck) → AA Fantasy 仍是高概率.
    // joker 单顶 等 R2-R5 拿 A. K 应上底 anchor (留 top 灵活).
    await testR1J('56 [R1]: 0 A used, dealt K + joker → joker 顶, K 必上底',
        [{rank:'9',suit:'s'},{rank:'2',suit:'c'},{rank:'X',suit:'j',jid:0},{rank:'5',suit:'h'},{rank:'K',suit:'h'}],
        r => cntJoker(r.top) >= 1 && hasCard(r.bottom, 'K', 'h'),
        { usedCards: [] }
    );

    // R3 dealt 含 A, 桌面 3 A used → 这是最后 A. 一定上顶配 joker
    await testRNJ('57 [R3]: R3 3 A used + dealt A → 必上顶配 joker',
        { top: [{rank:'X',suit:'j',jid:0}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'d'},{rank:'9',suit:'d'}] },
        [{rank:'A',suit:'h'},{rank:'8',suit:'h'},{rank:'2',suit:'h'}],
        3,
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1,
        2,
        [{rank:'A',suit:'d'},{rank:'A',suit:'s'},{rank:'A',suit:'c'}]  // 3 A used (放 discarded 槽)
    );

    // R3 dealt 含 A, 桌面 0 A used → 同样上顶但不那么紧迫
    // 这 case 期望也是上顶 (反正 A 在手就该锁). 没区别 vs DA7 — 验证 DA7 不是 false-positive
    await testRNJ('58 [R3]: R3 0 A used + dealt A → 上顶配 joker',
        { top: [{rank:'X',suit:'j',jid:0}],
          middle: [{rank:'3',suit:'c'},{rank:'4',suit:'c'},{rank:'5',suit:'c'}],
          bottom: [{rank:'2',suit:'s'},{rank:'7',suit:'d'},{rank:'9',suit:'d'}] },
        [{rank:'A',suit:'h'},{rank:'8',suit:'h'},{rank:'2',suit:'h'}],
        3,
        r => cntJoker(r.top) >= 1 && cntRank(r.top, 'A') >= 1
    );

    // ===== DA9-DA13: KK pair + 不同 A used 数量, 看 AI 怎么权衡 =====
    // 基础 state (R2): top[Qd], mid[5c 6c], bot[3h 9s] (5 张, R1 完)
    // R2 dealt: Kh Ks 4d. KK pair → 顶 (lock K Fantasy +8) OR 底 (anchor) OR 中 (foul).
    // 关键: 桌面 A used 多少 影响 "top 是否值得为 A 留 flexibility"
    //   - 0/1/2 A used: deck 还有 A (+ 鬼), 必上底 等升级
    //   - 3 A used: 边界, 顶 (锁 Fantasy) 或底 (赌最后 1 A + 鬼) 都可
    //   - 4 A used: 无 A 升级路径, 必上顶 锁 K-Fantasy

    await testRNJ('59 [R2]: R2 KK + 0 A used → KK 必上底 等 A/鬼',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.bottom, 'K') >= 2  // KK 必上底
    );

    await testRNJ('60 [R2]: R2 KK + 1 A used → KK 必上底 等 A/鬼',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.bottom, 'K') >= 2,
        2,
        [{rank:'A',suit:'d'}]
    );

    await testRNJ('61 [R2]: R2 KK + 2 A used → KK 必上底 等 A/鬼',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.bottom, 'K') >= 2,
        2,
        [{rank:'A',suit:'d'},{rank:'A',suit:'h'}]
    );

    // 3 A used: 边界, 顶 (锁 Fantasy) 或底 (赌最后 1 A + 鬼) 都可, 不可中
    await testRNJ('62 [R2]: R2 KK + 3 A used → KK 顶或底 (不可中)',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.middle, 'K') < 2,
        2,
        [{rank:'A',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'}]
    );

    // 4 A used: deck 无 A, 必上顶 锁 K-Fantasy
    await testRNJ('63 [R2]: R2 KK + 4 A used → KK 必上顶 锁 Fantasy',
        { top: [{rank:'Q',suit:'d'}], middle: [{rank:'5',suit:'c'},{rank:'6',suit:'c'}],
          bottom: [{rank:'3',suit:'h'},{rank:'9',suit:'s'}] },
        [{rank:'K',suit:'h'},{rank:'K',suit:'s'},{rank:'4',suit:'d'}],
        2,
        r => cntRank(r.top, 'K') >= 2,  // KK 必上顶
        2,
        [{rank:'A',suit:'d'},{rank:'A',suit:'h'},{rank:'A',suit:'s'},{rank:'A',suit:'c'}]
    );

    // ===== R5 余下 cases =====

    // RN40 删除 — state 必爆 (top AA 锁, mid 6c-high, A-high > 6 → foul)

    console.log(`\n=== 结果: ${passed}通过 / ${failed}失败 / ${passed+failed}总计 ===`);
})().catch(e => { console.error(e); process.exit(1); });
