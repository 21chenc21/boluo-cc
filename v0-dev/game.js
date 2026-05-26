// ============================================================
// 大菠萝 (Pineapple OFC) - Game Engine
// ============================================================

const RANKS = ['2','3','4','5','6','7','8','9','T','J','Q','K','A'];
const SUITS = ['c','d','h','s']; // clubs, diamonds, hearts, spades
const SUIT_SYMBOLS = { c:'♣', d:'♦', h:'♥', s:'♠' };
const SUIT_COLORS = { c:'black', d:'red', h:'red', s:'black' };
const RANK_DISPLAY = { '2':'2','3':'3','4':'4','5':'5','6':'6','7':'7','8':'8','9':'9','T':'10','J':'J','Q':'Q','K':'K','A':'A' };

function rankIndex(r) {
    if (r === 'X') return 12; // 鬼牌 heuristic: 视为最大 rank (仅用于 solver 启发式)
    return RANKS.indexOf(r);
}

// ============================================================
// 鬼牌 (Joker) 支持
// ============================================================
// 鬼牌表示: { rank:'X', suit:'j' }, 多张鬼牌共享此表示但在 deck 中用不同 id 区分
// cardId 对鬼牌返回 'Xj0' / 'Xj1' / ... 确保唯一

function isJoker(c) { return c && c.rank === 'X'; }

function countJokers(cards) { return cards.filter(isJoker).length; }

function parseCard(s) {
    // e.g. "Jc" => { rank:'J', suit:'c' }
    s = s.trim();
    const rank = s.length === 3 ? 'T' : s[0].toUpperCase();
    const suit = s[s.length - 1].toLowerCase();
    // Handle "10h" style
    if (s.length === 3 && s[0] === '1' && s[1] === '0') {
        return { rank: 'T', suit };
    }
    // Map rank aliases
    const rMap = {'1':'A','a':'A','k':'K','q':'Q','j':'J','t':'T'};
    const finalRank = rMap[rank.toLowerCase()] || rank.toUpperCase();
    return { rank: finalRank, suit };
}

function cardStr(card) {
    if (isJoker(card)) return '🃏';
    return RANK_DISPLAY[card.rank] + SUIT_SYMBOLS[card.suit];
}

function cardId(card) {
    // 鬼牌需要唯一 id (同 deck 多张鬼): 'Xj0','Xj1',...
    if (isJoker(card)) return 'Xj' + (card.jid ?? 0);
    return card.rank + card.suit;
}

// numJokers: 0/2/4. 0=标准 52 张 (与 v4_more 兼容)
function createDeck(numJokers = 0) {
    const deck = [];
    for (const s of SUITS) {
        for (const r of RANKS) {
            deck.push({ rank: r, suit: s });
        }
    }
    for (let i = 0; i < numJokers; i++) {
        deck.push({ rank: 'X', suit: 'j', jid: i });
    }
    return deck;
}

function shuffleDeck(deck) {
    const arr = [...deck];
    for (let i = arr.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [arr[i], arr[j]] = [arr[j], arr[i]];
    }
    return arr;
}

// ============================================================
// Hand Evaluation
// ============================================================

const HAND_TYPE = {
    HIGH_CARD: 0,
    PAIR: 1,
    TWO_PAIR: 2,
    THREE_OF_A_KIND: 3,
    STRAIGHT: 4,
    FLUSH: 5,
    FULL_HOUSE: 6,
    FOUR_OF_A_KIND: 7,
    STRAIGHT_FLUSH: 8,
    ROYAL_FLUSH: 9
};

const HAND_NAMES = {
    0: '高牌', 1: '一对', 2: '两对', 3: '三条',
    4: '顺子', 5: '同花', 6: '葫芦', 7: '四条',
    8: '同花顺', 9: '皇家同花顺'
};

const HAND_NAMES_TOP = {
    0: '高牌', 1: '一对', 3: '三条'
};

// 5 张 hand value 编码 (和 evaluate5 内的 makeValue 一致)
function make5Value(t, ...kickers) {
    let v = t * 1000000;
    for (let i = 0; i < kickers.length; i++) {
        v += kickers[i] * Math.pow(15, 4 - i);
    }
    return v;
}

function evaluate5(cards) {
    if (cards.length !== 5) return { type: -1, value: 0 };
    // 有鬼牌 → 自动分派到 joker 版本 (无 cap)
    if (cards.some(isJoker)) return evaluate5Joker(cards);

    const ranks = cards.map(c => rankIndex(c.rank)).sort((a, b) => b - a);
    const suits = cards.map(c => c.suit);

    // Count ranks
    const counts = {};
    for (const r of ranks) { counts[r] = (counts[r] || 0) + 1; }
    const groups = Object.entries(counts).map(([r, c]) => ({ rank: parseInt(r), count: c }));
    groups.sort((a, b) => b.count - a.count || b.rank - a.rank);

    const isFlush = suits.every(s => s === suits[0]);

    // Check straight
    let isStraight = false;
    const uniqueRanks = [...new Set(ranks)].sort((a, b) => b - a);
    if (uniqueRanks.length === 5) {
        if (uniqueRanks[0] - uniqueRanks[4] === 4) {
            isStraight = true;
        }
        // A-2-3-4-5 (wheel)
        if (uniqueRanks[0] === 12 && uniqueRanks[1] === 3 && uniqueRanks[2] === 2 && uniqueRanks[3] === 1 && uniqueRanks[4] === 0) {
            isStraight = true;
        }
    }

    // Determine hand type and value
    let type, value;
    const makeValue = (t, ...kickers) => {
        let v = t * 1000000;
        for (let i = 0; i < kickers.length; i++) {
            v += kickers[i] * Math.pow(15, 4 - i);
        }
        return v;
    };

    if (isFlush && isStraight) {
        if (uniqueRanks[0] === 12 && uniqueRanks[4] === 8) {
            type = HAND_TYPE.ROYAL_FLUSH;
            value = makeValue(9, 12);
        } else {
            type = HAND_TYPE.STRAIGHT_FLUSH;
            // Wheel straight flush
            const highCard = (uniqueRanks[0] === 12 && uniqueRanks[1] === 3) ? 3 : uniqueRanks[0];
            value = makeValue(8, highCard);
        }
    } else if (groups[0].count === 4) {
        type = HAND_TYPE.FOUR_OF_A_KIND;
        value = makeValue(7, groups[0].rank, groups[1].rank);
    } else if (groups[0].count === 3 && groups[1].count === 2) {
        type = HAND_TYPE.FULL_HOUSE;
        value = makeValue(6, groups[0].rank, groups[1].rank);
    } else if (isFlush) {
        type = HAND_TYPE.FLUSH;
        value = makeValue(5, ...uniqueRanks);
    } else if (isStraight) {
        type = HAND_TYPE.STRAIGHT;
        const highCard = (uniqueRanks[0] === 12 && uniqueRanks[1] === 3) ? 3 : uniqueRanks[0];
        value = makeValue(4, highCard);
    } else if (groups[0].count === 3) {
        type = HAND_TYPE.THREE_OF_A_KIND;
        const kickers = groups.filter(g => g.count === 1).map(g => g.rank).sort((a, b) => b - a);
        value = makeValue(3, groups[0].rank, ...kickers);
    } else if (groups[0].count === 2 && groups[1].count === 2) {
        type = HAND_TYPE.TWO_PAIR;
        const pairs = [groups[0].rank, groups[1].rank].sort((a, b) => b - a);
        value = makeValue(2, pairs[0], pairs[1], groups[2].rank);
    } else if (groups[0].count === 2) {
        type = HAND_TYPE.PAIR;
        const kickers = groups.filter(g => g.count === 1).map(g => g.rank).sort((a, b) => b - a);
        value = makeValue(1, groups[0].rank, ...kickers);
    } else {
        type = HAND_TYPE.HIGH_CARD;
        value = makeValue(0, ...uniqueRanks);
    }

    return { type, value, name: HAND_NAMES[type] };
}

// ============================================================
// 鬼牌判牌 (with cap support for 防犯规降级)
// ============================================================

// hand a 是否超过 hand b (都是 5 张): type 优先,再 value
function handExceeds5(a, b) {
    if (a.type > b.type) return true;
    if (a.type < b.type) return false;
    return a.value > b.value;
}

// 3 张 top vs 5 张 middle 的降级比较 (top > mid = 犯规)
function topExceedsMid(top, mid) {
    // top 只有 0/1/3 型 (高牌/对子/三条)
    if (top.type > mid.type) return true;
    if (top.type < mid.type) return false;
    if (top.type === 3) {
        // 三条: 比主 rank
        // top value = 3e6 + tripRank*15
        // mid value = 3e6 + tripRank*15^4 + ...
        const topTrip = Math.floor((top.value - 3000000) / 15);
        const midTrip = Math.floor((mid.value - 3000000) / Math.pow(15, 4));
        return topTrip > midTrip;
    }
    if (top.type === 1) {
        // 对子: 比主 rank, 再 kicker
        const topPair = Math.floor((top.value - 1000000) / 15);
        const topKicker = (top.value - 1000000) % 15;
        const midPair = Math.floor((mid.value - 1000000) / Math.pow(15, 4));
        if (topPair > midPair) return true;
        if (topPair < midPair) return false;
        // kickers: top 1 kicker, mid 3 kickers. top ≤ mid 只需最高 kicker 比
        const midKicker1 = Math.floor(((mid.value - 1000000) % Math.pow(15, 4)) / Math.pow(15, 3));
        return topKicker > midKicker1;
    }
    if (top.type === 0) {
        // 高牌: top 3 张,mid 5 张. value encoding 不同
        // top: r0*225+r1*15+r2
        // mid: r0*15^4+...+r4
        const topR0 = Math.floor(top.value / 225);
        const midR0 = Math.floor(mid.value / Math.pow(15, 4));
        return topR0 > midR0;
    }
    return false;
}

// 可用鬼牌替换 (避免同组已有 rank+suit 冲突)
function jokerSubstitutes(existingReal) {
    const used = new Set(existingReal.map(c => c.rank + c.suit));
    const subs = [];
    for (const s of SUITS) {
        for (const r of RANKS) {
            if (!used.has(r + s)) subs.push({ rank: r, suit: s });
        }
    }
    return subs;
}

// 评估缓存 (cardIds 排序 + cap 指纹作 key)
const _jokerEvalCache = new Map();
function _cacheKey(cards, cap) {
    const cardKey = cards.map(c => isJoker(c) ? 'X' : (c.rank + c.suit)).sort().join(',');
    const capKey = cap ? (cap.type + ':' + cap.value) : 'NC';
    return cardKey + '|' + capKey;
}

// 带 cap 的 5 张评估: cap = null 或 {type, value} (5张 hand)
// 返回最强且 ≤ cap 的 hand, 无鬼牌时退化为 evaluate5
function evaluate5Joker(cards, cap = null) {
    if (cards.length !== 5) return { type: -1, value: 0 };
    const ck = _cacheKey(cards, cap);
    if (_jokerEvalCache.has(ck)) return _jokerEvalCache.get(ck);

    const jokers = cards.filter(isJoker);
    const real = cards.filter(c => !isJoker(c));
    const k = jokers.length;

    if (k === 0) {
        const r = evaluate5(cards);
        if (cap && handExceeds5(r, cap)) {
            // 无鬼牌不能降级, 调用方需自己处理 (通常意味着犯规)
            return { ...r, overCap: true };
        }
        return r;
    }

    const subs = jokerSubstitutes(real);
    let best = null;
    const update = (r) => {
        if (cap && handExceeds5(r, cap)) return;
        if (!best || r.type > best.type || (r.type === best.type && r.value > best.value)) best = r;
    };

    // 全部 k ≥ 1 使用 analytical 枚举 (brute force 对 k=2+ 太慢)
    if (k >= 1 && k <= 4) {
        best = analyticalEval5(real, k, cap);
    } else if (k === 5) {
        // 全鬼: 默认 RF, cap 低则降级
        if (!cap || !handExceeds5({ type: 9, value: make5Value(9, 12) }, cap)) {
            best = { type: 9, value: make5Value(9, 12), name: '皇家同花顺' };
        } else {
            best = analyticalEval5([], 5, cap);
        }
    }

    if (_jokerEvalCache.size > 50000) _jokerEvalCache.clear();
    _jokerEvalCache.set(ck, best);
    return best;
}

// 带 cap 的 3 张评估 (头道) — cached
const _jokerEval3Cache = new Map();
function evaluate3Joker(cards, cap = null) {
    if (cards.length !== 3) return { type: -1, value: 0 };
    const ck = _cacheKey(cards, cap);
    if (_jokerEval3Cache.has(ck)) return _jokerEval3Cache.get(ck);

    const jokers = cards.filter(isJoker);
    const real = cards.filter(c => !isJoker(c));
    const k = jokers.length;

    if (k === 0) {
        const r = evaluate3(cards);
        if (cap && topExceedsMid(r, cap)) return { ...r, overCap: true };
        return r;
    }

    const subs = jokerSubstitutes(real);
    let best = null;
    const update = (r) => {
        if (cap && topExceedsMid(r, cap)) return;
        if (!best || r.type > best.type || (r.type === best.type && r.value > best.value)) best = r;
    };

    // 全 analytical 构造 {type, value}
    const rCount = {}; // realRank -> count
    for (const c of real) rCount[rankIndex(c.rank)] = (rCount[rankIndex(c.rank)] || 0) + 1;
    const realRanks = Object.keys(rCount).map(Number);
    const allDistinct = realRanks.every(r => rCount[r] === 1);

    // 三条 (type 3)
    for (let tr = 12; tr >= 0; tr--) {
        const got = rCount[tr] || 0;
        const need = 3 - got;
        if (need < 0 || need > k) continue;
        // 其余 reals 必须无 (否则多余卡)
        if (real.length - got > 0) continue;
        update({ type: 3, value: 3 * 1000000 + tr * 15, name: '三条' });
    }

    // 对子 (type 1)
    for (let pr = 12; pr >= 0; pr--) {
        const gotP = rCount[pr] || 0;
        if (gotP > 2) continue;
        const need = 2 - gotP;
        if (need > k) continue;
        const rOther = real.length - gotP;
        if (rOther + (k - need) !== 1) continue;
        let kicker;
        if (rOther === 1) {
            kicker = realRanks.find(rk => rk !== pr);
        } else {
            kicker = (pr === 12) ? 11 : 12;
        }
        update({ type: 1, value: 1 * 1000000 + pr * 15 + kicker, name: '一对' });
    }

    // 高牌 (type 0, fallback)
    if (!best && allDistinct) {
        const ranks = [...realRanks];
        for (let r = 12; r >= 0 && ranks.length < 3; r--) if (!rCount[r]) ranks.push(r);
        ranks.sort((a,b) => b-a);
        const [r0, r1, r2] = ranks;
        update({ type: 0, value: r0 * 225 + r1 * 15 + r2, name: '高牌' });
    }

    if (_jokerEval3Cache.size > 50000) _jokerEval3Cache.clear();
    _jokerEval3Cache.set(ck, best);
    return best;
}

// k≥1 的 5 张分析式评估 — 直接构造 {type, value} 跳过 evaluate5 回调 (10-30x 提速)
function analyticalEval5(real, k, cap = null) {
    let best = null;
    const update = (r) => {
        if (cap && handExceeds5(r, cap)) return;
        if (!best || r.type > best.type || (r.type === best.type && r.value > best.value)) best = r;
    };

    // 预计算 real 的统计
    const realRanks = real.map(c => rankIndex(c.rank));
    const realRanksSet = new Set(realRanks);
    const rankCount = {}; // rank -> count
    for (const r of realRanks) rankCount[r] = (rankCount[r] || 0) + 1;
    const suitCount = {}; // suit -> count
    for (const c of real) suitCount[c.suit] = (suitCount[c.suit] || 0) + 1;
    const allDistinct = realRanks.length === realRanksSet.size;

    // ===== Royal Flush (type 9) =====
    for (const s of SUITS) {
        if (real.some(c => c.suit !== s)) continue;
        if (!real.every(c => rankIndex(c.rank) >= 8)) continue;
        if (!allDistinct) continue;
        if (5 - real.length <= k) { update({ type: 9, value: make5Value(9, 12), name: '皇家同花顺' }); break; }
    }

    // ===== Straight Flush (type 8) =====
    for (const s of SUITS) {
        if (real.some(c => c.suit !== s)) continue;
        if (!allDistinct) continue;
        for (let high = 4; high <= 12; high++) {
            const sfRanks = [high-4, high-3, high-2, high-1, high];
            if (!realRanks.every(r => sfRanks.includes(r))) continue;
            if (5 - real.length > k) continue;
            update({ type: 8, value: make5Value(8, high), name: '同花顺' });
        }
        // Wheel
        const wheelRanks = [0,1,2,3,12];
        if (realRanks.every(r => wheelRanks.includes(r)) && 5 - real.length <= k) {
            update({ type: 8, value: make5Value(8, 3), name: '同花顺' });
        }
    }

    // ===== Four of a Kind (type 7) =====
    for (let r = 12; r >= 0; r--) {
        const got = rankCount[r] || 0;
        const need = 4 - got;
        if (need < 0 || need > k) continue;
        const remainingK = k - need;
        // 剩余 reals (非 r) + remainingK jokers 组成 1 张 kicker
        const realNotR = real.length - got;
        if (realNotR + remainingK !== 1) continue;
        let kickerRank;
        if (realNotR === 1) {
            // 找那张非 r 的 real
            kickerRank = realRanks.find(rk => rk !== r);
        } else {
            kickerRank = (r === 12) ? 11 : 12;
        }
        update({ type: 7, value: make5Value(7, r, kickerRank), name: '四条' });
    }

    // ===== Full House (type 6) =====
    for (let tr = 12; tr >= 0; tr--) {
        for (let pr = 12; pr >= 0; pr--) {
            if (tr === pr) continue;
            const gotT = rankCount[tr] || 0;
            const gotP = rankCount[pr] || 0;
            if (gotT > 3 || gotP > 2) continue;
            const needT = 3 - gotT, needP = 2 - gotP;
            if (needT + needP > k) continue;
            // 除 tr/pr 外不能有其他 real
            if (real.length - gotT - gotP > 0) continue;
            update({ type: 6, value: make5Value(6, tr, pr), name: '葫芦' });
        }
    }

    // ===== Flush (type 5) =====
    for (const s of SUITS) {
        if (real.some(c => c.suit !== s)) continue;
        if (!allDistinct) continue;
        if (5 - real.length > k) continue;
        const ranks = [...realRanks];
        for (let r = 12; r >= 0 && ranks.length < 5; r--) if (!realRanksSet.has(r)) ranks.push(r);
        ranks.sort((a,b) => b-a);
        update({ type: 5, value: make5Value(5, ...ranks.slice(0,5)), name: '同花' });
    }

    // ===== Straight (type 4, 需要 ≥2 花色以免被当 SF) =====
    // 如果 real 全同花, 构造 Straight 会被识别为 SF. 这里直接返回 Straight 值 (但 SF 已在上面 update 过, 取 max 即可)
    for (let high = 4; high <= 12; high++) {
        const sRanks = [high-4, high-3, high-2, high-1, high];
        if (!allDistinct) continue;
        if (!realRanks.every(r => sRanks.includes(r))) continue;
        if (5 - real.length > k) continue;
        update({ type: 4, value: make5Value(4, high), name: '顺子' });
    }
    // Wheel
    {
        const wheelRanks = [0,1,2,3,12];
        if (allDistinct && realRanks.every(r => wheelRanks.includes(r)) && 5 - real.length <= k) {
            update({ type: 4, value: make5Value(4, 3), name: '顺子' });
        }
    }

    // ===== Three of a Kind (type 3) =====
    for (let tr = 12; tr >= 0; tr--) {
        const gotT = rankCount[tr] || 0;
        const needT = 3 - gotT;
        if (needT < 0 || needT > k) continue;
        const remainingK = k - needT;
        const realOther = real.length - gotT;
        if (realOther + remainingK !== 2) continue;
        // 2 张 kickers (不同 rank, 非 tr)
        const otherRs = realRanks.filter(rk => rk !== tr);
        if (new Set(otherRs).size !== otherRs.length) continue;
        const kickers = [...otherRs];
        for (let r = 12; r >= 0 && kickers.length < 2; r--) {
            if (r === tr || kickers.includes(r)) continue;
            kickers.push(r);
        }
        if (kickers.length !== 2) continue;
        kickers.sort((a,b) => b-a);
        update({ type: 3, value: make5Value(3, tr, kickers[0], kickers[1]), name: '三条' });
    }

    // ===== Two Pair (type 2) =====
    for (let pr1 = 12; pr1 >= 0; pr1--) {
        for (let pr2 = pr1 - 1; pr2 >= 0; pr2--) {
            const g1 = rankCount[pr1] || 0, g2 = rankCount[pr2] || 0;
            if (g1 > 2 || g2 > 2) continue;
            const need = (2 - g1) + (2 - g2);
            if (need > k) continue;
            const rOther = real.length - g1 - g2;
            if (rOther + (k - need) !== 1) continue;
            let kicker;
            if (rOther === 1) {
                kicker = realRanks.find(rk => rk !== pr1 && rk !== pr2);
            } else {
                for (let r = 12; r >= 0; r--) if (r !== pr1 && r !== pr2) { kicker = r; break; }
            }
            update({ type: 2, value: make5Value(2, pr1, pr2, kicker), name: '两对' });
        }
    }

    // ===== Pair (type 1) =====
    for (let pr = 12; pr >= 0; pr--) {
        const gP = rankCount[pr] || 0;
        if (gP > 2) continue;
        const need = 2 - gP;
        if (need > k) continue;
        const rOther = real.length - gP;
        if (rOther + (k - need) !== 3) continue;
        const otherRs = realRanks.filter(rk => rk !== pr);
        if (new Set(otherRs).size !== otherRs.length) continue;
        const kickers = [...otherRs];
        for (let r = 12; r >= 0 && kickers.length < 3; r--) {
            if (r === pr || kickers.includes(r)) continue;
            kickers.push(r);
        }
        if (kickers.length !== 3) continue;
        kickers.sort((a,b) => b-a);
        update({ type: 1, value: make5Value(1, pr, kickers[0], kickers[1], kickers[2]), name: '一对' });
    }

    // ===== High Card (type 0, fallback) =====
    if (!best && allDistinct) {
        const ranks = [...realRanks];
        for (let r = 12; r >= 0 && ranks.length < 5; r--) if (!realRanksSet.has(r)) ranks.push(r);
        ranks.sort((a,b) => b-a);
        update({ type: 0, value: make5Value(0, ...ranks.slice(0,5)), name: '高牌' });
    }

    return best;
}

function evaluate3(cards) {
    if (cards.length !== 3) return { type: -1, value: 0 };
    if (cards.some(isJoker)) return evaluate3Joker(cards);

    const ranks = cards.map(c => rankIndex(c.rank)).sort((a, b) => b - a);
    const counts = {};
    for (const r of ranks) { counts[r] = (counts[r] || 0) + 1; }
    const groups = Object.entries(counts).map(([r, c]) => ({ rank: parseInt(r), count: c }));
    groups.sort((a, b) => b.count - a.count || b.rank - a.rank);

    let type, value;

    if (groups[0].count === 3) {
        type = HAND_TYPE.THREE_OF_A_KIND;
        value = 3 * 1000000 + groups[0].rank * 15;
    } else if (groups[0].count === 2) {
        type = HAND_TYPE.PAIR;
        value = 1 * 1000000 + groups[0].rank * 15 + groups[1].rank;
    } else {
        type = HAND_TYPE.HIGH_CARD;
        value = ranks[0] * 225 + ranks[1] * 15 + ranks[2];
    }

    return { type, value, name: HAND_NAMES_TOP[type] || HAND_NAMES[type] };
}

// ============================================================
// Scoring & Royalties (大菠萝计分)
// ============================================================

function getTopRoyalty(cards) {
    if (cards.length !== 3) return 0;
    const eval3 = evaluate3(cards);
    const ranks = cards.map(c => rankIndex(c.rank));
    const counts = {};
    for (const r of ranks) { counts[r] = (counts[r] || 0) + 1; }

    // Three of a kind on top: 222=10, 333=11, ..., AAA=22
    if (eval3.type === HAND_TYPE.THREE_OF_A_KIND) {
        const tripRank = parseInt(Object.entries(counts).find(([r, c]) => c === 3)[0]);
        return tripRank + 10;
    }

    // Pairs on top: 66=1, 77=2, 88=3, 99=4, TT=5, JJ=6, QQ=7, KK=8, AA=9
    if (eval3.type === HAND_TYPE.PAIR) {
        const pairRank = Object.entries(counts).find(([r, c]) => c === 2);
        if (pairRank) {
            const ri = parseInt(pairRank[0]);
            if (ri >= 4) return ri - 3; // 66(ri=4)=1, ..., AA(ri=12)=9
        }
    }
    return 0;
}

function getMiddleRoyalty(cards) {
    if (cards.length !== 5) return 0;
    const eval5 = evaluate5(cards);
    switch (eval5.type) {
        case HAND_TYPE.THREE_OF_A_KIND: return 2;
        case HAND_TYPE.STRAIGHT: return 4;
        case HAND_TYPE.FLUSH: return 8;
        case HAND_TYPE.FULL_HOUSE: return 12;
        case HAND_TYPE.FOUR_OF_A_KIND: return 20;
        case HAND_TYPE.STRAIGHT_FLUSH: return 30;
        case HAND_TYPE.ROYAL_FLUSH: return 50;
        default: return 0;
    }
}

function getBottomRoyalty(cards) {
    if (cards.length !== 5) return 0;
    const eval5 = evaluate5(cards);
    switch (eval5.type) {
        case HAND_TYPE.STRAIGHT: return 2;
        case HAND_TYPE.FLUSH: return 4;
        case HAND_TYPE.FULL_HOUSE: return 6;
        case HAND_TYPE.FOUR_OF_A_KIND: return 10;
        case HAND_TYPE.STRAIGHT_FLUSH: return 15;
        case HAND_TYPE.ROYAL_FLUSH: return 25;
        default: return 0;
    }
}

// 统一入口: 按 bot → mid → top 顺序评估, 后续行被前一行 cap 限制 (用于鬼牌降级)
// 返回 { top, middle, bottom, foul } 其中每个是 hand 对象
function evaluateBoardJoker(top, middle, bottom) {
    const hasJokers = [...top, ...middle, ...bottom].some(isJoker);
    if (!hasJokers) {
        const t = top.length === 3 ? evaluate3(top) : { type:-1, value:0 };
        const m = middle.length === 5 ? evaluate5(middle) : { type:-1, value:0 };
        const b = bottom.length === 5 ? evaluate5(bottom) : { type:-1, value:0 };
        const foul = (top.length===3 && middle.length===5 && bottom.length===5) && isFoul(top, middle, bottom);
        return { top: t, middle: m, bottom: b, foul };
    }
    const b = bottom.length === 5 ? evaluate5Joker(bottom) : { type:-1, value:0 };
    const m = middle.length === 5 ? evaluate5Joker(middle, b) : { type:-1, value:0 };
    const t = top.length === 3 ? evaluate3Joker(top, m) : { type:-1, value:0 };
    // 完整牌局才判犯规
    let foul = false;
    if (top.length === 3 && middle.length === 5 && bottom.length === 5) {
        if (!b || !m || !t) foul = true;
        else if (b.overCap || m.overCap || t.overCap) foul = true;
        else if (handExceeds5(m, b)) foul = true;
        else if (topExceedsMid(t, m)) foul = true;
    }
    return { top: t, middle: m, bottom: b, foul };
}

function isFoul(top, middle, bottom) {
    if (top.length !== 3 || middle.length !== 5 || bottom.length !== 5) return true;
    const topEval = evaluate3(top);
    const midEval = evaluate5(middle);
    const botEval = evaluate5(bottom);

    // 底道 >= 中道 (both 5-card, direct compare)
    if (midEval.value > botEval.value) return true;

    // 中道 >= 头道 (cross-size: compare by hand type first, then rank)
    // Hand types: HIGH_CARD=0, PAIR=1, TWO_PAIR=2, THREE_OF_A_KIND=3, ...
    // Top can only be: HIGH_CARD, PAIR, THREE_OF_A_KIND
    if (midEval.type < topEval.type) return true;
    if (midEval.type === topEval.type) {
        // Same type, compare primary rank
        const topRanks = top.map(c => rankIndex(c.rank)).sort((a, b) => b - a);
        const midRanks = middle.map(c => rankIndex(c.rank)).sort((a, b) => b - a);
        const topCounts = {};
        for (const r of topRanks) topCounts[r] = (topCounts[r] || 0) + 1;
        const midCounts = {};
        for (const r of midRanks) midCounts[r] = (midCounts[r] || 0) + 1;

        if (topEval.type === HAND_TYPE.THREE_OF_A_KIND) {
            const topTrip = parseInt(Object.entries(topCounts).find(([r,c]) => c===3)[0]);
            const midTrip = parseInt(Object.entries(midCounts).find(([r,c]) => c===3)[0]);
            if (midTrip < topTrip) return true;
        } else if (topEval.type === HAND_TYPE.PAIR) {
            const topPair = parseInt(Object.entries(topCounts).find(([r,c]) => c===2)[0]);
            const midPair = parseInt(Object.entries(midCounts).find(([r,c]) => c===2)[0]);
            if (midPair < topPair) return true;
            if (midPair === topPair) {
                // Compare kickers
                const topKickers = Object.entries(topCounts).filter(([r,c]) => c===1).map(([r]) => parseInt(r)).sort((a,b) => b-a);
                const midKickers = Object.entries(midCounts).filter(([r,c]) => c===1).map(([r]) => parseInt(r)).sort((a,b) => b-a);
                for (let i = 0; i < topKickers.length && i < midKickers.length; i++) {
                    if (midKickers[i] < topKickers[i]) return true;
                    if (midKickers[i] > topKickers[i]) break;
                }
            }
        } else {
            // HIGH_CARD: compare card by card
            for (let i = 0; i < topRanks.length && i < midRanks.length; i++) {
                if (midRanks[i] < topRanks[i]) return true;
                if (midRanks[i] > topRanks[i]) break;
            }
        }
    }

    return false;
}

// 基于评估后的 top hand 判断 fantasy (兼容鬼牌 downgrade)
function isFantasyLandFromEval(topEval) {
    if (!topEval || topEval.type < 0) return false;
    if (topEval.type === 3) return true; // 三条
    if (topEval.type === 1) {
        // 对子 rank ≥ Q (rank 10)
        const pairRank = Math.floor((topEval.value - 1000000) / 15);
        return pairRank >= 10;
    }
    return false;
}

// 原 signature (兼容 v4 无鬼牌代码)
function isFantasyLand(top) {
    if (top.length !== 3) return false;
    if (top.some(isJoker)) {
        return isFantasyLandFromEval(evaluate3Joker(top));
    }
    const ranks = top.map(c => rankIndex(c.rank));
    const counts = {};
    for (const r of ranks) { counts[r] = (counts[r] || 0) + 1; }
    for (const [r, c] of Object.entries(counts)) {
        if (c === 3) return true;
        if (c === 2 && parseInt(r) >= 10) return true;
    }
    return false;
}

// 基于评估后的 hand 算 royalty (兼容鬼牌 downgrade)
function getTopRoyaltyFromEval(topEval) {
    if (!topEval || topEval.type < 0) return 0;
    if (topEval.type === 3) {
        // 三条: 222=10 ... AAA=22
        const tripRank = Math.floor((topEval.value - 3000000) / 15);
        return tripRank + 10;
    }
    if (topEval.type === 1) {
        const pairRank = Math.floor((topEval.value - 1000000) / 15);
        if (pairRank >= 4) return pairRank - 3; // 66(ri=4)=1 ... AA(ri=12)=9
    }
    return 0;
}

function getMiddleRoyaltyFromEval(midEval) {
    if (!midEval) return 0;
    switch (midEval.type) {
        case 3: return 2;
        case 4: return 4;
        case 5: return 8;
        case 6: return 12;
        case 7: return 20;
        case 8: return 30;
        case 9: return 50;
        default: return 0;
    }
}

function getBottomRoyaltyFromEval(botEval) {
    if (!botEval) return 0;
    switch (botEval.type) {
        case 4: return 2;
        case 5: return 4;
        case 6: return 6;
        case 7: return 10;
        case 8: return 15;
        case 9: return 25;
        default: return 0;
    }
}

function scoreHand(top, middle, bottom) {
    if (top.length !== 3 || middle.length !== 5 || bottom.length !== 5) {
        return { foul: true, score: -100, royalties: 0, fantasy: false, detail: '未完成' };
    }

    const hasJoker = [...top, ...middle, ...bottom].some(isJoker);
    let topEval, midEval, botEval, foul;

    if (hasJoker) {
        const board = evaluateBoardJoker(top, middle, bottom);
        topEval = board.top;
        midEval = board.middle;
        botEval = board.bottom;
        foul = board.foul;
    } else {
        foul = isFoul(top, middle, bottom);
        topEval = evaluate3(top);
        midEval = evaluate5(middle);
        botEval = evaluate5(bottom);
    }

    if (foul) {
        return { foul: true, score: -20, royalties: 0, fantasy: false, detail: '犯规(炸)!' };
    }

    const topR = hasJoker ? getTopRoyaltyFromEval(topEval) : getTopRoyalty(top);
    const midR = hasJoker ? getMiddleRoyaltyFromEval(midEval) : getMiddleRoyalty(middle);
    const botR = hasJoker ? getBottomRoyaltyFromEval(botEval) : getBottomRoyalty(bottom);
    const totalR = topR + midR + botR;
    const fantasy = isFantasyLandFromEval(topEval);

    // score = 真实 royalty 之和, 不再加 +50 fantasy 奖励 (那是 v7 训练用的 reward shaping)
    // 进范本身的价值通过下手拿大牌体现, 不是直接加分
    return {
        foul: false,
        score: totalR,
        royalties: totalR,
        topRoyalty: topR,
        midRoyalty: midR,
        botRoyalty: botR,
        fantasy,
        topEval, midEval, botEval,
        detail: fantasy ? `进入范特西! (royalty:${totalR})` : `得分: ${totalR}`
    };
}

// ============================================================
// Game State
// ============================================================

class GameState {
    constructor(numJokers = 0) {
        this.top = [];
        this.middle = [];
        this.bottom = [];
        this.round = 0;
        this.history = [];
        this.deck = [];
        this.usedCards = new Set();
        this.numJokers = numJokers; // 0 / 2 / 4
    }

    clone() {
        const gs = new GameState(this.numJokers);
        gs.top = [...this.top];
        gs.middle = [...this.middle];
        gs.bottom = [...this.bottom];
        gs.round = this.round;
        gs.history = this.history.map(h => ({...h}));
        gs.deck = [...this.deck];
        gs.usedCards = new Set(this.usedCards);
        // 传播 instance 级 getRemainingDeck 重写 (visibility-aware)
        if (Object.prototype.hasOwnProperty.call(this, 'getRemainingDeck')) {
            gs.getRemainingDeck = this.getRemainingDeck;
        }
        return gs;
    }

    topSlots() { return 3 - this.top.length; }
    midSlots() { return 5 - this.middle.length; }
    botSlots() { return 5 - this.bottom.length; }
    totalSlots() { return this.topSlots() + this.midSlots() + this.botSlots(); }

    canPlace(row) {
        if (row === 'top') return this.topSlots() > 0;
        if (row === 'middle') return this.midSlots() > 0;
        if (row === 'bottom') return this.botSlots() > 0;
        return false;
    }

    placeCard(card, row) {
        if (row === 'top' && this.topSlots() > 0) {
            this.top.push(card);
        } else if (row === 'middle' && this.midSlots() > 0) {
            this.middle.push(card);
        } else if (row === 'bottom' && this.botSlots() > 0) {
            this.bottom.push(card);
        }
        this.usedCards.add(cardId(card));
    }

    isComplete() {
        return this.top.length === 3 && this.middle.length === 5 && this.bottom.length === 5;
    }

    getScore() {
        return scoreHand(this.top, this.middle, this.bottom);
    }

    getRemainingDeck() {
        return createDeck(this.numJokers).filter(c => !this.usedCards.has(cardId(c)));
    }
}

// ============================================================
// Action Generation
// ============================================================

function generatePlacements(cards, state) {
    // Generate all valid ways to place `cards` into the three rows
    const results = [];
    const rows = ['top', 'middle', 'bottom'];

    function backtrack(idx, placements) {
        if (idx === cards.length) {
            results.push([...placements]);
            return;
        }
        for (const row of rows) {
            // Count how many cards we're placing in each row
            const placed = { top: 0, middle: 0, bottom: 0 };
            for (const p of placements) placed[p]++;

            const availTop = state.topSlots() - placed.top;
            const availMid = state.midSlots() - placed.middle;
            const availBot = state.botSlots() - placed.bottom;

            if (row === 'top' && availTop > 0) {
                placements.push('top');
                backtrack(idx + 1, placements);
                placements.pop();
            } else if (row === 'middle' && availMid > 0) {
                placements.push('middle');
                backtrack(idx + 1, placements);
                placements.pop();
            } else if (row === 'bottom' && availBot > 0) {
                placements.push('bottom');
                backtrack(idx + 1, placements);
                placements.pop();
            }
        }
    }

    backtrack(0, []);
    return results;
}

function generateRound1Actions(cards, state) {
    // Round 1: place all 5 cards
    return generatePlacements(cards, state);
}

function generateRoundNActions(cards, state) {
    // Rounds 2-5: choose 1 to discard, place remaining 2
    const actions = [];
    for (let discard = 0; discard < cards.length; discard++) {
        const kept = cards.filter((_, i) => i !== discard);
        const placements = generatePlacements(kept, state);
        for (const placement of placements) {
            actions.push({ discard: discard, kept, placement });
        }
    }
    return actions;
}

// ============================================================
// 3人对局管理
// ============================================================

class ThreePlayerGame {
    constructor(numJokers = 0) {
        this.numJokers = numJokers;
        this.deck = shuffleDeck(createDeck(numJokers));
        this.deckIdx = 0;
        this.players = [new GameState(numJokers), new GameState(numJokers), new GameState(numJokers)];
        this.round = 0;
        // 所有场上可见牌 (3人已放的牌)
        this.visibleCards = new Set();
        // 所有已发出的牌 (已放+已弃, 用于发牌排除)
        this.dealtCards = new Set();
    }

    // 发牌给指定玩家
    dealCards(playerIdx, count) {
        const cards = [];
        for (let i = 0; i < count; i++) {
            if (this.deckIdx >= this.deck.length) break;
            cards.push(this.deck[this.deckIdx++]);
        }
        // 标记为已发出
        for (const c of cards) this.dealtCards.add(cardId(c));
        return cards;
    }

    // 玩家放牌 (放的牌变为场上可见)
    placeCard(playerIdx, card, row) {
        this.players[playerIdx].placeCard(card, row);
        this.visibleCards.add(cardId(card));
    }

    // 玩家弃牌 (弃的牌不可见, 但从牌堆排除)
    discardCard(playerIdx, card) {
        this.players[playerIdx].usedCards.add(cardId(card));
        // dealtCards已在发牌时标记, 不需要额外操作
    }

    // 获取某玩家视角的剩余牌 (排除: 自己用的 + 场上所有可见的)
    getRemainingForPlayer(playerIdx) {
        const excluded = new Set(this.visibleCards);
        // 加上自己弃的牌 (自己知道)
        for (const cid of this.players[playerIdx].usedCards) {
            excluded.add(cid);
        }
        return createDeck(this.numJokers).filter(c => !excluded.has(cardId(c)));
    }

    // 获取真实剩余牌堆 (排除所有已发出的牌)
    getTrueRemaining() {
        return createDeck(this.numJokers).filter(c => !this.dealtCards.has(cardId(c)));
    }

    // 获取场上所有可见牌数组
    getVisibleCards() {
        return [...this.visibleCards];
    }

    // 获取某玩家视角: 能看到的其他玩家的牌
    getOpponentCards(playerIdx) {
        const cards = [];
        for (let i = 0; i < 3; i++) {
            if (i === playerIdx) continue;
            cards.push(...this.players[i].top, ...this.players[i].middle, ...this.players[i].bottom);
        }
        return cards;
    }

    // === Self-play 范特西扩展 ===
    // 一次性放完整 FL 手 (top/middle/bottom 都满) + 标记弃牌
    placeFantasyHand(playerIdx, layout) {
        const p = this.players[playerIdx];
        for (const c of layout.top) p.top.push(c);
        for (const c of layout.middle) p.middle.push(c);
        for (const c of layout.bottom) p.bottom.push(c);
        for (const c of [...layout.top, ...layout.middle, ...layout.bottom]) {
            this.visibleCards.add(cardId(c));
            p.usedCards.add(cardId(c));
        }
        for (const c of layout.discards || []) {
            p.usedCards.add(cardId(c));
        }
    }

    // 重置玩家板 (新 hand 开始) — 保留 dealtCards/visibleCards (跨手累积)
    // 实际上 OFC 每手是独立的, 牌堆每手重洗。这里我们不重置牌堆 (Self-play episode 内部所有手共用一副牌? 还是每手新洗?)
    // 简化: 每个 episode 用同一副牌, 逐手发出 — 这模拟 "1 个 episode = 1 个连续打" 的实际游戏
    // 不,标准 OFC 是每手独立洗牌。让 Self-play train script 决定。
    resetPlayerBoards() {
        for (let i = 0; i < 3; i++) {
            this.players[i].top = [];
            this.players[i].middle = [];
            this.players[i].bottom = [];
            this.players[i].round = 0;
            this.players[i].usedCards = new Set();
            this.players[i].history = [];
        }
        this.visibleCards = new Set();
    }

    // 完全重置 + 新洗牌 (新 hand)
    newHand() {
        this.deck = shuffleDeck(createDeck(this.numJokers));
        this.deckIdx = 0;
        this.dealtCards = new Set();
        this.resetPlayerBoards();
        this.round = 0;
    }
}

// ============================================================
// Fantasy Land 触发判定 + 计分辅助 (Self-play 用)
// ============================================================

// 检查玩家完成手牌后是否触发范特西
// fromFantasy: 该玩家本手是否在范内 (true=范内→检查 re-enter, false=普通手→检查 enter)
// 返回: null / 'QQ' / 'KK' / 'AA' / 'trips' / 're-enter'
function checkFantasyTrigger(playerState, fromFantasy) {
    const { top, middle, bottom } = playerState;
    if (top.length !== 3 || middle.length !== 5 || bottom.length !== 5) return null;
    const hasJoker = [...top, ...middle, ...bottom].some(isJoker);
    let topEval, botEval, foul;
    if (hasJoker) {
        const board = evaluateBoardJoker(top, middle, bottom);
        topEval = board.top; botEval = board.bottom; foul = board.foul;
    } else {
        foul = isFoul(top, middle, bottom);
        topEval = evaluate3(top);
        botEval = evaluate5(bottom);
    }
    if (foul) return null;

    if (fromFantasy) {
        // 范内: 顶 trips 或 底 4-of-a-kind+ 触发再范
        if (topEval.type >= 3) return 're-enter';   // top trips
        if (botEval.type >= 7) return 're-enter';   // bot 4-of-a-kind/SF/RF (HAND_TYPE.FOUR_OF_A_KIND=7)
        return null;
    }
    // 普通手 → 进范
    if (topEval.type >= 3) return 'trips';
    if (topEval.type === 1) {
        // 用 topEval (cap-chain 已 cap 过) 取对子 rank, 不再自己重算
        // 之前的 bug: 顶 [X,A,5] mid=pair-9 时, joker 必须 cap 到 5 (否则爆),
        // 但手算 pairR=A → 误判 AA 触发. 现走 topEval.value, joker-aware cap 自动正确.
        const pairR = Math.floor((topEval.value - 1000000) / 15);
        if (pairR >= 12) return 'AA';
        if (pairR >= 11) return 'KK';
        if (pairR >= 10) return 'QQ';
    }
    return null;
}

function getFantasyDealCount(triggerType) {
    switch (triggerType) {
        case 'QQ': return 14;
        case 'KK': return 15;
        case 'AA': return 16;
        case 'trips': return 17;
        case 're-enter': return 17;
        default: return 0;
    }
}

// Head-to-head net: A 对 B 的净分 (B 对 A 取负)
// 用 scoreHand() 返回的对象作输入
function headToHeadNet(scoreA, scoreB) {
    if (scoreA.foul && scoreB.foul) return 0;
    if (scoreA.foul) return -(6 + scoreB.score);
    if (scoreB.foul) return 6 + scoreA.score;
    // 3 行比较
    let aWins = 0;
    if (scoreA.botEval.value > scoreB.botEval.value) aWins++;
    else if (scoreA.botEval.value < scoreB.botEval.value) aWins--;
    if (scoreA.midEval.value > scoreB.midEval.value) aWins++;
    else if (scoreA.midEval.value < scoreB.midEval.value) aWins--;
    if (scoreA.topEval.value > scoreB.topEval.value) aWins++;
    else if (scoreA.topEval.value < scoreB.topEval.value) aWins--;
    let net = aWins;
    if (aWins === 3) net += 3;       // sweep bonus
    else if (aWins === -3) net -= 3;
    net += scoreA.score - scoreB.score; // royalty + fantasy 差额
    return net;
}

// 一手三玩家分计: 返回 { nets: [p0,p1,p2], scores: [s0,s1,s2] }
function episodeHandScore(players) {
    const scores = players.map(p => scoreHand(p.top, p.middle, p.bottom));
    const nets = [0, 0, 0];
    for (let i = 0; i < 3; i++) {
        for (let j = 0; j < 3; j++) {
            if (i === j) continue;
            nets[i] += headToHeadNet(scores[i], scores[j]);
        }
    }
    return { nets, scores };
}
