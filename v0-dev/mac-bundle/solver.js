// ============================================================
// 大菠萝 MCTS Solver v3 — 专家策略 + 行权益估算 + 进化训练
// ============================================================

// 默认权重 (可通过训练优化)
const DEFAULT_WEIGHTS = {
    // 进范特西
    fantasyBonus: 600,
    // 犯规惩罚
    foulPenalty: -1500,
    // 头道对子加分
    topPairBase: 5,
    topPairPerRank: 3,
    topTripsBonus: 250,
    // Royalty权重
    royaltyWeight: 8,
    // 行强度权重 — 底道必须最强!
    botStrengthWeight: 6.0,
    midStrengthWeight: 2.5,
    topStrengthWeight: 0.3,
    // 顺序安全边际
    orderMarginWeight: 20,
    // draw潜力
    flushDrawWeight: 12,
    straightDrawWeight: 8,
    pairDrawWeight: 6,
    // 进范追求
    fantasyChaseWeight: 30,
    // 弃牌策略
    uselessDiscardBonus: 5,
};

// ============================================================
// 行权益估算 (Monte Carlo)
// ============================================================

function estimateRowEquity(partialCards, maxCards, remainingDeck, samples) {
    if (partialCards.length === maxCards) {
        const ev = maxCards === 3 ? evaluate3(partialCards) : evaluate5(partialCards);
        return { avgType: ev.type, avgValue: ev.value, royalty: maxCards === 3 ? getTopRoyalty(partialCards) : maxCards === 5 ? 0 : 0 };
    }
    if (partialCards.length === 0) {
        return { avgType: 0, avgValue: 0, royalty: 0 };
    }

    const slotsLeft = maxCards - partialCards.length;
    let totalType = 0, totalValue = 0, totalRoyalty = 0;
    const n = samples || 60;

    for (let s = 0; s < n; s++) {
        const shuffled = shuffleDeck(remainingDeck);
        const fill = shuffled.slice(0, slotsLeft);
        const fullHand = [...partialCards, ...fill];
        if (maxCards === 3) {
            const ev = evaluate3(fullHand);
            totalType += ev.type;
            totalValue += ev.value;
            totalRoyalty += getTopRoyalty(fullHand);
        } else {
            const ev = evaluate5(fullHand);
            totalType += ev.type;
            totalValue += ev.value;
        }
    }

    return { avgType: totalType / n, avgValue: totalValue / n, royalty: totalRoyalty / n };
}

// ============================================================
// Outs计算
// ============================================================

function countPairOuts(cards, remainingDeck) {
    // 已有牌的rank
    const ranks = cards.map(c => c.rank);
    const counts = {};
    for (const r of ranks) counts[r] = (counts[r] || 0) + 1;

    let outs = 0;
    for (const c of remainingDeck) {
        if (counts[c.rank]) outs++; // 能配对/三条
    }
    return outs;
}

function countFlushOuts(cards, remainingDeck) {
    if (cards.length < 2) return 0;
    const suitCounts = {};
    for (const c of cards) suitCounts[c.suit] = (suitCounts[c.suit] || 0) + 1;
    const best = Object.entries(suitCounts).sort((a, b) => b[1] - a[1])[0];
    if (!best || best[1] < 2) return 0;
    const targetSuit = best[0];
    const needed = 5 - best[1]; // 还需要几张
    if (needed > 3) return 0;
    return remainingDeck.filter(c => c.suit === targetSuit).length;
}

function countStraightOuts(cards, remainingDeck) {
    if (cards.length < 2) return 0;
    const ri = [...new Set(cards.map(c => rankIndex(c.rank)))].sort((a, b) => a - b);

    let bestOuts = 0;
    // 检查所有可能的5张顺子窗口
    for (let low = 0; low <= 9; low++) {
        const high = low + 4;
        const have = ri.filter(r => r >= low && r <= high).length;
        if (have >= cards.length - 1) { // 最多缺2张在窗口外
            const need = [];
            for (let r = low; r <= high; r++) {
                if (!ri.includes(r)) need.push(r);
            }
            if (need.length <= 3) {
                const outs = remainingDeck.filter(c => need.includes(rankIndex(c.rank))).length;
                bestOuts = Math.max(bestOuts, outs);
            }
        }
    }
    // A-2-3-4-5
    const wheelRanks = [0, 1, 2, 3, 12];
    const haveWheel = wheelRanks.filter(r => ri.includes(r)).length;
    if (haveWheel >= 2) {
        const needWheel = wheelRanks.filter(r => !ri.includes(r));
        if (needWheel.length <= 3) {
            const outs = remainingDeck.filter(c => needWheel.includes(rankIndex(c.rank))).length;
            bestOuts = Math.max(bestOuts, outs);
        }
    }

    return bestOuts;
}

// ============================================================
// 专家策略评估 (用于rollout和决策)
// ============================================================

class ExpertEvaluator {
    constructor(weights) {
        this.w = weights || DEFAULT_WEIGHTS;
    }

    // 完整状态评估
    evaluateComplete(state) {
        const score = state.getScore();
        if (score.foul) return this.w.foulPenalty;

        let value = 0;
        if (score.fantasy) value += this.w.fantasyBonus;
        value += score.royalties * this.w.royaltyWeight;
        value += score.topRoyalty * 5;
        // 用type + value细分
        value += score.botEval.type * this.w.botStrengthWeight;
        value += (score.botEval.value / 100000) * this.w.botStrengthWeight * 0.3;
        value += score.midEval.type * this.w.midStrengthWeight;
        value += (score.midEval.value / 100000) * this.w.midStrengthWeight * 0.3;
        return value;
    }

    // 部分状态评估 (关键函数)
    // 优先级: 不犯规 > 进范特西 > 得分最大化
    evaluatePartial(state, remainingDeck) {
        const { top, middle, bottom } = state;
        let score = 0;

        // ===== 完整状态: 直接用完整评估 =====
        if (top.length === 3 && middle.length === 5 && bottom.length === 5) {
            if (isFoul(top, middle, bottom)) return this.w.foulPenalty;
            return this.evaluateComplete(state);
        }

        const midSlots = 5 - middle.length;
        const botSlots = 5 - bottom.length;

        // ===== 第一优先级: 犯规风险 =====
        const foulRisk = this.estimateFoulRisk(state);
        if (foulRisk >= 0.95) return this.w.foulPenalty * 0.95;

        // 犯规风险惩罚: 分层处理
        // - 低风险(<0.5): 用较温和的惩罚, 允许有计划地冒险(如追范特西)
        // - 高风险(>=0.5): 快速升级惩罚, 逼近foulPenalty
        const totalPlaced = top.length + middle.length + bottom.length;
        const progressFactor = 0.3 + 0.7 * (totalPlaced / 13);
        if (foulRisk < 0.5) {
            // 温和惩罚: ~300 scale (不用foulPenalty的-1500)
            score -= foulRisk * 300 * progressFactor;
        } else {
            // 重惩罚: 接近foulPenalty
            score += this.w.foulPenalty * foulRisk * progressFactor;
        }

        // ===== 第二优先级: 范特西 (必须在犯规风险可控时才追) =====
        const fantasyAchieved = top.length === 3 && isFantasyLand(top);
        const topPairR = this.getMaxPairRank(top);

        if (fantasyAchieved) {
            // 范特西已达成 — 奖励与犯规风险挂钩
            score += this.w.fantasyBonus * Math.max(0, 0.7 - foulRisk);
        } else if (topPairR >= 10 && top.length >= 2) {
            // QQ+在头道 — 适度追范, 不过度
            if (foulRisk < 0.35) {
                score += this.w.fantasyChaseWeight * 2.0 * (1 - foulRisk * 2);
            } else if (foulRisk < 0.5) {
                score += this.w.fantasyChaseWeight * 0.5 * (1 - foulRisk);
            } else {
                score += this.w.fantasyChaseWeight * 0.05;
            }
        }

        // ===== 第三优先级: 行评估 + 得分 =====
        score += this.evalTop(top, remainingDeck);
        score += this.evalMiddle(middle, remainingDeck);
        score += this.evalBottom(bottom, remainingDeck);

        // ===== 行牌型趋势 =====
        score += this.evalOrdering(state, remainingDeck);

        // ===== 进范协同: 中底道支撑检查 =====
        if ((fantasyAchieved || topPairR >= 10) && top.length >= 2) {
            const midType = this.getPartialType(middle);
            const botType = this.getPartialType(bottom);

            // 层级正确时加分
            if (midType >= HAND_TYPE.PAIR && botType >= HAND_TYPE.PAIR) {
                const midPairR = this.getMaxPairRank(middle);
                const botPairR = this.getMaxPairRank(bottom);
                if (botPairR > midPairR) score += 50;
                if (midPairR > topPairR || midType > HAND_TYPE.PAIR) score += 30;
            }
        }

        // ===== 均衡性: 空行惩罚 =====
        // 每行至少放1张才有基础, 空行 = 后续轮次没有任何方向
        const cardsPlaced = top.length + middle.length + bottom.length;
        const emptyRows = (top.length === 0 ? 1 : 0) + (middle.length === 0 ? 1 : 0) + (bottom.length === 0 ? 1 : 0);
        if (cardsPlaced >= 3 && emptyRows >= 1) {
            // 已放3+张但有行完全空: 严重灵活性损失
            score -= emptyRows * 150;
        }
        if (cardsPlaced >= 4 && emptyRows >= 2) {
            // 放了4-5张但还有2行空: 极端不均衡
            score -= 200;
        }

        return score;
    }

    _countRanks(cards) {
        const counts = {};
        for (const c of cards) {
            const r = rankIndex(c.rank);
            counts[r] = (counts[r] || 0) + 1;
        }
        return counts;
    }

    evalTop(top, remaining) {
        if (top.length === 0) return 0;
        let score = 0;
        const ranks = top.map(c => rankIndex(c.rank));
        const counts = {};
        for (const r of ranks) counts[r] = (counts[r] || 0) + 1;

        const hasPair = Object.entries(counts).find(([r, c]) => c >= 2);
        const hasTrips = Object.entries(counts).find(([r, c]) => c >= 3);

        if (hasTrips) {
            score += this.w.topTripsBonus;
        } else if (hasPair) {
            const pairRank = parseInt(hasPair[0]);
            // 基础对子分 (66-AA都有royalty)
            if (pairRank >= 4) { // 66+
                score += this.w.topPairBase + pairRank * this.w.topPairPerRank;
            }
            // QQ+进范加分 (压低, 避免过度追范)
            if (pairRank >= 10) {
                score += this.w.fantasyChaseWeight * 0.5;
            }
        }

        // 头道无对子时: 高牌抬高犯规风险, 应该惩罚
        if (!hasPair && !hasTrips) {
            const maxRank = Math.max(...ranks);
            // 高牌在头道 = 犯规风险 (A=12, K=11, Q=10, J=9)
            if (maxRank >= 9) score -= (maxRank - 7) * 8;
            // 多张高牌更危险
            const highCount = ranks.filter(r => r >= 9).length;
            if (highCount >= 2 && top.length >= 2) score -= highCount * 10;
        }

        return score;
    }

    evalBottom(bottom, remaining) {
        if (bottom.length === 0) return 0;
        let score = 0;
        const ranks = bottom.map(c => rankIndex(c.rank));
        const counts = {};
        for (const r of ranks) counts[r] = (counts[r] || 0) + 1;
        const maxCount = Math.max(...Object.values(counts));

        if (bottom.length === 5) {
            const ev = evaluate5(bottom);
            score += ev.type * this.w.botStrengthWeight * 5;
            score += (ev.type / 9) * (ev.value % 1000000) / 100000 * this.w.botStrengthWeight; // 同牌型内细分(小权重)
            score += getBottomRoyalty(bottom) * this.w.royaltyWeight;
            // 对子rank加分
            const botPairR = this.getMaxPairRank(bottom);
            if (botPairR >= 0) score += botPairR * this.w.botStrengthWeight * 0.5;
        } else {
            // 对子/三条 + 对子rank加分
            score += maxCount * 10 * this.w.botStrengthWeight;
            const maxPairR = this.getMaxPairRank(bottom);
            if (maxPairR >= 0) score += maxPairR * this.w.botStrengthWeight * 0.5;

            // Draw潜力: 结合outs精确计算 + 同花/顺子张数奖励
            const flushOuts = countFlushOuts(bottom, remaining);
            const straightOuts = countStraightOuts(bottom, remaining);
            const pairOuts = countPairOuts(bottom, remaining);
            score += Math.min(pairOuts, 8) * this.w.pairDrawWeight * 0.3;

            // 高牌
            for (const r of ranks) score += r * 0.5;

            // 同花评估: 张数 + outs
            const suitCounts = {};
            for (const c of bottom) suitCounts[c.suit] = (suitCounts[c.suit] || 0) + 1;
            const maxSuit = Math.max(...Object.values(suitCounts));
            if (maxSuit >= 4) {
                // 4同花: 差1张成花! (~80%概率成花, 底道同花=4 royalty)
                score += this.w.flushDrawWeight * 12;
            } else if (maxSuit >= 3) {
                // 3同花: 强draw
                score += this.w.flushDrawWeight * 5;
                if (flushOuts >= 8) score += this.w.flushDrawWeight * 2;
                else if (flushOuts >= 5) score += this.w.flushDrawWeight;
            } else if (maxSuit >= 2) {
                score += this.w.flushDrawWeight * 0.5;
            }

            // 顺子评估: 连续张数 + outs
            const uniqueRI = [...new Set(ranks)].sort((a, b) => a - b);
            let bestRun = 1, curRun = 1;
            for (let i = 1; i < uniqueRI.length; i++) {
                if (uniqueRI[i] - uniqueRI[i-1] <= 2) {
                    curRun++;
                    bestRun = Math.max(bestRun, curRun);
                } else {
                    curRun = 1;
                }
            }
            if (bestRun >= 4) {
                // 4张接近顺子: 差1张!
                score += this.w.straightDrawWeight * 5;
            } else if (bestRun >= 3) {
                score += this.w.straightDrawWeight * 2;
                if (straightOuts >= 6) score += this.w.straightDrawWeight * 1.5;
                else if (straightOuts >= 3) score += this.w.straightDrawWeight * 0.5;
            } else if (bestRun >= 2 && straightOuts >= 3) {
                score += this.w.straightDrawWeight * 0.5;
            }

            // 同花顺潜力: 同花色牌也连续 = 超级奖励
            if (maxSuit >= 3 && bestRun >= 3) {
                const flushSuit = Object.entries(suitCounts).find(([s,c]) => c === maxSuit)[0];
                const flushRanks = bottom.filter(c => c.suit === flushSuit).map(c => rankIndex(c.rank)).sort((a,b) => a-b);
                let flushRun = 1;
                for (let i = 1; i < flushRanks.length; i++) {
                    if (flushRanks[i] - flushRanks[i-1] <= 2) flushRun++;
                }
                if (flushRun >= 3) {
                    // 同花顺潜力! (底道同花顺=15 royalty)
                    score += this.w.flushDrawWeight * 5;
                }
            }
        }

        return score;
    }

    evalMiddle(middle, remaining) {
        if (middle.length === 0) return 0;
        let score = 0;
        const ranks = middle.map(c => rankIndex(c.rank));
        const counts = {};
        for (const r of ranks) counts[r] = (counts[r] || 0) + 1;
        const maxCount = Math.max(...Object.values(counts));

        if (middle.length === 5) {
            const ev = evaluate5(middle);
            score += ev.type * this.w.midStrengthWeight * 5;
            score += (ev.type / 9) * (ev.value % 1000000) / 100000 * this.w.midStrengthWeight; // 同牌型内细分
            score += getMiddleRoyalty(middle) * this.w.royaltyWeight;
            const midPairR = this.getMaxPairRank(middle);
            if (midPairR >= 0) score += midPairR * this.w.midStrengthWeight * 1.5;
        } else {
            score += maxCount * 8 * this.w.midStrengthWeight;
            const maxPairR = this.getMaxPairRank(middle);
            if (maxPairR >= 0) score += maxPairR * this.w.midStrengthWeight * 0.8;

            // 配对潜力: 中道每张牌的配对outs (鼓励中道多放牌)
            for (const [r, cnt] of Object.entries(counts)) {
                if (cnt === 1) {
                    const outs = remaining.filter(c => rankIndex(c.rank) === parseInt(r)).length;
                    score += outs * 1.5; // 提高: 每张牌的配对outs更有价值
                }
            }

            // 中道有2+张时给"基础分" (鼓励R1中道放2张)
            if (middle.length >= 2) score += 15;

            // 中道draw潜力 (比底道权重低, 优先把draw材料放底道)
            const suitCountsMid = {};
            for (const c of middle) suitCountsMid[c.suit] = (suitCountsMid[c.suit] || 0) + 1;
            const maxSuitMid = Math.max(...Object.values(suitCountsMid));

            if (maxSuitMid >= 4) {
                score += this.w.flushDrawWeight * 5;
            } else if (maxSuitMid >= 3) {
                score += this.w.flushDrawWeight * 1.5;
            } else if (maxSuitMid >= 2) {
                score += this.w.flushDrawWeight * 0.3;
            }

            const straightOuts = countStraightOuts(middle, remaining);
            if (straightOuts >= 6) score += this.w.straightDrawWeight * 1.2;
            else if (straightOuts >= 3) score += this.w.straightDrawWeight * 0.5;
        }

        return score;
    }

    evalOrdering(state, remaining) {
        // 正确排序奖励 (犯规惩罚已在estimateFoulRisk中处理)
        let score = 0;
        const { top, middle, bottom } = state;

        const topType = this.getPartialType(top);
        const midType = this.getPartialType(middle);
        const botType = this.getPartialType(bottom);

        // ===== 底道牌型 >= 中道牌型: 奖励正确排序 =====
        if (bottom.length >= 2 && middle.length >= 2) {
            if (botType > midType) score += this.w.orderMarginWeight * 1.5;
            else if (botType === midType && botType >= HAND_TYPE.PAIR) {
                const botPR = this.getMaxPairRank(bottom);
                const midPR = this.getMaxPairRank(middle);
                if (botPR > midPR) score += this.w.orderMarginWeight * 0.8;
            }
        }

        // ===== 中道牌型 >= 头道牌型: 奖励正确排序 =====
        if (middle.length >= 2 && top.length >= 2) {
            if (midType > topType) {
                score += this.w.orderMarginWeight * 1.0;
            } else if (midType === topType && topType === HAND_TYPE.PAIR) {
                const midPR = this.getMaxPairRank(middle);
                const topPR = this.getMaxPairRank(top);
                if (midPR >= 0 && topPR >= 0 && midPR > topPR) score += this.w.orderMarginWeight * 0.6;
            }
        }

        // ===== 底道高牌趋势 =====
        if (bottom.length >= 1 && middle.length >= 1) {
            const botMax = Math.max(...bottom.map(c => rankIndex(c.rank)));
            const midMax = Math.max(...middle.map(c => rankIndex(c.rank)));
            if (botMax > midMax) score += 8;
        }

        return score;
    }

    // 估算犯规概率 (0~1)
    estimateFoulRisk(state) {
        const { top, middle, bottom } = state;
        let risk = 0;

        // === 中道 vs 底道 ===
        if (middle.length >= 2 && bottom.length >= 2) {
            const botSlots = 5 - bottom.length;

            if (middle.length === 5 && bottom.length === 5) {
                if (evaluate5(middle).value > evaluate5(bottom).value) return 1.0;
            } else {
                const midType = this.getPartialType(middle);
                const botType = this.getPartialType(bottom);
                const midPR = this.getMaxPairRank(middle);
                const botPR = this.getMaxPairRank(bottom);

                // 中道牌型 > 底道牌型: 底道需要追上
                if (midType > botType) {
                    // botSlots: 0=必死, 1=危险, 2=有救, 3=还好
                    const catchUp = [0, 0.20, 0.45, 0.65][botSlots] || 0.75;
                    risk = Math.max(risk, 1 - catchUp);
                }

                // 同牌型但中道对子rank更大
                if (midType === botType && midType >= HAND_TYPE.PAIR && midPR > botPR && midPR >= 0 && botPR >= 0) {
                    const catchUp = [0, 0.25, 0.50, 0.65][botSlots] || 0.75;
                    risk = Math.max(risk, 1 - catchUp);
                }

                // 中道有对子底道高牌, 底道快满了
                if (midType >= HAND_TYPE.PAIR && botType === HAND_TYPE.HIGH_CARD) {
                    const catchUp = [0, 0.15, 0.40, 0.60][botSlots] || 0.70;
                    risk = Math.max(risk, 1 - catchUp);
                }
            }
        }

        // === 头道 vs 中道 ===
        // QQ+在头道 + 中道不成熟: 提前预警 (根据底道支撑调整)
        if (top.length >= 2 && middle.length < 2) {
            const topType = this.getPartialType(top);
            const topPR = this.getMaxPairRank(top);
            const botType = this.getPartialType(bottom);
            if (topType >= HAND_TYPE.PAIR && topPR >= 10) {
                if (botType >= HAND_TYPE.TWO_PAIR) {
                    // 底道两对+: 较安全
                    risk = Math.max(risk, 0.20);
                } else if (botType >= HAND_TYPE.PAIR) {
                    // 底道有对子: 犯规风险中等偏高
                    risk = Math.max(risk, 0.38);
                } else {
                    // 底道没对子: 犯规风险很高
                    risk = Math.max(risk, 0.58);
                }
            } else if (topType >= HAND_TYPE.PAIR) {
                risk = Math.max(risk, 0.20);
            }
        }
        if (top.length >= 2 && middle.length >= 2) {
            const midSlots = 5 - middle.length;
            const topType = top.length === 3 ? evaluate3(top).type : this.getPartialType(top);
            const midType = this.getPartialType(middle);
            const topPR = this.getMaxPairRank(top);
            const midPR = this.getMaxPairRank(middle);

            if (top.length === 3 && middle.length === 5) {
                const topEval = evaluate3(top);
                const midEval = evaluate5(middle);
                if (midEval.type < topEval.type) return 1.0;
                if (midEval.type === topEval.type && topEval.type === HAND_TYPE.PAIR && midPR >= 0 && topPR >= 0 && midPR < topPR) return 1.0;
                if (midEval.type === topEval.type && topEval.type === HAND_TYPE.THREE_OF_A_KIND) {
                    const topTrip = parseInt(Object.entries(this._countRanks(top)).find(([r,c]) => c>=3)[0]);
                    const midTrip = parseInt(Object.entries(this._countRanks(middle)).find(([r,c]) => c>=3)?.[0] || '0');
                    if (midTrip < topTrip) return 1.0;
                }
            } else {
                // 头道牌型 > 中道牌型
                if (topType > midType) {
                    const catchUp = [0, 0.15, 0.40, 0.60][midSlots] || 0.70;
                    risk = Math.max(risk, 1 - catchUp);
                }

                // 同为对子但头道对子rank更大 (QQ+在头道追范的关键场景)
                // 中道需要两对+或更大对子才能超越, 概率很低!
                if (topType === midType && topType === HAND_TYPE.PAIR && topPR > midPR && topPR >= 0) {
                    if (topPR >= 10) {
                        // QQ+在头道: 中道几乎不可能用对子超越, 必须两对+
                        // 两对+概率: 0slot=0%, 1slot≈15%, 2slot≈25%, 3slot≈35%
                        const catchUp = [0, 0.12, 0.22, 0.32][midSlots] || 0.40;
                        risk = Math.max(risk, 1 - catchUp);
                    } else {
                        // 非QQ+: 中道可以用更大对子超越
                        const catchUp = [0, 0.20, 0.35, 0.50][midSlots] || 0.60;
                        risk = Math.max(risk, 1 - catchUp);
                    }
                }

                // 头道有对子中道高牌: 中道需要更大对子或两对+
                if (topType >= HAND_TYPE.PAIR && midType === HAND_TYPE.HIGH_CARD) {
                    if (topPR >= 10) {
                        // QQ+在头道, 中道高牌: 中道需要QQ+对子或两对+
                        const catchUp = [0, 0.08, 0.18, 0.30][midSlots] || 0.38;
                        risk = Math.max(risk, 1 - catchUp);
                    } else {
                        const catchUp = [0, 0.12, 0.30, 0.48][midSlots] || 0.58;
                        risk = Math.max(risk, 1 - catchUp);
                    }
                }

                // 都是高牌时比最大rank
                if (topType === HAND_TYPE.HIGH_CARD && midType === HAND_TYPE.HIGH_CARD) {
                    const topMax = Math.max(...top.map(c => rankIndex(c.rank)));
                    const midMax = Math.max(...middle.map(c => rankIndex(c.rank)));
                    if (topMax > midMax) {
                        // 头道有更高牌, 中道需要配对或拿更高牌
                        const gap = topMax - midMax;
                        const catchUp = [0, 0.20, 0.40, 0.55][midSlots] || 0.65;
                        // gap越大越难追
                        const gapFactor = Math.min(1, 0.6 + gap * 0.1);
                        risk = Math.max(risk, (1 - catchUp) * gapFactor);
                    }
                }
            }
        }

        return Math.min(risk, 1.0);
    }

    // 辅助: 获取牌组中最大对子rank (-1 if none)
    getMaxPairRank(cards) {
        const counts = {};
        for (const c of cards) counts[c.rank] = (counts[c.rank] || 0) + 1;
        let maxPair = -1;
        for (const [r, cnt] of Object.entries(counts)) {
            if (cnt >= 2) maxPair = Math.max(maxPair, rankIndex(r));
        }
        return maxPair;
    }

    // 辅助: 获取部分牌组的"最低牌型" (0=高牌, 1=对子, 3=三条)
    getPartialType(cards) {
        if (cards.length === 0) return 0;
        const counts = {};
        for (const c of cards) counts[c.rank] = (counts[c.rank] || 0) + 1;
        const maxCount = Math.max(...Object.values(counts));
        if (maxCount >= 3) return HAND_TYPE.THREE_OF_A_KIND;
        if (maxCount >= 2) return HAND_TYPE.PAIR;
        return HAND_TYPE.HIGH_CARD;
    }
}

// ============================================================
// 专家Rollout策略 (用于MCTS模拟)
// ============================================================

class ExpertRollout {
    constructor(evaluator) {
        this.eval = evaluator;
    }

    // 模拟一局从currentRound到结束
    rollout(state, currentRound) {
        const gs = state.clone();
        let remaining = gs.getRemainingDeck();
        remaining = shuffleDeck(remaining);
        let deckIdx = 0;

        for (let round = currentRound + 1; round <= 5; round++) {
            if (gs.isComplete()) break;
            const numCards = (round === 1) ? 5 : 3;
            if (deckIdx + numCards > remaining.length) break;

            const dealt = remaining.slice(deckIdx, deckIdx + numCards);
            deckIdx += numCards;

            if (round === 1) {
                this.expertPlace5(gs, dealt);
            } else {
                this.expertPlace3(gs, dealt);
            }
        }

        return gs;
    }

    expertPlace5(state, cards) {
        // 第1轮: 穷举所有去重分配
        const actions = generateRound1Actions(cards, state);
        const candidates = [];

        const seen = new Set();
        for (const action of actions) {
            const gs = state.clone();
            for (let i = 0; i < cards.length; i++) {
                gs.placeCard(cards[i], action[i]);
            }
            const key = gs.top.map(c => cardId(c)).sort().join() + '|' +
                        gs.middle.map(c => cardId(c)).sort().join() + '|' +
                        gs.bottom.map(c => cardId(c)).sort().join();
            if (seen.has(key)) continue;
            seen.add(key);

            // 用simpleEval做预筛 (对draw的评估比evaluatePartial准)
            const score = this.simpleEval(gs);
            candidates.push({ action, score, gs });
        }

        // 三阶段MC: 宽筛选 → 中筛 → 精选
        candidates.sort((a, b) => b.score - a.score);

        // 三阶段MC: 宽筛 → 中筛 → 精选
        // 默认: 30*30 + 8*60 + 3*150 = 1830 模拟 (~4s/R1, 比原 5200 快 ~3x)
        // 可通过 globalThis.ROLLOUT_CONFIG.r1Mult 整体缩放 (server.js 通过 R1_ROLLOUT_MULT 注入)
        const _RC = (typeof globalThis !== 'undefined' && globalThis.ROLLOUT_CONFIG) || {};
        let _mult = (typeof _RC.r1Mult === 'number' && _RC.r1Mult > 0) ? _RC.r1Mult : 1;
        // 含鬼: 自动降 50% — rollout 内部 trainedEval 调用爆炸, medium 鬼 hand 单局 20s
        // 强制降到 ~10s, 用 simpleEval blend 保证质量 (鬼上顶 +50 已经足够强)
        const _hasJoker = cards.some(c => c.rank === 'X');
        if (_hasJoker) _mult = Math.max(0.15, _mult * 0.5);
        const _s1c = Math.max(5, Math.round(30 * _mult)), _s1n = Math.max(10, Math.round(30 * _mult));
        const _s2c = Math.max(3, Math.round(8 * _mult)),  _s2n = Math.max(20, Math.round(60 * _mult));
        const _s3c = Math.max(2, Math.round(3 * _mult)),  _s3n = Math.max(40, Math.round(150 * _mult));
        // 鬼场景默认更倚重 simpleEval (鬼策略 simpleEval 已编码强, MC 噪声大)
        const _blend = (typeof _RC.r1SimpleBlend === 'number') ? (_hasJoker ? _RC.r1SimpleBlend * 1.5 : _RC.r1SimpleBlend) : 0.3;
        const _profile = _RC.profile === true;
        const _t0 = _profile ? Date.now() : 0;
        const _marks = [];
        const _mark = (label) => { if (_profile) _marks.push(`${label} +${Date.now() - _t0}ms`); };
        _mark('stage0-prep done, candidates=' + candidates.length);
        // 暴露给 caller (solve-worker.js 会读)
        if (_profile && typeof globalThis !== 'undefined') globalThis.__lastProfile = _marks;

        const stage1N = Math.min(candidates.length, _s1c);
        const stage1 = [];
        for (let i = 0; i < stage1N; i++) {
            const { action, gs } = candidates[i];
            let total = 0;
            for (let s = 0; s < _s1n; s++) total += this.quickRollout(gs, 1);
            stage1.push({ action, gs, avg: total / _s1n });
        }
        stage1.sort((a, b) => b.avg - a.avg);
        _mark(`stage1 done (${stage1N}*${_s1n})`);

        // 阶段2: 中筛
        const stage2N = Math.min(stage1.length, _s2c);
        const stage2 = [];
        for (let i = 0; i < stage2N; i++) {
            const { action, gs } = stage1[i];
            let total = 0;
            for (let s = 0; s < _s2n; s++) total += this.quickRollout(gs, 1);
            stage2.push({ action, gs, avg: total / _s2n });
        }
        stage2.sort((a, b) => b.avg - a.avg);
        _mark(`stage2 done (${stage2N}*${_s2n})`);

        // 阶段3: 精选 — MC + simpleEval 混合 (EV 接近时 simpleEval 决定: 引导小对去中道)
        const stage3N = Math.min(stage2.length, _s3c);
        let bestScore = -Infinity, bestAction = null;
        for (let i = 0; i < stage3N; i++) {
            const { action, gs } = stage2[i];
            let total = 0;
            for (let s = 0; s < _s3n; s++) total += this.quickRollout(gs, 1);
            const avg = total / _s3n;
            const simple = this.simpleEval(gs);
            const combined = avg + simple * _blend;
            if (combined > bestScore) { bestScore = combined; bestAction = action; }
        }
        _mark(`stage3 done (${stage3N}*${_s3n})`);

        if (!bestAction && candidates.length > 0) bestAction = candidates[0].action;
        if (bestAction) {
            for (let i = 0; i < cards.length; i++) {
                state.placeCard(cards[i], bestAction[i]);
            }
        }
    }

    expertPlace3(state, cards) {
        const actions = generateRoundNActions(cards, state);
        let bestScore = -Infinity, bestAction = null;

        // 去重所有候选
        const seen = new Set();
        const unique = [];
        for (const action of actions) {
            const gs = state.clone();
            gs.usedCards.add(cardId(cards[action.discard]));
            for (let i = 0; i < action.kept.length; i++) {
                gs.placeCard(action.kept[i], action.placement[i]);
            }
            const key = cardId(cards[action.discard]) + '|' +
                gs.top.map(c=>cardId(c)).sort().join() + '|' +
                gs.middle.map(c=>cardId(c)).sort().join() + '|' +
                gs.bottom.map(c=>cardId(c)).sort().join();
            if (seen.has(key)) continue;
            seen.add(key);
            unique.push({ action, gs });
        }

        // trainedEval排名 + outs奖励
        for (const item of unique) {
            let s = this.trainedEval(item.gs);
            // Outs奖励 + 高牌价值
            for (let i = 0; i < item.action.kept.length; i++) {
                const card = item.action.kept[i];
                const row = item.action.placement[i];
                const rowBefore = row === 'top' ? state.top : row === 'middle' ? state.middle : state.bottom;
                const ri = rankIndex(card.rank);
                if (rowBefore.some(c => c.rank === card.rank)) {
                    s += 20 + rankIndex(card.rank) * 2; // 大对子更有价值: KK=42, 99=34
                    // 配对后该行形成两对 = 额外大奖
                    const rowAfterRanks = {};
                    for (const c of rowBefore) rowAfterRanks[c.rank] = (rowAfterRanks[c.rank]||0)+1;
                    rowAfterRanks[card.rank] = (rowAfterRanks[card.rank]||0)+1;
                    const pairs = Object.values(rowAfterRanks).filter(v => v >= 2).length;
                    if (pairs >= 2) s += 15; // 两对!
                } else {
                    const allCards = [...state.top,...state.middle,...state.bottom];
                    const used = allCards.filter(c => c.rank === card.rank).length;
                    s += (3 - used) * 2; // 每个out +2
                }
                // 高牌价值
                if (ri >= 12) s += 5;       // A
                else if (ri >= 11) s += 4;  // K
                else if (ri >= 10) s += 3;  // Q

                // (回退: 高牌底道偏好 rule 在 50-hand 测试 royalty 反降, 后续再调)

                // QKA放头道 = 追范潜力
                if (row === 'top' && ri >= 10 && state.top.length <= 1) {
                    s += 20; // 头道还有空位, QKA上头=追范
                    if (ri >= 12) s += 10; // A上头 = AA范特西+120
                    else if (ri >= 11) s += 5; // K上头
                    if (state.top.some(c => c.rank === card.rank)) s += 40; // 直接进范!
                }
            }
            // 弃牌惩罚
            const discardCard = cards[item.action.discard];
            // === 硬规则: 永不弃鬼 (减法保留候选间相对差, 全鬼必弃时仍能挑较好的) ===
            if (isJoker(discardCard)) {
                s -= 1e6;
            }
            const discardRank = rankIndex(discardCard.rank);
            if (discardRank >= 12) s -= 8;
            else if (discardRank >= 11) s -= 6;
            else if (discardRank >= 10) s -= 4;

            // 头道追范保护: 头道有QKA且只1张, 放低牌到头道=好
            if (state.top.length === 1 && rankIndex(state.top[0].rank) >= 10) {
                for (let i = 0; i < item.action.kept.length; i++) {
                    if (item.action.placement[i] === 'top' && rankIndex(item.action.kept[i].rank) < 10) {
                        s += 10; // 低牌上头保追范
                    }
                }
                if (item.gs.top.length === 1) s += 5; // 头道没变=继续等
            }
            // === 鬼独顶耐心规则: dealt 含 K/A → 优先放顶配鬼成 KK/AA 进范 ===
            // 不罚顶填非高牌 (保留小三条范路径), 只重奖 K/A 上顶配鬼
            const topHasJoker_p3 = state.top.some(c => isJoker(c));
            if (topHasJoker_p3 && state.top.length < 3) {
                for (let i = 0; i < item.action.kept.length; i++) {
                    if (item.action.placement[i] === 'top') {
                        const kr = rankIndex(item.action.kept[i].rank);
                        if (kr === 12) s += 60;       // 鬼+A → AA 进范
                        else if (kr === 11) s += 45;  // 鬼+K → KK 进范
                    }
                }
            }
            // === 广义鬼上顶规则: 顶未已 chase QQ+ 时, 鬼放顶 +40 (博 trips/QQ+) ===
            // 不限于 K/A 在 dealt: 哪怕 dealt 是低牌, 鬼上顶仍是博进范的核心通道
            // 避开顶已有 QQ+ 大对的情况 (那时鬼应去补行牌型)
            if (state.top.length < 3) {
                const stateTopRealCnt = {};
                for (const c of state.top) if (c.rank !== 'X') stateTopRealCnt[c.rank] = (stateTopRealCnt[c.rank]||0)+1;
                const stateTopHasHighPair = Object.entries(stateTopRealCnt).some(([r,c]) => c >= 2 && rankIndex(r) >= 10);
                if (!stateTopHasHighPair) {
                    for (let i = 0; i < item.action.kept.length; i++) {
                        if (item.action.placement[i] === 'top' && isJoker(item.action.kept[i])) {
                            s += 40;
                        }
                    }
                }
            }
            item.teScore = s;
        }
        unique.sort((a, b) => b.teScore - a.teScore);

        // 两阶段MC: top-15×50次筛选 → top-5×300次精选
        // R2-R5 也按 ROLLOUT_CONFIG.r1Mult 整体缩放 (level 复用同一倍率)
        const _RC3 = (typeof globalThis !== 'undefined' && globalThis.ROLLOUT_CONFIG) || {};
        const _m3 = (typeof _RC3.r1Mult === 'number' && _RC3.r1Mult > 0) ? _RC3.r1Mult : 1;
        const _S1c3 = Math.max(5, Math.round(15 * _m3)), _S1n3 = Math.max(15, Math.round(50 * _m3));
        const _S2c3 = Math.max(2, Math.round(5  * _m3)), _S2n3 = Math.max(60, Math.round(300 * _m3));
        const stage1N = Math.min(unique.length, _S1c3);
        const stage1 = [];
        for (let i = 0; i < stage1N; i++) {
            const { action, gs } = unique[i];
            if (gs.isComplete()) {
                const score = gs.getScore();
                let fb = 0;
                if (score.fantasy && !score.foul) {
                    const te = score.topEval;
                    if (te && te.type === 3) fb = 400;
                    else if (te && te.type === 1) {
                        const pr = Math.floor((te.value - 1000000) / 15);
                        if (pr >= 12) fb = 200;
                        else if (pr >= 11) fb = 100;
                        else fb = 50;
                    }
                }
                stage1.push({ action, gs, avg: score.foul ? -20 : score.royalties + fb });
                continue;
            }
            let total = 0;
            for (let r = 0; r < _S1n3; r++) total += this.quickRollout(gs, state.round || 3);
            stage1.push({ action, gs, avg: total / _S1n3 });
        }
        stage1.sort((a, b) => b.avg - a.avg);

        const stage2N = Math.min(stage1.length, _S2c3);
        for (let i = 0; i < stage2N; i++) {
            const { action, gs } = stage1[i];
            if (gs.isComplete()) {
                if (stage1[i].avg > bestScore) { bestScore = stage1[i].avg; bestAction = action; }
                continue;
            }
            let total = 0;
            for (let r = 0; r < _S2n3; r++) total += this.quickRollout(gs, state.round || 3);
            const avg = total / _S2n3;
            if (avg > bestScore) { bestScore = avg; bestAction = action; }
        }

        // Fallback to evaluator-only best
        if (!bestAction && scored.length > 0) {
            bestAction = scored[0].action;
        }

        if (bestAction) {
            state.usedCards.add(cardId(cards[bestAction.discard]));
            for (let i = 0; i < bestAction.kept.length; i++) {
                state.placeCard(bestAction.kept[i], bestAction.placement[i]);
            }
        }
    }

    // === 范特西手 beam search 摆牌 (Self-play 用) ===
    // state: GameState (assumed fresh / 空板; FL 一次性摆 13 张)
    // dealt: 14-17 张
    // discardCount: N-13 (1=QQ, 2=KK, 3=AA, 4=trips/re-enter)
    // 返回: { layout: {top, middle, bottom, discards}, score, foul }
    // epsilon: ε 概率从 valid 候选随机选 (探索), 默认 0
    expertPlaceFantasy(state, dealt, discardCount, beamWidth = 10, epsilon = 0) {
        const N = dealt.length;
        if (N - discardCount !== 13) return null;
        const _prevFL = this._useFLMode;
        this._useFLMode = true;
        try {
            // === Phase 1: 反向枚举 re-fantasy 锚 (顶 trips / 底 4-kind / 底 SF) ===
            const refan = this._directReFanSearch(dealt, discardCount);
            if (refan) {
                this._useFLMode = _prevFL;
                return { layout: refan.layout, score: refan.royalty };
            }
            // === Phase 2: 无 re-fan 路径 → beam search 找最高 royalty ===
            return this._expertPlaceFantasyImpl(state, dealt, discardCount, beamWidth, epsilon);
        } finally {
            this._useFLMode = _prevFL;
        }
    }

    // === 反向枚举所有 re-fantasy 锚 ===
    // 每个锚 = 一组确定占据某行的牌 (top trips / bot quads / bot SF)
    _findReFanAnchors(dealt) {
        const real = dealt.filter(c => !isJoker(c));
        const jokers = dealt.filter(c => isJoker(c));
        const J = jokers.length;
        const byRank = {};
        for (const c of real) (byRank[c.rank] = byRank[c.rank] || []).push(c);
        const bySuit = {};
        for (const c of real) (bySuit[c.suit] = bySuit[c.suit] || []).push(c);
        const anchors = [];
        // (1) 顶 trips: 任意 rank 的 (real + jokers) >= 3
        for (const r of Object.keys(byRank)) {
            const cs = byRank[r];
            if (cs.length + J >= 3) {
                const realUsed = cs.slice(0, 3);
                const jokersNeeded = 3 - realUsed.length;
                anchors.push({ type: 'top-trips', row: 'top', cards: [...realUsed, ...jokers.slice(0, jokersNeeded)] });
            }
        }
        // 双鬼可形成虚 trips of A
        if (real.length === 0 && J >= 3) anchors.push({ type: 'top-trips', row: 'top', cards: jokers.slice(0, 3) });
        // (2) 底 4-of-a-kind
        for (const r of Object.keys(byRank)) {
            const cs = byRank[r];
            if (cs.length + J >= 4) {
                const realUsed = cs.slice(0, 4);
                const jokersNeeded = 4 - realUsed.length;
                anchors.push({ type: 'bot-quads', row: 'bot', cards: [...realUsed, ...jokers.slice(0, jokersNeeded)] });
            }
        }
        // (3) 底 SF: 每个 suit, 每个 5-rank window
        for (const suit of Object.keys(bySuit)) {
            const suitMap = {};
            for (const c of bySuit[suit]) suitMap[rankIndex(c.rank)] = c;
            const tryWindow = (winRanks, type) => {
                const have = winRanks.filter(rk => suitMap[rk]).map(rk => suitMap[rk]);
                const need = 5 - have.length;
                if (need >= 0 && need <= J) {
                    anchors.push({ type, row: 'bot', cards: [...have, ...jokers.slice(0, need)] });
                }
            };
            for (let rmin = 0; rmin <= 8; rmin++) {
                tryWindow([rmin, rmin+1, rmin+2, rmin+3, rmin+4], 'bot-sf');
            }
            // wheel A-2-3-4-5
            tryWindow([12, 0, 1, 2, 3], 'bot-sf-wheel');
        }
        return anchors;
    }

    // === 非 re-fan 高价值结构锚 (Phase 2 用, 当 re-fan 不可达) ===
    // 中道 4-kind / SF / 葫芦 / 顶 AA/KK/QQ 高对 → 各自高 royalty
    _findNonRefanAnchors(dealt) {
        const real = dealt.filter(c => !isJoker(c));
        const jokers = dealt.filter(c => isJoker(c));
        const J = jokers.length;
        const byRank = {};
        for (const c of real) (byRank[c.rank] = byRank[c.rank] || []).push(c);
        const bySuit = {};
        for (const c of real) (bySuit[c.suit] = bySuit[c.suit] || []).push(c);
        const anchors = [];
        // 中道 4-kind (mid quads royalty 20)
        for (const r of Object.keys(byRank)) {
            const cs = byRank[r];
            if (cs.length + J >= 4) {
                const realUsed = cs.slice(0, 4);
                const jU = jokers.slice(0, Math.max(0, 4 - realUsed.length));
                anchors.push({ type: 'mid-quads', row: 'mid4', cards: [...realUsed, ...jU] });
            }
        }
        // 中道 SF (mid SF royalty 30, RF 50)
        for (const suit of Object.keys(bySuit)) {
            const suitMap = {};
            for (const c of bySuit[suit]) suitMap[rankIndex(c.rank)] = c;
            const tryWindow = (winRanks, type) => {
                const have = winRanks.filter(rk => suitMap[rk]).map(rk => suitMap[rk]);
                const need = 5 - have.length;
                if (need >= 0 && need <= J) anchors.push({ type, row: 'mid', cards: [...have, ...jokers.slice(0, need)] });
            };
            for (let rmin = 0; rmin <= 8; rmin++) tryWindow([rmin, rmin+1, rmin+2, rmin+3, rmin+4], 'mid-sf');
            tryWindow([12, 0, 1, 2, 3], 'mid-sf-wheel');
        }
        // 顶 AA/KK/QQ 高对 (top royalty 9/8/7)
        for (const r of ['A', 'K', 'Q']) {
            if (byRank[r] && byRank[r].length + J >= 2) {
                const realUsed = byRank[r].slice(0, 2);
                const jU = jokers.slice(0, Math.max(0, 2 - realUsed.length));
                anchors.push({ type: 'top-pair-' + r, row: 'top2', cards: [...realUsed, ...jU] });
            }
        }
        // 中/底 葫芦 anchor (FH royalty 12 mid / 6 bot)
        const tripCands = Object.keys(byRank).filter(r => byRank[r].length + J >= 3);
        const pairCands = Object.keys(byRank).filter(r => byRank[r].length >= 2); // strict 真对, 不抢 joker
        for (const tr of tripCands) {
            const trReal = byRank[tr];
            const trJ = Math.max(0, 3 - trReal.length);
            const trCards = [...trReal.slice(0, 3 - trJ), ...jokers.slice(0, trJ)];
            const remJ = J - trJ;
            for (const pr of pairCands) {
                if (pr === tr) continue;
                const prReal = byRank[pr];
                const prJ = Math.max(0, 2 - prReal.length);
                if (prJ > remJ) continue;
                const prCards = [...prReal.slice(0, 2 - prJ), ...jokers.slice(trJ, trJ + prJ)];
                const fh = [...trCards, ...prCards];
                anchors.push({ type: `mid-fh-${tr}-${pr}`, row: 'mid', cards: fh });
                anchors.push({ type: `bot-fh-${tr}-${pr}`, row: 'bot5', cards: fh });
            }
        }
        return anchors;
    }

    // 给定 mid 5 (固定), 穷举 top + bot
    _enumTopBot(midCards, remaining, discardCount) {
        const N = remaining.length;
        if (N !== 3 + 5 + discardCount) return null;
        const midEval = midCards.some(isJoker) ? evaluate5Joker(midCards) : evaluate5(midCards);
        let bestLayout = null, bestRoy = -Infinity, bestSc = null;
        for (let i = 0; i < N - 2; i++)
        for (let j = i + 1; j < N - 1; j++)
        for (let k = j + 1; k < N; k++) {
            const top = [remaining[i], remaining[j], remaining[k]];
            const topEval = top.some(isJoker) ? evaluate3Joker(top) : evaluate3(top);
            if (handExceeds5(topEval, midEval)) continue;
            const rest = [];
            for (let x = 0; x < N; x++) if (x !== i && x !== j && x !== k) rest.push(remaining[x]);
            const M = rest.length;
            for (let a = 0; a < M - 4; a++)
            for (let b = a + 1; b < M - 3; b++)
            for (let c = b + 1; c < M - 2; c++)
            for (let d = c + 1; d < M - 1; d++)
            for (let e = d + 1; e < M; e++) {
                const bot = [rest[a], rest[b], rest[c], rest[d], rest[e]];
                const botEval = bot.some(isJoker) ? evaluate5Joker(bot) : evaluate5(bot);
                if (botEval.value < midEval.value) continue;
                const discards = [];
                for (let x = 0; x < M; x++) if (x !== a && x !== b && x !== c && x !== d && x !== e) discards.push(rest[x]);
                const sc = scoreHand(top, midCards, bot);
                if (sc.foul) continue;
                if (sc.royalties > bestRoy ||
                    (sc.royalties === bestRoy && bestSc && (
                        sc.topEval.value > bestSc.topEval.value ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value > bestSc.midEval.value) ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value === bestSc.midEval.value && sc.botEval.value > bestSc.botEval.value)
                    ))) {
                    bestRoy = sc.royalties;
                    bestSc = sc;
                    bestLayout = { top, middle: midCards, bottom: bot, discards };
                }
            }
        }
        return bestLayout ? { layout: bestLayout, royalty: bestRoy, sc: bestSc } : null;
    }

    // 处理非 re-fan 锚: mid 5 / top pair (2 + kicker)
    _buildLayoutFromNonRefanAnchor(anchor, dealt, discardCount) {
        const usedIds = new Set(anchor.cards.map(c => cardId(c)));
        const remaining = dealt.filter(c => !usedIds.has(cardId(c)));
        if (anchor.row === 'mid') {
            // Mid 5 已固定 (SF / 葫芦)
            const r = this._enumTopBot(anchor.cards, remaining, discardCount);
            return r ? r.layout : null;
        }
        if (anchor.row === 'bot5') {
            // Bot 5 已固定 (葫芦) — 不要求 re-fan
            const r = this._enumTopMidNoRefan(anchor.cards, remaining, discardCount);
            return r ? r.layout : null;
        }
        if (anchor.row === 'mid4') {
            // 4-kind on mid, 试 3 个候选 kicker
            const sortedDesc = [...remaining].sort((a, b) => (isJoker(b) ? 13 : rankIndex(b.rank)) - (isJoker(a) ? 13 : rankIndex(a.rank)));
            const kickerCands = [sortedDesc[0], sortedDesc[Math.floor(sortedDesc.length/2)], sortedDesc[sortedDesc.length-1]]
                .filter((c, i, arr) => c && arr.findIndex(x => x && cardId(x) === cardId(c)) === i);
            let best = null;
            for (const kicker of kickerCands) {
                const mid = [...anchor.cards, kicker];
                const restAfter = remaining.filter(c => cardId(c) !== cardId(kicker));
                const r = this._enumTopBot(mid, restAfter, discardCount);
                if (r) {
                    if (!best || r.royalty > best.royalty ||
                        (r.royalty === best.royalty && r.sc && best.sc && (
                            r.sc.topEval.value > best.sc.topEval.value ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value > best.sc.midEval.value) ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value === best.sc.midEval.value && r.sc.botEval.value > best.sc.botEval.value)
                        ))) best = r;
                }
            }
            return best ? best.layout : null;
        }
        if (anchor.row === 'top2') {
            // 顶 2 张 + 1 kicker, 然后穷举 mid + bot
            // 试每个 kicker (≤ 13 个 remaining), 每个 enum mid 5 + bot 5 from rest 12
            // 这个开销大: 13 * C(12,5) * C(7,5) ≈ 13 * 792 * 21 = 216K. 太慢
            // 简化: 只试 3-5 个低 rank kicker (顶不要冲高 type)
            // 限 4 候选 kicker (2 低 + 2 高), 兼顾 royalty 最大 (低 kicker) + 顶高 tie-break (高 kicker)
            const sortedAsc = [...remaining].sort((a, b) => (isJoker(a) ? 13 : rankIndex(a.rank)) - (isJoker(b) ? 13 : rankIndex(b.rank)));
            const _N = sortedAsc.length;
            const _seen = new Set();
            const kickerCands = [];
            const _tryAdd = c => { if (c && !_seen.has(cardId(c))) { _seen.add(cardId(c)); kickerCands.push(c); } };
            _tryAdd(sortedAsc[0]);                  // 最低
            _tryAdd(sortedAsc[1]);                  // 第 2 低
            _tryAdd(sortedAsc[_N - 2]);             // 第 2 高
            _tryAdd(sortedAsc[_N - 1]);             // 最高
            let best = null;
            for (const kicker of kickerCands) {
                const top = [...anchor.cards, kicker];
                const restAfter = remaining.filter(c => cardId(c) !== cardId(kicker));
                // Use _enumMidBot but without the type-3 pruning (top here is pair, not trips)
                const r = this._enumMidBotForTopPair(top, restAfter, discardCount);
                if (r) {
                    if (!best || r.royalty > best.royalty ||
                        (r.royalty === best.royalty && r.sc && best.sc && (
                            r.sc.topEval.value > best.sc.topEval.value ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value > best.sc.midEval.value) ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value === best.sc.midEval.value && r.sc.botEval.value > best.sc.botEval.value)
                        ))) best = r;
                }
            }
            return best ? best.layout : null;
        }
        return null;
    }

    // 给定 bot 5, 穷举 top + mid, 不要求 re-fan (Phase 2 用)
    _enumTopMidNoRefan(botCards, remaining, discardCount) {
        const N = remaining.length;
        if (N !== 3 + 5 + discardCount) return null;
        const botEval = botCards.some(isJoker) ? evaluate5Joker(botCards) : evaluate5(botCards);
        let bestLayout = null, bestRoy = -Infinity, bestSc = null;
        for (let i = 0; i < N - 2; i++)
        for (let j = i + 1; j < N - 1; j++)
        for (let k = j + 1; k < N; k++) {
            const top = [remaining[i], remaining[j], remaining[k]];
            const topEval = top.some(isJoker) ? evaluate3Joker(top) : evaluate3(top);
            const rest = [];
            for (let x = 0; x < N; x++) if (x !== i && x !== j && x !== k) rest.push(remaining[x]);
            const M = rest.length;
            for (let a = 0; a < M - 4; a++)
            for (let b = a + 1; b < M - 3; b++)
            for (let c = b + 1; c < M - 2; c++)
            for (let d = c + 1; d < M - 1; d++)
            for (let e = d + 1; e < M; e++) {
                const mid = [rest[a], rest[b], rest[c], rest[d], rest[e]];
                const midEval = mid.some(isJoker) ? evaluate5Joker(mid) : evaluate5(mid);
                if (midEval.value > botEval.value) continue;
                if (handExceeds5(topEval, midEval)) continue;
                const discards = [];
                for (let x = 0; x < M; x++) if (x !== a && x !== b && x !== c && x !== d && x !== e) discards.push(rest[x]);
                const sc = scoreHand(top, mid, botCards);
                if (sc.foul) continue;
                if (sc.royalties > bestRoy ||
                    (sc.royalties === bestRoy && bestSc && (
                        sc.topEval.value > bestSc.topEval.value ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value > bestSc.midEval.value) ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value === bestSc.midEval.value && sc.botEval.value > bestSc.botEval.value)
                    ))) {
                    bestRoy = sc.royalties;
                    bestSc = sc;
                    bestLayout = { top, middle: mid, bottom: botCards, discards };
                }
            }
        }
        return bestLayout ? { layout: bestLayout, royalty: bestRoy, sc: bestSc } : null;
    }

    // 顶为高对时穷举 mid+bot (mid type 不要求 >= trips, 只要 >= top pair)
    _enumMidBotForTopPair(topCards, remaining, discardCount) {
        const N = remaining.length;
        if (N !== 5 + 5 + discardCount) return null;
        const topEval = topCards.some(isJoker) ? evaluate3Joker(topCards) : evaluate3(topCards);
        let bestLayout = null, bestRoy = -Infinity, bestSc = null;
        for (let i = 0; i < N - 4; i++)
        for (let j = i + 1; j < N - 3; j++)
        for (let k = j + 1; k < N - 2; k++)
        for (let l = k + 1; l < N - 1; l++)
        for (let m = l + 1; m < N; m++) {
            const mid = [remaining[i], remaining[j], remaining[k], remaining[l], remaining[m]];
            const midEval = mid.some(isJoker) ? evaluate5Joker(mid) : evaluate5(mid);
            if (handExceeds5(topEval, midEval)) continue;
            const rest = [];
            for (let x = 0; x < N; x++) if (x !== i && x !== j && x !== k && x !== l && x !== m) rest.push(remaining[x]);
            const M = rest.length;
            for (let a = 0; a < M - 4; a++)
            for (let b = a + 1; b < M - 3; b++)
            for (let c = b + 1; c < M - 2; c++)
            for (let d = c + 1; d < M - 1; d++)
            for (let e = d + 1; e < M; e++) {
                const bot = [rest[a], rest[b], rest[c], rest[d], rest[e]];
                const botEval = bot.some(isJoker) ? evaluate5Joker(bot) : evaluate5(bot);
                if (botEval.value < midEval.value) continue;
                const discards = [];
                for (let x = 0; x < M; x++) if (x !== a && x !== b && x !== c && x !== d && x !== e) discards.push(rest[x]);
                const sc = scoreHand(topCards, mid, bot);
                if (sc.foul) continue;
                if (sc.royalties > bestRoy ||
                    (sc.royalties === bestRoy && bestSc && (
                        sc.topEval.value > bestSc.topEval.value ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value > bestSc.midEval.value) ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value === bestSc.midEval.value && sc.botEval.value > bestSc.botEval.value)
                    ))) {
                    bestRoy = sc.royalties;
                    bestSc = sc;
                    bestLayout = { top: topCards, middle: mid, bottom: bot, discards };
                }
            }
        }
        return bestLayout ? { layout: bestLayout, royalty: bestRoy, sc: bestSc } : null;
    }

    // === 给定锚 + dealt, 穷举所有合法 (mid, bot, discards) 分配, 选最高 royalty ===
    // 不依赖启发或 MLP, 纯枚举 + scoreHand 验证
    _buildLayoutFromAnchor(anchor, dealt, discardCount) {
        const usedIds = new Set(anchor.cards.map(c => cardId(c)));
        const remaining = dealt.filter(c => !usedIds.has(cardId(c)));
        if (anchor.row === 'bot') {
            // Bot 已 5 张 (SF) 或 4 张 (quads, 需 1 kicker)
            if (anchor.cards.length === 5) {
                return this._enumTopMid(anchor.cards, remaining, discardCount);
            }
            if (anchor.cards.length === 4) {
                // Bot 4-kind: kicker 不影响 bot 强度 (永远是 4-kind), 只影响剩余分配
                // 优化: 试 3 个候选 kicker (最低/最高/单 rank), 而不是全部 12
                let best = null;
                const sortedDesc = [...remaining].sort((a, b) =>
                    (isJoker(b) ? 13 : rankIndex(b.rank)) - (isJoker(a) ? 13 : rankIndex(a.rank)));
                const kickerCands = [
                    sortedDesc[0],                              // 最高 (常给 top 高 royalty)
                    sortedDesc[sortedDesc.length - 1],          // 最低 (留高牌给 mid)
                    sortedDesc[Math.floor(sortedDesc.length/2)]  // 中间
                ].filter((c, i, arr) => c && arr.findIndex(x => x && cardId(x) === cardId(c)) === i);
                for (const kicker of kickerCands) {
                    const bot = [...anchor.cards, kicker];
                    const restAfter = remaining.filter(c => cardId(c) !== cardId(kicker));
                    const r = this._enumTopMid(bot, restAfter, discardCount, true);
                    if (!r) continue;
                    if (!best ||
                        r.royalty > best.royalty ||
                        (r.royalty === best.royalty && r.sc && best.sc && (
                            r.sc.topEval.value > best.sc.topEval.value ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value > best.sc.midEval.value) ||
                            (r.sc.topEval.value === best.sc.topEval.value && r.sc.midEval.value === best.sc.midEval.value && r.sc.botEval.value > best.sc.botEval.value)
                        ))) {
                        best = r;
                    }
                }
                return best ? best.layout : null;
            }
            return null;
        }
        if (anchor.row === 'top') {
            return this._enumMidBot(anchor.cards, remaining, discardCount);
        }
        return null;
    }

    // 给定 top 3, 穷举 mid 5 + bot 5 + discards (top trips 锚用)
    // remaining 通常 13 张 (AA trigger), 复杂度 C(13,5)*C(8,5)=72K. 带类型剪枝降到 ~7K
    _enumMidBot(topCards, remaining, discardCount, returnLayout = false) {
        const N = remaining.length;
        if (N !== 5 + 5 + discardCount) return null;
        const topEval = topCards.some(isJoker) ? evaluate3Joker(topCards) : evaluate3(topCards);
        let bestLayout = null, bestRoy = -Infinity, bestSc = null;
        for (let i = 0; i < N - 4; i++)
        for (let j = i + 1; j < N - 3; j++)
        for (let k = j + 1; k < N - 2; k++)
        for (let l = k + 1; l < N - 1; l++)
        for (let m = l + 1; m < N; m++) {
            const mid = [remaining[i], remaining[j], remaining[k], remaining[l], remaining[m]];
            // 快速剪枝: mid 必须 >= top trips
            const midEval = mid.some(isJoker) ? evaluate5Joker(mid) : evaluate5(mid);
            if (midEval.type < topEval.type) continue;
            if (midEval.type === topEval.type && midEval.value <= topEval.value) continue;
            // bot from rest 8
            const rest = [];
            for (let x = 0; x < N; x++) if (x !== i && x !== j && x !== k && x !== l && x !== m) rest.push(remaining[x]);
            const M = rest.length;
            for (let a = 0; a < M - 4; a++)
            for (let b = a + 1; b < M - 3; b++)
            for (let c = b + 1; c < M - 2; c++)
            for (let d = c + 1; d < M - 1; d++)
            for (let e = d + 1; e < M; e++) {
                const bot = [rest[a], rest[b], rest[c], rest[d], rest[e]];
                const botEval = bot.some(isJoker) ? evaluate5Joker(bot) : evaluate5(bot);
                if (botEval.value <= midEval.value) continue;
                const discards = [];
                for (let x = 0; x < M; x++) if (x !== a && x !== b && x !== c && x !== d && x !== e) discards.push(rest[x]);
                const sc = scoreHand(topCards, mid, bot);
                if (sc.foul) continue;
                // re-fantasy: 顶 trips (type>=3) 或 底 4-kind+ (type>=7), 直接用 scoreHand evals 判, 跳过 checkFantasyTrigger
                if (sc.topEval.type < 3 && sc.botEval.type < 7) continue;
                if (sc.royalties > bestRoy ||
                    (sc.royalties === bestRoy && bestSc && (
                        sc.topEval.value > bestSc.topEval.value ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value > bestSc.midEval.value) ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value === bestSc.midEval.value && sc.botEval.value > bestSc.botEval.value)
                    ))) {
                    bestRoy = sc.royalties;
                    bestSc = sc;
                    bestLayout = { top: topCards, middle: mid, bottom: bot, discards };
                }
            }
        }
        if (!bestLayout) return null;
        return returnLayout ? { layout: bestLayout, royalty: bestRoy, sc: bestSc } : bestLayout;
    }

    // 给定 bot 5, 穷举 top 3 + mid 5 + discards (bot SF/quads 锚用)
    // remaining 通常 11 张, 复杂度 C(11,3)*C(8,5)=9240. 带剪枝 ~3K
    _enumTopMid(botCards, remaining, discardCount, returnLayout = false) {
        const N = remaining.length;
        if (N !== 3 + 5 + discardCount) return null;
        const botEval = botCards.some(isJoker) ? evaluate5Joker(botCards) : evaluate5(botCards);
        let bestLayout = null, bestRoy = -Infinity, bestSc = null;
        for (let i = 0; i < N - 2; i++)
        for (let j = i + 1; j < N - 1; j++)
        for (let k = j + 1; k < N; k++) {
            const top = [remaining[i], remaining[j], remaining[k]];
            const topEval = top.some(isJoker) ? evaluate3Joker(top) : evaluate3(top);
            const rest = [];
            for (let x = 0; x < N; x++) if (x !== i && x !== j && x !== k) rest.push(remaining[x]);
            const M = rest.length;
            for (let a = 0; a < M - 4; a++)
            for (let b = a + 1; b < M - 3; b++)
            for (let c = b + 1; c < M - 2; c++)
            for (let d = c + 1; d < M - 1; d++)
            for (let e = d + 1; e < M; e++) {
                const mid = [rest[a], rest[b], rest[c], rest[d], rest[e]];
                const midEval = mid.some(isJoker) ? evaluate5Joker(mid) : evaluate5(mid);
                if (midEval.value > botEval.value) continue;       // mid > bot foul
                if (handExceeds5(topEval, midEval)) continue;       // top > mid foul
                const discards = [];
                for (let x = 0; x < M; x++) if (x !== a && x !== b && x !== c && x !== d && x !== e) discards.push(rest[x]);
                const sc = scoreHand(top, mid, botCards);
                if (sc.foul) continue;
                if (sc.topEval.type < 3 && sc.botEval.type < 7) continue;
                if (sc.royalties > bestRoy ||
                    (sc.royalties === bestRoy && bestSc && (
                        sc.topEval.value > bestSc.topEval.value ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value > bestSc.midEval.value) ||
                        (sc.topEval.value === bestSc.topEval.value && sc.midEval.value === bestSc.midEval.value && sc.botEval.value > bestSc.botEval.value)
                    ))) {
                    bestRoy = sc.royalties;
                    bestSc = sc;
                    bestLayout = { top, middle: mid, bottom: botCards, discards };
                }
            }
        }
        if (!bestLayout) return null;
        return returnLayout ? { layout: bestLayout, royalty: bestRoy, sc: bestSc } : bestLayout;
    }

    // === 顶 trips 锚的智能分配 ===
    // 方案 A: 枚举 mid 高 rank trips 候选, 每个对 bot 穷举 C(N,5) 选最高 royalty
    // 方案 B: 退回 beam 搜 mid/bot (兜底, 处理非 trips mid 结构如 flush/straight)
    _buildTopTripsLayout(topCards, dealt, discardCount) {
        const topUsedIds = new Set(topCards.map(c => cardId(c)));
        const remaining = dealt.filter(c => !topUsedIds.has(cardId(c)));
        if (remaining.length !== 10 + discardCount) return null;
        const real = remaining.filter(c => !isJoker(c));
        const jokers = remaining.filter(c => isJoker(c));
        const J = jokers.length;
        const realTop = topCards.find(c => !isJoker(c));
        const topRankIdx = realTop ? rankIndex(realTop.rank) : 12;
        const byRank = {};
        for (const c of real) (byRank[c.rank] = byRank[c.rank] || []).push(c);
        let best = null;
        // 方案 A: 枚举高 rank mid trips 候选
        for (const r of Object.keys(byRank)) {
            if (rankIndex(r) <= topRankIdx) continue;
            const cs = byRank[r];
            if (cs.length + J >= 3) {
                const realUsed = cs.slice(0, 3);
                const jUsed = jokers.slice(0, Math.max(0, 3 - realUsed.length));
                const midCore = [...realUsed, ...jUsed];
                const r2 = this._enumBotForTopMidTrips(topCards, remaining, midCore, discardCount);
                if (r2 && (!best || r2.royalty > best.royalty)) best = r2;
            }
        }
        // 方案 B: beam 兜底 (catch mid flush/straight/full house 等)
        const beamLayout = this._distributeMidBotByBeam(topCards, remaining, discardCount, 15);
        if (beamLayout) {
            const sc = scoreHand(beamLayout.top, beamLayout.middle, beamLayout.bottom);
            if (!sc.foul) {
                const trig = checkFantasyTrigger({ top: beamLayout.top, middle: beamLayout.middle, bottom: beamLayout.bottom }, true);
                if (trig && (!best || sc.royalties > best.royalty)) {
                    best = { layout: beamLayout, royalty: sc.royalties };
                }
            }
        }
        return best ? best.layout : null;
    }

    // 给定 top + mid trips core, 穷举 bot 5-subset (C(10,5)=252) 选最高 royalty
    _enumBotForTopMidTrips(topCards, remaining, midCore, discardCount) {
        const midUsedIds = new Set(midCore.map(c => cardId(c)));
        const rest10 = remaining.filter(c => !midUsedIds.has(cardId(c)));
        if (rest10.length !== 7 + discardCount) return null; // 5 bot + 2 mid-kicker + discardCount = 10
        const N = rest10.length;
        const combos = [];
        const gen = (start, current) => {
            if (current.length === 5) { combos.push(current.slice()); return; }
            for (let i = start; i < N; i++) { current.push(i); gen(i + 1, current); current.pop(); }
        };
        gen(0, []);
        let best = null;
        for (const botIdxs of combos) {
            const bot = botIdxs.map(i => rest10[i]);
            const botSet = new Set(botIdxs);
            const restCards = [];
            for (let i = 0; i < N; i++) if (!botSet.has(i)) restCards.push(rest10[i]);
            // 排序 desc: top 2 → mid 补足, 后剩为 discards
            restCards.sort((a, b) => (isJoker(b) ? 13 : rankIndex(b.rank)) - (isJoker(a) ? 13 : rankIndex(a.rank)));
            const midKickers = restCards.slice(0, 2);
            const discards = restCards.slice(2);
            if (discards.length !== discardCount) continue;
            const fullMid = [...midCore, ...midKickers];
            const sc = scoreHand(topCards, fullMid, bot);
            if (sc.foul) continue;
            const trig = checkFantasyTrigger({ top: topCards, middle: fullMid, bottom: bot }, true);
            if (!trig) continue;
            if (!best || sc.royalties > best.royalty) {
                best = { layout: { top: topCards, middle: fullMid, bottom: bot, discards }, royalty: sc.royalties };
            }
        }
        return best;
    }

    // 给定 top, 用 beam search 把 remaining 分配到 mid/bot/discards (royalty 优化)
    _distributeMidBotByBeam(top, remaining, discardCount, beamWidth) {
        let beam = [{ mid: [], bot: [], discards: [], score: 0 }];
        for (const card of remaining) {
            const next = [];
            for (const item of beam) {
                if (item.mid.length < 5) next.push({ ...item, mid: [...item.mid, card] });
                if (item.bot.length < 5) next.push({ ...item, bot: [...item.bot, card] });
                if (item.discards.length < discardCount) next.push({ ...item, discards: [...item.discards, card] });
            }
            if (next.length === 0) return null;
            for (const n of next) n.score = this._partialRoyaltyScore(n);
            next.sort((a, b) => b.score - a.score);
            beam = next.slice(0, beamWidth);
        }
        // 完整 + 非 foul + max royalty
        let best = null;
        for (const b of beam) {
            if (b.mid.length !== 5 || b.bot.length !== 5 || b.discards.length !== discardCount) continue;
            const sc = scoreHand(top, b.mid, b.bot);
            if (sc.foul) continue;
            if (!best || sc.royalties > best.royalty) {
                best = { layout: { top, middle: b.mid, bottom: b.bot, discards: b.discards }, royalty: sc.royalties };
            }
        }
        return best ? best.layout : null;
    }

    // 简单 royalty 潜力评分 (用于 beam, 不是 MLP)
    _partialRoyaltyScore(state) {
        let score = 0;
        const evalRow = (row, weight) => {
            if (row.length === 0) return;
            const ranks = {}, suits = {};
            const jokers = row.filter(c => isJoker(c)).length;
            for (const c of row) {
                if (isJoker(c)) continue;
                ranks[c.rank] = (ranks[c.rank] || 0) + 1;
                suits[c.suit] = (suits[c.suit] || 0) + 1;
            }
            const rankCounts = Object.values(ranks);
            const maxSameRank = Math.max(0, ...rankCounts) + jokers;
            // 强结构: trips/quads/full house
            if (maxSameRank >= 4) score += 20 * weight;
            else if (maxSameRank >= 3) score += 10 * weight;
            else if (maxSameRank >= 2) score += 3 * weight;
            const pairCnt = rankCounts.filter(v => v >= 2).length;
            if (pairCnt >= 2) score += 5 * weight; // two pair
            // 同花潜力
            const maxSuit = Math.max(0, ...Object.values(suits)) + jokers;
            if (maxSuit >= 5) score += 12 * weight;
            else if (maxSuit >= 4) score += 5 * weight;
            else if (maxSuit >= 3) score += 1.5 * weight;
            // 顺子潜力
            const ri = [...new Set(row.filter(c => !isJoker(c)).map(c => rankIndex(c.rank)))].sort((a, b) => a - b);
            let bestRun = 1, run = 1;
            for (let i = 1; i < ri.length; i++) {
                if (ri[i] - ri[i-1] <= 2) { run++; bestRun = Math.max(bestRun, run); } else run = 1;
            }
            const effRun = bestRun + jokers;
            if (effRun >= 5) score += 10 * weight;
            else if (effRun >= 4) score += 4 * weight;
        };
        evalRow(state.bot, 1.5);  // bot 权重高
        evalRow(state.mid, 1.0);
        return score;
    }

    // === 直接 re-fan 搜: 枚举锚 → 构 layout → 选最高 royalty 非 foul ===
    // 排序锚 (按预期 royalty), 加 time budget 上限 (默认 2s)
    _directReFanSearch(dealt, discardCount, timeBudgetMs = 2000) {
        const anchors = this._findReFanAnchors(dealt);
        // 排锚优先级: top trips 按 rank 高的优先, bot 4-kind 按 rank 高的优先, bot SF 任意顺
        anchors.sort((a, b) => {
            const aRank = a.cards.find(c => !isJoker(c));
            const bRank = b.cards.find(c => !isJoker(c));
            const ar = aRank ? rankIndex(aRank.rank) : 12;
            const br = bRank ? rankIndex(bRank.rank) : 12;
            // bot anchors first (usually stronger), within: rank desc
            const aType = a.row === 'bot' ? 0 : 1;
            const bType = b.row === 'bot' ? 0 : 1;
            if (aType !== bType) return aType - bType;
            return br - ar;
        });
        const cands = [];
        const startTime = Date.now();
        for (const anchor of anchors) {
            if (Date.now() - startTime > timeBudgetMs) break;
            const layout = this._buildLayoutFromAnchor(anchor, dealt, discardCount);
            if (!layout) continue;
            const sc = scoreHand(layout.top, layout.middle, layout.bottom);
            if (sc.foul) continue;
            if (sc.topEval.type < 3 && sc.botEval.type < 7) continue;
            cands.push({ layout, royalty: sc.royalties, anchorType: anchor.type });
        }
        if (cands.length > 0) {
            // royalty 相同时优先: bot > mid > top 的 value 高 (赢行加分 + 视觉好看)
            for (const c of cands) {
                const sc = scoreHand(c.layout.top, c.layout.middle, c.layout.bottom);
                c._sc = sc;
            }
            cands.sort((a, b) => {
                if (b.royalty !== a.royalty) return b.royalty - a.royalty;
                // Tie-break: 顶高 > 中高 > 底高 (头道更常出现高牌决胜, 优先放高牌上去)
                if (b._sc.topEval.value !== a._sc.topEval.value) return b._sc.topEval.value - a._sc.topEval.value;
                if (b._sc.midEval.value !== a._sc.midEval.value) return b._sc.midEval.value - a._sc.midEval.value;
                return b._sc.botEval.value - a._sc.botEval.value;
            });
            return cands[0];
        }
        // === Phase 2: re-fan 不可达 → 试非 re-fan 高价值锚 (max royalty) ===
        const nrAnchors = this._findNonRefanAnchors(dealt);
        const nrCands = [];
        for (const anchor of nrAnchors) {
            if (Date.now() - startTime > timeBudgetMs) break;
            const layout = this._buildLayoutFromNonRefanAnchor(anchor, dealt, discardCount);
            if (!layout) continue;
            const sc = scoreHand(layout.top, layout.middle, layout.bottom);
            if (sc.foul) continue;
            nrCands.push({ layout, royalty: sc.royalties });
        }
        if (nrCands.length > 0) {
            for (const c of nrCands) {
                const sc = scoreHand(c.layout.top, c.layout.middle, c.layout.bottom);
                c._sc = sc;
            }
            nrCands.sort((a, b) => {
                if (b.royalty !== a.royalty) return b.royalty - a.royalty;
                // Tie-break: 顶高 > 中高 > 底高 (头道更常出现高牌决胜, 优先放高牌上去)
                if (b._sc.topEval.value !== a._sc.topEval.value) return b._sc.topEval.value - a._sc.topEval.value;
                if (b._sc.midEval.value !== a._sc.midEval.value) return b._sc.midEval.value - a._sc.midEval.value;
                return b._sc.botEval.value - a._sc.botEval.value;
            });
            return nrCands[0];
        }
        return null;
    }

    _expertPlaceFantasyImpl(state, dealt, discardCount, beamWidth, epsilon) {
        const N = dealt.length;
        // 高 rank 优先 (剪枝更好), joker 排到最后 (看完真牌再决定百搭去哪更优)
        const sorted = [...dealt].sort((a, b) => {
            if (isJoker(a) && !isJoker(b)) return 1;
            if (!isJoker(a) && isJoker(b)) return -1;
            if (isJoker(a) && isJoker(b)) return 0;
            return rankIndex(b.rank) - rankIndex(a.rank);
        });
        let beam = [{ top: [], mid: [], bot: [], discards: [], score: 0 }];
        for (let depth = 0; depth < N; depth++) {
            const card = sorted[depth];
            const next = [];
            for (const item of beam) {
                const opts = [];
                if (item.top.length < 3) opts.push({ ...item, top: [...item.top, card] });
                if (item.mid.length < 5) opts.push({ ...item, mid: [...item.mid, card] });
                if (item.bot.length < 5) opts.push({ ...item, bot: [...item.bot, card] });
                if (item.discards.length < discardCount) opts.push({ ...item, discards: [...item.discards, card] });
                for (const o of opts) next.push(o);
            }
            if (next.length === 0) return null;
            // 评估每个候选 (用 trainedEval)
            for (const n of next) {
                const tmp = state.clone();
                tmp.top = [...tmp.top, ...n.top];
                tmp.middle = [...tmp.middle, ...n.mid];
                tmp.bottom = [...tmp.bottom, ...n.bot];
                n.score = this.trainedEval(tmp);
                // 提前 foul 检测: 用极大惩罚 (-1e6) 确保 fouled 候选被踢出 beam
                // (旧 -200 在某些 ckpt 下被 MLP 高分压制, 导致 beam 全 foul)
                if (n.top.length === 3 && n.mid.length === 5) {
                    const tE = n.top.some(isJoker) ? evaluate3Joker(n.top) : evaluate3(n.top);
                    const mE = n.mid.some(isJoker) ? evaluate5Joker(n.mid) : evaluate5(n.mid);
                    if (handExceeds5(tE, mE)) n.score -= 1e6;
                }
                if (n.mid.length === 5 && n.bot.length === 5) {
                    const mE = n.mid.some(isJoker) ? evaluate5Joker(n.mid) : evaluate5(n.mid);
                    const bE = n.bot.some(isJoker) ? evaluate5Joker(n.bot) : evaluate5(n.bot);
                    if (mE.value > bE.value) n.score -= 1e6;
                }
            }
            next.sort((a, b) => b.score - a.score);
            beam = next.slice(0, beamWidth);
        }
        // 选最佳 valid 完整状态
        const completed = beam.filter(b => b.top.length === 3 && b.mid.length === 5 && b.bot.length === 5);
        if (completed.length === 0) {
            beam.sort((a, b) => (b.top.length + b.mid.length + b.bot.length) - (a.top.length + a.mid.length + a.bot.length));
            return null;
        }
        // === 硬过滤: 优先 non-foul layout ===
        const nonFoul = completed.filter(b => {
            const sc = scoreHand(b.top, b.mid, b.bot);
            return !sc.foul;
        });
        // 如果 beam 全 foul (理论上 14+ 张 dealt 不该发生, MLP 学坏的标志)
        // 用 anti-foul fallback: 试多种排法找 non-foul
        if (nonFoul.length === 0) {
            const safe = this._antiFoulFallback(dealt, discardCount);
            if (safe) return { layout: safe, score: -1e6 };
        }
        const valid = nonFoul.length > 0 ? nonFoul : completed;
        valid.sort((a, b) => b.score - a.score);
        let chosen;
        if (epsilon > 0 && Math.random() < epsilon && valid.length > 1) {
            chosen = valid[Math.floor(Math.random() * valid.length)];
        } else {
            chosen = valid[0];
        }
        return {
            layout: { top: chosen.top, middle: chosen.mid, bottom: chosen.bot, discards: chosen.discards },
            score: chosen.score
        };
    }

    // 兜底: 当 beam 全 foul 时尝试多种安全摆法
    _antiFoulFallback(dealt, discardCount) {
        const ascSorted = [...dealt].sort((a, b) =>
            (isJoker(a) ? 13 : rankIndex(a.rank)) - (isJoker(b) ? 13 : rankIndex(b.rank))
        );
        const tryLayout = (top, mid, bot, disc) => {
            if (top.length !== 3 || mid.length !== 5 || bot.length !== 5) return null;
            const sc = scoreHand(top, mid, bot);
            return sc.foul ? null : { top, middle: mid, bottom: bot, discards: disc };
        };

        // 策略 1: 升序, 弃最低, 顶 3 低 / 中 5 / 底 5 高 (鬼放底)
        {
            const disc = ascSorted.slice(0, discardCount);
            const rest = ascSorted.slice(discardCount);
            const r = tryLayout(rest.slice(0, 3), rest.slice(3, 8), rest.slice(8, 13), disc);
            if (r) return r;
        }
        // 策略 2: 弃中段 (让 top 极低, mid 较低, bot 留鬼+高)
        {
            const len = ascSorted.length;
            const top = ascSorted.slice(0, 3);
            const disc = ascSorted.slice(3, 3 + discardCount); // 弃顶上面那段
            const remaining = [...ascSorted.slice(3, 3 + discardCount).map(() => null), ...ascSorted.slice(3 + discardCount)].filter(Boolean);
            // 简化: top + remaining (10 cards) → mid 5 + bot 5
            const mid = remaining.slice(0, 5);
            const bot = remaining.slice(5, 10);
            const r = tryLayout(top, mid, bot, disc);
            if (r) return r;
        }
        // 策略 3: 弃最高 (让 bot 中等强度, mid/top 弱) — 罕见但可用
        {
            const disc = ascSorted.slice(-discardCount);
            const rest = ascSorted.slice(0, ascSorted.length - discardCount);
            const r = tryLayout(rest.slice(0, 3), rest.slice(3, 8), rest.slice(8, 13), disc);
            if (r) return r;
        }
        // 策略 4: 随机抽 dealt 几次, 升序填 top/mid/bot
        for (let i = 0; i < 20; i++) {
            const shuffled = [...ascSorted];
            for (let j = shuffled.length - 1; j > 0; j--) {
                const k = Math.floor(Math.random() * (j + 1));
                [shuffled[j], shuffled[k]] = [shuffled[k], shuffled[j]];
            }
            const disc = shuffled.slice(0, discardCount);
            const rest = shuffled.slice(discardCount).sort((a, b) =>
                (isJoker(a) ? 13 : rankIndex(a.rank)) - (isJoker(b) ? 13 : rankIndex(b.rank))
            );
            const r = tryLayout(rest.slice(0, 3), rest.slice(3, 8), rest.slice(8, 13), disc);
            if (r) return r;
        }
        return null;
    }

    // 快速rollout: 随机发剩余牌, 贪心放置, 返回完整评估
    // rollout评估: 牌力 + 排序 + draw + 犯规防护 + 范特西感知
    simpleEval(state) {
        const { top, middle, bottom } = state;
        let score = 0;

        // === 快速牌型识别 (joker-aware: 鬼牌可以补任意 rank, 提升虚牌型) ===
        const getType = (cards) => {
            if (cards.length === 0) return 0;
            const jokerCnt = cards.filter(c => c.rank === 'X').length;
            const realCounts = {};
            for (const c of cards) if (c.rank !== 'X') realCounts[c.rank] = (realCounts[c.rank] || 0) + 1;
            const vals = Object.values(realCounts);
            const mx = vals.length > 0 ? Math.max(...vals) : 0;
            const pairs = vals.filter(v => v >= 2).length;
            // 鬼牌策略: 倾向补强最大的真实组 (mx + jokerCnt 4= 4K, 3=trip, ...)
            const eff = mx + jokerCnt;
            if (eff >= 4) return HAND_TYPE.FOUR_OF_A_KIND;
            if (eff >= 3 && pairs >= 2) return HAND_TYPE.FULL_HOUSE; // trip+2nd pair
            if (eff >= 3 && jokerCnt >= 1 && vals.length >= 2) return HAND_TYPE.FULL_HOUSE; // trip+pair via joker补另一对
            if (eff >= 3) return HAND_TYPE.THREE_OF_A_KIND;
            if (pairs >= 2 || (pairs === 1 && jokerCnt >= 1)) return HAND_TYPE.TWO_PAIR;
            if (eff >= 2) return HAND_TYPE.PAIR;
            return HAND_TYPE.HIGH_CARD;
        };

        const getPairRank = (cards) => {
            const jokerCnt = cards.filter(c => c.rank === 'X').length;
            const counts = {};
            for (const c of cards) if (c.rank !== 'X') counts[c.rank] = (counts[c.rank] || 0) + 1;
            let mx = -1;
            for (const [r, cnt] of Object.entries(counts)) {
                if (cnt >= 2) mx = Math.max(mx, rankIndex(r));
            }
            if (jokerCnt >= 1 && mx < 0) {
                const realRanks = Object.keys(counts).map(rankIndex);
                if (realRanks.length > 0) mx = Math.max(...realRanks);
                else mx = 12;
            }
            return mx;
        };

        const botFull = bottom.length === 5;
        const midFull = middle.length === 5;
        const topFull = top.length === 3;
        const botType = botFull ? evaluate5(bottom).type : getType(bottom);
        const midType = midFull ? evaluate5(middle).type : getType(middle);
        const topType = topFull ? evaluate3(top).type : getType(top);

        // === 1. 牌型得分 ===
        // 底道: 越强越好 (支撑整个牌面)
        score += botType * 22;
        // 中道: 高牌是最大问题
        score += midType * 18;
        // 头道: 对子好
        score += topType * 6;

        // 底道完整时加royalty预估
        if (botFull) {
            const br = getBottomRoyalty(bottom);
            score += br * 3;
        }
        // 中道完整时加royalty
        if (midFull) {
            const mr = getMiddleRoyalty(middle);
            score += mr * 3;
        }

        // === 2. 排序 (最关键: 犯规 = -20) ===
        // 确定性犯规检测
        if (topFull && midFull) {
            const tE = evaluate3(top);
            const mE = evaluate5(middle);
            if (mE.type < tE.type) return -500;
            if (mE.type === tE.type && tE.type === HAND_TYPE.PAIR) {
                if (getPairRank(middle) < getPairRank(top)) return -500;
            }
        }
        if (midFull && botFull) {
            if (evaluate5(middle).value > evaluate5(bottom).value) return -500;
        }

        // 潜在犯规: 头道 vs 中道
        if (top.length >= 2 && middle.length >= 1) {
            if (topType > midType) {
                const ms = 5 - middle.length;
                if (topType >= HAND_TYPE.THREE_OF_A_KIND) {
                    // 头道三条: 中道需要三条+ (极难!)
                    score -= ms >= 3 ? 60 : ms >= 2 ? 100 : ms >= 1 ? 200 : 500;
                } else if (topType === HAND_TYPE.PAIR && getPairRank(top) >= 10 && ms >= 3) {
                    // QQ+追范 + 中道空位多(>=3): 轻惩罚 (4轮可以补)
                    score -= 8;
                } else {
                    score -= ms >= 3 ? 15 : ms >= 2 ? 30 : ms >= 1 ? 60 : 200;
                }
            }
            if (topType === midType && topType >= HAND_TYPE.PAIR) {
                const tpr = getPairRank(top), mpr = getPairRank(middle);
                if (tpr >= 0 && mpr >= 0) {
                    if (mpr > tpr) score += 8;
                    else if (mpr < tpr) {
                        const ms = 5 - middle.length;
                        score -= ms >= 2 ? 20 : ms >= 1 ? 50 : 150;
                    }
                }
            }
        }
        if (middle.length >= 2 && bottom.length >= 2) {
            if (midType > botType) {
                const bs = 5 - bottom.length;
                score -= bs >= 3 ? 10 : bs >= 2 ? 20 : bs >= 1 ? 50 : 150;
            }
            if (midType === botType && midType >= HAND_TYPE.PAIR) {
                const mpr = getPairRank(middle), bpr = getPairRank(bottom);
                if (mpr >= 0 && bpr >= 0) {
                    if (bpr > mpr) score += 8;
                    else if (bpr < mpr) {
                        const bs = 5 - bottom.length;
                        score -= bs >= 2 ? 15 : bs >= 1 ? 35 : 100;
                    }
                }
            }
        }
        // 正确排序奖励
        if (botType > midType && bottom.length >= 2 && middle.length >= 2) score += 15;
        if (midType > topType && middle.length >= 2 && top.length >= 1) score += 10;

        // 小对错位惩罚 / 中道种子对子奖励 (R1~R3) — 用户偏好 fan 率高 vs foul 接近
        // 底道小对早期易被中道反超, 应让对子搬中道. 加大权重防 MC 翻盘
        // 22(idx0): -64 ; 55(idx3): -40 ; 66(idx4): -32 ; 77(idx5): -24 ; 88(idx6): -16
        if (!botFull && botType === HAND_TYPE.PAIR && bottom.length >= 2) {
            const bpr = getPairRank(bottom);
            const botJoker = bottom.some(c => c.rank === 'X');
            if (!botJoker && bpr >= 0 && bpr < 7) score -= (8 - bpr) * 8;
        }
        // 中道小/中对 (R1~R3): 是优质 "种子" — 后续 4 轮可成 trips/two-pair
        if (!midFull && midType === HAND_TYPE.PAIR && middle.length <= 3 && bottom.length >= 2) {
            const mpr = getPairRank(middle);
            const midJoker = middle.some(c => c.rank === 'X');
            if (!midJoker && mpr >= 0 && mpr < 10) score += 18;
        }

        // === 3. 头道 ===
        if (top.length > 0) {
            const topMax = Math.max(...top.map(c => rankIndex(c.rank)));
            if (topType === HAND_TYPE.HIGH_CARD) {
                // 头道无对子
                if (topFull && topMax >= 10) score -= (topMax - 9) * 8; // 满了+大牌=犯规风险
                else if (!topFull && topMax >= 10) {
                    // QKA (rank>=10) 有范特西配对潜力
                    const highCards = top.filter(c => rankIndex(c.rank) >= 10);
                    if (highCards.length >= 2) score += 12;
                    else score += 5;
                }
                else if (topMax === 9) score -= 10; // J不能进范
                // 8-J (rank 6-9) 在头道: 不能进范 + 抬高犯规风险
                if (!topFull && topMax >= 6 && topMax <= 9) score -= (topMax - 4) * 3;
            }
            // 头道有对子但低于QQ: 惩罚 (TT=5分,JJ=6分,不值得犯规风险)
            if (topType === HAND_TYPE.PAIR) {
                const tpr = getPairRank(top);
                if (tpr >= 0 && tpr < 10) {
                    // QQ以下的对子在头道: royalty很低(1-6分), 犯规风险高
                    score -= (10 - tpr) * 3; // TT=-6, 99=-9, 88=-12, ...
                }
            }
        }

        // === 4. 范特西感知 ===
        const topPR = top.length >= 2 ? getPairRank(top) : -1;
        const topTrips = topFull && topType >= HAND_TYPE.THREE_OF_A_KIND;
        const chasingFantasy = topPR >= 10 || topTrips;
        if (chasingFantasy) {
            // QQ+在头道: 范特西价值 = +50分 + 7~9 royalty ≈ +57~59
            // R1阶段(总5张): 中道还有4轮发展, 给大奖励让它进预筛
            const totalCards = top.length + middle.length + bottom.length;
            if (totalCards <= 5) {
                // R1刚摆完: 根据头道pair rank给不同奖励
                // KK/AA 的进范奖励更高 (KK=58, AA=59), 值得更积极追
                if (topTrips) {
                    score += 45; // 三条: 评估+400
                } else if (topPR >= 12) {
                    score += 35; // AA: 评估+200
                } else if (topPR >= 11) {
                    score += 28; // KK: 评估+100
                } else {
                    score += 18; // QQ: 评估+50
                }
                if (middle.length >= 1) {
                    const midMax = Math.max(...middle.map(c => rankIndex(c.rank)));
                    if (midMax >= 10) score += 5;
                }
            } else if (topTrips) {
                // 三条头道: 中道需要三条+ (royalty 巨大: 55+三条值)
                if (midType >= HAND_TYPE.THREE_OF_A_KIND) score += 60;
                else if (midType >= HAND_TYPE.TWO_PAIR) score += 20;
                else if (midType >= HAND_TYPE.PAIR) score += 5;
                else if (middle.length >= 3) score -= 40;
            } else {
                // QQ+/KK+/AA+ 后续轮次: 根据中道支撑调整
                if (midType >= HAND_TYPE.TWO_PAIR) score += 40;
                else if (midType >= HAND_TYPE.PAIR) {
                    const mpr = getPairRank(middle);
                    if (mpr >= topPR) score += 35;
                    else if (mpr >= 8) score += 20;
                    else score += 10;
                } else if (middle.length >= 3) {
                    score -= 30;
                }
            }
            if (botType >= HAND_TYPE.PAIR) score += 8;
        }

        // === 5. Draw潜力 (底道和中道) ===
        for (const row of [bottom, middle]) {
            if (row.length < 2 || row.length >= 5) continue;
            const isBot = row === bottom;
            const w = isBot ? 1.0 : 0.6; // 底道draw价值更高

            // 同花draw
            const sc = {};
            for (const c of row) sc[c.suit] = (sc[c.suit] || 0) + 1;
            const mxs = Math.max(...Object.values(sc));
            if (mxs >= 4) score += 25 * w;      // 差1张
            else if (mxs >= 3) score += 12 * w;  // 差2张

            // 顺子draw: 检查连牌
            const ri = [...new Set(row.map(c => rankIndex(c.rank)))].sort((a,b) => a-b);
            let bestRun = 1, run = 1;
            for (let i = 1; i < ri.length; i++) {
                if (ri[i] - ri[i-1] <= 2) { run++; bestRun = Math.max(bestRun, run); }
                else run = 1;
            }
            if (bestRun >= 4) score += 15 * w;
            else if (bestRun >= 3 && row.length <= 3) score += 6 * w;

            // 同花顺潜力
            if (mxs >= 3 && bestRun >= 3) {
                const fs = Object.entries(sc).find(([s,c]) => c >= 3)[0];
                const fr = row.filter(c => c.suit === fs).map(c => rankIndex(c.rank)).sort((a,b)=>a-b);
                let frun = 1;
                for (let i = 1; i < fr.length; i++) { if (fr[i]-fr[i-1] <= 2) frun++; }
                if (frun >= 3) score += 20 * w;
            }
        }

        // === 4a. 浪费追范惩罚 (R1) — 顶没自带范, 中底却堆了 2+ 个 QQ+ 真对 ===
        // 至少一个高对应该上顶进范, 都丢中底 = 浪费 fantasy 机会
        if (!chasingFantasy && top.length + middle.length + bottom.length === 5) {
            let highPairsInMidBot = 0;
            for (const row of [middle, bottom]) {
                const cnt = {};
                for (const c of row) if (c.rank !== 'X') cnt[c.rank] = (cnt[c.rank]||0) + 1;
                for (const [r, v] of Object.entries(cnt)) {
                    if (v >= 2 && rankIndex(r) >= 10) highPairsInMidBot++;
                }
            }
            if (highPairsInMidBot >= 2) score -= 35;
        }

        // === 4b. 鬼上顶倾向 (R1) — 鬼是 fan 钥匙, 应上顶博进范 ===
        // 用户原则: 鬼的价值在"博未来 fan", 不在"现状虚 pair". 单鬼 +50 royalty fan vs 鬼上底 +6 虚 pair = 一个量级差距
        // chasingFantasy 已 fired (顶有 QQ+/trips) 时不重复奖
        // veto: 中/底有 QQ+ 真对时不奖 — 说明大对应该上顶, 鬼上顶是次优配置
        if (!chasingFantasy && top.length >= 1 && top.length < 3) {
            const topJokerCnt = top.filter(c => c.rank === 'X').length;
            if (topJokerCnt >= 1) {
                let hasHighPairElsewhere = false;
                for (const row of [middle, bottom]) {
                    const cnt = {};
                    for (const c of row) if (c.rank !== 'X') cnt[c.rank] = (cnt[c.rank]||0) + 1;
                    for (const [r, v] of Object.entries(cnt)) {
                        if (v >= 2 && rankIndex(r) >= 10) { hasHighPairElsewhere = true; break; }
                    }
                    if (hasHighPairElsewhere) break;
                }
                if (!hasHighPairElsewhere) score += 50 * topJokerCnt;
            }
        }

        // === 4c. R1 鬼放底/中 仅靠虚 PAIR 撑分: 重惩罚 ===
        // botType=PAIR 但实际没真对子 (只是 joker 虚拟成对) — 鬼应去顶博 fan
        // 仅 R1 触发 (totalCards=5), 后期允许鬼撑底牌
        if (top.length + middle.length + bottom.length === 5) {
            const realPair = (cards) => {
                const cnt = {};
                for (const c of cards) if (c.rank !== 'X') cnt[c.rank] = (cnt[c.rank]||0) + 1;
                return Object.values(cnt).some(v => v >= 2);
            };
            const botJoker = bottom.some(c => c.rank === 'X');
            const midJoker = middle.some(c => c.rank === 'X');
            // 鬼在底/中 + 该行无真对 + 顶没鬼 → 错位严重 (鬼浪费在虚 pair, 不博 fan)
            if (botJoker && !realPair(bottom) && !top.some(c => c.rank === 'X')) score -= 40;
            if (midJoker && !realPair(middle) && !top.some(c => c.rank === 'X')) score -= 25;
        }

        // === 5b. 自然 SF draw (3+ 真牌同花在 5-rank 窗口) — 仅在有鬼场景启用 ===
        // 修 R1 拆 TJQc 问题: 鬼凑虚 PAIR 容易拿 +22 type 分, 把"真 SF 三连留同行"挤出 stage1。
        // 只在场上/手上有鬼时加奖, 避免扰乱无鬼场景 (v7 R006 14/14 已稳定)。
        const hasAnyJoker = top.some(c=>c.rank==='X') || middle.some(c=>c.rank==='X') || bottom.some(c=>c.rank==='X');
        if (hasAnyJoker) {
            for (const row of [bottom, middle]) {
                if (row.length < 3) continue;
                const realBySuit = {};
                for (const c of row) {
                    if (c.rank === 'X') continue;
                    if (!realBySuit[c.suit]) realBySuit[c.suit] = [];
                    realBySuit[c.suit].push(rankIndex(c.rank));
                }
                let sfNat = false;
                for (const s of Object.keys(realBySuit)) {
                    const sorted = [...new Set(realBySuit[s])].sort((a,b) => a-b);
                    if (sorted.length < 3) continue;
                    for (let i = 0; i <= sorted.length - 3; i++) {
                        if (sorted[i+2] - sorted[i] <= 4) { sfNat = true; break; }
                    }
                    if (sfNat) break;
                }
                if (sfNat) score += row === bottom ? 50 : 30;
            }
        }

        // === 6. 中道惩罚/奖励 ===
        if (middle.length >= 3 && !midFull && midType === HAND_TYPE.HIGH_CARD) {
            score -= 12; // 中道3+张还没对子
        }

        // === 7. 均衡性 ===
        const totalCards = top.length + middle.length + bottom.length;

        if (totalCards === 5) {
            // R1: 头道空 = 好! 保留完整追范灵活性
            // 中道或底道空 = 不好(后面填不满)
            if (middle.length === 0) score -= 40;
            if (bottom.length === 0) score -= 30;
            if (bottom.length === 1) score -= 18; // R1 底只 1 张 = 太薄, 后续填不出像样底道
            // 头道0-1张都好
            if (top.length === 0) score += 8;  // 全空=最大灵活性
            if (top.length === 1 && bottom.length >= 2) score += 5;
        }

        // === 硬规则: 头道灵活性 ===
        // 头道3张满了但没三条 = R1战略失误(永远不该这么做)
        if (top.length === 3 && totalCards <= 7 && topType < HAND_TYPE.THREE_OF_A_KIND) {
            if (topType === HAND_TYPE.PAIR && getPairRank(top) >= 10) {
                // QQ+/KK+/AA+ = 追范, OK
            } else {
                score -= 200; // R1把头道填满 = 灾难
            }
        }
        // 头道2张无对: 只在前期惩罚
        if (top.length === 2 && topType < HAND_TYPE.PAIR && totalCards <= 9) {
            score -= 50;
        }

        // 行均衡: 一行满了但其他行空位多 → 后面填不完
        if (totalCards >= 5) {
            const roundsPlayed = Math.ceil((totalCards - 5) / 2) + 1;
            if (middle.length === 5 && roundsPlayed <= 3) {
                const need = (3 - top.length) + (5 - bottom.length);
                const can = (5 - roundsPlayed) * 2;
                if (need > can) score -= 100;
                else if (need === can) score -= 20;
            }
            if (bottom.length === 5 && roundsPlayed <= 3) {
                const need = (3 - top.length) + (5 - middle.length);
                const can = (5 - roundsPlayed) * 2;
                if (need > can) score -= 100;
                else if (need === can) score -= 20;
            }
        }

        return score;
    }

    // 训练出来的评估函数 (替代simpleEval用于rollout)
    // 权重来自80298个MC样本的线性回归, 排序准确率66.4%
    trainedEval(state) {
        const { top, middle, bottom } = state;

        // 快速牌型 (joker-aware)
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
            for (const [r, cnt] of Object.entries(counts)) { if (cnt >= 2) mx = Math.max(mx, rankIndex(r)); }
            if (jokerCnt >= 1 && mx < 0) {
                const realRanks = Object.keys(counts).map(rankIndex);
                if (realRanks.length > 0) mx = Math.max(...realRanks);
                else mx = 12;
            }
            return mx;
        };
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

        const botType = getType(bottom), midType = getType(middle), topType = getType(top);
        const botPR = getPairRank(bottom), midPR = getPairRank(middle), topPR = getPairRank(top);
        const topMax = top.length > 0 ? Math.max(...top.map(c => rankIndex(c.rank))) : 0;
        const topTrips = top.length === 3 && topType >= 3;
        const chasing = topPR >= 10 || topTrips;
        const placed = top.length + middle.length + bottom.length;

        const roundNum = placed <= 5 ? 1 : Math.ceil((placed - 5) / 2) + 1;
        const botMaxSuit = getMaxSuit(bottom), midMaxSuit = getMaxSuit(middle);
        const botRun = getBestRun(bottom);
        const midHasPair = midType >= 1;
        let defSafe = 0, defFoul = 0;
        if (top.length === 3 && middle.length === 5) {
            const tE = evaluate3(top), mE = evaluate5(middle);
            if (mE.type > tE.type) defSafe = 1;
            else if (mE.type < tE.type) defFoul = 1;
            else if (mE.type === tE.type && tE.type === 1) {
                if (midPR > topPR) defSafe = 1; else if (midPR < topPR) defFoul = 1;
            }
        }
        if (middle.length === 5 && bottom.length === 5) {
            if (evaluate5(middle).value > evaluate5(bottom).value) defFoul = 1;
            else defSafe = Math.max(defSafe, 1);
        }

        const _rn = placed <= 5 ? 1 : Math.ceil((placed - 5) / 2) + 1;
        const bMaxS = getMaxSuit(bottom), mMaxS = getMaxSuit(middle);
        const bRn = getBestRun(bottom);
        const mRn = getBestRun(middle);
        const mHP = midType >= 1;
        let _dS = 0, _dF = 0;
        if (top.length === 3 && middle.length === 5) {
            const tE = evaluate3(top), mE = evaluate5(middle);
            if (mE.type > tE.type) _dS = 1;
            else if (mE.type < tE.type) _dF = 1;
            else if (mE.type === tE.type && tE.type === 1) {
                if (midPR > topPR) _dS = 1; else if (midPR < topPR) _dF = 1;
            }
        }
        if (middle.length === 5 && bottom.length === 5) {
            if (evaluate5(middle).value > evaluate5(bottom).value) _dF = 1;
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
            top.length, top.length === 1 ? 1 : 0,
            (top.length === 2 && topType < 1) ? 1 : 0,
            (topType === 0 && topMax >= 10) ? 1 : 0, top.length === 0 ? 1 : 0,
            _rn,
            Math.abs(top.length-1)+Math.abs(middle.length-_rn*5/3)/2+Math.abs(bottom.length-_rn*5/3)/2,
            1, botType, midType, 5-bottom.length,
            bMaxS >= 4 ? 1 : 0, (bMaxS >= 3 && bMaxS < 4) ? 1 : 0,
            bRn >= 4 ? 1 : 0, mMaxS >= 4 ? 1 : 0, (mMaxS >= 3 && mMaxS < 4) ? 1 : 0,
            (chasing && mHP) ? 1 : 0,
            (topPR === 10 && mHP) ? 1 : 0, (topPR === 11 && mHP) ? 1 : 0,
            ((topPR >= 12 || topTrips) && midHasPair) ? 1 : 0,
            (chasing && botType >= 2) ? 1 : 0,
            (chasing && midType === 0 && middle.length >= 3) ? 1 : 0,
            _dS, _dF,
            // === 新增 8 维 feature (v4-ext, 须与train-loop.js一致) ===
            bMaxS * bMaxS / 25,                     // 38
            mMaxS * mMaxS / 25,                     // 39
            bRn / 5,                                // 40
            mRn / 5,                                // 41
            (botPR + 1) / 13,                       // 42
            (midPR + 1) / 13,                       // 43
            (topPR + 1) / 13,                       // 44
            (botType >= 1 && midType >= 1) ? 1 : 0, // 45
            // === v6 新增 4 维 feature (大对/排序错位) ===
            (botPR >= 0 && midPR >= 0 && midPR > botPR) ? 1 : 0, // 46
            botPR >= 8 ? 1 : 0,                                  // 47
            midPR >= 8 ? 1 : 0,                                  // 48
            (botPR >= 0 && midPR >= 0 && botPR > midPR) ? 1 : 0, // 49
            // === v7 新增 6 维 joker-aware feature ===
            (top.filter(c => c.rank === 'X').length + (topPR >= 0 ? 1 : 0)) / 2,    // 50
            (middle.filter(c => c.rank === 'X').length + (midPR >= 0 ? 1 : 0)) / 2, // 51
            (bottom.filter(c => c.rank === 'X').length + (botPR >= 0 ? 1 : 0)) / 2, // 52
            Math.min(1, (bMaxS + bottom.filter(c => c.rank === 'X').length) / 5),   // 53
            Math.min(1, (mMaxS + middle.filter(c => c.rank === 'X').length) / 5),   // 54
            (top.filter(c => c.rank === 'X').length > 0 && topMax >= 10) ? 1 : 0,   // 55
        ];

        // === FL 模式: _useFLMode=true 且 _flWeights 设置时, 用 FL 专用 MLP ===
        // expertPlaceFantasy 在调用前设置 _useFLMode=true, 调用后还原
        if (this._useFLMode && this._flWeights) return this._mlpForward(f, this._flWeights);
        // === 自我对弈: 若 this._weights 设置, 用注入的 MLP (跳过 inline) ===
        // train-loop.js 在每轮训练后注入新权重, 实现 policy iteration loop
        if (this._weights) return this._mlpForward(f, this._weights);

        return this._fastForward(f);



    }

    // === 通用 MLP 前向 (供 self-play 注入权重时调用) ===
    // W: { means, stds, w1, b1, w2, b2, w3, b3, yMean, yStd }, 架构从 W 形状推断

    // _fastForward — 优化版 MLP 前向传播 (56→128→64→1)
    // 首次调用: 解析 inline 权重为 Float32Array (flat row-major) + pre-allocated buffer
    // 后续: 直接使用缓存, 0 分配, ~3-5x 比原 inline 快
    _fastForward(f) {
        if (!this._mlpW) {
        const _m = [0.5128,0.4423,0.1454,0.0400,0.0094,0.3647,0.0663,0.0150,0.0192,0.0327,1.1788,0.5264,0.7702,-0.0772,1.8057,0.2283,0.1478,0.4265,0.1883,3.0262,2.9502,1.0000,1.8485,0.6697,1.2816,0.1082,0.1874,0.0651,0.0192,0.1422,0.0447,0.0111,0.0136,0.0204,0.0354,0.0118,0.2638,0.0590,0.2323,0.1523,0.3724,0.3641,0.4264,0.2550,0.0964,0.3919,0.1067,0.3530,0.1674,0.2541,0.1066,0.2910,0.4596,0.4870,0.3641,0.0652];
        const _s = [0.4998,0.4967,0.3525,0.1960,0.0964,0.4813,0.2489,0.1217,0.1372,0.1779,2.0633,0.8755,3.2185,1.7903,1.1638,0.4197,0.3549,0.4946,0.3910,1.4232,1.6999,1.0000,2.0801,0.8597,1.2440,0.3106,0.3902,0.2468,0.1374,0.3492,0.2065,0.1046,0.1159,0.1413,0.1848,0.1080,0.4407,0.2356,0.2215,0.1272,0.1971,0.1891,0.3764,0.3233,0.2597,0.4882,0.3087,0.4779,0.3734,0.4353,0.2749,0.3094,0.4023,0.2289,0.1794,0.2469];
        const _w1 = [[-0.0570,0.0845,0.0110,0.1450,-0.0827,-0.0316,-0.0269,0.0401,0.0435,0.0039,0.0325,0.1066,0.0149,0.0148,-0.1086,-0.0480,-0.0968,-0.0086,0.1067,0.0668,0.0694,0.0785,0.0948,0.0263,0.2282,0.0243,-0.0656,0.0732,-0.0986,-0.0833,-0.0293,0.0443,-0.0595,0.0078,0.0680,0.0131,-0.0179,-0.1064,-0.1121,0.0108,-0.0557,0.0878,-0.1166,0.0633,-0.0352,-0.0724,-0.0501,0.0491,0.0170,-0.0503,0.0194,0.1111,0.0109,-0.0039,0.0413,0.0008],[0.0298,-0.0145,0.0509,0.0503,-0.0864,0.0394,-0.0038,-0.0441,-0.0923,0.0754,0.0426,0.1062,0.0500,-0.0022,0.0661,-0.0178,-0.1028,0.0758,-0.0380,0.0310,-0.0375,-0.0756,0.0216,-0.0221,-0.0254,0.0934,0.0808,-0.0621,-0.0923,-0.0817,-0.0946,-0.0375,0.1008,-0.1060,0.0143,0.0369,-0.0969,0.0218,-0.0076,-0.1055,0.0863,0.0294,0.0759,-0.0211,-0.0846,-0.0800,0.0225,-0.0367,0.0554,-0.0540,0.0526,-0.1059,-0.0042,0.0429,0.0531,-0.0964],[0.1327,0.0229,-0.0871,-0.0559,0.0542,0.0670,-0.0397,-0.0529,0.0702,0.0350,0.0476,-0.0382,0.0179,-0.0035,0.0487,0.0275,0.1573,-0.1011,0.0351,0.0375,-0.0295,-0.0812,-0.0442,-0.0685,0.0339,-0.0648,-0.0096,-0.0769,0.0048,-0.0677,-0.0164,-0.0081,-0.0968,-0.0812,-0.0012,0.0744,0.1228,0.1193,-0.0111,-0.0782,0.0456,0.0979,-0.0428,-0.0693,-0.0226,-0.0129,0.0764,0.0044,-0.0292,0.0419,0.0278,-0.0272,0.0308,-0.0120,-0.0392,-0.0792],[0.0457,-0.0067,-0.0475,-0.0370,0.0326,0.0464,0.0099,-0.0855,-0.0890,0.0576,-0.0745,-0.0325,0.0996,0.0154,0.1044,-0.1089,0.0584,-0.0847,0.0626,-0.0670,0.0901,-0.1037,-0.0626,-0.0199,0.1351,-0.1017,-0.0337,-0.0514,-0.0005,0.0290,-0.0253,-0.1015,-0.0255,-0.0755,-0.0050,-0.0558,-0.1055,0.0306,0.0936,-0.1004,0.0689,-0.0575,-0.1172,-0.0025,-0.0911,-0.0465,-0.0939,-0.0344,0.0488,0.0371,-0.0425,-0.0646,-0.0163,0.0294,-0.0508,0.0237],[0.0837,0.0532,0.1076,-0.0119,-0.0247,-0.1402,0.0268,0.0625,0.0153,-0.0511,-0.0359,-0.0516,0.0878,-0.0070,-0.1235,0.0755,-0.0328,-0.0482,0.0459,0.0419,0.0152,0.0914,0.0409,0.0479,-0.0048,0.0187,-0.0530,-0.0950,-0.0023,0.0327,-0.0080,0.0935,-0.0909,-0.1121,0.0896,0.1092,0.0717,0.0855,-0.0678,0.0748,0.1068,-0.0513,-0.0029,-0.0458,-0.0897,-0.0732,-0.0544,0.0712,0.0539,0.0277,-0.0549,-0.0162,-0.1419,0.0996,-0.0194,-0.0674],[0.0006,0.1619,0.1008,0.0498,-0.1048,-0.0004,0.0697,-0.0244,0.0735,-0.0359,-0.0578,0.0094,-0.0810,-0.1065,-0.0566,0.0426,0.1100,-0.0732,-0.0588,-0.0930,-0.0884,-0.0157,0.0251,0.0694,0.1149,-0.0573,0.0775,-0.1319,0.0020,-0.0619,0.0043,-0.0833,-0.0627,-0.0147,0.1197,0.0089,-0.0548,-0.0879,0.0872,0.0915,0.0913,0.0227,0.0556,0.0516,-0.0192,0.1027,0.0762,-0.0449,-0.0175,-0.0280,-0.0696,-0.0148,0.1513,0.0773,0.0657,-0.0135],[0.0636,-0.0017,-0.1059,0.0305,-0.1068,0.0401,0.0251,0.0896,-0.0449,-0.0084,-0.1710,0.0258,0.0163,-0.0027,-0.0991,-0.0404,0.0188,-0.1171,-0.1054,0.0886,-0.0071,-0.0751,-0.0953,0.0177,-0.3023,-0.0835,-0.0857,0.0045,-0.0690,-0.0194,-0.0553,-0.0866,0.0011,0.0553,-0.0425,-0.1004,-0.0624,-0.2033,0.0972,0.0075,-0.0306,-0.0573,0.0155,-0.0266,-0.0116,0.0970,0.0965,0.0230,0.0202,0.1177,-0.1159,0.0588,-0.0596,0.0261,0.0175,0.1332],[-0.1521,-0.0113,-0.0977,0.0060,-0.0310,-0.1104,-0.0153,0.0456,0.0679,-0.0927,0.1903,0.0335,-0.0501,0.0789,-0.0225,0.0361,0.0967,-0.1201,-0.0230,-0.0550,-0.0099,-0.0194,0.1629,0.1012,-0.2151,0.0030,-0.0703,0.0400,0.1238,-0.0601,-0.0729,0.0642,0.0969,0.1204,0.0981,-0.0287,0.0042,-0.0288,0.0487,0.0577,0.1505,0.0557,-0.0183,-0.0131,-0.1224,-0.0294,-0.0889,-0.0554,-0.0277,0.0336,0.0806,0.0377,-0.1044,-0.0411,0.0297,-0.0060],[-0.0945,0.1644,0.0315,-0.0355,0.0990,0.0601,-0.0597,-0.0231,-0.0504,-0.0118,0.0822,0.0903,0.0771,-0.0112,-0.0158,-0.0812,-0.0761,0.0821,0.0563,-0.0876,0.0428,0.0719,0.0941,0.1193,-0.0874,-0.0940,-0.0359,0.0004,0.0467,0.0210,0.0297,0.0081,0.0084,-0.0045,0.1379,0.0482,-0.0056,-0.0247,-0.0570,-0.0354,0.1116,0.1120,-0.0761,-0.0422,-0.0677,0.0413,0.0686,0.0408,0.0695,0.0026,-0.1095,0.1295,0.0473,0.0596,-0.0503,-0.1137],[-0.1713,0.0805,0.1310,-0.0092,-0.0505,0.0310,0.0949,0.0923,0.0101,-0.0721,-0.0382,0.0580,-0.0089,0.0613,-0.0155,-0.0372,0.0239,-0.0170,0.0399,0.0625,0.1267,-0.0384,0.0661,-0.0836,-0.1031,0.0308,0.0219,-0.0652,-0.0970,0.0170,0.0348,-0.0347,-0.0225,0.0555,-0.0647,0.0326,-0.0153,-0.0484,0.0584,-0.0947,0.0215,0.0112,-0.1031,-0.0577,-0.0480,0.0879,0.0240,0.1042,-0.0112,0.1036,0.0700,0.1059,0.0493,0.1396,-0.0339,-0.0453],[0.1039,0.0024,-0.0819,0.0021,0.0710,-0.0620,-0.0455,-0.1000,0.0627,0.0084,-0.0634,0.0102,-0.0701,0.1307,-0.0011,0.0341,-0.0468,0.0900,-0.0663,-0.0406,0.0295,0.0002,-0.0608,-0.0460,0.1001,-0.0622,-0.0667,-0.0331,-0.0456,-0.0879,-0.0140,-0.0095,-0.0400,0.0121,-0.1093,-0.0447,-0.0995,0.0335,0.0328,-0.0950,-0.0957,0.1131,-0.0561,0.0132,0.0851,-0.0889,0.0404,0.0334,0.0683,0.0855,-0.0612,0.0859,0.1143,-0.0151,-0.0705,-0.0585],[0.0004,0.0317,0.0987,-0.1206,0.0604,0.1597,-0.1080,0.0403,-0.1059,-0.0774,0.1341,-0.0493,0.0590,-0.0306,-0.0415,-0.0480,-0.0576,0.0933,0.0936,0.0909,0.0619,0.0979,0.0128,-0.0355,0.1287,-0.0785,0.0658,-0.0793,-0.0415,0.0567,0.1023,0.0344,0.0442,-0.0605,0.0441,-0.0861,0.0985,0.1549,0.0142,-0.0114,0.0088,-0.0486,-0.0152,0.0335,0.0186,0.0088,0.0494,-0.0275,-0.0004,-0.0574,0.0056,-0.0744,0.0582,-0.0416,0.0555,-0.0699],[-0.0155,-0.0905,-0.0383,0.0004,0.0227,0.0839,0.0660,-0.0625,-0.0512,0.0587,0.1034,0.0157,-0.0608,0.0384,-0.0202,0.0186,0.0665,-0.1261,-0.0152,0.0231,-0.0510,-0.0957,-0.0522,-0.1151,0.0048,-0.0640,0.0357,-0.0258,0.0980,-0.0681,0.0522,-0.0924,-0.1110,0.1430,0.0026,0.0603,0.0415,-0.0307,0.0001,-0.0142,0.1038,-0.0508,-0.1262,0.0416,-0.0797,0.0585,0.0531,0.0036,-0.0487,0.0583,-0.0453,0.0306,-0.0506,0.0371,-0.0492,-0.1164],[-0.0038,-0.1605,0.1315,-0.0292,-0.0469,-0.1116,-0.0185,0.0684,-0.0012,0.1111,-0.0414,-0.0289,-0.1079,0.0688,0.0096,-0.0439,-0.0795,-0.0032,0.0779,0.0885,0.0131,-0.0008,-0.0783,0.1152,-0.0673,0.0031,-0.0312,0.0251,0.0369,-0.0300,0.0490,0.0375,-0.0144,0.1025,-0.1102,-0.1052,-0.0047,0.0415,-0.0362,-0.0436,0.0711,0.0496,0.1147,0.1113,0.0796,0.1102,0.1030,-0.0242,0.1236,0.1321,-0.0011,0.0449,-0.1135,0.0559,-0.0373,0.0603],[-0.0437,0.0855,0.1060,-0.0034,0.1731,-0.0172,-0.0752,-0.0478,-0.0339,-0.0072,-0.0334,0.0231,0.1258,0.0541,0.0509,0.0499,0.1053,0.0511,0.0396,0.0189,-0.0521,0.0296,-0.0964,-0.1186,-0.0984,-0.1001,-0.0765,0.0804,-0.0874,-0.0592,0.0770,0.0304,-0.0002,-0.1052,0.0150,-0.0554,-0.0673,0.0641,-0.0628,-0.0663,-0.1270,-0.0342,-0.0508,0.0318,-0.0201,-0.0116,-0.0591,0.0351,-0.0108,-0.0032,0.0314,-0.1295,-0.0426,-0.0492,-0.0837,-0.0542],[-0.0432,-0.0864,-0.0565,0.0510,0.0026,0.0495,0.0264,0.0786,0.0270,0.0883,-0.1035,-0.1073,0.0126,0.0086,-0.0902,-0.0214,-0.0790,-0.1172,-0.0298,-0.0006,0.0943,0.0211,0.0068,-0.0235,-0.2134,0.0998,0.0284,0.0602,-0.0413,-0.0199,0.0006,0.1056,0.0221,-0.0409,-0.0772,0.0481,-0.1666,0.1277,0.0050,0.0118,0.0487,-0.0433,0.0843,0.0150,0.0389,-0.1015,0.0558,-0.0405,-0.0166,-0.0308,0.0425,-0.1261,-0.0024,0.0047,-0.1225,0.0007],[-0.0393,-0.0382,0.0574,-0.0706,-0.1016,0.0797,0.0189,0.0703,0.0300,-0.0206,0.0501,-0.1219,-0.0107,0.0511,0.0960,0.0757,-0.0584,0.0270,0.0335,0.0777,0.0857,-0.0743,0.1413,-0.1180,0.0202,-0.0288,-0.0616,-0.1190,0.0277,-0.1282,-0.0209,0.0888,0.0623,0.0761,-0.1035,-0.0572,0.0396,0.0204,0.0238,-0.0394,0.0579,0.0110,-0.0827,-0.0491,-0.0290,-0.0514,0.1591,0.0256,0.1472,0.0418,-0.0436,-0.0093,-0.0111,-0.0095,0.0078,-0.1600],[-0.0626,0.1615,0.0602,0.0830,0.0653,-0.0135,-0.0200,0.0314,-0.1285,0.0282,-0.0245,-0.0760,0.1068,-0.1173,-0.0444,-0.1227,0.0510,-0.1043,0.0347,-0.0185,0.0003,-0.0211,-0.0397,0.1278,-0.0452,0.0435,0.0122,0.0013,0.0471,-0.0430,-0.0278,-0.0200,-0.0826,0.1084,0.0095,-0.0084,0.0263,0.0450,0.0039,0.0238,-0.0243,-0.0245,-0.0405,-0.1210,0.0848,0.0127,-0.0213,-0.0887,0.0890,0.0645,-0.0324,-0.0438,-0.0219,0.0012,0.0377,-0.0102],[-0.1653,-0.2512,-0.0732,-0.0085,-0.0550,-0.0735,-0.0443,0.0446,0.0153,-0.0190,-0.1334,-0.0088,-0.0444,-0.0307,-0.1674,-0.0561,0.0704,0.0031,-0.0237,-0.0026,0.0047,0.0732,-0.0411,0.1595,-0.0881,0.0218,0.0216,-0.0402,0.0197,0.0381,0.0453,-0.0216,0.0056,0.0819,-0.0177,0.1172,-0.2575,-0.1066,0.0622,-0.0295,-0.0314,0.0431,-0.0530,-0.0663,-0.0170,0.1015,0.0265,-0.0003,0.0101,-0.0167,0.0829,0.0338,0.1079,-0.0220,0.0454,-0.1571],[0.0469,0.1057,0.0927,0.0674,0.0720,-0.0062,-0.0000,0.0115,-0.0532,-0.0407,-0.0205,-0.0510,0.0397,-0.0732,0.0078,0.0536,0.0098,0.0768,-0.0332,-0.0046,0.0502,0.0190,-0.1533,0.0773,0.1208,-0.1576,-0.1202,-0.0616,0.0098,-0.0147,0.1118,-0.0708,-0.1068,0.0718,0.0673,-0.0269,-0.0305,-0.0993,-0.0060,-0.1008,0.0117,-0.0186,0.0227,0.0227,-0.0146,0.0315,0.0435,-0.0429,-0.1064,-0.0561,-0.0667,-0.1131,-0.1092,0.0799,0.0749,0.0290],[-0.0145,-0.1214,0.0204,0.0502,-0.1006,-0.0067,-0.0187,-0.0486,0.0525,-0.0154,0.0092,-0.1117,-0.0044,-0.0928,0.0446,0.0718,0.0677,-0.0129,0.0364,0.0633,-0.0533,-0.0581,0.0468,0.0426,0.0691,-0.0217,-0.0144,0.1395,0.0073,-0.0272,-0.1147,0.0494,-0.1085,0.1105,0.1048,0.0874,0.0637,0.1211,0.0769,-0.0255,0.0364,-0.0918,-0.0932,0.0440,0.0253,0.0645,-0.0499,0.1183,-0.0020,-0.0759,-0.0308,-0.0211,0.0516,-0.0040,-0.0398,-0.1194],[0.0565,-0.1478,-0.0171,-0.0176,-0.1143,0.1191,-0.0594,0.0047,0.1110,0.0607,0.1106,-0.0234,-0.0671,0.0602,-0.0246,0.0943,0.0688,0.0724,0.0874,0.0344,-0.0909,0.0458,0.0453,-0.1313,0.1211,0.1014,-0.0814,0.0316,0.0310,-0.0549,0.0136,-0.1072,-0.0399,-0.0795,-0.0179,-0.0843,0.0258,-0.1145,0.0847,-0.0198,0.0378,-0.0788,-0.0441,0.0945,-0.0629,-0.0881,-0.0111,0.0601,-0.0197,-0.0352,0.0210,0.0402,0.0466,-0.0970,-0.0175,0.0388],[-0.0926,-0.0716,-0.0248,0.0059,-0.0582,0.0554,-0.0516,-0.0889,-0.0247,0.0283,0.0567,0.0306,0.1099,0.1121,0.0754,0.0632,0.0669,0.1859,0.0394,0.1078,-0.0132,0.0899,0.0462,-0.0191,0.1107,-0.0144,0.0270,-0.0401,-0.0225,-0.0796,0.0049,-0.0601,0.1504,0.0573,0.1049,-0.0807,0.1037,0.0311,0.0289,0.0901,0.0232,0.0776,0.0051,-0.0025,0.1282,0.1451,-0.0479,0.0615,0.1088,0.0758,0.0991,0.1384,0.0089,0.0076,0.0964,-0.0217],[0.0971,0.1376,0.0373,0.0713,-0.0217,0.0394,0.0210,0.0440,0.0141,-0.0392,-0.0463,0.1566,-0.0638,-0.1021,0.0701,0.1111,0.0831,-0.0213,0.0459,0.0711,0.0379,0.0653,-0.0897,0.1148,-0.0193,0.1219,-0.0831,-0.0094,0.0508,-0.0695,0.0233,0.0158,-0.0300,0.0632,-0.0705,-0.0309,0.0053,0.0460,0.1009,0.0358,0.1246,-0.0917,0.0568,-0.0610,0.0478,0.1112,0.0243,-0.0181,0.1398,-0.0903,0.0145,0.0598,0.0099,-0.0274,0.1031,-0.1208],[0.0333,-0.0428,-0.0173,-0.0831,0.1003,-0.1332,0.0673,-0.1034,-0.1045,0.0315,0.0420,-0.0804,-0.0247,-0.0105,-0.0152,-0.0138,-0.0048,0.0811,-0.1266,-0.0483,0.0639,-0.0740,-0.0570,-0.1277,-0.0944,-0.1110,0.0110,0.0078,0.0714,0.1140,-0.0242,-0.0847,-0.0356,0.0640,-0.0438,-0.0906,0.0048,0.1022,0.0520,-0.0554,0.0815,0.0668,-0.0797,0.0611,-0.0100,-0.0482,0.1095,-0.0321,0.0379,0.0067,-0.0228,-0.0760,-0.0971,-0.0505,0.0697,0.0288],[0.0158,0.1553,-0.0893,0.0383,-0.1035,0.0689,-0.0928,-0.1004,-0.0732,-0.0145,0.0353,0.0062,-0.0684,-0.0855,0.0191,0.1177,-0.1279,-0.1262,0.0060,0.1038,0.0291,0.0448,0.0719,-0.0204,-0.1853,-0.0718,-0.0073,-0.0248,0.0263,-0.0699,0.0497,-0.0571,0.0912,0.0499,-0.1192,-0.0371,0.0509,-0.0501,-0.0890,0.0853,-0.0005,-0.1074,0.0123,-0.1046,-0.0809,0.0370,0.0545,-0.0163,0.0877,-0.0344,-0.1104,-0.0289,-0.0214,0.0310,0.0431,-0.0569],[0.0231,-0.1218,0.1227,-0.2165,-0.0863,-0.0763,-0.0453,-0.0009,-0.0455,-0.0285,-0.1004,0.0282,-0.0683,-0.0851,0.0680,0.0160,-0.1059,-0.0204,-0.0405,0.1188,-0.0861,0.0686,-0.0995,0.0119,-0.1316,-0.0330,-0.0347,-0.0753,-0.0556,-0.0379,0.0171,0.0500,0.1286,-0.0986,0.0764,-0.1013,-0.0808,0.0570,0.0468,-0.0051,0.0299,-0.0079,-0.0274,0.0907,0.0197,-0.0042,-0.0319,0.0211,-0.1018,-0.0488,0.0938,-0.1507,-0.0328,-0.0552,-0.1105,-0.0249],[-0.1090,0.0608,0.1105,-0.0353,-0.0194,-0.0642,-0.0708,-0.0743,-0.1027,0.0232,0.0462,-0.0468,0.0371,0.1347,0.0921,0.0793,-0.0558,0.0356,0.0126,-0.0177,0.0105,0.0539,0.0668,0.0745,0.0054,0.1010,-0.0407,-0.0264,0.0794,0.0354,-0.0869,0.0213,-0.0723,0.0768,-0.1125,-0.0926,0.0811,-0.0872,0.0063,0.0834,0.0381,-0.0423,-0.0523,0.0400,0.1096,0.0276,0.0226,-0.0402,0.0290,0.1291,0.0015,0.0310,-0.1093,-0.0773,-0.0387,-0.0887],[0.0193,-0.0247,0.0617,0.0208,0.0424,-0.0691,-0.0789,0.0245,0.0065,-0.0756,-0.0856,0.0302,-0.1003,0.1150,-0.0311,0.0301,0.0983,-0.0206,0.0950,-0.0075,0.0107,-0.0975,-0.1729,-0.0856,0.0673,-0.0627,-0.0048,0.0045,-0.0051,0.0049,-0.0292,0.0141,0.0457,-0.0175,-0.0595,0.0441,-0.1833,0.0559,0.0709,-0.0397,-0.0268,-0.0477,0.1568,-0.0467,-0.0977,0.0827,-0.0270,0.0287,-0.0521,0.1032,-0.0625,-0.0479,0.0504,-0.0743,0.0602,0.0170],[-0.0463,-0.0647,0.0449,0.0231,0.0199,-0.0903,-0.0778,0.0730,-0.0390,-0.1094,0.0626,-0.0216,-0.0923,0.0828,-0.0275,0.0855,0.0013,0.0204,0.0841,0.0554,-0.0067,-0.0558,-0.0355,0.0272,0.0065,0.1343,0.0591,0.1511,-0.0657,0.0663,-0.1331,-0.0299,-0.0262,-0.0538,0.0351,-0.0068,-0.1572,-0.0320,-0.0479,-0.0549,-0.0873,0.0770,0.0803,0.0141,-0.0663,-0.0488,0.0459,-0.0228,0.0951,0.0178,0.0440,-0.0226,-0.0224,0.0347,-0.0687,0.0910],[-0.0512,0.1610,0.0161,-0.0481,0.0141,0.0990,0.0192,-0.0611,-0.0003,-0.0766,-0.0313,0.1414,-0.0787,0.0880,0.0502,0.0079,-0.1027,0.0020,-0.0275,0.0156,-0.0322,-0.1010,-0.0574,-0.0894,-0.1420,0.0200,-0.0293,-0.0163,0.0018,-0.0463,0.0232,0.0662,-0.0173,0.0033,0.0642,-0.1130,-0.0315,-0.1051,-0.0214,0.0968,0.0686,-0.1372,-0.0843,0.0502,-0.0896,0.1377,0.0260,0.0162,-0.0457,0.0844,0.1227,-0.1523,0.0247,-0.0055,0.0040,0.0822],[-0.0007,-0.0723,0.0463,0.1087,0.0323,-0.0582,0.1451,-0.0976,0.1105,-0.0648,-0.0704,-0.1136,0.0272,-0.0748,0.0310,0.0498,0.0487,0.0735,-0.0574,0.0180,0.0708,0.0734,-0.0623,-0.0038,-0.0673,0.0446,0.0636,-0.0196,-0.0700,0.0155,0.1130,-0.0068,0.0377,-0.0716,0.0172,0.0638,0.0342,0.1064,-0.0182,0.0677,0.1283,0.0942,0.0636,-0.0390,0.0655,-0.0924,-0.0688,-0.0883,-0.0557,-0.0519,-0.0882,-0.1196,0.0563,0.0890,-0.0437,0.0519],[0.0546,-0.0268,0.1386,0.0028,0.1436,0.0393,-0.0875,-0.0790,-0.0564,0.0328,-0.0868,-0.0525,-0.0296,0.0766,0.0089,0.0268,-0.0936,-0.1011,0.0514,0.0676,0.0043,-0.0373,-0.1348,0.0693,0.1153,0.0741,-0.0130,-0.0317,0.0544,0.0785,-0.0275,0.0576,-0.0381,0.0652,-0.0689,0.0480,0.0560,0.0042,-0.0009,-0.0996,-0.0398,-0.0036,0.0211,0.0993,-0.1021,0.0436,-0.0670,0.0379,0.0409,-0.0793,-0.0871,0.0973,0.0550,-0.0849,0.0155,-0.0622],[0.0756,0.0120,-0.0191,0.0094,0.0503,-0.1659,0.0749,-0.0404,0.1193,-0.0683,0.0597,-0.0039,-0.0466,-0.0470,0.0035,-0.1161,-0.0339,-0.0816,-0.0412,-0.0648,0.1597,0.0497,0.2178,0.0246,-0.0917,-0.0401,-0.0219,-0.0475,-0.0171,-0.0038,0.1325,0.0637,0.0007,0.0555,0.1003,-0.0232,0.1613,-0.1611,-0.0094,-0.0638,-0.0632,-0.0532,0.0115,-0.0927,0.0646,0.0103,-0.0766,0.0781,0.0075,-0.0394,0.1750,0.0888,0.0962,0.1796,0.0644,0.1476],[-0.0324,0.0099,0.0730,-0.0473,-0.1053,-0.0685,0.0566,-0.1031,0.0812,0.0591,-0.1404,0.0748,-0.0625,0.0037,-0.0470,-0.0422,-0.1070,-0.1056,0.0620,-0.0216,0.0677,0.0635,-0.1114,0.0753,0.0198,0.0386,0.1285,-0.1146,-0.0838,0.0565,-0.0416,0.0353,0.0159,-0.0813,0.0800,-0.0175,0.0359,-0.0275,0.0597,0.0280,-0.0513,-0.1179,0.0490,0.0833,0.0009,-0.0115,0.1161,0.0094,0.0591,0.0080,-0.0370,-0.0053,-0.0941,0.0158,-0.1081,-0.0932],[-0.0104,0.0323,-0.0701,-0.0701,0.0730,0.1488,0.1214,0.0039,0.0428,-0.0165,-0.1612,-0.0472,0.0143,0.0247,-0.0070,-0.0897,0.0087,0.0539,0.0269,-0.1419,-0.0344,0.0650,-0.0606,-0.0293,0.1090,-0.0647,0.0476,-0.0786,-0.0419,-0.0504,-0.0759,-0.0779,0.0915,0.0456,-0.0337,0.1022,-0.0337,0.0102,-0.1117,-0.0782,0.1243,-0.0130,0.0030,-0.0970,0.0010,-0.0003,0.0684,0.0095,-0.0213,0.0353,0.0635,-0.0097,0.1031,0.0202,0.0166,-0.0458],[0.0887,0.0683,-0.0725,-0.0461,-0.0891,0.2246,-0.0401,-0.0241,-0.1056,-0.1243,-0.0943,0.0893,-0.0264,-0.0188,0.0631,0.0069,0.0150,-0.0325,0.0141,-0.1445,-0.0810,-0.0419,-0.1453,0.0598,0.2461,0.0239,-0.0639,0.0667,0.0040,0.0149,0.0185,0.0757,-0.0677,0.0232,-0.0925,0.1243,0.0238,-0.0267,-0.0633,-0.0301,-0.0115,0.0056,-0.0557,-0.0532,-0.0130,-0.0914,-0.0695,-0.0355,-0.0304,-0.0188,-0.0584,0.0517,-0.0665,0.0508,0.0430,0.1375],[0.0221,0.0355,0.0301,0.0867,-0.0397,-0.0354,0.0249,-0.0544,-0.0205,0.0168,0.0169,0.0056,0.0704,-0.0637,-0.0809,0.0754,0.0420,-0.0558,-0.0426,0.0981,0.0404,0.0123,0.0904,0.0565,0.1751,-0.0609,0.0363,-0.0958,0.0029,-0.0184,0.0418,-0.0848,-0.0873,-0.0827,0.1005,-0.0010,-0.0573,-0.0182,0.0022,0.0409,-0.0347,0.0237,0.0473,-0.0763,0.0743,-0.0530,-0.0385,-0.0612,-0.1034,0.1623,0.0209,-0.1107,-0.0807,0.0260,-0.0445,0.1351],[-0.0158,0.0774,-0.0229,0.0604,-0.0979,0.1192,0.1242,0.0312,0.0010,0.0658,-0.0007,-0.0008,0.0756,0.0270,0.0278,0.0547,-0.0470,0.0397,-0.0501,-0.0562,-0.0906,0.0504,0.0458,0.0285,0.1365,0.1031,-0.0276,0.0097,-0.0517,0.0016,-0.0248,-0.0655,-0.0393,-0.1194,0.0018,0.1313,-0.0454,-0.0530,0.1086,-0.0032,-0.0231,0.0692,0.0438,-0.0716,0.1358,-0.0872,-0.1176,0.0140,-0.0034,-0.0617,-0.0807,-0.0890,-0.0110,0.1400,-0.0774,-0.0509],[-0.0930,-0.1232,0.0592,-0.1512,0.0347,0.1017,0.0380,-0.0447,-0.0231,-0.0851,0.0639,0.0687,0.0556,0.1578,-0.0035,0.1162,-0.0390,0.0349,-0.0239,-0.0673,-0.1105,-0.0075,0.1229,-0.0663,-0.0689,0.0180,0.0600,-0.0124,0.0583,-0.0092,-0.0991,0.0365,0.0323,-0.0895,-0.0616,0.0595,0.0498,-0.1317,0.0131,0.0172,-0.0141,0.0151,-0.0512,0.1563,0.0544,-0.0538,-0.0127,0.0387,0.0416,0.0162,-0.0659,-0.0281,-0.0456,0.0149,-0.0520,-0.0678],[-0.0989,0.0171,-0.0303,0.1233,0.0208,0.0032,0.0119,0.0076,0.0527,0.0122,0.0861,0.0544,-0.0434,-0.0950,0.0376,0.0101,0.0001,-0.1113,-0.0424,0.0269,0.0035,-0.0975,0.0787,-0.0456,-0.1680,0.0849,-0.0824,0.0117,0.0097,0.0413,-0.0156,-0.0495,0.0041,-0.0525,0.0782,0.0708,-0.0650,-0.0568,0.0130,-0.0515,-0.0864,0.0715,-0.0269,0.1453,0.1568,0.1104,0.0635,-0.0743,-0.0026,0.1341,0.1106,-0.0948,-0.0178,0.1156,-0.0856,0.1449],[-0.0468,0.0470,-0.0480,-0.0376,0.0376,-0.0181,0.0393,-0.0975,0.0605,-0.0357,-0.0486,0.0986,0.0457,-0.0002,-0.0759,0.0498,0.0089,0.0441,-0.0069,-0.0859,-0.0981,0.0455,-0.0741,0.1162,-0.0725,0.1172,-0.0136,0.1028,-0.0646,-0.0702,-0.0285,0.0372,-0.0034,-0.0859,-0.0822,0.0793,-0.0218,0.1253,-0.0438,0.0181,-0.0224,0.0566,0.1578,0.0735,-0.0046,-0.0383,0.0646,0.1072,-0.0267,-0.0705,-0.0085,-0.0895,-0.0959,-0.0407,-0.0891,-0.0926],[0.0089,0.0667,0.0427,-0.0107,-0.0249,-0.0465,-0.0459,-0.0007,-0.0063,0.0492,-0.0218,-0.1085,0.0127,0.1013,-0.1417,-0.0737,-0.0112,-0.0275,0.0616,0.0256,-0.0485,-0.0988,-0.0593,-0.0451,0.0742,-0.1020,-0.0991,0.0099,-0.0584,0.0639,-0.1208,0.0576,0.0269,0.0530,-0.0857,0.0648,0.0292,-0.0846,-0.0912,-0.0262,-0.0465,0.0807,0.0052,-0.0578,0.0603,0.0225,0.0521,-0.0620,0.0740,-0.0121,0.0587,-0.0224,-0.0470,0.0979,-0.0902,0.1590],[0.0104,0.1400,-0.0430,-0.0496,-0.1030,0.1276,0.0477,0.0766,0.0631,0.0737,-0.1338,0.1168,-0.0743,-0.0794,-0.1103,-0.0701,-0.0416,0.0810,-0.0384,0.0514,-0.0059,0.0892,-0.0928,0.1506,0.1284,0.0865,0.0458,-0.0480,0.0285,0.0063,0.0979,0.1155,0.0535,0.0207,0.1341,-0.1395,-0.0522,-0.0657,0.0329,-0.0474,-0.0748,0.0351,0.1297,0.0276,0.1482,-0.0326,-0.0776,-0.0296,-0.0287,0.0491,0.0147,0.0372,-0.1189,-0.0395,0.1028,-0.0199],[-0.0144,0.0861,-0.0188,-0.1104,-0.1186,-0.0629,-0.0229,0.0068,0.0207,-0.0691,0.1035,-0.0805,-0.0416,0.0085,-0.0374,0.0288,-0.0589,0.0258,0.0719,-0.0843,0.0195,-0.0419,0.0366,-0.0542,-0.1346,-0.0768,0.0201,0.0314,-0.0119,-0.0614,0.0811,0.1188,0.0340,-0.0986,0.0288,0.0570,-0.0823,0.0192,0.0231,0.1324,-0.0437,0.0192,-0.0944,0.0270,-0.0338,0.0253,-0.1295,-0.1438,-0.0528,-0.0468,-0.0221,-0.0258,0.0379,0.0024,0.0310,0.0063],[0.0111,0.0113,-0.0255,0.1142,-0.0395,0.0886,-0.0982,0.0734,-0.0692,-0.0797,-0.0024,0.0341,-0.0828,-0.0154,0.1051,0.0612,-0.0120,0.0726,-0.0591,0.0317,-0.0148,0.0197,-0.0660,-0.0822,-0.1467,-0.0604,0.0428,-0.0686,0.0887,-0.0414,0.0248,0.0082,0.0447,-0.0596,-0.0207,0.0376,-0.0739,0.1512,-0.1454,-0.0460,-0.0180,-0.0550,0.0992,-0.0470,-0.0512,0.0429,0.0710,-0.0323,0.0009,-0.0036,0.0851,0.1376,0.1338,-0.0881,0.0499,0.0938],[-0.0978,-0.0047,0.0852,0.0440,-0.0468,0.1013,-0.0987,-0.0554,0.0119,0.0497,0.0968,0.1686,0.1004,0.0834,-0.0235,-0.0265,-0.0032,-0.0087,0.0750,-0.1511,-0.0256,0.0871,0.2962,0.2496,-0.0937,-0.0129,-0.0166,0.0132,-0.0830,-0.0223,0.0321,0.0738,0.0456,0.0023,0.1385,-0.0415,-0.0678,-0.0427,-0.0046,0.0156,-0.0244,-0.0066,-0.0910,-0.0348,-0.1029,-0.1535,-0.0246,0.0154,-0.0362,-0.0387,0.0201,0.1627,-0.1375,-0.0467,-0.0360,0.0772],[0.0992,-0.0789,0.0976,-0.0475,0.0622,0.0134,0.0996,-0.0385,-0.0868,-0.0755,-0.0658,-0.0884,0.0291,0.0362,-0.0359,-0.1819,0.1235,-0.0052,-0.0476,-0.0747,-0.0724,0.0414,-0.0840,-0.1931,0.0421,-0.0895,0.0306,-0.0675,0.0563,-0.0095,0.0705,0.0313,0.0451,0.0661,0.0231,-0.0569,0.0835,-0.1203,-0.0201,-0.0161,0.0747,0.0541,-0.0225,0.1125,0.0791,0.0626,0.0883,-0.1350,-0.0329,-0.0070,-0.0645,-0.0559,0.1448,0.0021,-0.0522,0.0246],[-0.1227,0.0344,-0.1332,-0.0094,0.0747,0.0749,0.0526,0.1161,-0.0078,0.0809,-0.0836,-0.0023,-0.0123,-0.1326,0.1219,-0.0881,-0.0969,-0.1205,-0.0525,-0.1669,-0.0149,-0.0392,-0.1214,0.0361,-0.0651,0.1038,0.0696,0.0395,-0.0831,-0.0319,0.0139,-0.0121,0.0180,-0.0123,0.1095,-0.0319,-0.0755,-0.0520,-0.0349,0.0502,-0.0090,0.0060,0.1533,0.0505,0.1985,0.0723,-0.0003,-0.0084,0.0007,-0.0747,0.1471,-0.0965,-0.0777,-0.0623,-0.0910,0.0487],[0.0974,0.1119,-0.0244,0.0220,-0.0212,-0.0814,0.0184,0.0016,-0.0467,0.0124,0.0966,0.0661,-0.0373,-0.0373,0.0471,-0.0478,-0.0258,0.0511,-0.0400,-0.0498,0.0177,-0.0676,0.0867,-0.1101,-0.2315,-0.0220,-0.1953,-0.0695,-0.0164,-0.0597,-0.0802,-0.0141,-0.0583,0.0306,-0.0515,0.0001,0.1128,-0.1436,-0.0855,0.0073,-0.0017,0.0154,0.0389,0.0853,0.0599,0.0566,0.0731,-0.0033,-0.0512,0.0375,0.0202,-0.0302,-0.0249,-0.1100,-0.0923,-0.0901],[-0.3149,0.0523,0.1279,-0.0062,-0.0013,-0.1315,-0.0283,0.0304,-0.0143,-0.0327,-0.2128,0.0283,0.0643,0.0486,0.0398,-0.0660,-0.0070,-0.0071,-0.0560,-0.1101,-0.0173,0.0449,-0.2319,0.0413,0.0673,0.0045,0.0973,0.0638,-0.0315,0.0214,0.0004,-0.0260,0.0025,0.0394,0.0295,0.0660,0.0634,0.0624,-0.0038,0.1043,-0.0143,-0.0589,-0.0788,-0.0255,0.0358,0.0834,0.0956,-0.0460,0.0529,-0.1259,-0.0098,-0.2828,-0.1152,-0.1317,-0.1525,0.0665],[-0.0930,0.1418,0.1162,-0.0009,-0.0143,0.0802,-0.1083,-0.0317,0.0925,-0.0428,-0.0960,-0.1046,-0.0024,0.0835,0.1421,0.1265,-0.0619,-0.1921,-0.1615,-0.1217,0.0577,0.0889,0.0685,0.1085,-0.0650,-0.0189,-0.0375,-0.0132,-0.0550,-0.0621,0.0625,0.0235,-0.0557,0.0207,-0.1328,-0.0111,-0.1297,0.0925,0.0948,0.1248,0.0045,-0.0146,-0.0771,0.0386,-0.0321,0.1038,0.0943,0.0489,-0.0472,0.0276,-0.0021,-0.0896,-0.0126,-0.0906,-0.0777,0.0015],[-0.0451,0.0519,0.1652,-0.0450,0.0083,-0.0221,0.0724,-0.0543,0.0263,0.0412,-0.0933,0.0265,0.0224,-0.0530,-0.0332,-0.1166,0.0628,-0.0438,-0.0226,-0.0507,-0.0392,-0.0263,-0.0507,-0.0604,0.0371,-0.0327,0.0094,-0.0148,-0.0238,0.0378,0.0853,-0.0174,0.0044,0.0821,0.0862,0.0697,-0.0047,0.1317,-0.0181,-0.0684,-0.0254,-0.0035,-0.0764,-0.0185,-0.0459,0.0367,0.0487,0.0254,0.0340,-0.0701,0.2486,0.1522,0.0112,-0.0608,0.0003,0.1167],[0.0084,0.1074,-0.0395,0.0680,-0.1238,0.0055,-0.1104,-0.1024,0.0784,-0.0187,0.0841,0.1161,-0.0791,0.0770,0.0876,0.0352,-0.0594,-0.0511,0.0018,0.0828,-0.0313,0.0934,-0.0199,0.0906,-0.0579,0.1052,0.0368,-0.0583,-0.0489,0.0385,0.0698,0.0486,0.0369,-0.0703,-0.0595,0.1011,0.1086,-0.1037,0.1107,-0.0286,0.0619,0.0142,0.0774,0.0222,0.0775,-0.0135,0.0700,0.0308,-0.0023,0.0378,-0.0896,-0.0149,-0.1182,0.0398,-0.0227,-0.0414],[0.1003,-0.0765,-0.0132,0.0417,-0.0881,0.2101,-0.0341,-0.0796,-0.0108,-0.0598,-0.1247,-0.0551,-0.0159,0.0930,-0.0242,-0.0191,0.0678,0.0968,0.0834,-0.0881,-0.0896,0.0741,-0.1202,-0.0329,0.0975,0.0689,0.0859,0.0426,-0.0350,-0.0463,-0.1010,-0.0740,-0.0558,-0.0862,0.0770,0.1046,-0.0104,-0.0298,-0.0643,0.0077,-0.1059,0.0281,-0.0405,0.0317,-0.0427,-0.0303,0.0432,0.0523,-0.0130,0.0375,0.0326,0.0370,-0.2097,-0.0392,-0.0044,0.0111],[0.0399,0.0732,-0.0376,-0.1179,-0.0440,0.0277,-0.0350,0.0740,0.0107,-0.0367,0.0260,0.0131,0.0274,-0.1316,-0.0492,-0.0721,-0.0154,0.0079,0.0294,0.0822,0.0331,-0.0239,0.0461,-0.0299,-0.0702,0.0035,0.0900,0.0412,-0.1049,0.0067,-0.0690,-0.0350,0.0213,-0.0464,-0.0153,0.0373,-0.1130,0.0262,-0.0897,-0.0927,0.0727,0.0471,0.0155,-0.0438,-0.0587,-0.0356,0.0917,0.0213,-0.0085,-0.0825,0.0020,-0.0603,0.0559,-0.0193,0.0304,0.0147],[-0.0092,-0.2219,-0.0516,0.0291,-0.0422,0.0788,0.0878,0.0511,0.0772,0.0341,0.0230,-0.3179,-0.0426,-0.0857,-0.1104,0.0411,0.0295,0.0540,0.0603,0.1950,-0.0184,0.0060,0.0164,0.0060,0.1317,0.0293,-0.0354,-0.0119,-0.0332,-0.0455,-0.0247,-0.0303,-0.0594,-0.0024,-0.0541,-0.0802,-0.0595,0.2323,-0.0340,0.0896,0.0086,0.0153,0.0372,-0.1054,0.0238,0.2347,-0.0858,-0.0166,0.0305,-0.1117,-0.2722,-0.1122,0.0034,-0.0257,-0.0133,-0.3024],[0.0078,0.0858,0.1229,-0.0533,-0.0312,0.0783,-0.0811,0.0721,-0.0862,0.0872,-0.0283,-0.0122,0.0602,0.0422,-0.0450,0.0445,0.0037,0.0670,0.0608,0.1335,-0.0532,0.0006,-0.2269,-0.0728,-0.4173,0.0424,-0.0570,0.0375,0.0243,-0.0223,0.0558,0.0244,-0.0442,0.0421,-0.0655,-0.0955,0.2815,0.0325,0.0708,-0.0159,-0.0151,0.0195,0.0722,0.0620,-0.0229,0.0218,0.0570,0.0176,0.0310,0.0219,-0.0133,-0.0172,-0.0475,-0.0545,-0.0092,0.0365],[-0.0528,-0.0733,-0.0701,0.0506,-0.0944,0.1600,-0.0484,-0.0687,0.0651,0.0511,-0.1026,0.0789,0.0844,-0.0147,-0.0042,0.0877,-0.0216,-0.0578,-0.0293,-0.0545,-0.0131,0.0472,-0.1182,-0.0920,0.1621,-0.0919,-0.0716,0.0390,0.0074,-0.0854,-0.1121,-0.1093,-0.0796,0.0224,0.0823,0.1341,0.0455,0.0663,-0.0497,-0.0001,-0.1191,-0.0762,0.0517,-0.0100,0.0929,0.0482,-0.1414,-0.0544,-0.0704,-0.0495,-0.0064,-0.0185,-0.0436,-0.0021,0.0430,-0.0980],[0.0626,0.0123,-0.0319,-0.1280,0.0810,0.0477,0.0187,-0.1169,-0.0248,0.0560,-0.0364,-0.0393,-0.0459,0.0774,0.0112,-0.0025,0.0299,0.1087,-0.1177,0.0472,-0.0680,0.0160,-0.1318,0.0638,0.1061,0.0354,0.0897,-0.0097,-0.0398,0.0711,0.0271,0.0677,-0.0049,-0.1055,0.0631,-0.0905,0.0408,0.0039,0.1152,-0.0909,0.0362,0.0529,-0.0081,0.0344,-0.1343,0.0390,0.0268,-0.0322,-0.0439,0.1136,-0.0635,-0.0883,-0.0346,0.1456,-0.0317,-0.0447],[0.0235,-0.1092,0.0614,-0.0387,0.0063,-0.1049,0.0620,-0.0680,-0.0610,-0.0189,0.0008,-0.0088,0.0074,0.0351,0.0165,-0.0014,-0.0157,0.0558,-0.0832,-0.0254,0.0329,0.0045,-0.1452,0.0313,-0.0657,-0.0924,0.0626,-0.0109,0.1016,-0.0361,-0.0564,0.0372,0.0033,0.0955,-0.0154,0.0449,0.0383,0.1585,-0.0510,-0.1102,-0.0965,0.0547,-0.0678,0.0342,0.1039,-0.0051,-0.0082,0.0025,-0.0430,-0.1208,0.0054,0.0044,0.1000,0.1214,-0.0828,0.1287],[-0.0570,0.0140,0.1363,0.0112,0.0145,-0.0579,0.0745,-0.0144,-0.0262,-0.0361,0.1325,0.0776,-0.0584,-0.0073,-0.0261,-0.0548,-0.0182,0.0408,-0.0522,-0.0945,-0.0541,-0.0816,0.1398,0.0331,-0.0902,-0.0802,-0.0945,-0.0437,-0.0285,0.0093,-0.0299,0.0038,-0.0665,0.0274,0.0716,-0.0696,-0.0155,-0.0151,0.0393,0.0487,-0.1058,0.0515,0.1219,0.0407,0.0975,0.0313,0.1274,-0.0346,-0.0744,-0.0438,0.0866,0.1518,-0.0923,0.0106,-0.0618,-0.0675],[-0.0059,-0.0120,0.0948,-0.0428,-0.0697,-0.0082,-0.0571,0.0132,0.0390,0.1092,0.0817,-0.0466,-0.1253,0.0833,0.0731,-0.0788,0.0243,0.0618,-0.0336,0.0034,-0.0396,0.0578,0.0814,-0.0207,-0.0678,-0.0920,-0.1084,0.0417,0.0656,0.0409,-0.0356,0.0304,0.0750,0.1495,-0.0637,0.0249,0.0038,0.0587,0.0768,0.0505,0.1501,-0.0650,-0.1085,-0.0437,0.1355,-0.0177,0.0323,-0.0468,0.0998,-0.1105,-0.0889,0.0349,0.0052,-0.0280,0.0467,-0.0925],[0.0949,-0.0520,-0.1185,-0.0225,0.0699,-0.0472,0.0135,-0.0334,-0.0907,-0.0551,-0.0890,-0.1088,0.0010,-0.0309,0.0305,-0.0402,-0.0160,-0.0380,0.0343,-0.1056,0.0407,-0.0492,-0.0810,0.0712,-0.0948,-0.0506,-0.0469,-0.1030,0.0302,0.0945,0.1046,-0.0339,0.0415,0.0634,0.0172,-0.0390,-0.0920,0.0314,-0.0755,-0.0209,0.1103,0.0753,0.0402,0.1068,-0.0737,-0.0442,-0.0176,-0.0851,0.0616,0.0646,0.0575,0.0582,0.1117,0.1172,0.0368,-0.0035],[0.1318,0.1251,0.0356,0.0792,0.0125,0.0865,0.0300,0.0144,0.0901,0.0384,-0.0496,0.0685,-0.0244,0.0142,-0.0375,-0.0016,0.0224,0.0381,0.0446,-0.0626,-0.0330,-0.0852,-0.0791,0.0981,-0.0935,0.0496,-0.0117,-0.0327,0.0465,0.0983,0.0514,-0.0026,0.0852,-0.0256,0.1233,0.0566,-0.0125,0.0864,0.0083,-0.1246,0.0041,-0.0921,0.1078,0.0652,0.1289,-0.0320,-0.0152,-0.0321,0.0831,0.0646,-0.0131,0.0361,0.0100,-0.0667,-0.0697,-0.0390],[-0.0986,-0.0434,-0.1341,0.0297,0.0922,0.0102,0.0364,0.0207,0.0134,-0.0018,0.1396,0.0517,0.1059,0.0675,0.0472,0.1011,0.0274,0.0548,0.0435,0.0639,-0.1094,0.0052,0.1514,0.1270,0.0046,-0.0573,-0.0861,-0.0806,-0.0725,-0.0633,-0.0192,0.0713,0.1178,-0.0776,-0.1126,0.0480,0.0020,0.0323,-0.0769,0.1353,-0.0314,0.0714,-0.0561,-0.0075,0.0909,0.1238,-0.0741,0.0263,0.0886,-0.0886,0.0406,-0.0753,0.0876,0.0863,-0.0718,0.0584],[0.0250,0.0255,-0.1088,0.0156,0.0876,-0.0381,0.0042,-0.0688,0.0033,-0.0866,0.1628,0.0366,-0.0319,0.0448,0.1223,0.0369,-0.0842,-0.0019,0.0665,0.0876,0.1543,0.0567,0.1362,0.0071,0.1048,-0.0538,-0.0598,-0.1051,-0.0667,-0.0177,-0.0443,0.0174,-0.0123,-0.0534,0.0202,-0.1017,-0.0093,-0.0609,-0.1176,0.1027,-0.0748,-0.0813,-0.1210,0.0785,-0.0798,0.0858,-0.0550,-0.0548,0.1266,0.1043,0.0246,0.1729,0.0688,0.0644,0.0371,0.1038],[0.0189,-0.0922,-0.0487,-0.0495,-0.0076,-0.0386,-0.0525,-0.1383,0.0953,0.0870,-0.0528,-0.0261,0.0112,0.1182,-0.0293,0.0639,0.0568,0.1024,0.0644,-0.1017,-0.0183,0.1034,-0.0540,-0.0665,-0.1261,-0.1060,-0.0351,0.0158,0.0403,0.0122,0.0946,0.0471,0.0891,-0.0605,0.0517,-0.0711,-0.0876,-0.1634,0.0824,-0.0587,-0.0078,0.1054,-0.0124,0.0541,0.0277,-0.1316,-0.0157,-0.0594,0.0544,-0.0806,-0.1588,0.0057,-0.0104,-0.0286,0.0116,-0.0012],[-0.1443,0.0577,0.0897,0.0333,0.0819,0.0970,0.0382,0.0571,-0.0973,-0.0730,-0.0953,0.2899,-0.0217,0.0308,0.1321,-0.0665,0.0377,-0.1385,-0.0320,-0.0818,0.1914,0.0973,0.0786,0.2751,-0.0441,0.0291,-0.0044,0.0127,-0.0032,-0.0392,-0.1025,-0.0965,0.0678,0.0171,-0.0098,0.0867,-0.0409,0.0715,-0.0560,0.1191,-0.0241,0.0039,0.0443,0.1200,-0.0249,-0.0276,-0.0860,0.0086,0.0128,-0.1206,0.0010,-0.1488,-0.0624,0.0383,-0.1116,0.0312],[-0.1350,0.0394,0.0989,0.1036,0.0564,-0.1274,0.0656,0.0243,-0.0634,0.0253,-0.0443,-0.0498,0.0869,0.0105,-0.0647,-0.0043,-0.0950,-0.0795,-0.0083,-0.1512,0.0771,0.0566,0.0656,0.0264,0.0361,0.0080,0.0488,-0.0634,-0.0627,-0.0188,0.0628,-0.0820,0.0288,-0.0138,-0.0283,-0.1039,-0.0648,-0.0157,-0.0945,-0.0324,0.0047,-0.1048,0.0168,0.0796,-0.0315,-0.0935,0.0857,0.0407,0.1011,-0.1482,-0.0477,-0.0197,-0.0044,0.0384,-0.0692,0.0197],[0.0511,0.0425,0.0053,0.1083,0.0207,0.1036,0.0150,0.1013,0.1150,0.0164,-0.0369,0.1023,0.0568,-0.1377,-0.0435,-0.0021,0.0918,-0.0706,-0.0490,-0.0032,-0.0479,0.0969,-0.1025,0.0892,0.0338,0.0405,0.0189,-0.0855,-0.0378,0.0686,-0.0000,-0.0171,-0.0159,-0.0916,-0.0197,0.1070,0.1025,0.1015,0.0368,0.0319,-0.0466,-0.1393,0.0602,0.0337,0.0093,0.0374,0.1334,0.0657,-0.0689,-0.0023,0.0138,0.0628,0.0876,0.0869,-0.0453,-0.0666],[-0.0089,0.0342,0.0080,0.1452,0.0097,0.0683,0.0656,0.0216,0.0100,0.1275,0.0657,-0.1288,-0.0870,-0.0257,0.0659,-0.0110,0.0013,0.0327,-0.1126,0.0454,-0.1012,0.0707,-0.0009,-0.0239,-0.0708,0.1335,0.0334,-0.0167,-0.0793,0.0419,-0.0613,-0.0472,-0.0472,0.0903,0.0398,-0.0054,0.0068,0.0915,0.1106,0.0933,-0.0624,0.0005,0.0778,-0.0210,-0.1154,0.1134,0.0980,-0.0298,0.1051,0.0710,0.0259,-0.1455,0.0224,-0.0310,0.0306,0.0083],[-0.0639,0.0307,-0.1145,0.1108,-0.0241,-0.1673,0.0324,0.0036,0.0167,0.0598,0.1140,-0.0487,0.0465,-0.0033,0.0662,-0.0792,-0.0510,0.0872,-0.1129,-0.0337,0.0802,-0.0448,-0.0308,0.0567,0.0582,-0.0531,0.0002,-0.0831,-0.0610,0.0179,-0.0419,-0.0488,0.0267,0.0764,-0.1058,0.0740,0.0212,0.0159,-0.0129,-0.0188,-0.0235,-0.0463,0.0577,0.0313,0.0073,0.0422,0.1376,-0.0758,0.1050,0.0362,-0.0301,0.0130,-0.1223,0.0212,-0.1241,-0.1244],[0.0851,0.0663,-0.0001,0.0271,-0.0576,0.0109,-0.0573,0.0116,-0.1356,0.0531,0.0095,-0.0665,-0.0820,0.0779,-0.0401,0.0446,-0.0546,-0.0462,0.0742,-0.0789,0.0680,-0.0665,-0.0461,0.0637,0.1094,-0.1015,-0.0416,-0.0066,0.0095,-0.1029,-0.0993,-0.0720,0.0560,0.0190,0.0060,0.1031,0.0808,0.0874,-0.0972,-0.0014,-0.1311,-0.1443,0.0924,0.0238,0.0231,0.0979,0.0544,0.1113,0.0126,0.0746,-0.0831,-0.0859,-0.0489,-0.0525,-0.0917,-0.0344],[-0.0124,0.0775,-0.0595,0.0727,-0.0346,0.0334,-0.0568,0.0302,-0.0485,-0.0282,-0.0966,-0.0372,-0.1017,0.0788,-0.0430,-0.0290,0.0519,0.0055,-0.0032,0.0236,0.0374,0.0876,0.0691,0.0732,-0.0960,0.0516,0.0154,-0.0039,0.0342,-0.0622,0.0109,-0.0464,0.0628,0.1020,0.1011,-0.0125,-0.0852,0.1103,-0.0597,0.0714,0.0012,-0.0819,0.1180,-0.0185,-0.0751,-0.0830,0.0719,-0.0130,0.0199,0.0265,0.0794,-0.1084,0.0626,0.0127,-0.0611,0.0515],[0.0608,0.0314,0.1345,0.0677,-0.0827,0.0187,0.0374,-0.0200,0.0975,-0.0501,0.0510,-0.0849,0.0617,-0.0483,-0.0163,0.0184,0.0062,0.0598,0.1069,0.1279,-0.0206,-0.0388,-0.0762,-0.0741,0.1059,0.0908,0.0329,-0.0007,-0.0446,0.0026,-0.0550,0.0794,0.0043,-0.0101,0.1083,-0.0685,-0.1191,-0.0931,-0.0798,0.1560,0.1196,0.1195,0.0101,0.0326,0.0420,-0.0400,0.1032,-0.0997,0.0949,-0.1171,-0.0488,-0.0014,-0.0553,0.0816,0.1193,-0.0298],[0.2906,-0.0953,-0.1224,0.0509,-0.0443,0.0524,-0.0647,-0.0145,-0.0496,-0.1443,-0.0070,-0.1325,-0.0205,0.0214,-0.0151,0.1443,-0.1062,0.0388,-0.0487,0.2282,0.0638,0.1021,0.0709,-0.0540,0.2593,-0.0445,0.0204,-0.0089,0.0504,-0.0053,-0.0058,-0.0237,0.0256,0.0938,0.0472,0.1046,0.0800,-0.0926,-0.0117,-0.0164,-0.0291,0.0742,-0.0015,-0.1116,-0.0364,0.1390,-0.0787,0.0334,-0.0474,0.2151,-0.1023,0.2546,-0.0263,-0.0559,0.1104,0.0337],[0.1146,0.1275,0.0386,-0.0748,-0.0003,0.0087,-0.0410,-0.0526,-0.0295,0.0219,-0.0705,0.0013,-0.1303,0.0649,-0.0680,-0.0697,0.0376,0.0464,0.0608,0.1195,-0.0265,-0.0420,-0.1005,0.1132,-0.1614,0.0437,-0.0658,-0.0295,-0.0488,-0.0137,-0.0310,0.1090,0.0663,0.0032,0.1438,-0.0853,0.0958,-0.0833,0.1284,-0.0430,0.0690,0.0037,-0.0386,0.0059,-0.0249,0.1504,0.0212,-0.0548,-0.0752,0.0504,0.0098,0.1378,-0.0401,-0.0359,0.0296,-0.0998],[-0.0344,0.0025,0.0287,-0.0294,-0.0330,0.0772,-0.1170,0.0766,-0.0435,-0.0006,0.0464,-0.0597,-0.0182,0.0646,-0.0745,0.1228,0.0286,0.0142,0.0726,0.0056,-0.1000,0.0675,-0.0117,0.0780,-0.0490,-0.0943,-0.1175,-0.0230,0.0559,-0.0867,-0.0639,0.1047,0.0685,-0.0908,-0.0690,0.1050,0.0560,0.0035,-0.0677,-0.1034,-0.0872,0.0308,-0.0591,0.0234,-0.0554,0.0489,-0.0770,0.0251,0.0645,-0.0362,0.0279,-0.0348,-0.0369,0.0412,-0.0843,0.0289],[0.0417,-0.0517,-0.0720,-0.0396,-0.0019,-0.0622,-0.0872,-0.0726,-0.0378,0.1076,-0.0434,-0.0058,-0.0206,0.1071,0.0080,0.0198,0.0375,0.0900,0.1125,-0.0832,-0.0370,-0.0483,-0.0587,0.0332,0.0937,-0.1214,0.0027,0.0548,-0.0282,-0.1457,0.0138,0.1126,0.0354,0.0904,0.0904,-0.0861,-0.1200,-0.0882,-0.0478,0.0323,-0.0584,0.0108,0.1101,0.0794,-0.0037,-0.0834,0.0242,-0.0543,0.0904,-0.0481,0.0526,0.0549,-0.0480,-0.1684,-0.0918,-0.0858],[-0.0571,-0.1171,-0.0057,-0.0370,-0.0597,0.1122,-0.0016,-0.0080,-0.0549,0.1046,-0.0332,0.1738,0.0614,0.0073,-0.0080,-0.0147,-0.1167,-0.0681,-0.1288,0.0386,0.1016,0.0431,0.0149,0.1505,0.0140,-0.0674,-0.0392,-0.0887,0.1162,0.0082,-0.0242,0.0141,0.1143,-0.0395,-0.1010,0.0545,0.0097,-0.1030,0.0251,0.0444,0.0441,0.1098,0.0524,-0.0272,0.0219,0.0181,0.0104,-0.0531,0.0862,-0.1461,-0.0141,0.0919,-0.1055,0.1007,-0.1071,-0.0438],[0.0396,-0.1189,-0.1044,-0.0006,-0.0956,-0.0747,0.0756,-0.0030,-0.0849,0.0582,0.1432,0.1017,-0.0309,-0.0722,-0.0353,0.0405,-0.0568,0.0402,-0.0410,0.0127,-0.0016,0.0486,0.1258,0.0270,-0.1294,0.0833,0.0625,0.1360,0.0197,0.0787,-0.0449,-0.0298,-0.0138,-0.0633,0.0064,0.0770,0.1650,0.0321,0.0105,-0.0322,-0.1396,-0.0870,0.1059,-0.0272,-0.0629,0.0949,-0.0420,0.0805,0.0758,-0.0326,-0.0610,-0.1071,-0.0280,0.0243,0.0351,0.0248],[0.0865,-0.0932,0.0600,-0.0210,0.0695,0.0495,0.0341,0.0725,0.0021,0.0585,0.0784,-0.0473,0.0092,-0.0564,-0.0067,0.0157,-0.0340,-0.1120,-0.1067,0.0822,0.1131,0.0641,0.0846,-0.0176,0.0451,0.0195,-0.0579,0.0947,-0.0318,-0.0348,0.0919,0.0817,0.0281,-0.0172,-0.0002,0.0733,-0.0414,0.0281,0.0261,-0.1116,-0.0326,0.0519,0.0862,-0.0224,0.0962,-0.1057,-0.0138,-0.0770,0.0084,-0.0563,0.0187,0.0177,-0.0619,-0.0206,0.0500,-0.0893],[0.0112,0.0900,0.0483,0.1184,0.0623,0.0069,0.0317,0.0063,0.0014,0.0554,-0.0485,0.2130,0.1020,0.0178,0.0364,-0.1021,-0.0552,0.1200,-0.0212,-0.0326,-0.1088,0.0044,0.0495,0.1736,-0.0107,0.0779,0.0222,0.1052,-0.0779,-0.0551,-0.0744,-0.0333,0.0409,0.0715,0.0197,-0.0301,0.1547,-0.1573,-0.1126,-0.0606,-0.0707,-0.0295,0.0623,0.0924,0.1768,-0.0091,0.0473,-0.0143,0.0170,0.0769,0.1269,0.2416,-0.0536,-0.0035,0.0797,0.1643],[0.0408,-0.0289,0.0937,-0.0822,-0.0272,0.0527,0.0806,-0.0321,0.0928,0.1449,-0.0158,-0.0812,0.0177,0.1720,-0.0845,0.0137,0.0238,0.0948,0.0446,0.1973,-0.0933,-0.0537,-0.1071,-0.1379,-0.2836,0.0379,0.0621,0.0202,-0.0004,-0.0443,0.0626,-0.0132,-0.0282,0.0960,0.1793,0.0737,0.0159,-0.0978,0.0287,0.0625,-0.0173,0.0612,-0.0396,-0.0328,0.0495,-0.0045,0.1086,0.0049,-0.0322,-0.0172,-0.1084,-0.0191,0.0189,-0.0689,-0.0859,0.1037],[-0.0963,0.0418,-0.1323,-0.0752,-0.0165,0.0891,-0.0713,-0.1079,0.0523,-0.0191,0.0664,-0.0473,0.0921,-0.0835,0.0220,-0.0603,-0.0936,-0.1162,0.0040,-0.1106,0.0476,0.0165,0.1439,-0.0202,-0.1105,-0.0235,0.0940,-0.0180,-0.0342,0.0232,-0.0641,-0.0756,-0.0842,0.1067,-0.0536,0.0089,0.0274,-0.0283,-0.0306,0.0456,-0.0388,0.0560,-0.0454,0.0432,0.1092,-0.0493,-0.0295,0.0052,-0.0149,0.1185,0.0076,0.0739,0.1339,-0.0959,-0.0319,-0.0523],[-0.0074,0.1621,-0.0865,0.0577,-0.0467,0.0536,-0.0071,-0.0509,-0.0335,-0.0612,-0.0598,0.0767,0.0777,-0.1362,0.0373,-0.0587,-0.0868,-0.0199,-0.0017,-0.0955,-0.0124,0.0198,-0.0854,0.1180,-0.0136,0.1268,0.0024,0.0716,-0.0248,-0.0260,0.1308,0.1004,0.0695,0.0466,-0.0140,0.0238,0.0195,-0.0626,0.0475,-0.1072,-0.0329,0.0009,0.0124,-0.1266,-0.0507,0.0099,-0.0140,-0.0060,-0.0280,-0.0220,-0.1215,0.1652,0.1017,0.1091,0.1476,-0.1763],[0.1173,0.1243,-0.0456,0.0651,-0.0008,0.0137,0.0531,-0.0002,-0.0477,-0.0800,-0.0417,0.0047,-0.0784,0.0019,-0.0211,-0.0654,0.0750,-0.0146,0.0301,-0.0603,0.0594,0.0300,0.1068,-0.0933,0.0576,0.1269,0.0653,-0.0070,-0.0224,0.0105,0.1254,0.1118,-0.0255,0.0016,0.0761,0.0803,-0.1345,0.0097,0.0232,-0.1033,-0.1249,-0.0631,0.0168,-0.0096,0.0981,-0.0455,-0.0991,0.0251,-0.0389,-0.0474,0.0039,-0.0808,0.0848,0.0319,0.0496,-0.0966],[0.0578,-0.0412,-0.0342,-0.1078,0.1024,0.0678,-0.1085,0.0548,-0.0998,0.0037,-0.0196,0.0346,-0.0230,0.0401,0.0211,0.0173,0.1100,-0.0405,-0.0245,-0.0833,-0.0474,-0.0297,0.0494,0.0782,-0.0083,0.0790,-0.1228,-0.0543,-0.0215,-0.0302,-0.0510,-0.1548,-0.0270,0.1030,0.1302,-0.0678,0.0173,0.0118,0.0258,0.0158,-0.0583,-0.0274,0.1233,0.0607,0.0125,-0.0662,0.0782,0.0666,0.0978,-0.0188,-0.0633,0.1861,0.0077,-0.1021,0.1858,0.0040],[0.0108,-0.0562,0.0302,-0.1021,-0.0248,0.0358,0.1229,0.0222,0.0202,-0.0485,-0.0771,0.0423,-0.0860,-0.0238,0.0672,-0.1069,-0.0754,0.0353,0.0523,-0.0745,0.0838,-0.0725,-0.0814,0.0932,0.0573,-0.0298,0.0059,0.0762,0.0789,0.0617,-0.0008,0.0803,0.0417,0.1080,-0.0532,0.0779,-0.0181,0.1432,-0.0802,0.0185,0.0023,0.0677,-0.0089,0.0531,-0.0042,-0.0545,-0.0815,-0.0825,0.0650,-0.0278,0.1288,-0.0841,-0.0403,-0.0896,-0.0346,0.0049],[0.0329,0.0764,0.0343,0.0545,0.0705,-0.0991,0.0689,0.0989,-0.0046,0.0230,0.0027,0.0606,-0.0297,0.0704,-0.0447,-0.0865,-0.0061,0.1600,-0.0036,0.1119,-0.0034,0.0212,-0.0760,-0.0741,-0.1614,-0.0546,0.0800,-0.0614,-0.0350,0.0113,-0.0589,0.1078,0.0882,-0.0034,-0.0669,0.0526,0.0956,0.1450,0.1131,0.0339,-0.0406,0.0680,0.0983,0.0596,0.0992,-0.1088,-0.0326,0.0179,0.0539,0.0859,-0.0091,-0.0383,-0.0677,-0.0099,0.0290,0.0412],[-0.0048,-0.1505,-0.0138,0.0144,-0.0273,-0.0248,0.0796,-0.0623,-0.0243,0.0599,-0.0743,-0.0219,0.0639,-0.0131,0.0043,-0.0083,-0.0806,-0.0709,-0.0331,0.0112,-0.1038,0.0003,-0.0602,-0.1001,0.0092,-0.0336,0.0659,0.0502,-0.0750,0.0435,-0.1016,-0.0061,-0.0246,0.0577,-0.0000,-0.0577,-0.0622,-0.1348,-0.0403,-0.0198,-0.0510,-0.0226,-0.1043,-0.0059,0.0639,0.0210,-0.0400,-0.1004,0.0834,-0.1135,-0.0713,-0.0640,-0.0485,-0.0899,0.0959,-0.0184],[-0.0108,-0.1230,-0.0984,-0.0779,0.0281,0.0188,0.0140,0.0303,-0.0024,0.0433,0.0358,0.0995,0.0223,-0.2149,-0.1048,0.0431,-0.0423,-0.0257,0.0324,0.0375,0.0620,-0.0427,-0.0801,-0.0797,-0.0674,0.1013,-0.0304,0.1064,0.0511,-0.0358,0.1475,0.0426,0.0218,-0.0251,0.1192,0.0632,-0.0733,0.1797,0.0087,0.0762,-0.0023,0.0086,0.0634,0.0388,0.0536,0.0405,0.0396,-0.0455,-0.0044,0.0999,0.1246,-0.0426,0.0965,-0.0383,0.0206,0.0042],[-0.1167,0.1691,0.0831,-0.0588,0.0473,0.0147,-0.0730,0.0920,-0.0669,-0.0056,-0.1067,0.0656,0.0995,0.0049,-0.1535,0.0175,0.0430,-0.0269,-0.0448,0.0334,0.0221,0.0131,-0.0777,-0.0559,-0.0473,-0.0002,0.0098,0.0833,0.0135,0.0125,-0.0610,-0.0664,0.0479,-0.0730,0.1112,0.0056,-0.0255,0.0283,0.0815,0.0624,0.1087,-0.0398,0.0732,0.0106,0.0475,0.1648,0.0408,-0.0776,-0.0905,-0.0894,0.0212,-0.0149,0.1709,0.0138,-0.0723,0.0555],[0.1167,-0.0720,-0.0960,-0.0388,-0.0383,0.0835,-0.0695,0.0608,0.0697,-0.0877,0.0543,0.1048,-0.0515,0.0286,-0.0224,0.0676,0.0461,-0.0138,-0.0863,-0.0805,-0.1126,0.0362,0.1936,0.1059,0.2230,-0.0758,0.0704,-0.0551,0.0615,0.0108,0.0889,0.0428,0.0545,0.0825,-0.0339,0.0627,-0.1109,-0.0503,0.0507,-0.0641,0.0488,0.0294,0.0543,0.0186,0.0877,-0.0256,-0.0155,-0.0005,0.0422,0.0819,0.1623,-0.0417,0.0850,0.0710,0.0363,0.0862],[0.0953,0.0049,-0.0132,-0.0991,0.1208,0.0492,0.0744,0.0587,0.0909,0.0867,0.0007,0.0116,-0.0041,0.0186,0.0016,-0.0082,-0.0089,-0.0777,0.0444,-0.0701,-0.1225,-0.0979,-0.1297,0.0703,0.1472,0.0105,-0.0600,0.0408,-0.0783,0.1066,0.1003,0.0542,0.0018,-0.0046,-0.0267,-0.0754,0.0698,-0.0287,-0.0202,-0.0545,0.0912,0.0591,0.0813,0.0010,0.0253,0.0157,0.0462,0.0674,-0.0723,0.0794,-0.0864,-0.0470,-0.0207,0.1382,-0.0676,-0.1505],[0.0320,0.0943,-0.1351,0.0036,0.1427,0.0523,-0.0909,0.0450,0.1357,0.0208,0.1192,0.0244,-0.0063,-0.0466,0.0723,-0.0696,-0.0848,-0.1839,-0.0397,-0.1115,0.0688,0.1027,0.0932,0.0916,-0.1872,0.0794,0.0460,-0.0345,0.0578,-0.0119,0.0104,-0.0629,-0.0484,-0.0396,-0.0365,-0.0186,0.1528,-0.2415,-0.0304,-0.1113,0.0366,-0.0713,0.0942,-0.0941,-0.0056,0.0449,-0.0471,0.0745,-0.0325,-0.0741,-0.0316,0.0956,0.0267,0.0834,-0.0671,-0.0435],[0.0681,-0.0882,-0.0878,0.0537,-0.0928,-0.0266,0.0440,0.0533,0.0934,0.0490,-0.0133,-0.0912,0.0438,-0.0459,-0.0368,-0.1145,0.0262,-0.0445,-0.0598,-0.1012,0.0828,-0.0632,0.0089,0.0718,-0.1146,-0.0695,-0.0327,-0.0466,-0.0818,0.0525,0.0334,0.0742,0.1001,0.0483,0.0104,0.0692,-0.0449,0.0375,0.0735,-0.0629,-0.0446,-0.0817,-0.0434,0.0513,0.0244,-0.0508,-0.0110,0.0036,-0.0421,-0.0827,-0.0339,0.0196,-0.0181,0.1077,-0.0403,-0.1152],[0.0827,-0.0098,-0.0823,-0.0214,0.1408,-0.0033,0.0302,0.0231,-0.0618,-0.0959,-0.0435,0.0129,0.0523,-0.1000,-0.0899,-0.0533,0.1660,0.0066,0.0255,-0.0627,0.0268,0.0298,-0.1319,-0.0112,-0.0173,-0.0514,-0.0058,-0.0482,-0.0759,-0.0290,0.0280,-0.0487,-0.0052,-0.0229,0.0148,-0.0511,0.0196,-0.0349,0.0075,0.0544,0.1046,0.0291,-0.1065,-0.0366,-0.0780,0.0571,-0.0071,-0.1361,-0.0579,-0.0654,0.0335,0.0599,-0.0761,0.0014,-0.0405,0.1466],[0.0752,0.1193,0.0715,-0.0862,-0.0222,0.0893,-0.0691,0.0516,0.0245,0.1223,0.0632,0.0245,0.0124,-0.0872,-0.0642,-0.0060,0.0179,-0.0899,-0.0885,0.0496,0.0845,-0.0822,-0.0130,0.1483,-0.0880,-0.0478,0.0009,-0.0194,0.0232,0.0433,-0.0526,-0.0405,-0.1167,0.0361,-0.0340,-0.0828,0.0616,0.0308,-0.0964,0.0862,0.0670,0.0846,-0.0286,0.1108,-0.0062,0.1043,0.1086,0.0840,-0.0059,0.0490,0.0641,0.0064,0.1096,0.0001,-0.0701,0.0258],[0.0774,0.0473,0.0038,-0.1065,-0.1438,0.0786,0.0493,-0.0504,0.0622,0.0139,-0.1064,0.0770,0.0417,-0.1472,-0.1425,0.0890,0.0748,-0.0849,0.0499,0.0070,0.0717,0.0829,0.0279,0.0583,-0.1618,0.0208,0.0874,-0.0413,-0.0890,0.0706,0.0693,-0.0443,-0.1043,0.0056,-0.0218,0.1119,0.0755,-0.0342,0.0583,-0.0043,0.0056,-0.0672,-0.0360,-0.0111,-0.0371,-0.0021,0.0029,0.0495,0.0161,0.1044,0.0828,0.0005,-0.0071,0.0505,0.0136,0.0151],[0.0637,0.0207,-0.0773,-0.0316,-0.0425,0.0775,-0.0341,-0.1069,-0.1040,-0.0542,0.0114,0.0757,-0.0728,0.0091,-0.0850,0.0407,0.0132,0.0261,0.0785,-0.0459,-0.0887,0.0738,-0.0390,-0.0811,-0.0969,-0.0050,-0.1166,0.0494,-0.1275,0.0442,-0.0837,0.0030,-0.0828,-0.0907,-0.0935,0.0150,-0.0664,-0.1160,-0.0664,0.0705,0.0458,0.0257,-0.0261,-0.0483,-0.0619,0.0539,0.0040,-0.0293,-0.0467,0.0885,0.0462,0.0184,0.0433,-0.1446,-0.0403,-0.0120],[0.1019,-0.0420,-0.1299,0.1199,0.0116,0.1619,0.1276,-0.0264,-0.0674,-0.0157,0.1464,-0.0861,-0.0419,-0.0645,0.0582,-0.0437,0.0392,0.0322,0.1042,0.0093,-0.0008,0.0063,0.1405,0.0780,-0.1312,-0.0063,-0.0565,0.0932,-0.0767,-0.1060,-0.0871,-0.0168,0.0129,-0.0037,0.0363,0.0759,0.1623,-0.0853,0.0781,-0.0023,-0.0518,-0.0509,0.1413,0.0722,0.0467,0.0730,-0.1294,0.0472,-0.0267,0.0714,0.1239,0.0066,-0.2187,-0.0328,0.0593,-0.0798],[0.1217,-0.0575,0.0616,-0.0933,0.0837,-0.0991,-0.0482,-0.0766,0.0113,-0.0244,0.0650,-0.0212,-0.1978,0.0784,0.0096,-0.0869,-0.0317,-0.0520,0.0850,-0.0245,0.0262,0.0135,0.0031,0.0646,-0.0052,-0.0519,0.0034,0.0251,0.0059,0.1097,-0.0392,-0.0201,0.0235,-0.0628,0.0665,-0.0368,-0.0238,-0.0869,-0.0043,-0.0777,-0.0802,-0.0387,-0.0237,0.0559,-0.0006,-0.0207,0.0712,-0.1327,0.0236,-0.0952,0.1475,-0.0521,0.0243,-0.0855,0.0271,0.0194],[0.1061,0.0720,-0.0180,-0.0638,0.0768,-0.1151,-0.0467,0.0907,-0.0834,-0.0658,-0.0290,-0.0099,-0.0797,-0.1889,0.0769,-0.1032,0.1047,-0.0784,0.0441,0.0914,-0.0306,0.0609,0.0426,0.0459,0.1092,-0.0139,-0.0074,0.0035,0.0355,0.0451,0.0712,-0.0475,0.0216,-0.0024,-0.1669,0.1033,-0.0929,-0.0911,0.0049,0.0192,0.0308,-0.0624,-0.0561,-0.0621,0.0098,0.1215,0.0843,0.0835,-0.0799,0.0451,-0.0320,0.0474,-0.0008,-0.0483,0.0299,0.0152],[-0.0413,0.0562,0.0883,0.0940,-0.0212,-0.0152,0.0792,0.0264,-0.0361,0.0616,0.1074,0.0025,0.0245,0.0486,0.0576,-0.0470,-0.1132,0.0380,-0.0453,0.0341,0.0210,-0.0903,0.1157,0.1302,0.0141,-0.0843,-0.0481,-0.0034,0.0743,0.0041,-0.0023,-0.1156,-0.0132,-0.1131,-0.0594,0.0562,-0.0245,-0.0777,0.0567,0.0548,0.1156,0.0510,-0.0583,0.0569,-0.0655,-0.0647,0.0793,0.0237,0.0473,0.0223,-0.1221,-0.0162,0.1212,-0.0592,-0.1135,-0.1201],[-0.0214,-0.0950,0.0028,-0.0913,-0.0922,-0.0264,0.0425,0.0809,-0.0934,0.0104,0.0932,0.0128,-0.0339,-0.0878,-0.0664,-0.0132,-0.0478,-0.0707,0.0176,-0.1003,-0.0845,0.0098,-0.0198,0.0520,0.0595,0.0750,-0.0235,0.0465,-0.0713,0.0184,0.0799,0.0241,0.0410,0.0481,0.1033,-0.0743,0.1295,0.0673,-0.0581,0.0570,-0.1386,0.0762,-0.0266,0.0819,0.0809,-0.0789,0.0415,0.0332,-0.0647,0.0324,-0.0323,-0.0943,-0.0014,0.0873,0.0031,0.0546],[-0.1053,-0.1173,0.1389,0.0104,0.1004,0.0520,0.0659,0.0254,0.0343,0.0893,-0.0813,-0.0588,-0.0363,-0.1062,-0.0548,-0.0089,0.0209,-0.0121,0.0099,0.0195,-0.0939,-0.0715,-0.0185,-0.0490,-0.2422,0.0255,-0.0545,-0.0500,-0.0638,-0.0649,0.1217,0.0631,0.1306,0.1302,-0.0040,-0.0968,-0.1076,0.0941,0.1027,0.0743,0.0276,-0.0300,-0.1028,-0.0510,0.1200,-0.0301,0.0881,-0.0212,-0.0027,0.1300,0.0563,-0.0007,-0.0321,-0.0593,-0.0259,-0.1314],[-0.0475,0.0480,-0.0433,0.0530,0.0097,-0.0557,-0.0331,-0.0825,-0.0487,0.0042,0.0176,-0.0631,0.0252,-0.0938,-0.0202,-0.1109,-0.0543,0.0308,0.0189,-0.0959,0.0012,0.0120,-0.0387,0.0963,0.1272,0.1021,-0.0118,0.0480,0.0306,-0.0279,0.0023,-0.0072,0.0403,-0.0981,-0.0510,0.0144,-0.0142,0.0198,0.0497,-0.0021,0.0857,-0.1023,0.0279,-0.0039,-0.0369,0.0420,0.1315,-0.0817,0.1223,-0.0484,0.0167,-0.1268,0.1677,0.1682,0.0128,0.0100],[-0.0352,0.0247,0.0759,0.1079,-0.0405,-0.1174,0.0757,0.0852,0.0143,0.0120,0.0266,0.0156,0.0800,0.0278,-0.0055,-0.0348,-0.0027,-0.0236,0.1315,-0.1877,-0.0433,0.0437,-0.0124,-0.0256,0.2895,0.1494,0.0361,0.0203,-0.0966,-0.0189,0.0501,-0.0695,0.0527,-0.0855,0.0631,0.0030,-0.1700,-0.1882,0.0395,-0.0535,-0.0251,0.0119,-0.0673,-0.0117,0.0792,-0.0244,0.0587,-0.0744,0.0268,0.0307,0.0180,-0.0541,0.2285,0.1328,0.0553,0.0521],[0.0152,0.0117,0.0727,-0.0741,0.0245,-0.0622,-0.0514,-0.0770,0.0051,-0.0680,-0.0023,-0.0060,-0.0491,-0.0193,0.0398,0.0837,0.0550,-0.0515,0.0309,0.0878,-0.0471,0.0154,0.0102,0.0347,-0.0545,-0.1189,0.0506,-0.0186,0.0137,-0.0077,-0.0107,0.0839,0.0201,0.0616,-0.0308,-0.1024,0.2019,-0.1567,-0.0743,-0.1019,-0.0928,0.0216,-0.0560,-0.0858,-0.0670,-0.1876,-0.0973,0.0177,0.0668,-0.0675,0.0059,0.1797,-0.0838,-0.0291,0.1515,-0.0090],[0.0063,-0.0611,0.0822,-0.0827,-0.0763,0.0604,0.1331,0.0402,0.0015,0.0603,0.1293,-0.0934,-0.0139,-0.0921,0.0816,-0.0452,-0.0272,-0.0873,0.0956,-0.0014,-0.0543,0.0570,0.1045,-0.0688,0.1182,-0.0845,0.0715,-0.1068,0.0079,0.0495,0.0665,0.0869,-0.0506,0.1534,-0.0361,-0.0408,-0.0123,0.0149,-0.0699,0.0117,-0.1112,-0.0570,-0.0307,0.0700,-0.0727,-0.0475,0.0759,0.0047,-0.0246,0.0159,0.0579,0.0276,-0.0665,0.0713,-0.0757,-0.1246],[-0.0787,0.0900,-0.0943,-0.0882,-0.0191,0.0726,-0.0250,0.0147,0.0672,-0.0480,0.1343,0.0148,-0.0842,0.0044,0.0584,-0.0652,-0.0672,0.0123,-0.0153,-0.0326,-0.1114,0.0928,0.1228,0.1403,0.0204,-0.0431,0.0546,-0.0213,0.1400,0.0081,0.0921,0.1119,0.0824,0.0190,0.1333,-0.0960,-0.1450,0.0369,0.0086,-0.0583,0.0826,0.0439,-0.0765,-0.0140,0.0425,0.0545,0.1141,0.0692,0.0353,-0.1186,0.0179,0.0413,-0.0362,0.0104,-0.1088,0.0814],[0.1004,0.0692,0.0076,-0.0261,0.0202,0.0079,-0.0299,-0.0294,0.0830,0.0290,-0.0855,0.1208,0.0272,-0.0519,0.0185,-0.0337,0.0822,0.0119,-0.0229,0.0796,0.0031,-0.0561,0.0147,-0.0739,0.0624,-0.0812,0.1043,-0.0263,0.0505,0.0993,-0.0196,0.0975,-0.0085,0.0247,0.1220,0.0329,-0.0320,0.0060,0.0468,-0.0093,-0.0910,0.0279,-0.0084,0.1516,-0.0367,0.0845,0.1075,0.0822,0.0576,-0.1126,-0.0165,-0.1171,0.0389,0.0044,-0.0415,0.0234],[0.0180,0.0241,0.0742,-0.0939,-0.0049,0.0477,0.0047,0.0266,-0.0691,0.0586,-0.0554,0.0927,-0.0756,0.0236,0.0306,-0.0649,-0.0334,-0.0618,0.0871,0.0737,0.0417,-0.0328,0.1319,0.0402,-0.1008,0.1132,0.0037,-0.0019,-0.0219,0.0940,-0.0105,-0.0317,0.0164,0.0907,-0.0204,0.0299,0.0154,0.0442,0.1318,0.0421,-0.0579,-0.0163,-0.0095,0.1140,0.0950,-0.0321,-0.0085,-0.0753,-0.0388,-0.1263,0.0604,-0.2429,0.0520,0.1247,-0.1276,-0.0161],[-0.0390,-0.0864,0.0049,-0.1131,0.1403,-0.1311,0.0455,0.1070,0.0734,-0.0524,0.1923,0.0513,0.0356,0.0058,0.0227,0.0452,0.1220,0.0538,-0.0632,0.0542,-0.0649,-0.0039,0.0592,0.1255,-0.1366,-0.0474,0.0810,0.0161,0.0348,0.0063,-0.1230,-0.0213,0.0431,-0.0204,0.1153,-0.0924,0.0674,-0.1374,-0.0483,-0.0118,-0.1039,0.0075,0.0053,-0.0937,-0.1374,0.0371,-0.0263,0.0363,0.0150,-0.1029,-0.0763,-0.0749,-0.0216,-0.0230,0.1054,-0.1127],[0.0961,0.0678,-0.1525,0.0620,-0.0857,-0.0192,-0.1208,-0.1200,0.1056,-0.0088,-0.0320,-0.0988,0.0179,-0.0520,0.1003,0.0588,0.1043,0.0914,-0.0344,-0.0025,-0.0945,-0.0583,0.1267,0.0828,-0.1503,-0.0261,-0.0512,-0.0762,-0.0182,0.0375,0.0853,0.0561,0.0598,-0.0667,-0.0116,-0.0548,0.0924,-0.0964,-0.0393,-0.0399,0.0432,-0.0039,-0.0138,0.0309,0.0575,0.0545,0.0452,-0.0537,0.0555,0.0215,-0.0079,-0.0565,-0.1158,-0.0461,0.0079,-0.1392],[0.0574,-0.0649,-0.0807,0.0901,-0.0170,0.0348,0.0410,0.0609,-0.0728,0.0546,0.0311,0.0066,-0.0445,0.0477,-0.0058,-0.1267,0.0503,-0.1076,0.1196,0.0314,0.0576,-0.0929,0.0213,0.0847,0.0480,0.0673,-0.0194,-0.0041,0.1309,0.0991,0.1128,-0.0150,-0.0381,0.0377,0.0212,0.0974,0.0796,-0.0710,-0.1179,-0.0161,0.0853,-0.0629,-0.0442,0.1238,0.0637,0.0353,0.1376,-0.0707,0.0226,0.0292,-0.0028,0.1397,0.0518,-0.0812,0.1208,0.1513],[-0.0336,-0.0550,-0.0212,0.1257,0.1447,-0.0404,0.0669,-0.0443,-0.0118,-0.0112,-0.0071,0.0722,0.0500,0.0158,-0.0030,-0.0637,-0.0224,0.0741,-0.0685,0.0426,-0.0317,-0.0064,0.1107,-0.0911,-0.1605,0.0485,0.0817,0.0099,-0.1062,-0.0571,0.0909,-0.0150,-0.0479,0.0432,-0.0427,0.0003,0.0198,-0.0249,0.0949,-0.0930,0.1120,-0.0642,-0.0281,-0.0305,-0.0492,0.0057,0.0773,-0.1088,0.0714,-0.0514,0.0291,0.1310,-0.1081,-0.0225,0.1371,-0.0851],[-0.0360,-0.0034,0.0876,-0.0673,-0.0021,0.0331,0.0915,-0.0156,0.0506,0.0882,-0.0349,0.0292,0.0864,-0.0821,-0.0744,0.0135,0.0173,0.1289,0.0944,0.1174,-0.0013,-0.0288,0.0056,0.0472,-0.0825,0.0103,-0.0750,0.0599,-0.0313,0.0373,-0.0833,0.0948,0.0946,-0.0585,-0.0857,0.1031,0.1404,-0.0592,0.0063,-0.1466,-0.1459,0.1028,0.0650,0.0521,-0.0119,0.0458,0.0404,-0.0258,-0.0904,-0.0458,-0.0280,-0.0347,-0.1082,-0.0517,0.0319,-0.0234],[0.0525,0.0402,-0.0077,0.0464,0.0371,0.0374,0.0162,0.0042,-0.0798,0.0074,0.0147,-0.0388,-0.0312,0.1119,0.0859,0.0185,0.0533,0.0469,0.0113,0.0471,-0.0156,0.0557,-0.0163,0.1702,-0.0864,0.0880,0.0783,0.0973,0.0959,0.0781,0.0279,0.0738,-0.1104,0.1294,0.0991,0.0773,0.1224,0.0251,-0.0840,0.1013,0.0242,0.0085,0.0276,-0.0161,-0.0677,0.0793,0.0061,-0.0750,-0.0760,0.1162,0.1488,-0.0048,0.1211,0.0542,0.1377,0.1020],[0.0901,-0.0093,0.0880,-0.0811,0.0491,0.1309,-0.0406,-0.0000,0.0358,-0.0782,0.0072,0.1084,-0.0266,-0.0372,0.0348,0.0133,-0.0398,-0.0126,-0.0395,0.0055,-0.0110,-0.0799,0.1253,0.0473,0.0949,0.0510,0.0591,0.0570,-0.0369,-0.0162,-0.0128,-0.0271,-0.0934,0.1026,-0.1139,0.0778,-0.0375,-0.1316,-0.0336,-0.0373,0.0026,0.0567,-0.0269,0.0849,-0.0072,0.1172,-0.0095,-0.1246,0.0513,-0.0185,0.0090,0.0257,0.2050,0.0481,0.0416,0.0824],[-0.0173,0.0039,0.0136,0.1214,0.0237,0.1139,-0.0434,-0.0281,-0.0121,-0.0241,-0.1059,-0.0113,-0.0904,0.0456,0.0963,0.1046,-0.0985,0.0345,-0.0161,0.0696,-0.0380,0.0169,-0.1303,-0.0365,0.0024,-0.0748,0.0266,-0.0032,-0.0879,0.0728,-0.0744,0.0493,0.0752,0.0241,-0.1369,0.0067,-0.0386,-0.0807,-0.0785,0.0435,0.0135,-0.0417,0.0935,-0.0359,0.0675,-0.0085,0.0407,0.1221,0.0491,-0.0666,-0.0400,-0.1281,0.0955,0.1691,-0.0587,-0.0482],[-0.0315,-0.2027,0.0359,-0.0875,0.0427,-0.0391,0.0399,-0.0386,0.0483,0.0576,-0.0534,-0.0829,0.0220,0.0372,0.0419,0.0206,-0.0622,-0.0649,0.0992,-0.1100,-0.1190,-0.0936,-0.0017,0.0720,0.1630,0.0965,0.0336,-0.0503,-0.0682,-0.0193,-0.0565,0.0425,-0.0384,-0.0443,-0.0431,0.1216,-0.0111,0.1167,0.0294,0.1073,-0.0005,0.0589,-0.0906,0.0639,-0.0130,-0.0485,0.0159,0.0259,-0.0229,0.0705,0.1737,-0.0667,-0.0802,-0.0635,-0.0432,0.0515],[0.0926,-0.0154,0.0632,0.0662,-0.0915,0.0080,-0.0044,-0.0159,-0.0697,0.0131,0.0815,0.0375,-0.0620,0.0835,0.0182,0.0235,0.0549,0.0315,-0.1699,0.0827,-0.0527,0.0940,-0.0845,0.0174,0.1770,-0.1349,0.0470,-0.1187,0.0547,-0.0205,0.0555,-0.0826,-0.0315,0.0235,0.1489,0.0369,-0.1236,-0.0312,-0.1299,-0.0802,0.0853,-0.0780,0.0878,0.0468,-0.0170,0.0491,0.0190,0.0576,-0.0396,-0.0145,0.0538,-0.0067,0.0200,-0.0428,-0.1165,0.0996],[0.0904,0.0018,0.0024,0.0833,0.0030,0.0094,-0.1128,0.0576,-0.0626,-0.0159,-0.0933,-0.0587,-0.0354,-0.0346,-0.0643,-0.1561,-0.0101,0.0172,-0.0871,-0.0677,0.0706,-0.0213,0.0014,-0.0679,-0.2510,0.0427,0.1015,0.0202,-0.0896,0.0313,0.0217,-0.0090,0.0040,-0.0012,0.0631,0.1007,-0.0996,0.0008,0.0004,-0.0687,-0.0511,-0.0469,-0.0853,-0.0922,-0.0976,0.0116,0.0400,-0.0596,0.0866,0.1207,-0.1197,0.0008,-0.0056,0.0255,0.0002,0.0614],[0.0705,0.1213,-0.0090,0.0497,-0.0158,-0.0594,-0.0259,-0.0147,-0.1186,0.0439,-0.0053,-0.0896,0.0677,0.0109,0.0115,-0.0070,0.0341,0.0928,-0.0700,-0.1234,-0.1580,0.0506,-0.1472,-0.0371,0.0359,0.0555,0.0338,-0.0147,0.0462,-0.0069,-0.1313,0.0595,0.0786,0.0763,-0.0192,0.0454,0.0378,-0.0497,0.0584,-0.0871,-0.0186,-0.0412,0.0616,0.0414,0.0261,0.0734,-0.1101,-0.0731,-0.0176,0.0012,0.0365,-0.0614,-0.0638,-0.0803,0.0815,0.0408],[-0.0223,0.0599,-0.0707,-0.0942,-0.0123,-0.0485,0.0099,0.0829,-0.0115,-0.1065,-0.0474,-0.0638,0.0856,-0.0412,-0.0310,-0.0528,-0.0271,0.0957,0.0762,-0.0735,0.0041,0.0439,-0.0429,0.0939,-0.0176,0.0755,0.1129,0.0510,0.0493,-0.0187,-0.0184,0.1383,0.0478,-0.0855,0.0420,-0.0900,-0.0188,0.0553,0.0736,-0.0867,0.0601,-0.0651,-0.1043,0.0892,-0.1150,-0.0793,0.0149,-0.0086,0.0044,-0.0279,-0.0224,0.0555,-0.0635,0.0209,-0.0280,-0.0141]];
        const _b1 = [-0.0340,-0.0015,0.0292,0.0179,0.0029,-0.0204,0.1444,-0.0325,-0.0059,-0.0052,-0.0132,0.0246,0.0043,0.0684,0.0354,-0.0183,0.0135,-0.0334,-0.0369,0.0715,0.0167,0.0401,0.0223,0.0051,0.0165,0.0026,0.0026,0.0379,0.0626,0.0269,0.0392,-0.0356,-0.0218,-0.0664,0.0248,0.0237,0.0550,0.0362,0.0159,-0.0229,-0.0148,-0.0270,0.0179,0.0281,-0.0621,0.0249,-0.0382,-0.0066,-0.0385,-0.0060,-0.0478,0.0637,0.0136,0.0301,0.0176,0.0030,0.0924,-0.0276,0.0793,0.0142,-0.0164,0.0266,0.0174,0.0314,0.0046,0.0039,0.0445,0.0285,-0.0914,-0.0182,0.0211,0.0299,0.0045,0.0355,-0.0378,0.0176,0.2045,0.0264,0.0034,0.0375,0.0319,-0.0001,0.0119,-0.0348,0.0377,0.0042,0.0083,0.0421,0.0368,-0.0423,0.0338,0.0082,0.0012,-0.0119,-0.0608,0.0037,-0.0075,-0.0032,0.0144,-0.0183,0.0502,0.0468,-0.0036,0.0250,0.0589,0.0309,0.0091,-0.0010,0.0142,-0.0447,0.0948,0.0197,0.0042,0.0208,0.0106,0.0159,-0.0213,0.0141,0.0130,-0.0180,-0.0084,0.0544,0.0706,0.0166,0.0298,-0.0065,-0.0208,0.0363];
        const _w2 = [[0.0179,-0.0682,0.0019,-0.0872,-0.0268,0.0409,-0.0300,-0.0168,0.0619,0.0099,-0.0744,0.0160,0.1119,-0.0985,-0.0908,-0.0399,0.0963,-0.0942,0.1029,-0.1332,0.0609,0.1209,-0.0801,0.0385,-0.0212,0.0958,0.0210,0.0573,0.0773,-0.0542,-0.0507,0.0401,-0.0416,-0.0165,0.0839,0.0694,0.0318,0.0154,-0.0169,0.0923,0.0475,0.1438,-0.0124,0.0460,-0.0146,-0.0788,0.1022,0.0358,-0.0852,-0.0019,0.1160,0.0090,-0.1133,0.0088,-0.0104,-0.0636,0.0767,-0.1301,0.0221,-0.0571,0.0775,0.0358,0.0784,0.0592,0.0832,0.0933,0.0182,0.0998,0.0256,0.0457,-0.0962,0.0611,-0.0954,0.0163,-0.1080,-0.0017,0.0579,-0.0799,0.0686,-0.0911,0.0650,-0.0055,-0.0543,0.0025,-0.1086,0.0329,-0.1091,0.0086,0.0641,-0.0095,-0.0236,0.0253,0.0376,-0.0896,-0.0954,-0.0241,0.0172,-0.0151,0.0480,-0.0681,-0.0162,0.0860,-0.0334,-0.0170,-0.0443,-0.0277,0.0184,0.0872,-0.0711,0.0759,-0.0572,0.0773,-0.0456,-0.0883,0.1019,-0.0304,0.0181,-0.0057,0.0740,0.0633,-0.0858,0.0169,-0.0656,-0.0030,-0.0248,0.0254,0.0736,-0.0864],[0.0296,0.0896,-0.0226,-0.0530,0.0190,0.0931,0.0021,0.0863,-0.0999,0.0086,0.0995,-0.0656,0.0273,0.0318,-0.0485,-0.0419,0.0442,0.0636,0.0439,0.0878,0.0084,-0.0980,-0.0212,-0.0243,0.0718,-0.0153,-0.0534,0.0906,-0.0115,-0.0078,0.0674,0.0077,-0.0191,-0.0031,-0.0177,0.0514,0.0830,-0.0817,0.0020,-0.0557,-0.1111,0.0607,0.1016,-0.0239,0.0493,0.0710,-0.0739,-0.0512,-0.0017,0.0743,0.0418,0.0045,-0.0291,0.1153,-0.0288,-0.0244,0.0002,-0.1206,0.0116,-0.0038,0.0823,0.0031,0.1151,-0.1041,0.0509,-0.0588,0.0912,0.1138,-0.0472,0.0172,0.0196,0.0405,-0.0861,-0.0277,-0.0295,-0.0766,-0.0780,-0.0887,0.0251,-0.0994,0.0094,-0.0177,-0.0049,-0.0271,-0.1023,-0.0626,-0.0442,-0.0787,-0.0750,0.1295,0.0581,-0.0340,-0.0684,-0.0936,0.1007,-0.0027,-0.0293,0.1100,-0.0191,0.0211,0.0407,-0.0376,-0.0933,-0.0161,-0.0087,0.0812,0.0507,-0.0823,0.0634,-0.0199,0.0368,-0.1009,-0.0182,0.0502,-0.0636,-0.0102,0.1089,-0.0018,0.0238,-0.0473,0.0455,-0.0754,0.0642,0.0912,0.0845,-0.0846,-0.0862,-0.0108],[-0.0637,0.0500,0.0400,0.0131,-0.0050,0.0110,0.0479,0.0207,0.1261,0.0880,0.0692,-0.1225,-0.1161,-0.0059,-0.0246,0.0427,-0.0695,-0.0545,0.1262,0.0520,0.0522,0.0392,-0.0460,-0.0012,-0.0203,0.0595,-0.0067,-0.0379,0.0105,0.0425,-0.0229,-0.0505,-0.0918,0.0353,0.1209,-0.0511,-0.0439,0.0538,-0.1259,0.0391,0.1217,-0.0139,0.0515,0.1057,0.0781,-0.0333,0.0073,-0.0156,0.0514,-0.0969,-0.0532,-0.0635,0.0066,-0.0572,0.1380,-0.0670,-0.0002,-0.0563,0.0766,-0.0385,-0.0280,-0.0302,-0.0933,0.0674,-0.0275,-0.0753,-0.0383,0.0699,0.0848,0.0580,-0.0341,0.1255,-0.1100,0.0332,-0.0504,0.0764,0.0782,0.0073,0.0099,-0.0652,0.0074,-0.0142,-0.0676,0.0552,0.0179,0.1104,0.0845,-0.0207,-0.1428,-0.0637,0.0643,0.0863,-0.0053,-0.0335,-0.0026,-0.0364,-0.0411,-0.0849,0.0525,-0.0252,-0.0343,-0.1051,-0.1349,0.0922,0.0060,-0.0577,-0.1051,0.0734,-0.0581,-0.1091,0.0741,-0.0471,0.0907,-0.0517,0.0064,-0.0814,-0.1059,-0.0023,-0.0041,0.1055,-0.0043,-0.1324,-0.0571,-0.0082,-0.0295,0.0052,-0.0988,0.1016],[0.0444,0.0609,-0.0718,0.0884,-0.0399,0.0772,0.0776,-0.0782,-0.0007,-0.1100,0.0015,0.0580,-0.0758,-0.0347,0.0761,0.0811,0.0392,-0.0846,0.0950,0.0278,0.0405,-0.0908,-0.0758,-0.0225,-0.0847,-0.0768,-0.0838,-0.0654,-0.1293,-0.0415,0.0995,0.0592,-0.0304,-0.0115,-0.0855,0.0616,0.0357,0.0677,-0.0077,-0.0059,-0.0274,-0.0931,0.0424,-0.0727,0.0636,-0.0331,0.0463,0.0293,-0.0327,-0.0776,-0.0966,-0.0874,0.0007,0.0566,-0.1047,0.0764,-0.1121,-0.0615,0.0748,-0.1020,0.0138,0.0539,0.0124,0.0405,-0.0812,0.0847,-0.0186,0.0121,0.0389,0.0566,-0.0543,-0.0396,-0.0384,0.0877,-0.0136,0.0056,-0.1037,0.0164,0.0526,-0.0717,-0.0125,0.0576,-0.0272,0.0097,0.0155,-0.0407,-0.0972,-0.0255,0.0532,-0.0212,0.0779,0.0821,-0.0404,-0.0343,0.0398,-0.0608,-0.0009,0.0226,-0.0473,0.0820,-0.0488,0.0971,-0.0873,-0.0661,0.0640,-0.0872,-0.0756,0.0566,0.0628,0.1003,-0.0232,-0.0215,0.0081,0.0830,0.1092,0.0325,-0.0766,-0.0397,-0.0923,-0.0879,0.0237,-0.0964,0.0880,-0.0494,-0.0764,0.0122,0.0035,-0.0921],[-0.0616,0.0559,0.0581,0.0364,0.1024,0.0286,0.0882,-0.0461,-0.0951,-0.0597,-0.0302,0.0974,0.0153,-0.0560,0.0244,0.0043,-0.0763,0.0853,-0.0151,0.0447,-0.0765,-0.0504,0.0602,-0.0966,0.0201,0.0947,0.0472,0.0125,0.0164,0.0361,0.0986,-0.0040,-0.0856,-0.0507,-0.0409,-0.0351,-0.0837,0.0585,0.0150,-0.0178,-0.0553,-0.0646,-0.0186,-0.1038,-0.0045,0.0056,-0.0650,-0.0871,-0.1419,-0.0441,-0.0087,-0.0020,-0.0019,0.1064,-0.0944,0.0421,0.0426,0.0707,0.0729,-0.0399,0.0354,-0.0457,0.0372,-0.0152,-0.0339,-0.0154,0.0810,-0.1193,-0.0284,0.0095,-0.0040,0.0535,-0.0698,0.1032,0.0815,0.0938,0.1177,0.1097,0.0459,-0.0617,-0.0083,0.0053,0.1041,-0.0983,0.0625,-0.0633,0.0131,0.0154,-0.0170,-0.0418,0.0051,0.0129,0.0752,0.0868,-0.0738,-0.1230,0.0146,-0.0359,-0.0915,0.0495,0.0985,0.1211,0.1552,-0.1209,-0.0657,0.0456,0.0697,-0.0369,-0.0080,0.0709,-0.0848,0.1298,-0.0651,-0.0807,0.1288,-0.0696,0.0317,-0.0267,-0.0841,0.0234,-0.0140,-0.0270,0.0387,-0.0157,-0.0797,0.0171,0.0348,0.0705],[-0.0133,0.0039,0.0008,0.0019,-0.0495,0.0008,0.0880,-0.0487,-0.0636,0.0337,-0.0803,-0.0580,-0.0103,-0.0764,-0.0570,-0.0498,-0.0576,-0.0737,0.0758,-0.0107,0.0265,0.0394,0.0004,0.0294,-0.0651,0.0834,-0.0860,0.0319,-0.0852,-0.0230,0.0440,-0.0961,0.0578,0.0405,-0.0257,0.0371,0.0789,0.0268,0.0750,-0.0005,-0.0278,0.0815,0.0209,0.1069,0.0192,-0.0121,0.0091,0.0316,-0.0642,-0.0825,0.0248,-0.0042,0.0816,0.0209,0.0207,-0.0848,0.0549,-0.0829,0.0423,0.0955,0.0781,0.1116,-0.0020,0.0188,0.0323,-0.0238,0.0288,-0.0558,-0.0824,0.0108,-0.0327,0.0787,0.0524,0.0784,-0.0461,-0.0352,0.0471,0.0685,-0.0735,0.0082,-0.0733,0.0784,0.0449,0.0197,-0.1059,-0.0474,0.1083,-0.0255,0.0997,0.0680,0.0244,0.0366,-0.0653,-0.0190,-0.0888,-0.0129,0.0061,0.0864,0.0351,0.0955,0.0282,-0.0718,0.0405,0.0353,-0.0058,0.0182,0.0311,-0.0405,0.0043,0.0333,-0.0863,-0.0044,-0.0145,0.0654,0.0707,-0.0513,0.0382,-0.0843,0.0483,-0.0860,0.0060,0.0486,0.0686,0.0675,-0.0288,-0.0625,0.0777,0.0054],[-0.0132,-0.0013,0.0555,-0.0034,0.0208,0.0189,-0.0040,-0.0677,0.0172,0.1053,-0.0187,-0.0070,0.0786,0.0204,-0.0115,-0.0567,-0.0350,-0.1153,-0.0641,0.0677,-0.0188,-0.0709,-0.0870,-0.0993,-0.0430,0.0585,0.0806,-0.1119,0.0091,-0.0312,-0.0792,-0.0580,-0.0546,-0.0164,0.0519,-0.0574,0.0918,0.0327,-0.0899,0.0976,-0.0020,-0.0314,0.0262,0.0188,0.0543,-0.0816,0.0029,0.0461,-0.0174,-0.0583,-0.0213,0.1129,-0.0542,-0.0118,0.1018,0.0495,0.1090,-0.0380,-0.1104,0.0814,-0.0816,-0.0500,-0.0239,-0.0909,0.0958,0.0242,-0.0478,0.0361,-0.0753,-0.0241,0.0983,0.0285,-0.0612,0.0348,0.0059,0.0160,-0.0274,0.0705,0.0551,-0.0033,0.0089,-0.0434,-0.0210,0.0058,0.0679,0.0164,-0.1075,0.0174,-0.0340,-0.0744,0.0753,0.0866,0.0848,0.1172,-0.0239,-0.0825,-0.0337,0.1032,0.0406,0.0195,0.0273,-0.0367,0.0984,-0.0410,0.0881,0.0732,-0.0515,-0.0002,-0.0931,0.0960,0.0550,-0.0343,0.0644,-0.0064,-0.0762,-0.0127,0.0750,0.0617,-0.0217,-0.0755,-0.1243,-0.0851,0.0322,-0.0861,-0.0074,0.0574,-0.0206,0.0517],[-0.0809,0.0511,0.0522,-0.0709,0.0719,-0.1144,0.0741,-0.0490,0.0187,0.0673,0.0470,0.0080,0.0529,0.1341,0.0325,-0.0644,0.0962,-0.0231,-0.0867,-0.0621,-0.0222,0.0080,-0.0362,0.0205,-0.0206,-0.0974,-0.0702,0.0483,-0.1354,0.1204,0.0756,0.0772,-0.0672,0.0173,0.0334,0.0351,-0.0838,0.0454,-0.0130,-0.0450,-0.1619,0.0271,-0.0698,-0.0287,0.0192,0.0212,-0.0109,-0.0755,-0.1226,-0.0923,-0.0861,0.0746,-0.1084,0.0493,-0.0952,0.1046,-0.0076,-0.0198,-0.0274,0.1186,0.0213,-0.0021,-0.0613,0.0513,-0.0597,0.1240,0.0306,-0.0077,-0.0311,-0.0192,0.1211,-0.0378,0.0750,-0.0947,-0.0608,-0.1753,-0.0886,0.0721,0.0084,0.1339,0.0917,-0.0707,-0.0475,-0.0058,0.0515,-0.0164,0.1463,-0.0532,-0.0053,-0.0297,-0.1191,0.1097,-0.0874,-0.0762,-0.0322,0.0359,0.0144,-0.0027,0.0922,0.0400,0.0720,0.0350,-0.0093,0.1319,0.0230,0.0032,-0.0327,0.0831,0.0197,-0.0919,0.0510,0.1058,-0.0144,-0.0741,-0.0536,-0.0451,-0.0187,0.0757,0.0163,0.0178,0.0553,-0.0065,-0.0197,0.0152,-0.0223,0.0482,-0.0726,0.1071],[0.0136,-0.0335,0.0754,-0.0367,-0.0092,-0.0594,-0.0836,-0.0862,0.1025,-0.0451,0.0683,0.0170,-0.0500,-0.0115,0.0184,-0.0478,0.0149,-0.0259,0.0384,-0.1022,0.0069,0.1133,0.0133,-0.0409,-0.0918,0.0663,0.0062,0.0356,0.0461,-0.0751,-0.0362,-0.0965,-0.0177,0.0837,-0.0147,0.0834,0.0822,-0.0225,0.0735,0.0814,-0.0713,0.0874,0.0703,0.0848,-0.0275,-0.0258,-0.0151,-0.0736,0.0857,0.0822,0.0393,-0.0796,-0.0320,-0.0773,0.0726,0.0648,-0.0812,-0.0262,-0.0766,-0.0554,-0.0199,0.0005,0.0704,-0.0722,0.0022,-0.1009,0.0947,0.0010,0.0185,-0.1165,0.0298,0.0210,-0.0982,0.0566,-0.0620,-0.0239,0.0484,-0.0235,-0.0890,0.0073,-0.0492,0.0330,-0.0788,-0.0708,-0.0283,-0.0445,-0.0709,-0.0509,0.0041,-0.0951,0.0779,-0.0845,0.0776,-0.0233,-0.0549,-0.0576,0.1307,-0.0274,0.0050,0.0102,0.0773,-0.0447,0.0230,-0.0169,0.0842,-0.0031,-0.0254,-0.1044,-0.0594,-0.0073,-0.0437,0.0644,0.0444,0.0766,-0.0194,-0.0664,-0.0839,-0.0506,-0.1082,0.0231,0.0008,0.0165,-0.0499,-0.0090,-0.0086,-0.0663,-0.0252,-0.0400],[0.0209,0.0654,0.0569,0.0780,-0.0348,0.0101,0.0120,0.0220,-0.0063,0.0025,-0.0492,0.1552,0.0297,0.0201,-0.0718,-0.0195,0.0819,0.0002,-0.0339,-0.0931,0.1387,-0.0227,-0.0029,0.0571,0.0330,-0.0971,0.0126,-0.0590,-0.0311,-0.0721,0.0695,0.0646,-0.0043,-0.1617,0.0431,0.0375,-0.0344,0.0674,0.0759,-0.0851,-0.0235,-0.0822,0.0919,0.0066,-0.0995,-0.1468,0.0171,-0.0534,0.0627,-0.0407,0.1022,0.0637,-0.0798,-0.0343,-0.1028,0.0212,0.1104,0.1050,-0.0595,-0.0082,-0.0886,0.0562,-0.0698,0.0830,-0.0197,0.1516,0.0338,-0.0034,0.1123,0.0322,-0.0404,0.0567,-0.0085,0.0397,0.0575,-0.0015,0.0878,-0.0475,0.0175,0.0560,0.0069,0.0295,-0.0603,-0.0811,-0.0396,0.0775,-0.0669,-0.0298,0.0225,-0.0279,0.0109,-0.0155,-0.0795,0.0316,-0.0571,-0.0077,0.0346,0.1057,-0.0847,0.0307,-0.1112,-0.0332,-0.1004,0.0233,0.1328,0.0666,-0.0076,0.1083,-0.0184,-0.0394,-0.0053,0.1210,0.0570,0.0626,0.0134,0.0191,0.0176,0.0155,-0.0954,0.0184,-0.0549,0.0850,-0.0862,0.0537,-0.0414,0.0480,0.0999,-0.0003],[-0.0687,0.1246,-0.1103,0.0680,0.1112,-0.1237,0.0651,-0.0334,0.0167,-0.0546,-0.0950,0.0837,-0.0726,0.0730,-0.0320,-0.0877,0.1771,0.0722,0.0315,0.0479,0.0353,-0.0514,0.1543,-0.0450,-0.1413,-0.1579,-0.0524,-0.0093,-0.0703,0.0345,0.0561,0.0421,0.0707,0.0525,0.1113,0.0709,-0.1074,-0.0003,0.0931,-0.0263,-0.0170,-0.0427,0.0629,-0.0032,0.0806,-0.0815,-0.0129,0.1013,0.0354,-0.0541,-0.0505,-0.1236,-0.0732,0.0960,-0.0974,-0.0778,0.0853,-0.1216,-0.0234,-0.0943,-0.0915,-0.0731,0.0049,-0.0589,-0.1557,-0.0403,-0.0448,0.1166,-0.1166,0.0890,-0.1253,0.0590,0.0121,0.0921,0.0505,0.0917,0.0965,0.1035,-0.0567,-0.0224,-0.0560,0.0378,0.1206,-0.1039,0.1238,0.0765,-0.0917,-0.1143,-0.0469,-0.0770,-0.0892,-0.0840,-0.0418,0.0910,0.1206,0.1325,-0.1807,-0.0054,0.0123,-0.0005,-0.1197,0.0156,0.0097,-0.1114,0.0222,-0.0004,0.0262,0.0296,-0.0353,0.0788,0.0468,0.1291,-0.1056,-0.0438,-0.0059,-0.1134,0.0297,-0.0722,-0.0216,-0.0481,0.0204,-0.0600,-0.1485,0.0327,-0.0015,-0.0102,-0.0112,0.0322],[0.0299,0.0073,0.0923,-0.0860,0.0542,-0.1029,-0.0096,0.0672,-0.0747,-0.0565,-0.0942,0.0637,0.0831,0.0841,-0.0319,0.0052,0.1002,-0.0149,0.0641,0.0834,0.0856,-0.0380,0.0745,0.0299,0.1007,-0.0016,0.0854,0.0512,0.0192,-0.0867,0.0818,0.0100,-0.0364,0.0429,0.0640,-0.0723,-0.0583,-0.0429,0.0866,0.0725,0.0340,0.0784,0.1021,0.0084,0.0724,0.0271,0.0780,0.0814,0.0218,0.0166,-0.0490,-0.0009,0.0780,0.0593,0.0786,-0.0203,-0.0325,0.0225,0.0377,-0.0738,-0.0484,0.0700,0.0112,-0.0611,-0.0864,-0.0795,-0.0484,0.0666,-0.1107,0.0755,0.0335,0.0580,0.0293,-0.0626,-0.0112,-0.0364,-0.0315,0.0310,-0.0196,0.0891,0.0871,0.0767,0.0619,0.0202,-0.0799,-0.0648,0.0703,0.0119,-0.0449,-0.0425,-0.0608,0.0939,-0.0036,0.0135,-0.0725,0.0614,0.0989,0.0860,-0.0664,0.0020,-0.0953,-0.0746,0.0104,0.0333,0.0496,0.0357,0.0826,0.0221,-0.0764,0.0163,-0.0236,-0.0164,0.0184,0.0568,-0.0764,-0.0894,0.0649,0.0835,-0.0119,-0.0484,-0.0057,0.0678,-0.0585,0.0255,0.0584,0.0965,0.0174,0.0679],[-0.1170,0.0854,0.0392,0.0293,-0.0737,-0.0123,-0.1242,-0.1212,-0.1137,0.0172,-0.0242,0.0474,-0.0873,0.0848,-0.0383,-0.0375,0.0732,0.0672,-0.1206,0.0588,0.0284,0.0366,-0.0705,0.0910,0.0362,0.0523,0.0467,-0.0208,0.0309,-0.0318,-0.0893,0.0001,-0.0915,0.0311,-0.0402,0.0937,-0.0435,-0.0690,0.0520,0.0965,0.0138,-0.0432,-0.0686,0.1184,0.0996,0.1042,0.1313,0.0136,0.1185,0.0424,-0.0406,0.0585,0.0206,0.0613,0.1059,0.0392,-0.0423,-0.0560,-0.0178,-0.0423,0.0733,-0.0071,0.0566,0.0545,0.0391,0.0083,-0.1189,-0.0559,-0.0290,-0.0196,0.0523,0.0186,-0.0755,-0.1057,0.0266,0.0447,0.0203,0.0037,0.0474,-0.0499,0.0073,0.1062,-0.0132,0.1125,-0.0229,0.0851,0.0672,0.0196,0.0035,-0.0159,-0.0044,-0.0960,-0.0433,0.0233,0.0850,0.0823,-0.0622,-0.0527,0.1059,0.0691,-0.0453,0.0586,0.1124,0.0022,-0.0642,0.0818,0.0827,-0.0282,-0.0772,0.1118,-0.0266,0.0207,-0.0214,0.1149,0.1129,0.0598,0.0162,0.0491,-0.1318,-0.0566,0.0284,0.0919,0.1206,0.0334,-0.0333,-0.1334,-0.0473,-0.0531],[0.0768,-0.0435,-0.0323,-0.0358,0.0267,0.0416,-0.0535,0.0322,-0.0621,0.0635,0.0003,0.0247,-0.0592,-0.0500,-0.1214,-0.0883,0.1008,0.0970,0.0475,-0.0480,0.0484,0.0327,0.0230,-0.0047,0.1023,-0.0375,0.0065,0.0563,0.0316,-0.0985,-0.0630,-0.0762,-0.0372,0.0525,0.0086,0.0318,0.0811,0.0090,-0.0306,-0.0245,0.0393,0.0154,-0.0285,0.0738,-0.0633,-0.0801,-0.0445,0.0922,0.0845,0.0211,0.0158,-0.0493,-0.0109,-0.0214,0.0709,-0.0840,-0.1284,-0.0185,-0.0120,0.0066,0.0806,0.0123,-0.0464,0.0446,-0.0334,0.1069,0.0740,0.0470,-0.0922,-0.1121,0.0871,0.0088,-0.0016,-0.0850,0.0181,0.0751,-0.1221,-0.1039,0.0601,-0.0976,-0.0271,0.0547,-0.0614,0.0701,-0.0414,0.0095,0.0386,-0.0026,0.0090,0.0987,-0.0151,0.0574,-0.1129,0.0622,0.0478,0.0076,-0.0402,-0.0066,-0.0600,-0.0241,-0.1024,-0.0511,0.0191,-0.0118,-0.0759,0.0586,0.0976,-0.0127,0.1273,0.1247,-0.0125,0.0158,-0.0948,-0.0197,0.1032,0.0021,0.0830,-0.0543,0.0194,0.0361,-0.0520,0.0427,0.0680,-0.0625,-0.0006,-0.0766,-0.0201,0.0854],[-0.0085,0.0180,0.0152,-0.0711,-0.0439,0.0827,-0.0873,-0.0040,0.0664,-0.0016,0.1031,0.0208,0.0901,-0.0359,0.0731,-0.1056,0.0532,0.0580,-0.0694,-0.0509,0.0825,0.0934,0.0836,-0.0003,-0.0976,0.0347,-0.0910,0.0381,-0.0771,0.0865,-0.0156,-0.0522,-0.0590,-0.0472,-0.0747,-0.0217,-0.0348,0.0105,0.0138,-0.0169,-0.0199,0.0367,0.0519,0.0228,0.0834,-0.0424,-0.0562,0.1070,0.0185,-0.0154,-0.1086,-0.0739,0.0634,0.0563,0.0340,-0.0806,0.0716,0.0319,0.0959,-0.0048,-0.1164,0.0480,0.0516,0.1009,-0.0305,0.0836,0.0839,0.0447,-0.1371,-0.1062,0.0447,0.0915,-0.0819,0.0737,-0.0080,0.0176,0.0822,0.0233,-0.0593,0.0211,-0.1048,0.0310,0.0438,0.0724,-0.0083,-0.0365,-0.0465,0.0767,0.0865,-0.1205,0.0490,-0.0609,-0.0765,0.0794,0.0062,-0.0104,0.1064,-0.0315,0.0139,-0.0429,0.0990,0.1007,-0.0142,0.0868,0.0074,-0.0045,0.0315,0.0077,0.1075,-0.0527,0.1020,0.0370,0.0840,-0.0473,-0.0941,-0.0087,0.1231,-0.0191,0.0498,0.0614,0.0912,-0.0398,-0.0440,-0.0208,-0.0509,0.0229,-0.0232,0.0436],[-0.0134,-0.0155,-0.0301,0.0768,-0.0352,-0.0056,-0.0072,-0.0856,-0.0936,-0.0591,0.0320,-0.0796,0.0822,0.0046,0.0759,-0.0752,0.0649,-0.0362,0.0677,-0.0157,-0.0212,-0.0855,-0.0344,-0.0537,-0.0171,0.0444,0.0329,0.0990,0.0971,0.0371,0.1001,0.0130,0.0348,0.0179,0.0044,-0.0405,0.0533,-0.0342,0.0989,-0.0676,0.0402,0.0945,-0.0066,-0.0925,0.0187,0.0324,-0.0979,-0.0858,0.0819,0.0769,0.0315,-0.0882,0.0146,0.0228,-0.1007,-0.0638,-0.0175,-0.0649,0.0157,0.0241,0.0910,0.0944,-0.0016,-0.0253,0.0557,-0.0682,0.0138,0.0947,0.0484,0.0771,-0.0217,-0.0841,0.0439,-0.0446,-0.0505,0.0736,-0.0278,0.0276,-0.0762,0.0370,0.0228,-0.0592,-0.0034,-0.0552,0.0027,-0.0285,-0.0266,-0.0433,0.0011,-0.0488,0.0932,-0.0894,0.0265,0.0938,0.0692,0.0534,-0.0884,0.0138,0.0907,-0.0724,0.1028,0.0344,-0.0018,-0.0655,0.0044,-0.0291,0.0854,-0.0624,0.0871,-0.0330,0.0502,-0.1015,-0.0277,0.0327,-0.0714,-0.0634,-0.0467,-0.0090,0.0509,-0.0640,0.0526,-0.0172,0.1010,-0.0551,-0.0449,-0.0009,0.0268,0.1053],[0.1196,0.0563,0.0600,-0.0211,0.0228,-0.0578,-0.0468,0.1393,0.0566,-0.0476,-0.0689,-0.0348,0.0740,0.1335,-0.1020,0.0108,-0.0032,0.0024,-0.1214,-0.0704,0.0579,-0.1300,-0.0571,-0.1130,0.0941,-0.0459,0.0475,-0.0325,-0.0441,-0.0921,0.0166,0.0077,0.0766,0.0420,-0.0156,0.0742,0.0442,-0.0117,0.0278,0.1130,0.0071,-0.0952,-0.1062,-0.0460,0.0454,-0.0242,0.1017,-0.0121,0.0515,-0.0830,0.0622,-0.1706,-0.1266,0.0973,0.0231,-0.0460,-0.0563,-0.1609,0.0244,-0.0042,-0.0383,-0.0357,-0.0955,-0.0203,-0.1165,0.1419,0.0146,-0.0389,0.0674,-0.0280,-0.0215,0.1002,-0.0126,0.0628,0.1082,0.0311,-0.2414,-0.1203,-0.0791,-0.0229,-0.0279,0.1633,-0.0567,0.0224,-0.0252,0.0425,-0.1156,-0.0293,-0.0989,0.0035,-0.0542,-0.0163,0.0644,-0.1538,0.0154,-0.0511,0.1453,0.0805,0.0484,0.0718,-0.0414,-0.0672,0.0501,0.0242,-0.0662,0.1452,0.1169,0.0856,-0.1262,-0.0453,0.1253,0.0390,-0.0431,-0.1367,0.0155,0.0989,-0.0121,-0.0621,0.0584,-0.0864,0.0824,-0.0627,0.0000,0.0864,0.0406,-0.0935,-0.0839,-0.0834],[-0.0288,-0.0785,-0.0542,0.0300,0.0344,0.0203,-0.0311,0.1190,0.0856,-0.0362,0.0374,-0.0498,-0.0361,-0.0872,0.0731,-0.0822,0.1194,0.1316,0.0250,-0.0981,-0.0231,-0.0738,-0.1017,-0.0432,-0.0199,0.0141,-0.0231,0.0526,0.0504,-0.0271,-0.0508,-0.0216,-0.0452,0.1336,-0.0722,-0.0168,0.0475,-0.0330,0.0562,-0.0117,-0.0054,0.0436,0.0627,-0.0958,0.0606,0.0820,0.0858,-0.1103,-0.0007,0.0725,0.1274,-0.0717,-0.0340,0.0622,0.0904,0.0557,-0.0459,-0.1219,0.0085,0.0500,-0.1128,0.0196,-0.0038,-0.0147,-0.0534,0.0531,0.1135,0.0432,0.0181,0.0266,0.0803,0.0879,0.0385,-0.0658,-0.0543,-0.0955,-0.1120,-0.0547,0.0089,0.0284,-0.0957,-0.0227,0.0918,0.0131,0.0239,0.0397,-0.0082,-0.0312,-0.0939,0.0049,-0.0598,0.0132,-0.0028,-0.0040,-0.0031,0.0209,-0.0449,0.0379,0.0582,0.0646,-0.0273,-0.0413,0.0331,-0.0707,-0.0274,0.1102,0.0905,0.0647,0.0950,0.0033,0.0183,-0.0439,-0.0632,-0.0871,0.0728,-0.0160,-0.0571,-0.0033,0.0783,-0.0703,-0.0083,0.0425,-0.0792,0.0245,-0.1366,0.0760,0.0767,-0.0983],[0.0954,0.0616,0.0182,0.0912,0.0471,0.0803,0.0547,0.0762,0.0525,-0.0471,0.0417,0.0156,0.0567,0.0160,-0.0630,0.0732,0.0417,0.0011,-0.0484,0.0731,0.0448,0.0881,-0.0053,0.0121,0.0755,-0.0751,0.0926,-0.0613,0.0329,0.0060,0.0500,0.0394,0.0333,-0.0698,-0.0679,-0.0434,0.0436,0.0062,0.0639,0.0192,-0.0106,0.0268,-0.0668,0.0574,-0.0880,0.0487,0.0800,0.0685,-0.0890,-0.0923,0.0724,-0.0658,0.0933,0.0668,-0.0468,0.0121,-0.0590,-0.0101,-0.0032,0.0448,0.0837,-0.0310,0.0303,0.0659,0.0638,-0.0639,0.0918,0.0559,0.0736,-0.0798,-0.0253,-0.0000,-0.0509,-0.1086,-0.0545,-0.0367,-0.0541,0.0624,0.0188,-0.0652,0.0480,0.0161,0.0227,-0.0892,0.1020,0.0888,0.0607,0.0954,-0.1009,0.0159,-0.0046,0.0869,0.0098,0.0921,-0.0069,-0.0948,0.0149,0.0128,-0.0837,-0.0626,-0.1063,0.0274,-0.0648,-0.0375,0.0446,-0.0493,0.0086,0.0490,0.0047,-0.0345,0.0817,0.0967,-0.0381,0.0647,0.0415,0.0710,-0.0023,0.0612,-0.0167,-0.0876,0.0033,0.0544,-0.0047,-0.0663,0.0842,-0.0476,0.0520,0.0449],[0.0274,0.0716,0.0479,-0.0594,-0.0615,0.0534,-0.0853,-0.0821,-0.0083,-0.0807,-0.0343,0.0731,0.0150,-0.0850,-0.0610,0.0269,0.0529,0.0051,-0.0710,0.0788,0.0776,0.0194,0.0409,-0.0155,0.1271,-0.0647,0.0115,0.0198,0.0490,-0.1006,0.0701,0.0194,-0.1341,-0.0121,0.0146,-0.0804,0.0088,0.0424,0.1128,-0.0576,-0.0390,-0.0945,-0.0168,-0.0465,0.0445,0.0522,-0.0487,0.0299,0.0694,0.0853,-0.1026,0.0116,-0.0405,-0.0669,0.0339,-0.0084,0.0218,0.0796,-0.0903,-0.0321,-0.0443,0.0653,0.0934,0.0122,-0.0260,0.1521,0.1384,0.0442,0.1262,0.0695,-0.0172,0.0861,0.0251,-0.0474,-0.0972,-0.0813,0.0353,-0.0073,-0.0625,-0.1154,-0.0517,-0.1145,0.0375,0.0486,-0.0790,0.0009,-0.0434,-0.0294,-0.0148,-0.0598,0.0327,0.0832,0.0784,-0.0785,0.0934,-0.1056,0.1362,-0.0552,0.0744,0.0101,-0.0377,-0.0740,0.0211,0.0196,-0.0782,0.0638,-0.0463,-0.0672,0.0118,0.1136,0.0357,-0.0076,0.0496,0.0677,-0.0642,0.0606,-0.0837,-0.0292,-0.0314,-0.0864,-0.0517,0.0274,0.0679,-0.0049,0.0149,-0.0102,0.1241,-0.0438],[0.1225,-0.0477,0.0450,-0.1068,0.0522,-0.0088,0.0537,0.0187,0.0393,0.0230,-0.0376,-0.1433,-0.0490,-0.0052,0.0011,0.0989,-0.1736,0.0695,-0.0294,-0.0664,-0.0149,-0.0436,-0.0962,-0.0175,0.0862,0.0933,-0.0864,-0.0413,-0.0623,0.0857,0.0489,-0.0333,-0.0872,0.0716,-0.0059,-0.0126,-0.1404,-0.0650,-0.0873,0.0660,0.0263,0.0087,0.0511,-0.0038,0.0310,0.0455,-0.0718,0.0157,0.1225,0.0110,-0.0122,0.0497,-0.0630,0.0171,-0.0015,0.0494,-0.0708,-0.0073,-0.0622,-0.0874,0.0999,0.0551,-0.0539,-0.0610,-0.0037,0.1096,0.0354,0.0742,-0.0276,0.0685,0.0137,0.0521,-0.0525,0.0320,0.0394,0.0676,0.1012,-0.0629,0.0014,-0.0106,-0.0164,-0.1086,0.0070,0.0032,0.0181,0.0883,0.0306,0.0957,-0.1206,0.0299,-0.0110,0.0192,-0.0935,0.0573,-0.1341,-0.1222,0.1118,-0.0124,0.0296,-0.0310,0.0256,0.1186,-0.0025,-0.0444,0.1139,-0.0901,-0.0957,-0.0254,-0.0808,0.0364,-0.0278,-0.0865,0.0823,-0.0053,-0.0760,0.0189,-0.0805,0.1697,0.1227,0.0179,-0.1183,-0.0961,0.0637,-0.0093,0.0898,0.1171,0.0128,0.0275],[0.0300,-0.0768,0.0224,-0.0285,-0.0821,0.0302,0.0004,0.0330,-0.0486,-0.0662,0.0649,-0.0709,-0.0651,-0.0177,0.0491,-0.0972,0.0313,0.0841,-0.0866,0.0144,0.0427,-0.0147,0.1159,-0.1021,0.0889,-0.0938,-0.0248,0.1090,0.0242,-0.0054,0.0570,-0.0459,-0.0915,-0.0714,0.0740,0.0017,-0.0348,0.0752,0.0279,0.0289,-0.0673,-0.0888,-0.0550,-0.0549,-0.0489,0.0439,-0.0085,0.0045,-0.0474,0.0050,0.0902,0.0127,0.0343,0.0187,-0.0415,0.0283,-0.0131,0.0044,-0.0893,-0.0357,-0.0243,0.0046,-0.0955,0.0822,-0.0560,-0.0461,0.0890,-0.0513,0.0500,0.0653,-0.0346,-0.0160,0.0250,0.0473,0.0771,-0.0564,0.0243,0.0213,-0.0441,-0.0815,-0.0720,0.0786,0.0021,-0.0289,-0.1002,-0.0773,0.0021,0.0518,-0.0736,0.0784,-0.0695,0.0606,0.0104,0.0240,0.0530,0.0062,-0.0266,-0.1008,0.0864,-0.0234,-0.0677,0.0537,-0.0362,-0.0765,0.0638,0.0806,0.0253,-0.0030,0.0042,-0.0369,-0.0966,-0.0255,0.0763,-0.1129,-0.0570,0.0502,-0.0625,0.0362,-0.0881,-0.0860,-0.0710,-0.0066,0.0162,0.0702,-0.0264,-0.0328,0.0907,0.0816],[-0.0681,0.0186,0.0839,0.0051,-0.0377,-0.0064,-0.0682,-0.0862,0.0210,-0.0730,-0.1356,-0.0029,-0.0715,0.1454,-0.0167,0.0675,0.0984,0.0222,-0.0529,0.1004,0.0771,-0.0736,0.0580,-0.0658,0.0381,-0.0628,0.0829,0.1029,-0.0330,-0.0966,0.0912,-0.0901,-0.0989,-0.0541,0.0254,-0.0854,0.0707,0.0878,-0.0924,-0.0325,0.0095,0.0841,-0.0643,-0.0109,0.0708,-0.1086,-0.0564,0.0233,0.0163,-0.0625,0.1741,0.1060,-0.0713,-0.0359,-0.0414,0.0436,-0.0383,-0.0027,-0.0374,0.1147,-0.0967,0.0686,0.0021,-0.0802,-0.0493,0.1270,-0.0568,0.0307,0.1244,-0.0477,0.0016,-0.0242,0.0025,-0.0994,0.0757,0.0201,0.0805,-0.0780,0.0180,0.0344,0.1199,-0.0147,0.0282,-0.1203,0.0364,-0.0250,-0.1529,-0.0972,-0.1135,0.0669,-0.0003,-0.0492,0.0288,0.0845,0.0189,-0.0023,-0.1840,0.0968,0.0654,0.0614,0.0112,-0.0578,-0.0993,-0.0589,0.0742,-0.0509,0.0430,0.0545,-0.0156,-0.0690,0.0856,0.0424,0.0754,0.1088,0.0840,-0.0019,-0.0009,-0.0859,-0.0739,-0.0620,-0.0866,0.0025,0.0297,-0.1438,0.0800,-0.0053,-0.0683,0.0278],[-0.0673,-0.0743,-0.0829,-0.0788,-0.0103,-0.0241,0.0222,0.0761,0.0512,0.1048,-0.0237,-0.0039,0.0225,0.0847,0.0104,0.0995,0.1002,0.0707,0.0987,-0.0469,0.0148,-0.0071,-0.0319,-0.0438,0.0991,-0.0098,0.1045,0.0751,0.0491,0.0318,-0.0157,0.0892,0.0532,-0.0929,0.0033,0.0074,-0.1152,0.0711,0.0721,0.0773,-0.0415,0.0784,-0.0259,0.0156,0.0755,-0.0451,-0.1034,-0.0838,-0.0455,0.0040,0.0395,0.0034,-0.0228,0.0039,0.0564,-0.0231,-0.0869,-0.0465,0.0218,-0.0227,-0.0752,-0.0830,-0.0673,0.0767,0.0771,-0.0298,-0.0614,0.0021,-0.0327,0.0734,-0.0224,0.0875,0.0644,-0.0131,-0.0737,0.0156,-0.0363,-0.0098,-0.0212,0.0016,0.1005,-0.0537,-0.0143,0.0232,-0.0114,0.0640,-0.0221,-0.0910,-0.0965,-0.0164,0.0121,0.1176,0.0922,-0.0688,-0.0409,-0.0704,0.0524,0.0320,-0.1022,0.0542,0.0796,-0.0949,0.0114,-0.0833,-0.0666,0.0005,0.0633,0.0860,0.0799,-0.0569,-0.0509,-0.0495,-0.0236,0.0227,0.0907,-0.0557,0.0414,-0.0756,0.0298,-0.0355,0.0947,-0.0845,0.0688,-0.0666,0.0433,-0.0326,-0.0885,0.0206],[-0.0054,0.0631,0.0640,-0.0848,-0.0858,-0.0737,0.0022,-0.1061,0.1443,0.0047,0.0072,0.0434,0.0364,-0.0723,-0.0857,-0.0596,0.0318,0.0127,0.0726,0.0650,0.1002,0.0298,0.1131,0.0068,0.0548,0.0748,0.0317,-0.1098,-0.0000,0.0172,-0.0995,-0.0838,-0.0732,0.0077,-0.0738,0.0677,-0.0862,0.0916,0.0836,-0.0065,0.0489,0.0597,0.1064,0.0739,-0.1006,-0.0213,0.0646,-0.0557,-0.0714,-0.0923,0.0083,-0.0768,-0.0109,-0.0810,-0.0132,0.0910,0.0757,-0.0126,-0.1204,0.0149,0.0517,0.0312,-0.1008,-0.1167,0.0009,0.1226,0.0252,0.0776,-0.1092,0.0592,0.0920,0.0166,0.1044,-0.0485,-0.0905,0.1454,-0.0428,0.0383,0.0946,-0.0015,0.0331,-0.0993,-0.0251,-0.0140,-0.0906,-0.0909,0.0225,0.0671,0.0305,-0.0411,-0.0850,-0.0665,-0.0887,-0.0567,0.0491,-0.0502,0.1111,0.0609,-0.0441,-0.0883,-0.0210,-0.0163,-0.1099,0.0380,0.0466,-0.0644,-0.0550,0.0562,0.0108,-0.0340,-0.0500,0.0510,-0.0570,0.0139,-0.0890,0.0153,-0.0939,0.1125,-0.0867,-0.0670,-0.0329,0.0443,0.0746,0.1112,0.0786,-0.0848,-0.0059,-0.0903],[-0.0912,0.0571,0.0536,-0.0477,0.0319,0.0801,-0.0675,0.0833,-0.0260,-0.0927,-0.0243,-0.0882,0.0310,-0.0572,0.0320,0.0942,-0.0566,0.0303,0.0896,0.0053,0.0977,-0.0876,-0.0444,0.0608,-0.0863,-0.0556,-0.0360,0.0113,0.0133,0.0444,0.0511,-0.0483,-0.0755,-0.0517,-0.0002,0.0328,-0.0957,-0.0577,-0.0685,-0.0850,-0.0581,0.0638,-0.0369,0.0413,-0.0637,-0.0320,0.0330,-0.0965,0.0901,-0.0651,0.0366,-0.0252,-0.0592,0.0256,-0.0727,-0.0716,-0.0559,0.0792,-0.0264,0.0724,-0.0640,0.0747,0.0054,-0.0922,-0.0269,0.0412,-0.0003,0.0900,-0.0028,-0.0817,0.0318,-0.0243,0.0686,-0.0502,-0.0239,-0.0073,0.0813,0.0901,0.0560,-0.0585,-0.0150,0.0828,0.0151,0.0155,0.0170,-0.0455,0.0502,-0.0280,-0.0390,0.0172,-0.0399,-0.1006,-0.0435,-0.0082,0.0965,-0.0767,-0.0478,0.0697,0.0909,0.0952,-0.0076,0.0410,-0.0166,-0.0528,-0.0941,-0.0303,0.0315,-0.0153,0.0068,0.0380,0.0665,-0.0626,-0.0796,0.0691,-0.0048,0.0350,0.0985,0.0294,0.0515,0.0305,0.0874,0.0325,-0.0312,-0.0982,-0.0375,-0.0076,-0.0019,0.0959],[-0.0189,-0.0508,0.0134,0.0600,-0.0536,0.1898,-0.0981,-0.0906,0.0196,0.0147,0.0515,-0.0865,-0.0096,-0.0346,-0.0301,-0.0098,0.0410,0.0579,0.0109,-0.0013,0.0335,0.0647,-0.1209,0.0787,-0.0058,0.0307,0.0488,0.0308,-0.0677,-0.0078,0.0666,0.0337,-0.0596,-0.1327,0.0683,0.0170,-0.0353,0.0475,0.0866,0.0719,0.0744,-0.1013,-0.0483,-0.0591,0.0045,-0.0666,0.0372,0.0735,-0.0522,-0.0301,0.0696,0.0130,-0.0962,-0.0916,-0.0400,0.0489,0.0050,-0.1042,-0.1136,0.1029,0.1017,-0.0556,0.0926,0.0648,0.0602,-0.0749,-0.1160,-0.0967,0.0130,0.1135,0.0173,0.0936,0.0265,-0.1417,0.0121,-0.0018,0.0556,-0.1375,-0.0454,-0.0497,0.0469,-0.0431,0.1196,0.0234,0.0449,-0.1079,-0.0067,-0.0852,-0.1396,0.1104,0.0233,-0.0735,-0.0121,0.0103,-0.0066,0.0690,-0.0301,0.0039,0.0813,0.0254,-0.0286,0.0050,-0.0294,-0.1275,0.0902,0.0411,-0.0565,0.0129,0.0878,0.1672,-0.1174,0.0642,-0.0763,-0.0842,0.0882,-0.0840,-0.0687,-0.0403,-0.0374,0.0216,-0.0948,0.0252,0.0579,0.1077,-0.0037,-0.1568,-0.0647,0.1274],[-0.0939,-0.0149,-0.1012,0.0621,0.0631,-0.1387,-0.0217,0.1620,0.0098,-0.0190,0.0326,0.0371,0.0557,0.0768,-0.0448,-0.0270,-0.1254,-0.0033,-0.1237,-0.0804,0.0269,-0.0092,0.1103,0.0326,0.0042,0.1505,-0.0970,0.0371,0.0943,-0.0813,-0.1166,-0.1589,-0.0150,0.1527,0.0501,-0.0030,-0.0479,-0.0202,-0.0201,0.1169,0.1257,0.0093,-0.1223,0.0049,0.0530,-0.0405,0.0375,-0.0967,0.0151,0.0775,-0.2220,-0.1938,0.0456,0.0881,-0.0043,0.1165,-0.3272,0.0332,0.0523,0.1256,-0.0557,0.0301,-0.0342,-0.0561,0.0993,0.1423,0.0132,0.0039,-0.1206,-0.1031,-0.0591,-0.0857,-0.0639,0.0468,0.1218,0.0370,-0.1688,0.0598,-0.0494,-0.0324,-0.0828,0.0015,-0.0106,0.1011,-0.0398,0.1420,-0.1002,-0.0950,0.0614,-0.0199,-0.0034,0.0402,-0.0694,-0.0068,-0.0386,-0.1153,0.0496,-0.0204,0.0467,0.0652,0.0758,-0.0430,-0.0105,-0.0841,-0.0995,0.0565,-0.0946,-0.0837,-0.0764,-0.0233,-0.0385,-0.0013,0.0693,-0.0255,-0.0409,0.0915,0.0567,0.0417,0.1190,-0.0397,0.0062,-0.1165,-0.1130,0.1083,-0.1211,0.0193,-0.0037,-0.0284],[-0.0537,0.0186,-0.0073,0.0233,-0.0365,-0.0856,0.0938,-0.0730,0.0917,-0.0736,-0.0632,0.0859,0.0184,0.0974,0.0868,0.0685,0.0725,0.0445,-0.0384,0.0430,0.0800,-0.0343,0.0234,0.1006,0.0403,0.0058,-0.0231,-0.0132,-0.0389,0.0382,0.0380,0.0268,0.0494,0.0495,0.0270,-0.0393,0.0267,0.0727,0.0415,0.0156,0.0180,-0.0801,0.0263,-0.0729,-0.0387,-0.0667,-0.0669,-0.0274,-0.0742,0.0644,-0.0778,0.0841,0.0940,0.1143,-0.0162,-0.0629,-0.0317,0.0953,-0.0363,0.0489,-0.0465,0.0022,-0.0223,0.0799,0.0074,-0.0571,-0.0010,0.0424,-0.0619,0.0777,-0.0344,-0.0529,0.0005,0.0493,0.0187,-0.0613,0.0196,-0.0106,0.0630,-0.0090,-0.0277,0.1603,0.1083,0.0098,-0.0940,-0.0711,-0.0508,-0.0281,-0.0200,0.0772,0.0908,-0.0583,-0.0447,0.0453,-0.1259,0.0378,0.1414,0.0884,0.0081,-0.0541,0.0179,0.0674,0.0564,-0.0324,0.0035,-0.1173,-0.0085,-0.0529,0.0722,-0.1001,0.0621,-0.0077,-0.0914,0.0145,0.0454,0.0398,0.0361,-0.0636,0.0340,0.1372,-0.0161,0.0073,0.0202,0.0649,-0.0680,0.0758,0.0248,-0.0069],[0.1202,0.0047,0.0625,-0.0942,-0.0083,-0.1178,-0.0480,0.0010,0.0153,0.0755,-0.0762,-0.0228,-0.0336,-0.0757,0.0063,0.0076,-0.0055,-0.0122,0.0502,0.0681,-0.0635,-0.0259,-0.0500,0.1193,-0.0170,-0.0307,0.0515,-0.0097,-0.0158,0.0612,0.0259,-0.0166,0.0748,0.0043,0.0922,0.0396,0.0397,0.0495,0.0145,0.0686,0.0438,-0.0833,-0.0727,-0.0285,-0.1013,-0.0211,0.1183,-0.1371,0.0832,-0.0549,0.0909,-0.0015,0.0816,0.0487,-0.0510,-0.0859,-0.0098,0.0368,0.0822,-0.0156,0.0523,-0.1016,0.0502,0.0018,0.0896,-0.0931,0.0754,0.0751,0.0983,0.0288,0.0166,0.0172,-0.0376,-0.0650,-0.0265,-0.0549,-0.0880,0.0873,-0.0650,-0.1073,-0.0153,0.0560,-0.0524,0.0499,-0.1143,0.0190,0.0282,0.1034,0.1010,-0.0232,0.0970,-0.1213,-0.1038,0.0388,0.1241,0.0853,0.0289,0.0489,-0.0857,-0.0349,0.0840,-0.0909,-0.0018,0.0052,-0.0924,-0.0913,0.0720,-0.0593,0.0755,0.0004,-0.1005,0.0273,-0.0541,0.0120,0.0147,-0.0965,-0.0760,-0.0200,-0.0963,-0.0620,0.0012,0.1052,-0.0047,0.0434,0.0141,-0.0943,0.0213,-0.0091],[-0.0410,0.0721,0.0170,0.0026,0.0125,0.0311,-0.0197,0.1078,-0.0302,0.1519,0.0573,-0.0567,-0.0318,0.1632,-0.0799,-0.1198,-0.0261,0.0038,-0.0910,0.0299,-0.0443,0.0317,-0.0139,-0.0765,-0.0210,0.0728,-0.1353,-0.0244,0.0670,0.1143,0.0262,-0.0540,0.0118,0.0495,0.1023,0.1566,0.1091,-0.1263,0.0959,0.0590,0.0401,-0.0269,-0.0296,0.0149,0.0728,0.0907,0.1101,0.0444,0.0130,0.1113,-0.1533,-0.0442,0.0587,-0.0747,-0.0258,0.0843,-0.1588,-0.1696,0.1482,-0.0333,-0.0956,0.0890,-0.1415,0.1355,0.0670,0.0326,-0.1166,0.0523,-0.2408,0.0466,0.0412,-0.0965,-0.0751,0.0901,0.1112,-0.0654,-0.1198,0.0475,0.0498,0.0349,-0.0487,0.0386,0.0230,0.0780,-0.0621,0.0649,0.0586,0.0671,-0.0227,0.1162,-0.0946,-0.0217,0.0068,-0.0354,0.0174,0.0122,0.1014,-0.0239,-0.1314,0.0263,0.0530,0.1019,0.0663,0.0457,-0.0920,0.0233,-0.0953,-0.1011,0.0208,-0.0281,-0.1133,-0.0381,-0.0849,0.0716,-0.0689,0.0138,0.0322,0.1126,-0.0439,-0.0843,0.1647,-0.0089,0.1403,-0.0139,-0.0788,-0.0837,-0.0942,0.1039],[0.0398,0.0162,-0.0726,-0.0414,0.1344,0.0079,-0.0237,0.0593,-0.0862,-0.0282,0.0349,-0.0587,-0.0060,0.0154,0.0514,-0.1101,0.0583,0.0873,0.0230,-0.0252,0.0188,-0.0267,0.0929,0.0801,-0.0173,0.0422,-0.0426,-0.0055,0.1097,0.0856,0.0860,-0.1106,-0.0125,0.1469,-0.0093,0.0150,0.1234,-0.0814,-0.0882,0.1049,-0.0877,0.0638,0.0911,0.0517,0.0764,0.0308,0.1038,-0.0206,-0.0678,0.0675,-0.0573,-0.0078,0.0154,-0.0582,-0.0353,-0.0323,0.0097,-0.0207,0.0540,0.0407,-0.0008,-0.0685,0.0767,-0.0573,-0.0975,-0.0105,-0.0040,-0.0351,-0.0127,-0.0570,-0.1150,0.0050,-0.0260,0.0543,-0.1217,0.0567,0.0494,-0.0445,-0.0913,-0.1059,-0.0259,-0.0137,0.0143,-0.1142,0.0842,-0.0349,-0.0819,-0.0126,0.0609,-0.0193,0.1012,-0.1114,0.0228,-0.0225,-0.0483,0.0414,-0.0982,0.0305,-0.0079,-0.1029,0.0684,-0.0736,0.0340,-0.0086,-0.0423,-0.0719,0.0393,-0.0033,0.0350,-0.0787,0.0729,-0.0081,0.0480,-0.0578,-0.0497,-0.0146,-0.0666,0.1078,-0.0836,0.0712,0.0562,-0.1010,-0.0882,-0.0808,-0.0110,0.0476,0.1035,0.0040],[-0.0465,-0.0020,-0.0766,-0.0689,0.0036,0.0370,0.0526,-0.0213,0.0112,0.0337,-0.0649,0.0645,-0.0760,0.0521,-0.0647,0.0805,0.0428,-0.0750,0.1294,0.0594,0.0759,-0.0169,-0.1079,-0.1220,-0.0671,-0.0892,0.0528,-0.0556,-0.1073,-0.0131,0.0544,0.0562,-0.0220,-0.0481,0.0948,-0.0429,0.0822,0.0473,0.0142,-0.0251,0.0418,0.0044,-0.0108,-0.0362,-0.1208,0.0524,0.0016,-0.1066,0.0582,-0.0149,-0.0127,0.0597,0.0612,0.0399,-0.0729,-0.0171,0.0894,0.0633,-0.0061,-0.0516,0.0187,0.1150,-0.1075,-0.0961,0.0987,0.0084,-0.0583,0.0812,0.0862,-0.0219,0.0860,0.0684,-0.0694,0.0418,0.0343,0.0891,0.0540,-0.0448,-0.0859,-0.1324,0.0527,0.0050,0.0449,0.0839,-0.0472,-0.0782,-0.0988,0.0110,0.0349,-0.0947,-0.0338,-0.0126,0.0714,0.0294,0.0955,-0.0286,-0.0495,-0.0426,0.0497,0.0770,0.0092,0.0361,0.0804,-0.0697,0.0474,0.0262,-0.0041,0.0242,-0.0108,0.0528,0.0608,-0.0261,0.1101,0.0852,-0.0933,-0.0854,0.0140,-0.0433,0.0339,-0.0670,0.0790,-0.0294,-0.1111,-0.0620,-0.0792,-0.0458,-0.0552,-0.0373],[0.0620,-0.0487,0.0171,-0.1108,-0.0198,-0.0543,-0.0369,-0.0192,-0.0481,0.0947,-0.0708,0.0148,-0.0896,-0.0707,-0.0016,-0.0039,-0.1163,-0.0025,0.0322,-0.0221,-0.0185,0.0021,-0.0631,0.0924,0.0152,-0.0970,0.0502,0.0109,0.0405,0.0113,0.0558,0.0173,-0.0643,-0.0753,0.0341,0.0625,-0.0348,-0.0788,0.0506,-0.0030,-0.0365,0.0104,0.0311,0.0405,0.0540,-0.0733,-0.0678,-0.0906,0.0915,0.0303,0.0122,0.0533,-0.0201,0.0865,0.0858,-0.0775,0.0702,0.0037,0.0452,-0.0140,0.0750,0.0406,0.0588,-0.0668,-0.0567,-0.0081,-0.0084,-0.0786,-0.1031,-0.0636,0.1033,-0.0599,-0.0619,-0.0135,0.0177,-0.0841,0.0160,0.0337,0.0775,-0.0323,0.0529,-0.1085,0.0403,-0.0636,0.0584,-0.0860,-0.0197,-0.0655,0.0247,0.0817,0.0430,0.0105,0.0449,0.0380,0.0007,-0.0744,-0.0357,0.0947,0.0292,0.0023,-0.0337,0.0831,0.0513,-0.0874,0.0888,-0.0449,-0.1121,0.0915,-0.1002,-0.0520,-0.0798,0.0095,-0.0904,0.0285,-0.0042,-0.0012,-0.0033,-0.0173,-0.0781,-0.0088,-0.0792,0.1004,0.0786,0.0010,0.0577,0.0731,-0.0679,0.0845],[-0.0309,0.0916,0.0983,0.0768,0.0896,0.0601,-0.0445,-0.0013,-0.0222,0.0667,-0.0549,0.0236,0.1068,0.0330,0.0364,0.0225,-0.0973,-0.0122,-0.0047,0.0084,0.0694,-0.0871,-0.0718,0.0590,0.0963,0.0377,-0.0264,0.0384,-0.0857,0.0320,0.0216,0.0856,-0.0822,-0.0739,-0.0947,0.0166,0.0690,-0.0328,0.0197,-0.0138,0.0646,-0.0029,-0.0031,0.0851,0.0791,-0.0111,-0.0668,0.0396,-0.0921,0.0699,-0.0115,-0.0732,-0.0814,0.0313,0.0359,0.0635,-0.0668,0.0160,0.0633,-0.0343,-0.0507,0.0776,-0.0893,-0.0605,0.0679,0.0121,-0.0964,-0.0304,-0.0915,-0.0163,-0.0014,0.1007,0.0762,0.0366,-0.0603,-0.0532,-0.0122,0.0949,0.0022,0.0219,0.0462,0.1056,0.0652,-0.0507,-0.0065,0.0455,0.0496,0.0500,0.0736,0.0254,0.0246,0.0465,-0.0383,-0.0351,0.0941,0.0348,-0.0126,-0.0924,-0.0244,0.0408,0.0430,-0.0121,-0.0295,0.0575,0.0094,-0.0485,-0.0902,-0.0247,-0.0039,-0.0459,0.0274,-0.0350,0.0920,-0.0355,-0.0122,0.0439,-0.1016,-0.0970,-0.0092,-0.0361,-0.0099,-0.0336,-0.0864,-0.0831,0.0296,-0.0722,0.0555,-0.0246],[-0.0538,-0.0194,0.0436,0.0273,0.0745,-0.0096,0.0426,-0.0818,0.0693,-0.0730,-0.0376,0.0054,0.0673,-0.0951,0.0505,0.0775,-0.0619,0.0023,0.0106,-0.0981,0.0662,0.0622,-0.0157,-0.0989,-0.0070,-0.0842,-0.0868,0.0484,-0.0094,0.0293,-0.0370,0.0179,0.0652,-0.0404,-0.0154,0.0611,-0.0456,0.0910,-0.0503,-0.0250,0.0322,0.0428,-0.1001,0.0327,0.0812,-0.0625,0.0862,0.0392,0.0348,-0.0499,0.0702,-0.0593,-0.0768,0.0539,-0.0477,0.0018,0.0007,0.0655,0.0999,-0.0286,-0.0944,-0.0987,0.0709,0.0278,-0.0700,0.0566,-0.0709,0.0019,0.0783,-0.0641,0.0450,-0.0377,0.0886,-0.0363,-0.0619,0.0720,-0.0518,-0.0797,0.0077,0.0884,-0.1062,-0.0892,0.0304,-0.0037,0.0780,0.0136,0.0471,0.0712,-0.0630,-0.1080,0.0943,-0.0219,-0.0747,-0.0821,0.0575,0.0838,0.0356,-0.0994,0.0614,0.0444,-0.0617,0.0797,-0.0514,-0.0384,-0.0381,0.0938,-0.0431,0.0146,0.0072,-0.0229,-0.0996,0.0644,-0.0523,0.0326,-0.0132,-0.0524,-0.0953,-0.0027,-0.0525,0.0303,0.0395,0.0821,0.0775,0.0859,0.0522,0.0427,-0.0941,0.0332],[0.0654,0.0327,-0.0864,0.0126,0.0426,-0.0915,0.0994,0.0006,-0.0745,-0.0817,0.0575,-0.0271,-0.0130,-0.0868,0.0062,-0.0090,-0.0122,0.0157,-0.0831,-0.1007,-0.0679,-0.0563,-0.0166,0.0488,0.0435,-0.0754,0.0851,-0.0481,0.0691,0.0550,-0.0610,-0.1007,-0.0699,0.0670,0.0326,-0.0011,-0.0250,0.0145,0.0532,-0.0943,-0.0434,-0.0259,-0.0681,-0.0876,0.0755,0.0188,-0.0352,-0.0124,0.0801,0.1041,0.0416,0.0372,0.0075,-0.0425,-0.0295,-0.0100,0.0999,-0.0149,0.0170,0.0920,0.0394,0.0337,-0.0552,0.0351,0.0159,0.0166,0.0492,-0.1185,0.0161,-0.0220,-0.0269,0.0539,-0.0698,0.0903,0.0373,-0.0916,-0.0060,0.0134,0.0050,0.0079,-0.1049,0.0883,-0.1076,-0.0107,0.0333,0.0019,0.0119,0.0261,0.0168,0.0317,-0.0727,-0.0845,0.0905,0.0377,0.0914,-0.0112,0.0272,-0.0961,0.0381,0.0791,-0.0280,-0.0671,-0.0851,-0.0117,-0.0886,-0.0971,0.0433,0.0525,-0.0596,-0.0232,-0.0390,0.0808,0.0382,-0.0231,0.0379,-0.0462,-0.0920,0.0957,-0.0431,0.0154,0.0234,0.0895,0.0821,0.0036,-0.0606,0.0487,-0.0749,-0.0408],[-0.0487,0.1014,-0.0737,0.0604,0.0222,0.0786,-0.1148,0.1007,0.0493,0.0804,-0.0710,-0.0785,0.0337,-0.0756,0.0294,0.0871,-0.0911,-0.0408,0.0456,-0.0796,-0.0734,0.0592,0.0385,-0.0676,0.0999,-0.0002,0.1154,-0.0564,-0.0072,-0.0654,0.0234,0.1000,-0.0984,-0.0926,-0.0153,0.0724,-0.1056,0.0017,0.0113,0.0182,0.0585,0.0115,-0.0442,-0.0749,-0.1089,0.0908,0.0233,0.0269,0.0777,-0.1062,-0.0876,0.0495,-0.0097,0.0332,-0.0753,-0.0251,-0.0993,0.0028,0.0624,0.0282,0.0318,0.0345,0.0333,0.0044,0.0494,-0.0062,0.1102,0.0797,0.0026,0.0596,-0.0820,0.0658,0.0875,0.0714,0.0040,-0.0359,0.0075,-0.0020,0.0502,-0.0716,-0.0521,0.0070,0.0029,-0.1000,0.0338,-0.0096,-0.0764,-0.0292,0.0199,-0.0254,-0.0939,0.0364,0.0015,-0.0259,0.0153,0.0638,0.0454,0.0448,0.0754,0.0874,0.0703,-0.0993,0.0539,-0.0782,-0.0102,0.1039,0.0341,-0.0192,-0.0182,0.0565,-0.0321,0.0782,-0.0234,-0.0020,0.0501,-0.0885,0.0842,0.0052,0.0963,0.0632,0.0850,-0.0420,0.0706,-0.0263,-0.0480,-0.0763,-0.1003,-0.0993],[-0.0900,-0.0217,-0.0850,-0.0106,0.0871,-0.0235,-0.0413,-0.0536,0.0260,-0.0825,-0.0819,-0.0881,-0.0958,0.0639,-0.0299,0.0286,-0.0300,0.0366,-0.0182,-0.0730,-0.0389,0.0940,0.0394,0.0283,0.0573,-0.0873,0.0111,0.0801,-0.0450,0.0932,-0.0391,-0.0209,-0.0592,-0.0516,-0.0801,0.0979,0.0796,-0.0095,0.1093,0.0414,-0.0680,0.0268,0.0037,-0.0110,0.0401,-0.0558,-0.0651,0.0164,-0.0399,0.0197,0.0667,-0.0549,-0.0257,0.0623,-0.0533,0.0989,-0.0404,-0.0244,0.0933,0.0493,0.0327,0.0202,0.0357,-0.0517,0.0734,-0.0470,-0.0823,0.0407,-0.0220,0.0225,-0.0392,-0.0353,0.0108,-0.0464,-0.0065,-0.0318,0.0838,0.0038,0.0207,0.0333,-0.0568,0.0418,0.0018,0.0979,-0.0157,0.0598,0.0915,0.0129,0.0819,0.0404,0.1054,0.0983,0.0798,0.0575,0.0594,0.0791,0.0694,-0.0128,-0.0960,0.0072,-0.0778,0.0768,0.0941,0.0549,-0.0157,0.0161,-0.0380,-0.0014,-0.0953,0.0337,-0.0284,0.0846,0.0358,0.0465,0.0272,-0.0585,-0.0656,0.0702,0.0271,0.0875,-0.0367,0.0483,0.0048,-0.0586,0.0020,-0.0835,0.0757,0.0784],[-0.0104,-0.1099,-0.0231,0.0157,-0.0959,0.0749,-0.0588,-0.0000,0.0043,0.0654,0.0466,-0.0543,0.0872,-0.0036,0.0844,-0.0147,-0.0134,0.0883,0.0846,-0.0757,0.0869,0.0473,-0.0624,-0.0600,0.0422,-0.0672,0.0067,0.0242,-0.0970,-0.0246,-0.0746,-0.0547,-0.0856,0.0209,-0.1195,0.0734,-0.1102,-0.0695,0.0572,0.0688,-0.0914,-0.0969,-0.0684,-0.0835,0.0743,-0.0126,0.0951,-0.0117,0.0715,-0.0481,-0.0460,0.0771,0.0229,-0.0845,0.0227,-0.0154,-0.0832,0.0752,-0.0696,-0.0052,0.0288,-0.0802,-0.0778,0.0488,-0.0568,-0.0010,-0.0523,0.0179,0.0833,0.0537,-0.0456,-0.0422,0.0792,0.1086,0.0464,-0.0829,-0.0006,0.0343,-0.0206,-0.0953,0.0672,0.0417,-0.1088,-0.0920,-0.0690,-0.0002,0.0578,0.0394,0.0381,-0.0153,-0.0221,-0.0756,0.0440,0.0392,0.0921,0.0857,0.0319,0.0029,-0.1124,0.0609,0.0233,-0.0251,-0.1352,0.0971,0.0148,0.0594,0.0020,0.0578,-0.0969,-0.1038,0.0689,0.0352,0.1298,-0.0115,0.0246,-0.1062,-0.0860,-0.0073,-0.0711,-0.1066,0.0500,0.0351,-0.0972,-0.0022,0.0061,-0.0529,-0.1029,0.0141],[-0.0766,-0.0669,-0.0038,0.0254,0.0341,0.0517,0.0115,-0.0504,0.0820,0.1183,-0.0196,-0.0726,0.0883,-0.0084,0.0931,-0.0559,-0.0441,-0.0466,-0.1045,0.0004,0.0296,0.0017,0.1038,-0.0706,0.0359,0.0392,0.0403,0.1196,-0.0328,0.0161,-0.0654,0.0813,0.0899,-0.0763,-0.0407,0.1000,-0.0389,-0.0858,0.0836,0.0589,0.0254,0.1021,0.0456,0.0476,0.0236,0.1295,-0.0227,0.1023,-0.0158,0.0799,0.0269,0.0297,0.1019,-0.0307,-0.0784,-0.1059,-0.1590,0.0192,-0.0732,-0.0659,-0.0229,0.0786,0.0245,0.0249,-0.0204,0.0210,0.0115,-0.0736,-0.0933,0.0890,0.0295,0.0112,-0.0705,0.0488,-0.0943,0.0729,0.0179,-0.0765,-0.0009,-0.0698,-0.0652,0.0654,-0.0364,0.1130,0.0192,-0.0848,-0.0341,0.0592,0.0873,-0.0374,-0.0294,-0.0396,0.0864,0.0401,-0.0451,-0.0874,-0.0332,0.0027,0.0877,-0.0972,-0.0052,0.0447,0.0449,-0.0497,0.0126,0.0223,-0.0656,-0.1011,-0.0693,0.0891,-0.0648,0.0899,0.1053,-0.0096,-0.0704,0.0526,0.0108,-0.0676,0.0344,0.0779,-0.0374,0.0808,-0.0061,-0.0674,0.0915,-0.0569,0.0396,0.0360],[-0.0399,-0.0710,-0.0445,0.0198,-0.0296,0.0500,-0.1178,-0.0214,0.0563,0.0097,-0.0295,0.0871,0.0297,-0.0218,-0.0852,-0.0251,0.0723,0.0177,-0.0069,-0.0049,-0.0184,-0.0580,0.0961,0.0193,-0.0242,-0.1044,0.0877,0.1064,0.0019,0.0978,-0.0742,-0.0260,0.0535,0.0835,0.1172,-0.0164,-0.0582,0.0045,0.1100,0.0073,-0.0620,0.0467,-0.0091,-0.0044,-0.0650,-0.0578,0.0749,-0.0142,0.0530,0.0033,0.1618,0.0556,-0.0880,0.0086,0.0305,0.0691,0.0862,0.0606,-0.1167,-0.0218,0.1146,-0.1100,0.0100,0.0061,-0.0674,-0.1053,0.0443,-0.0542,0.0988,0.0846,0.0929,0.0748,0.0279,-0.0511,-0.0860,0.0898,0.0469,0.0574,-0.0787,-0.0518,-0.0019,-0.0496,-0.1052,-0.0095,-0.0211,-0.0899,0.0449,0.0885,-0.0694,0.0826,-0.0819,-0.0294,-0.0161,0.0389,-0.0765,-0.0463,-0.0150,0.0436,0.0906,-0.0588,-0.0717,0.0008,-0.0938,-0.0408,0.0493,-0.0276,-0.0723,-0.0222,0.1281,-0.0132,0.0581,-0.1035,-0.0235,-0.0362,-0.0522,0.0557,-0.0687,-0.0477,-0.0975,-0.0750,0.0167,0.0210,0.0014,-0.0725,-0.0056,-0.0714,0.0163,-0.0115],[0.0871,-0.1101,-0.0747,-0.0095,0.0910,0.0075,0.0200,-0.0590,0.1290,-0.0830,0.0177,0.0765,-0.0372,-0.0159,-0.0389,-0.0113,-0.1069,-0.0067,0.0313,0.1105,0.0937,0.0166,0.0316,0.0920,0.0236,-0.0191,0.1117,0.0605,-0.1161,0.0565,-0.0991,0.0350,0.0107,0.0274,-0.0672,0.0008,-0.0188,0.1119,0.1121,-0.0298,0.1440,-0.0608,0.1325,0.0935,0.0751,-0.0838,-0.0896,-0.0122,-0.0377,-0.1297,0.0692,-0.0281,-0.0159,-0.0260,-0.0466,0.0643,0.1054,0.0550,-0.0076,-0.1035,-0.0957,0.0620,0.0116,-0.0747,-0.0798,0.0108,0.0608,0.0218,0.1302,0.0633,0.0054,0.0002,0.0611,-0.0130,-0.0520,0.0310,0.1004,-0.0082,-0.1072,0.0255,0.0223,0.0297,0.0491,0.0801,0.0332,0.0457,-0.0572,-0.0744,-0.0976,0.0851,-0.0245,-0.0502,0.1103,0.1109,-0.0932,-0.0164,-0.0450,-0.0873,-0.1159,-0.0759,0.0181,-0.0963,0.0287,0.0485,0.0852,-0.0383,-0.0151,0.0551,-0.0365,0.0544,-0.0727,0.0155,0.0034,0.0886,-0.0635,0.0250,0.0148,0.0382,0.0981,0.0715,0.0297,-0.0497,-0.0378,-0.0406,0.0707,-0.0097,-0.0704,0.0064],[0.0183,0.0408,0.1059,0.0699,0.0511,-0.0709,0.0408,0.0491,0.0594,-0.0922,-0.0382,0.0805,0.1004,0.0450,-0.0694,0.0344,-0.0168,0.0408,0.0008,-0.0779,0.0489,0.0849,-0.0079,-0.1186,0.0939,0.0384,-0.0886,-0.0423,0.0880,-0.0664,0.0997,-0.0892,-0.0430,-0.0118,0.0835,0.0183,-0.0891,0.0764,-0.0033,-0.0906,-0.0934,-0.0106,-0.0227,-0.0523,-0.0883,0.0352,-0.0768,0.0998,0.0550,0.0055,0.0470,-0.0877,-0.0755,0.0656,-0.0216,-0.0607,-0.0512,0.0678,0.0158,0.0202,0.0664,0.0107,-0.0305,0.0130,-0.1029,-0.0685,0.0709,0.0183,0.0702,0.0396,0.0351,-0.0483,-0.0849,0.0562,-0.0409,-0.0008,0.1053,-0.0665,0.0702,-0.0271,0.0743,0.0751,0.0876,-0.0854,0.0851,-0.0811,-0.0997,-0.0785,-0.0384,-0.0002,-0.0742,0.0440,-0.0863,0.0705,0.0010,0.0288,-0.0050,0.0800,0.0691,0.0479,-0.0656,0.0233,0.0694,0.0860,-0.0551,0.0593,0.0430,-0.0667,0.0958,-0.0296,0.0194,0.0746,0.0832,0.0097,0.0058,0.0540,0.0872,0.0055,-0.0927,0.0446,0.0860,-0.0675,-0.0935,0.0656,0.1032,-0.0239,-0.0458,0.0271],[-0.0429,0.0297,-0.1063,-0.0738,-0.0401,-0.0830,0.0472,-0.0918,0.0174,-0.0801,-0.0573,0.0500,0.0089,-0.0942,0.0362,0.1015,0.0342,0.1036,0.0046,-0.0310,-0.0301,-0.0166,0.0248,0.1146,-0.0904,-0.0017,-0.0937,-0.0064,-0.0641,0.0775,0.0451,-0.0840,-0.0510,-0.1210,0.0352,0.0333,0.0256,0.0535,0.0922,0.0254,-0.0780,0.0271,0.0544,0.0653,-0.0886,-0.0191,0.0904,0.1073,0.0705,0.0911,-0.0433,0.0055,0.0635,-0.1090,0.0417,0.0031,-0.0811,-0.0864,-0.0662,-0.0850,-0.0797,-0.0237,-0.0892,-0.0199,0.1060,0.0190,-0.0493,-0.1007,-0.0432,-0.0839,0.0655,0.0726,-0.0181,0.0218,0.0500,0.0535,-0.0320,-0.0144,0.0361,0.0008,-0.0389,-0.0586,0.0641,-0.0191,-0.0129,-0.0027,-0.0499,-0.0529,-0.0143,0.0446,0.0413,-0.0811,0.0809,0.0255,-0.0017,-0.0825,-0.0538,0.0212,-0.0416,0.0141,0.0800,0.0566,0.0238,0.1070,0.0519,-0.0598,-0.0783,-0.0157,0.0468,0.0408,-0.0875,-0.0026,-0.0303,0.0122,-0.0905,0.0321,-0.0158,-0.0047,-0.0989,-0.0441,-0.0272,-0.0381,0.0671,-0.0252,0.0680,0.0875,-0.0638,-0.0204],[-0.0541,0.0596,0.0301,-0.0508,0.0803,0.0434,0.0232,0.0784,-0.0954,-0.0461,0.0044,-0.0207,0.0725,0.0178,-0.1215,-0.0923,0.0649,0.0396,0.0853,-0.0818,-0.0363,0.0443,-0.0151,-0.0365,-0.0583,-0.0228,0.0687,-0.0822,-0.0090,-0.0111,0.0007,-0.0186,-0.0001,0.0512,0.0046,-0.0181,-0.0437,0.0805,-0.1022,-0.0806,-0.0178,-0.1159,-0.0246,-0.0362,0.1080,-0.1046,-0.0511,-0.0089,-0.0947,0.0321,-0.1371,-0.0006,0.0758,-0.0727,-0.0860,0.0891,-0.0914,0.1280,-0.0114,0.0582,0.0734,0.0302,0.0369,0.1186,-0.0360,0.0476,0.0877,0.0374,0.0155,0.0952,0.0677,-0.0702,0.0541,0.0522,0.1177,0.0485,-0.1436,0.0995,-0.0357,0.0448,0.0892,0.0910,0.0673,-0.0087,0.0113,0.0669,0.0579,-0.0692,0.0113,0.1028,0.0600,0.0158,-0.0196,-0.0421,-0.0189,-0.1084,0.0154,-0.0695,0.0641,0.0766,0.0442,-0.0803,0.0743,0.0243,0.0012,-0.0121,0.0769,-0.1178,0.0171,-0.0117,-0.0497,-0.0790,0.0871,-0.0883,-0.0152,0.0594,-0.0790,0.0252,0.0672,-0.0237,-0.0346,-0.0561,0.0568,0.0496,0.0431,-0.0302,0.0751,0.0362],[0.1258,0.0386,0.0833,0.0229,0.0343,0.0291,-0.0033,0.0708,0.1168,0.0519,0.0110,-0.0885,0.0176,-0.0358,0.0704,0.0569,0.0479,0.0355,0.1020,-0.1020,0.0268,-0.0688,0.1510,0.1097,0.0503,0.0489,-0.0741,0.0077,-0.0450,0.0150,-0.0887,-0.0336,0.0585,-0.0611,-0.0841,0.0504,-0.0640,0.0630,0.0665,-0.0288,-0.0153,0.0717,0.1109,-0.0292,-0.0689,-0.0098,-0.0111,0.0824,-0.1247,0.0894,0.0432,-0.0607,-0.0402,-0.0542,-0.0487,-0.0696,0.0905,-0.0083,-0.0596,-0.0940,-0.0706,0.0147,-0.0147,-0.0649,-0.0294,0.0119,0.1051,0.0645,-0.0859,-0.0111,-0.0379,-0.0829,-0.0771,0.0840,-0.1217,0.0700,0.0243,0.0784,-0.0019,-0.0050,-0.0853,-0.1355,-0.0724,0.1061,0.0383,0.0750,-0.0070,0.0585,0.0372,-0.0198,-0.0342,-0.0229,0.0279,0.0212,0.1162,-0.0724,0.0397,-0.0155,0.0586,-0.1288,-0.0472,-0.0558,0.0353,-0.0740,0.0917,-0.0023,-0.0938,-0.0320,-0.0632,-0.0038,-0.0101,0.0930,0.0971,0.0338,-0.1111,0.0060,0.0817,0.1495,0.0196,-0.0795,0.0767,-0.0419,-0.0115,-0.0672,-0.0221,0.0799,0.1247,0.0491],[0.0866,-0.0478,0.0422,0.0011,0.0924,0.0549,0.0137,0.0344,-0.0436,0.0229,0.0152,-0.0028,-0.0514,-0.0301,-0.0984,-0.0133,-0.0531,-0.0602,-0.0709,-0.0754,0.0755,0.0854,0.0810,-0.1000,-0.0583,0.0728,-0.0354,0.0449,-0.0976,-0.0543,-0.0066,-0.0454,-0.0142,-0.0021,-0.1069,-0.0209,-0.1075,-0.0718,-0.0221,0.0030,-0.0891,0.0032,-0.0773,0.0252,0.0145,-0.0620,0.0132,0.0308,-0.0218,0.0659,0.0330,0.0732,0.0697,0.0699,0.0019,-0.0490,-0.0101,0.0167,0.0749,0.1000,0.0771,0.0853,-0.0535,0.0739,-0.0712,-0.0380,-0.0337,-0.0098,0.0641,-0.0107,0.0070,-0.0598,-0.0176,0.0299,0.0195,0.0940,0.0057,-0.0433,-0.0115,0.0205,-0.0858,0.0615,-0.0649,0.0287,0.1212,0.1003,-0.0547,0.0715,-0.0904,-0.0368,0.0949,-0.0691,0.0209,0.0276,-0.0640,-0.0508,-0.0579,0.0350,0.0170,-0.0731,0.0683,0.0767,0.0966,0.0690,-0.0515,0.0751,-0.0537,0.0691,-0.0299,0.0482,-0.1079,0.0251,-0.0620,0.0499,-0.0126,-0.0729,-0.0454,-0.0286,-0.0896,0.0303,-0.0345,0.1191,-0.0987,0.0632,-0.0348,-0.0264,-0.0129,-0.1048],[-0.0390,-0.0218,0.1281,0.0826,-0.0531,0.0472,0.0723,0.0821,0.0352,-0.0899,-0.0028,-0.0368,0.0536,-0.0434,0.0891,-0.1330,-0.0065,0.0810,0.0112,0.0784,-0.0543,-0.0823,-0.0408,-0.0121,0.0300,-0.0893,0.0226,-0.0259,-0.0689,0.0877,0.0285,-0.0986,0.0789,0.0694,-0.0295,-0.0154,-0.0085,-0.0558,-0.0416,0.0375,0.0819,0.0036,-0.0759,0.0454,0.0528,0.0824,-0.1154,0.0756,-0.0252,0.0880,0.0342,0.0326,0.0067,-0.0612,-0.0403,-0.0871,0.0437,0.0482,-0.0231,0.1055,0.0507,-0.0370,0.0525,-0.0351,0.0163,0.0338,-0.0161,0.1073,-0.1302,0.0485,0.0363,0.0415,0.0147,0.1065,-0.0619,-0.0751,-0.0597,0.1480,-0.0744,0.0278,0.0734,0.1318,-0.0493,0.0947,-0.0555,0.0567,-0.0255,-0.0822,0.0569,-0.0566,0.0265,-0.0762,0.0472,0.0371,-0.1038,0.0075,0.1009,0.0894,0.0191,-0.0927,-0.0740,-0.0678,-0.0697,0.1022,-0.0005,-0.1172,0.0557,-0.0271,-0.1078,-0.0549,0.0596,-0.0916,-0.0944,-0.0923,-0.0362,0.0952,-0.0290,0.0142,0.1112,0.0899,-0.0358,-0.0195,0.0505,0.0511,-0.0867,0.0761,0.0612,0.0595],[-0.0793,-0.0760,-0.0222,-0.0072,0.0018,-0.0153,0.0851,0.0669,-0.0989,0.0056,-0.0752,0.0394,-0.0284,0.0198,0.0483,0.0775,0.0903,-0.0712,0.0062,-0.0767,0.0227,0.0138,0.1000,-0.0457,0.0151,-0.0642,0.0042,-0.0804,0.0941,0.0150,0.0909,0.0118,0.0077,0.0262,-0.0895,0.0600,-0.0209,0.0971,-0.0712,-0.1029,0.1003,0.1062,0.0520,-0.0892,0.0747,0.0586,-0.0861,0.0428,0.0865,0.0413,0.0295,0.0433,0.0681,0.0903,-0.0105,-0.0495,-0.0188,0.0816,0.0516,0.0427,-0.0455,-0.0706,-0.0645,-0.0064,0.0143,0.0343,0.0639,0.0287,-0.0005,-0.0615,0.1094,0.0096,0.1088,0.0277,0.0704,-0.0596,0.0843,-0.0574,0.0464,-0.0711,0.0392,0.0048,0.0052,0.0421,0.0780,0.0515,0.0789,-0.0015,0.0017,0.0549,0.1129,0.0580,-0.0642,-0.0462,-0.0512,0.0159,-0.1403,0.0849,-0.0065,-0.0288,-0.0052,0.0583,-0.0926,0.0560,0.0224,0.0525,0.0583,0.0708,0.0629,0.0125,0.0183,-0.0474,0.0793,-0.0599,-0.0596,0.0431,-0.1021,-0.0383,-0.0851,-0.0981,-0.1184,-0.0185,0.0916,-0.0708,-0.0680,0.0919,0.0423,-0.0954],[-0.0878,0.0771,0.0212,0.0793,0.0748,0.0177,-0.0316,0.0627,-0.0789,0.0617,0.0700,0.0365,-0.1059,-0.0369,0.0839,0.0645,-0.0729,-0.0480,0.0414,0.0613,0.0241,-0.0334,-0.0478,0.0234,-0.0792,-0.0676,0.0958,0.0573,0.0635,0.0854,0.0028,-0.0646,0.0505,-0.0872,-0.0938,0.0476,-0.0688,0.0960,0.0537,0.0409,-0.0329,0.0677,0.0411,0.0541,-0.0606,0.0977,0.1144,-0.0712,0.0921,-0.0081,-0.0348,0.0282,0.0542,0.0408,0.1013,0.1052,0.0058,0.0822,-0.0695,0.0100,-0.0517,-0.0262,0.0763,-0.0015,-0.0207,0.0767,0.0313,0.0062,0.1052,0.0285,0.0884,-0.0272,-0.0689,0.0911,0.0443,-0.0101,0.0293,-0.0978,0.0155,0.0433,-0.0468,0.0904,-0.0026,0.0829,0.0226,0.0127,-0.0720,0.0092,-0.0534,-0.0458,-0.0561,-0.0978,-0.0537,0.0242,0.0439,0.0893,0.0121,0.0288,0.0419,-0.0797,-0.0018,-0.0590,-0.1036,-0.0410,-0.0638,0.0674,-0.0488,0.0365,0.0149,-0.0804,0.0128,0.0844,-0.0473,0.0645,-0.0968,0.0971,-0.0964,0.0270,-0.0176,-0.0710,0.0611,-0.0978,0.0461,0.0611,0.0426,0.0337,0.0829,0.0859],[-0.0949,-0.0012,-0.0040,-0.0403,0.1153,-0.1103,0.1054,0.0945,-0.0532,0.0173,-0.0572,0.0808,0.1000,-0.0234,0.0668,-0.0210,0.0529,-0.0650,0.1043,0.0880,-0.0178,-0.0149,0.0317,-0.0556,0.0502,-0.0136,0.1024,-0.0094,-0.0105,-0.0033,-0.0056,-0.0082,-0.0004,-0.0022,0.0533,-0.0709,-0.1208,-0.0193,0.0564,-0.0082,0.0728,0.0484,0.0041,0.0659,0.0081,0.0633,-0.0957,-0.0829,0.0244,0.1101,0.0491,0.0862,-0.0401,0.0363,-0.0689,-0.0258,0.0865,0.0606,0.0522,-0.0809,-0.0215,-0.0576,0.0276,0.0679,0.0206,-0.0186,-0.1259,0.0382,0.1025,-0.0601,0.0218,0.1126,0.0201,0.0151,0.0363,-0.0759,0.0531,0.0832,0.0183,-0.0603,-0.0129,0.0601,0.0082,-0.0829,0.1410,-0.0724,0.0013,-0.0751,-0.0173,-0.0315,0.0853,0.0892,0.0024,0.0917,-0.2081,-0.0280,0.0266,-0.0545,0.0428,0.0849,0.0494,0.0556,0.0181,0.0417,-0.0115,-0.0655,0.0185,0.0744,0.0590,0.0170,0.0307,-0.1036,-0.0876,0.0346,0.1070,-0.0395,0.0448,-0.0737,0.0704,0.0061,0.0650,-0.0537,0.0050,-0.0840,-0.1298,0.0164,-0.0430,0.0292],[0.0556,-0.0253,0.0023,0.0525,0.0047,-0.0738,0.0031,-0.0947,0.0337,-0.0219,0.0757,-0.0876,-0.0716,-0.0214,0.0818,0.1104,-0.0339,-0.0549,-0.0669,0.0141,-0.0191,0.0807,-0.0523,0.0347,-0.0756,0.0468,0.0719,0.0045,0.0814,0.0083,0.0650,-0.0934,-0.0849,-0.0151,0.0314,0.0416,-0.0726,0.1085,-0.0291,0.0396,0.0530,0.1103,0.0020,-0.0431,0.0184,-0.0713,-0.0692,0.0337,0.1196,-0.0044,-0.0707,-0.0335,-0.0513,0.0602,0.0819,-0.0753,-0.0076,0.0878,0.0163,-0.0888,-0.0053,-0.0036,-0.0819,-0.0257,-0.0769,0.0298,-0.1214,0.0021,0.1050,-0.0769,0.0946,-0.0550,0.0735,-0.0269,0.0306,-0.0634,-0.0474,0.0061,0.0030,-0.0804,0.0103,-0.0054,-0.0341,0.0039,-0.1000,0.0282,-0.0687,0.0788,-0.0994,0.0322,-0.0942,-0.0685,0.0024,0.1033,0.0751,-0.0349,-0.0193,0.0021,0.0622,0.0890,0.0416,-0.0598,-0.0561,-0.0255,-0.0218,-0.0586,0.0053,-0.0091,0.0177,-0.1000,0.0530,-0.0917,-0.0742,0.0072,0.0163,-0.0714,0.0019,0.0810,0.0090,-0.0339,0.0689,0.0806,-0.0312,0.0297,-0.0680,-0.0599,0.0811,-0.0965],[-0.0618,0.0592,-0.1097,-0.0769,-0.0483,-0.0602,-0.0966,0.1044,0.0550,0.0109,-0.0248,-0.0527,-0.0384,0.0610,-0.0055,0.0599,-0.0462,0.1521,-0.0982,0.0562,0.0436,0.0883,0.0037,-0.0230,-0.0265,0.1143,-0.0928,0.0402,-0.0151,0.0017,0.0703,-0.0200,-0.1107,0.0184,-0.0731,0.0611,0.0546,0.0695,-0.0660,0.0212,-0.0071,-0.0378,0.0978,0.0927,0.0903,0.1308,0.1283,0.0423,0.0081,-0.0964,-0.1181,-0.0918,-0.0420,-0.0592,0.0607,0.0254,-0.0508,-0.1053,0.0756,-0.0529,-0.1008,0.1137,0.0045,-0.0971,0.0476,-0.0275,-0.1183,0.0689,0.0234,-0.0237,0.0446,0.0471,-0.0059,-0.0713,0.1378,-0.0605,-0.1509,-0.0848,-0.0914,0.0921,-0.1106,-0.0707,0.0220,0.0395,-0.0239,0.0860,0.0750,-0.1028,-0.0211,-0.0821,-0.0958,0.0498,-0.1472,-0.1148,0.0380,0.0814,0.0495,-0.0288,-0.0665,0.1019,0.0299,0.0209,0.0950,0.1221,-0.0930,-0.0380,-0.0879,0.0922,-0.1465,-0.0934,-0.1516,-0.1135,0.0425,-0.0482,0.0716,0.1890,0.1297,0.0002,-0.0326,-0.0519,-0.0774,-0.0069,-0.0088,0.0629,-0.1208,0.0200,0.0927,-0.0763],[0.0842,-0.0749,-0.0540,0.0913,-0.0126,-0.0578,0.0995,0.0212,0.0756,0.0811,-0.0471,-0.0660,-0.0540,0.0578,0.0869,-0.0321,0.0954,-0.0971,-0.0229,0.0896,0.0053,0.0790,0.0568,-0.0691,0.0796,-0.0397,-0.0675,0.0912,-0.0433,-0.0936,-0.0751,0.0915,-0.0745,-0.0650,0.0763,0.0202,-0.0899,-0.0040,-0.0252,0.0780,-0.0794,-0.1040,0.0679,0.0274,-0.0911,-0.0698,0.0662,0.0429,0.0859,0.0617,-0.0371,0.0140,0.0151,-0.0310,-0.0071,0.0383,0.0358,-0.0313,0.0052,-0.0642,0.0447,0.0181,-0.0503,-0.0349,-0.0138,0.0421,0.0555,0.0341,0.0576,0.0859,-0.0669,0.0599,0.0502,-0.0589,0.0927,0.0435,0.0229,-0.0837,-0.0567,0.0552,0.0846,0.0644,0.0654,-0.0631,0.0406,-0.0014,-0.0059,-0.0408,-0.1256,-0.0239,-0.0705,-0.0621,-0.0686,0.0553,-0.0693,-0.0517,0.0114,0.1051,0.0257,-0.0011,-0.0564,0.0029,-0.1096,0.0258,-0.0914,-0.0433,0.0249,0.0629,0.0139,0.0555,-0.0411,0.0022,-0.0550,0.0566,-0.0297,0.0398,-0.0993,-0.0709,0.0399,-0.0153,0.0723,-0.0583,0.0425,-0.0726,-0.0096,-0.0286,-0.0955,0.0473],[0.0206,0.0637,-0.0604,0.0894,-0.0398,0.0208,0.0638,-0.0785,0.0525,-0.0335,0.0511,-0.0937,0.1015,0.0042,-0.0244,0.0451,0.0007,0.0375,0.1122,0.0258,0.0020,-0.0124,0.0597,0.0210,0.1001,-0.0149,-0.0545,-0.1052,-0.0725,0.0108,-0.0096,0.0692,-0.0604,-0.1392,-0.0170,0.0420,-0.0926,0.0725,0.0108,0.0107,0.0502,0.0587,0.0553,0.0383,0.0670,0.0238,0.0806,-0.0008,-0.0949,0.0748,-0.1168,0.0395,-0.0604,0.0275,0.0892,0.0739,-0.0140,0.0301,0.0640,0.0338,0.0044,0.0111,0.0734,0.0986,-0.0951,-0.0230,-0.1414,0.0070,-0.0480,-0.0513,0.0390,0.0075,0.0525,0.0214,0.0824,0.0558,-0.0029,-0.0204,0.0152,0.0293,0.0274,0.0709,0.0860,0.0159,0.0801,0.0953,-0.0272,0.0156,-0.0588,0.0181,-0.0477,0.0086,0.0256,0.0659,-0.0147,-0.0112,-0.0029,0.0564,-0.0751,-0.0858,0.0945,-0.0995,-0.1001,0.0144,-0.0952,0.0212,0.0381,0.0834,-0.0477,-0.1141,0.0863,0.0494,-0.0633,0.0008,0.0948,-0.0149,-0.0235,-0.0910,-0.0072,0.0921,0.0608,-0.0405,0.0800,0.0021,-0.0811,0.1024,0.0398,-0.0066],[0.1227,-0.0835,0.0577,0.0451,-0.0682,0.0188,-0.0395,0.0053,-0.0823,0.0912,0.0292,-0.0002,0.1016,0.0967,0.0390,-0.0045,0.0112,0.0859,0.0313,-0.0198,0.0947,-0.0724,0.0450,0.1110,0.0048,0.0760,-0.0325,0.0537,-0.0796,0.0682,-0.0477,-0.0331,0.0870,-0.0712,0.0055,0.0636,0.0315,0.0109,-0.0443,-0.0056,-0.0619,-0.0154,0.0026,-0.0068,-0.0293,0.0836,-0.0867,-0.0545,-0.1228,0.0882,-0.0582,0.0885,-0.0039,0.1175,-0.0502,-0.1059,-0.1117,0.0750,-0.0200,0.0354,-0.0659,-0.0081,0.0745,-0.0459,-0.0124,0.0615,0.0659,0.0499,-0.0766,0.0692,-0.0461,0.0787,-0.0728,-0.0762,-0.0931,-0.0432,-0.0843,0.0587,-0.0633,-0.0811,-0.0367,-0.0470,0.0047,-0.0062,-0.1031,0.0324,0.1213,-0.1089,-0.0990,0.0439,0.0993,0.0070,0.0663,0.0573,-0.1358,0.0365,0.0332,0.0743,-0.0402,0.0747,-0.0389,-0.0684,0.0795,-0.0054,0.0017,0.0241,0.0214,0.0564,-0.0792,-0.0193,-0.1015,0.0658,0.0127,0.0021,0.0902,0.0922,-0.0210,0.0203,0.0854,0.1029,0.0248,-0.0018,0.0164,0.0481,-0.0597,0.0562,-0.0876,0.1003],[-0.0575,0.0308,-0.0612,-0.0960,0.0727,-0.0555,0.0343,0.0641,0.0457,-0.0125,0.0298,-0.0352,-0.0518,0.0922,0.0152,-0.0101,0.0119,-0.0305,-0.0479,0.0107,-0.0763,0.0462,0.0097,-0.0286,-0.0248,-0.0535,-0.0369,-0.0776,-0.0411,0.0032,-0.0680,0.0017,-0.0220,0.0851,-0.0858,-0.0961,-0.0331,0.0216,0.0680,-0.0631,0.0269,0.0120,0.0544,-0.0398,-0.0276,-0.0773,-0.0134,-0.0292,-0.0561,0.0107,0.0692,-0.0739,0.0887,0.0519,-0.0631,0.0112,0.0664,0.0781,0.0865,-0.0571,0.1000,0.0611,-0.0390,0.0469,0.0276,0.0157,-0.0649,0.0544,-0.0646,0.0954,-0.0282,-0.0972,0.0413,-0.0968,0.0624,0.0638,-0.0318,0.0139,0.0005,0.0096,0.0085,0.0130,-0.0806,0.1015,-0.0859,-0.0897,0.0591,-0.0909,-0.0023,0.0134,-0.0246,-0.0629,0.0284,-0.0581,-0.0277,0.0553,-0.0109,0.0419,-0.0641,-0.0813,0.0301,0.0655,-0.1227,-0.0073,0.0496,0.0067,0.0281,-0.0117,-0.0397,-0.0558,-0.0665,0.0190,-0.0043,0.0611,-0.0402,-0.0749,0.0494,0.1013,0.0769,-0.0819,-0.0710,-0.0875,0.0642,-0.0590,-0.0379,-0.0612,0.0861,-0.0462],[-0.0108,-0.0233,0.0342,0.0756,-0.0378,0.0027,0.0253,-0.0554,0.0418,0.0117,-0.0108,0.0583,0.0329,-0.0632,0.0794,-0.0573,-0.0269,-0.0870,-0.0001,0.0796,0.0230,0.0142,0.0485,-0.0070,-0.0766,-0.0477,0.0967,-0.0251,0.0901,-0.1041,0.0608,-0.0991,0.0550,0.0007,-0.0434,-0.0447,0.0417,0.0609,-0.0770,0.0582,-0.0595,0.0452,-0.0078,-0.0120,-0.0801,-0.0070,0.0381,0.0311,-0.0301,-0.0497,0.1078,-0.0680,0.1168,-0.0041,-0.0317,-0.0733,0.0861,-0.0252,0.0700,0.0071,0.0663,0.0436,-0.0074,-0.0357,0.0002,0.0986,-0.0554,0.0361,-0.0926,0.0410,-0.0081,0.0570,0.1019,0.0546,0.0865,0.0257,-0.0073,-0.0758,-0.1031,0.1040,0.0784,-0.0661,-0.0015,-0.0279,-0.0423,-0.0295,-0.0314,0.0001,0.0303,0.1126,-0.0294,0.1021,-0.0301,-0.0278,0.0432,0.0245,-0.0674,-0.0648,0.0901,-0.0128,-0.0865,-0.0883,0.0272,-0.1025,0.0015,-0.0040,0.1003,-0.0703,0.0181,0.0972,0.0663,0.0577,0.1099,0.0528,0.0221,0.0973,-0.0197,0.0356,-0.1042,-0.1130,-0.0518,-0.0823,-0.0567,-0.0718,-0.0296,0.0746,0.0826,-0.0550],[-0.0640,-0.0507,0.0999,0.0051,-0.0333,-0.0536,0.0420,-0.1212,-0.0163,0.0115,0.1324,-0.0741,-0.0965,0.0329,-0.0031,-0.1051,0.0198,0.0284,-0.0276,0.0873,-0.0665,-0.0480,-0.0042,0.0247,0.0048,-0.0416,0.0728,-0.0353,0.1251,0.0387,-0.0097,-0.1100,-0.0572,0.0146,0.0610,0.0956,-0.0273,0.0096,0.0206,0.0692,-0.0080,-0.0040,-0.0795,-0.0950,-0.1001,0.0362,0.0815,-0.0053,-0.1000,-0.0002,-0.0148,-0.0024,-0.0177,-0.0912,0.1078,0.0038,-0.0554,0.0066,0.1413,-0.0338,0.0070,0.0298,0.0831,0.0131,0.0490,-0.1217,-0.1243,-0.0016,-0.0799,-0.0627,-0.0397,-0.0118,-0.0595,-0.0194,-0.0056,-0.0053,-0.0727,0.0468,-0.0911,-0.0356,0.0746,-0.0250,0.0581,0.0628,0.0081,-0.0592,0.0426,-0.0450,0.0415,0.0825,0.0321,0.1116,0.0512,-0.0469,-0.0643,0.0937,-0.0620,-0.0713,0.0293,0.0219,0.0791,0.0746,-0.0515,0.0417,0.0696,-0.0883,-0.0158,0.0837,0.0306,-0.1154,0.0141,-0.0515,-0.0765,-0.0061,-0.0320,-0.1399,-0.0996,0.0081,0.1082,0.1220,0.0582,-0.1093,0.1083,0.1524,0.0624,-0.0519,0.0113,-0.0248],[0.0848,0.0740,-0.1329,-0.0203,-0.0345,-0.0359,0.1091,-0.0141,-0.0008,0.1161,0.0307,0.0279,0.0647,-0.0588,0.0818,0.0087,-0.0903,-0.1037,0.0709,-0.0692,-0.0533,0.0922,-0.0082,0.0634,-0.0863,-0.0929,0.0070,-0.0106,-0.1109,0.0924,0.1124,0.0267,-0.0894,-0.0542,-0.1209,0.0280,0.0310,-0.1045,-0.0417,-0.0472,0.1775,0.0094,-0.0764,-0.0944,-0.0503,-0.0382,0.0462,-0.0237,0.0318,-0.0689,-0.0048,-0.1035,-0.0360,0.0022,-0.0153,-0.0121,0.0594,0.1005,-0.0276,-0.0308,-0.0432,-0.0239,0.0772,-0.0224,0.1310,0.0315,0.0606,0.0422,-0.0138,-0.1067,-0.0443,0.0560,0.0903,0.0271,-0.0789,-0.1000,-0.0664,0.0711,0.0218,0.0589,0.0061,-0.0307,-0.0756,0.0272,-0.0121,-0.0633,0.1040,-0.0760,-0.0136,0.0160,-0.0088,0.0669,-0.0726,0.0468,-0.1424,-0.0973,0.0571,0.0322,0.0119,0.0000,0.0674,0.0128,0.1201,-0.1367,-0.0189,0.0041,-0.0986,0.1058,0.1141,-0.0312,-0.0678,-0.0007,-0.0772,0.0515,0.0549,-0.0969,-0.1073,0.0304,0.0853,-0.0024,-0.0514,-0.0663,0.0718,-0.1611,-0.0015,0.0043,-0.0335,-0.0603],[0.0620,-0.0877,-0.0743,-0.0498,0.0600,0.0078,-0.0058,-0.1158,-0.0174,-0.0382,0.0126,-0.0206,-0.0165,-0.0261,0.1145,0.0455,-0.0436,-0.0839,0.0529,0.0568,0.0722,-0.0810,0.1062,-0.1056,0.0071,-0.0514,0.0269,-0.0376,-0.0622,0.0833,0.0080,-0.0956,-0.0330,0.1136,-0.0348,-0.0460,0.0856,-0.0539,-0.0976,-0.0343,0.0527,0.0417,-0.0416,0.0254,-0.0971,0.0266,0.0055,-0.0636,0.0057,0.0570,-0.0221,-0.0921,-0.0652,-0.0445,-0.0424,-0.0493,0.0491,0.0028,0.0291,0.0323,0.0901,0.0856,0.0923,-0.0173,0.0188,0.0329,-0.0449,0.1047,-0.1067,-0.0455,-0.0444,-0.0656,-0.0638,-0.0991,0.0709,0.0818,0.0714,-0.1205,-0.0083,-0.0779,0.0097,-0.0724,0.0832,0.0629,0.0637,0.0016,-0.1289,0.0552,0.0647,-0.0370,0.0591,-0.1120,-0.0782,-0.0057,0.1007,-0.0806,-0.0213,0.0715,-0.0520,0.0044,-0.0829,0.0637,-0.0746,0.0838,0.0595,0.1032,0.0663,-0.1191,0.0322,-0.0495,-0.0371,0.0146,-0.0647,-0.0328,-0.0059,-0.0510,0.0173,0.0290,-0.0476,-0.0926,-0.0640,-0.0284,0.0124,0.0202,0.0344,0.0635,-0.0667,-0.0046],[-0.0835,0.0352,-0.0561,0.0066,0.0014,0.0090,-0.0174,-0.0804,0.1345,0.1140,-0.0492,0.0382,-0.0336,-0.0701,-0.0473,-0.1165,-0.0131,0.0866,-0.0906,0.0268,-0.1268,0.0339,-0.0668,0.0074,-0.1186,-0.0746,-0.1273,0.0550,-0.0023,0.0001,0.0729,0.0162,0.0064,0.0792,-0.0320,0.0155,0.0469,-0.0920,0.1011,0.1090,-0.0103,0.0278,0.0289,0.1092,0.0164,0.0922,0.0733,0.0517,0.1080,-0.0043,-0.1921,0.0273,0.0219,-0.0450,0.0141,0.0362,0.0236,-0.1286,0.0964,0.0437,-0.0717,0.0789,0.1101,-0.0842,0.0123,-0.0189,-0.0872,-0.0849,0.0379,-0.1409,0.0711,-0.0413,-0.0793,-0.0930,-0.0586,-0.0317,0.0872,0.1420,0.0199,-0.0136,-0.0292,-0.0729,0.0837,0.0180,0.0970,0.1045,0.0634,0.0228,0.0339,0.0496,-0.1445,0.1092,-0.1129,0.0938,-0.0367,0.0067,0.1276,0.0595,-0.0389,-0.0077,-0.1099,0.0686,0.0032,-0.0120,0.0298,0.1032,-0.0568,-0.0538,-0.1411,-0.0219,-0.1843,0.0838,-0.1187,-0.0893,0.0819,0.1020,0.0769,-0.0141,0.0109,0.0421,0.0365,0.0025,-0.0122,0.0055,-0.0612,0.0888,-0.1103,-0.1198],[-0.0864,0.0432,-0.1046,0.1120,-0.0372,-0.0594,-0.0827,0.1386,0.0544,0.0897,-0.0135,-0.0903,0.0499,0.0577,-0.0343,-0.1016,0.0166,-0.0696,-0.0673,-0.0295,-0.1108,0.0591,-0.0105,0.0267,-0.0370,-0.0515,-0.0734,0.1562,0.0579,0.0690,-0.1018,-0.0530,-0.1157,0.0646,0.0123,0.0918,0.1149,0.0092,0.1388,0.0741,0.0912,0.0758,-0.0488,0.0954,0.0005,0.0649,0.0915,0.1473,0.1058,0.1233,-0.1896,-0.1833,-0.0114,-0.0471,0.1692,-0.0007,-0.2388,-0.1875,0.0096,0.0908,0.0641,0.1156,-0.0256,-0.0307,-0.0297,-0.0361,0.0355,0.0720,-0.0567,-0.0393,-0.0108,0.0196,-0.0708,-0.0048,-0.0467,0.0655,-0.0866,-0.0502,0.0497,-0.0677,-0.0807,-0.0465,-0.0441,0.1237,-0.2009,0.1043,0.1173,0.0171,0.0519,-0.0700,-0.1174,0.0334,-0.0706,-0.0105,0.0346,0.0826,-0.1473,-0.0225,-0.1107,-0.0111,-0.0620,-0.0150,0.0910,-0.0067,-0.0251,-0.0347,-0.0202,-0.0020,0.0846,0.2274,-0.0846,0.0275,0.0317,0.0572,0.0363,0.0937,0.0147,0.0756,0.0083,-0.1138,0.0272,0.0165,0.1716,0.1022,-0.0634,-0.0372,-0.0343,0.0366]];
        const _b2 = [-0.0732,0.0155,-0.0011,-0.0053,0.0180,-0.0105,0.0402,0.0147,-0.0098,0.0631,0.0157,-0.0022,-0.0343,0.0011,0.0360,0.0069,-0.0831,-0.0366,-0.0071,0.0005,0.0789,0.0080,0.0418,0.0101,-0.0020,-0.0080,-0.0132,-0.0316,0.0306,-0.0154,-0.0769,0.0088,-0.0166,0.0009,0.0102,0.0025,0.0094,-0.0111,-0.0076,-0.0060,0.0046,-0.0141,-0.0051,0.0534,0.0166,-0.0615,0.0686,0.0239,0.0352,0.0114,-0.0068,0.0286,-0.0256,-0.0640,0.0047,-0.0034,-0.0238,-0.0224,-0.0057,0.0218,-0.0099,0.0020,-0.0478,-0.0320];
        const _w3 = [[0.2871,-0.2665,-0.3420,-0.2313,-0.2713,0.1453,-0.1802,-0.3566,0.2046,-0.3255,-0.5340,0.0527,0.3357,0.2557,0.1444,0.0789,0.5105,0.3089,0.0987,-0.2654,-0.3694,-0.1782,-0.4721,-0.0879,-0.3540,-0.0562,0.4316,0.6157,-0.1982,-0.2853,0.4806,-0.3082,-0.2525,-0.1446,-0.0153,0.0843,-0.1078,-0.0723,0.0772,-0.2978,0.1686,0.2680,-0.2171,-0.0825,0.2486,0.2554,-0.2711,-0.1822,-0.2165,0.1915,0.1123,-0.3132,0.1501,0.5218,-0.1768,-0.1661,0.2589,-0.1198,-0.1789,-0.3285,-0.3392,-0.2512,0.4316,0.5481]];
        const _b3 = [0.0825];
            const flatW = (mat) => {
                const H = mat.length, W = mat[0].length;
                const o = new Float32Array(H * W);
                for (let i = 0; i < H; i++) {
                    const r = mat[i];
                    for (let j = 0; j < W; j++) o[i*W+j] = r[j];
                }
                return o;
            };
            this._mlpW = {
                IN: _m.length, H1: _w1.length, H2: _w2.length,
                m: Float32Array.from(_m), s: Float32Array.from(_s),
                w1: flatW(_w1), w2: flatW(_w2),
                w3: Float32Array.from(_w3[0]),
                b1: Float32Array.from(_b1), b2: Float32Array.from(_b2),
                b3: _b3[0], yStd: 19.8454, yMean: -0.6042,
            };
            this._mlpBuf = {
                fn: new Float32Array(_m.length),
                h1: new Float32Array(_w1.length),
                h2: new Float32Array(_w2.length),
            };
        }
        const W = this._mlpW, B = this._mlpBuf;
        const fn = B.fn, h1 = B.h1, h2 = B.h2;
        const IN = W.IN, H1 = W.H1, H2 = W.H2;
        const w1 = W.w1, w2 = W.w2, w3 = W.w3;
        const b1 = W.b1, b2 = W.b2;
        // normalize
        const mArr = W.m, sArr = W.s;
        for (let i = 0; i < IN; i++) fn[i] = (f[i] - mArr[i]) / sArr[i];
        // layer 1: ReLU(W1 * fn + b1)
        for (let i = 0; i < H1; i++) {
            let v = b1[i];
            const base = i * IN;
            for (let j = 0; j < IN; j++) v += w1[base + j] * fn[j];
            h1[i] = v > 0 ? v : 0;
        }
        // layer 2: ReLU(W2 * h1 + b2)
        for (let i = 0; i < H2; i++) {
            let v = b2[i];
            const base = i * H1;
            for (let j = 0; j < H1; j++) v += w2[base + j] * h1[j];
            h2[i] = v > 0 ? v : 0;
        }
        // layer 3 + scale
        let score = W.b3;
        for (let j = 0; j < H2; j++) score += w3[j] * h2[j];
        return score * W.yStd + W.yMean;
    }

    _mlpForward(f, W) {
        const _IN = W.means.length;
        const fn = new Array(_IN);
        for (let i = 0; i < _IN; i++) fn[i] = (f[i] - W.means[i]) / W.stds[i];
        const _H1 = W.w1.length;
        const h1 = new Array(_H1);
        for (let i = 0; i < _H1; i++) {
            let v = W.b1[i];
            for (let j = 0; j < _IN; j++) v += W.w1[i][j] * fn[j];
            h1[i] = v > 0 ? v : 0;
        }
        const _H2 = W.w2.length;
        const h2 = new Array(_H2);
        for (let i = 0; i < _H2; i++) {
            let v = W.b2[i];
            for (let j = 0; j < _H1; j++) v += W.w2[i][j] * h1[j];
            h2[i] = v > 0 ? v : 0;
        }
        let score = W.b3[0];
        for (let j = 0; j < _H2; j++) score += W.w3[0][j] * h2[j];
        return score * W.yStd + W.yMean;
    }

    simpleGetType(cards) {
        if (cards.length === 0) return 0;
        const counts = {};
        for (const c of cards) counts[c.rank] = (counts[c.rank] || 0) + 1;
        const mx = Math.max(...Object.values(counts));
        if (mx >= 3) return HAND_TYPE.THREE_OF_A_KIND;
        if (mx >= 2) return HAND_TYPE.PAIR;
        return HAND_TYPE.HIGH_CARD;
    }

    quickRollout(state, currentRound) {
        const gs = state.clone();
        let remaining = gs.getRemainingDeck();
        remaining = shuffleDeck(remaining);
        let deckIdx = 0;

        for (let round = currentRound + 1; round <= 5; round++) {
            if (gs.isComplete()) break;
            const numCards = 3;
            if (deckIdx + numCards > remaining.length) break;
            const dealt = remaining.slice(deckIdx, deckIdx + numCards);
            deckIdx += numCards;

            // 边界: 不可能完成 → 提前退出
            const slotsNow = gs.totalSlots();
            const roundsLeft = 5 - round; // 当前轮之后还有几轮
            const canPlaceAfter = (roundsLeft) * 2; // 之后能放几张
            if (slotsNow > canPlaceAfter + 2) {
                // 即使这轮放2张，后面也放不完 → 无法完成
                return -20;
            }

            // 边界: 只剩1-2个空位, 只需放1张
            if (slotsNow <= 1) {
                const row = gs.midSlots() > 0 ? 'middle' : gs.botSlots() > 0 ? 'bottom' : 'top';
                let bestCard = null, bestS2 = -Infinity;
                for (const c of dealt) {
                    const tmp = gs.clone();
                    tmp.placeCard(c, row);
                    const s = this.trainedEval(tmp);
                    if (s > bestS2) { bestS2 = s; bestCard = c; }
                }
                if (bestCard) {
                    gs.placeCard(bestCard, row);
                    for (const c of dealt) gs.usedCards.add(cardId(c));
                }
                continue;
            }

            // 贪心放置: trainedEval + 结构性约束
            const actions = generateRoundNActions(dealt, gs);
            let best = null, bestS = -Infinity;

            for (const action of actions) {
                const tmp = gs.clone();
                tmp.usedCards.add(cardId(dealt[action.discard]));
                for (let i = 0; i < action.kept.length; i++) {
                    tmp.placeCard(action.kept[i], action.placement[i]);
                }
                let s = this.trainedEval(tmp);

                // 结构性硬约束
                const tmpSlots = tmp.totalSlots();
                const roundsLeft = 5 - round;
                if (tmpSlots > roundsLeft * 2) s -= 200; // 不可能完成

                // 头道2张无对: 锁死追范
                if (tmp.top.length === 2) {
                    const tc = {};
                    for (const c of tmp.top) tc[c.rank] = (tc[c.rank]||0)+1;
                    if (Math.max(...Object.values(tc)) < 2) {
                        const totalPlaced = tmp.top.length + tmp.middle.length + tmp.bottom.length;
                        if (totalPlaced <= 9) s -= 60;
                        else s -= 20;
                    }
                }

                // 一行太早填满
                if (tmp.middle.length === 5 || tmp.bottom.length === 5) {
                    const need = (3 - tmp.top.length) + (5 - tmp.middle.length) + (5 - tmp.bottom.length);
                    if (need > roundsLeft * 2) s -= 100;
                }

                // Outs + 高牌价值
                for (let i = 0; i < action.kept.length; i++) {
                    const card = action.kept[i];
                    const row = action.placement[i];
                    const rowBefore = row === 'top' ? gs.top : row === 'middle' ? gs.middle : gs.bottom;
                    if (rowBefore.some(c => c.rank === card.rank)) {
                        // 配对奖励: 大对子更有价值 (底道KK vs 99差距大)
                        const pairRi = rankIndex(card.rank);
                        s += 15 + pairRi * 2; // KK(11)=37, 99(7)=29, 22(0)=15
                        // 形成两对
                        const rrc = {};
                        for (const c2 of rowBefore) rrc[c2.rank] = (rrc[c2.rank]||0)+1;
                        rrc[card.rank] = (rrc[card.rank]||0)+1;
                        if (Object.values(rrc).filter(v=>v>=2).length >= 2) s += 10;
                    } else {
                        const rankCount = [...gs.top,...gs.middle,...gs.bottom].filter(c => c.rank === card.rank).length;
                        const outs = 3 - rankCount;
                        if (outs <= 0) s -= 3;
                    }
                }
                // 弃牌惩罚: 弃高牌=损失
                const dCard = dealt[action.discard];
                // === 硬规则: rollout 也永不弃鬼 (减法, 全鬼必弃时仍能挑) ===
                if (isJoker(dCard)) s -= 1e6;
                const dri = rankIndex(dCard.rank);
                if (dri >= 12) s -= 6;
                else if (dri >= 11) s -= 4;
                else if (dri >= 10) s -= 3;

                // 头道追范保护: 头道有QKA且只1张时, 放低牌到头道=保留配对空间
                if (gs.top.length === 1) {
                    const topRank = rankIndex(gs.top[0].rank);
                    if (topRank >= 10) { // 头道有QKA
                        for (let i = 0; i < action.kept.length; i++) {
                            if (action.placement[i] === 'top') {
                                const placedRank = rankIndex(action.kept[i].rank);
                                if (placedRank < 10) s += 8; // 低牌上头=好(保留追范)
                            }
                        }
                        // 如果这个动作没放牌到头道 = 头道还是1张 = 灵活性保持
                        const topAfter = tmp.top.length;
                        if (topAfter === 1) s += 5; // 头道没变 = 继续等
                    }
                }

                // === 鬼独顶耐心规则: dealt 含 K/A → 优先放顶配鬼成 KK/AA 进范 ===
                // 不罚顶填非高牌 (保留小三条范路径), 只重奖 K/A 上顶配鬼
                const topHasJoker_qr = gs.top.some(c => isJoker(c));
                if (topHasJoker_qr && gs.top.length < 3) {
                    for (let i = 0; i < action.kept.length; i++) {
                        if (action.placement[i] === 'top') {
                            const kr = rankIndex(action.kept[i].rank);
                            if (kr === 12) s += 60;       // 鬼+A → AA 进范
                            else if (kr === 11) s += 45;  // 鬼+K → KK 进范
                        }
                    }
                }

                if (s > bestS) { bestS = s; best = action; }
            }
            if (best) {
                gs.usedCards.add(cardId(dealt[best.discard]));
                for (let i = 0; i < best.kept.length; i++) {
                    gs.placeCard(best.kept[i], best.placement[i]);
                }
            }
        }

        if (gs.isComplete()) {
            const score = gs.getScore();
            if (score.foul) return -20;
            if (score.fantasy) {
                // 分级范特西评估值 (joker-aware)
                const jokerCnt = gs.top.filter(c => c.rank === 'X').length;
                const realCnts = {};
                for (const c of gs.top) if (c.rank !== 'X') realCnts[c.rank] = (realCnts[c.rank]||0)+1;
                const realMax = Object.values(realCnts).length > 0 ? Math.max(...Object.values(realCnts)) : 0;
                const effMax = realMax + jokerCnt;
                if (effMax >= 3) return score.royalties + 400;  // 三条 (含 [X,X,任意] / [X,A,A] 等)
                // 鬼牌可与任意 rank 配对, 找最高
                let pairR = -1;
                if (jokerCnt >= 1) {
                    for (const r of Object.keys(realCnts)) pairR = Math.max(pairR, rankIndex(r));
                    if (pairR < 0 && jokerCnt >= 2) pairR = 12; // 2鬼自配 默认 AA
                } else {
                    for (const [r, cnt] of Object.entries(realCnts)) { if (cnt >= 2) pairR = Math.max(pairR, rankIndex(r)); }
                }
                if (pairR >= 12) return score.royalties + 200;  // AA
                if (pairR >= 11) return score.royalties + 100;  // KK
                return score.royalties + 50;                    // QQ
            }
            return score.royalties;
        }
        return -10; // 未完成
    }
}

// ============================================================
// MCTS求解器 (使用专家rollout)
// ============================================================

class MCTSSolver {
    constructor(options = {}) {
        this.simulations = options.simulations || 3000;
        this.weights = options.weights || DEFAULT_WEIGHTS;
        this.evaluator = new ExpertEvaluator(this.weights);
        this.rolloutEngine = new ExpertRollout(this.evaluator);
    }

    updateWeights(newWeights) {
        this.weights = { ...this.weights, ...newWeights };
        this.evaluator = new ExpertEvaluator(this.weights);
        this.rolloutEngine = new ExpertRollout(this.evaluator);
    }

    evaluateState(state) {
        if (!state.isComplete()) return this.weights.foulPenalty;
        return this.evaluator.evaluateComplete(state);
    }

    solveRound1(state, cards, progressCallback) {
        const actions = generateRound1Actions(cards, state);
        return this.evaluateActions(state, cards, actions, 1, true, progressCallback);
    }

    solveRoundN(state, cards, round, progressCallback) {
        const actions = generateRoundNActions(cards, state);
        return this.evaluateActions(state, cards, actions, round, false, progressCallback);
    }

    evaluateActions(state, cards, actions, round, isRound1, progressCallback) {
        if (actions.length === 0) return null;

        const uniqueActions = this.deduplicateActions(state, cards, actions, isRound1);

        // 第一阶段: 快速评估所有方案
        const candidates = [];
        for (const action of uniqueActions) {
            const gs = state.clone();
            if (isRound1) {
                for (let i = 0; i < cards.length; i++) {
                    gs.placeCard(cards[i], action[i]);
                }
            } else {
                gs.usedCards.add(cardId(cards[action.discard]));
                for (let i = 0; i < action.kept.length; i++) {
                    gs.placeCard(action.kept[i], action.placement[i]);
                }
            }
            const remaining = gs.getRemainingDeck();
            const quickScore = this.evaluator.evaluatePartial(gs, remaining);
            candidates.push({ action, quickScore, gs });
        }

        // 按快速评分排序, 取top N
        candidates.sort((a, b) => b.quickScore - a.quickScore);
        const topN = Math.min(candidates.length, 25);
        const topCandidates = candidates.slice(0, topN);

        // 第二阶段: MCTS深度评估top N
        const simsPerAction = Math.max(Math.floor(this.simulations / topN), 80);
        const actionScores = new Array(topN).fill(0);

        for (let a = 0; a < topN; a++) {
            const gs = topCandidates[a].gs;

            if (gs.isComplete()) {
                actionScores[a] = this.evaluateState(gs) * simsPerAction;
                continue;
            }

            for (let s = 0; s < simsPerAction; s++) {
                const result = this.rolloutEngine.rollout(gs, round);
                actionScores[a] += this.evaluateState(result);
            }

            if (progressCallback) {
                progressCallback(Math.round(((a + 1) / topN) * 100));
            }
        }

        let bestIdx = 0;
        for (let i = 1; i < topN; i++) {
            if (actionScores[i] > actionScores[bestIdx]) bestIdx = i;
        }

        const results = topCandidates.map((c, idx) => ({
            action: c.action,
            avgScore: actionScores[idx] / simsPerAction,
            quickScore: c.quickScore,
            totalSims: simsPerAction
        }));
        results.sort((a, b) => b.avgScore - a.avgScore);

        return {
            best: topCandidates[bestIdx].action,
            bestScore: actionScores[bestIdx] / simsPerAction,
            results: results.slice(0, 10),
            totalActions: uniqueActions.length,
            isRound1
        };
    }

    deduplicateActions(state, cards, actions, isRound1) {
        const seen = new Set();
        const unique = [];
        for (const action of actions) {
            const gs = state.clone();
            let key;
            if (isRound1) {
                for (let i = 0; i < cards.length; i++) {
                    gs.placeCard(cards[i], action[i]);
                }
                key = this.stateKey(gs);
            } else {
                for (let i = 0; i < action.kept.length; i++) {
                    gs.placeCard(action.kept[i], action.placement[i]);
                }
                key = cardId(cards[action.discard]) + '|' + this.stateKey(gs);
            }
            if (!seen.has(key)) {
                seen.add(key);
                unique.push(action);
            }
        }
        return unique;
    }

    stateKey(state) {
        const sortCards = cs => cs.map(cardId).sort().join(',');
        return `${sortCards(state.top)}|${sortCards(state.middle)}|${sortCards(state.bottom)}`;
    }
}

// ============================================================
// 单局模拟 (用于训练)
// ============================================================

function simulateOneGame(weights, simCount) {
    // simCount <= 0: 纯贪心模式(极快, 用于训练)
    // simCount > 0: MCTS模式(较慢, 用于正式对局)
    const useGreedy = !simCount || simCount <= 0;

    const evaluator = new ExpertEvaluator(weights || DEFAULT_WEIGHTS);
    const rolloutEngine = new ExpertRollout(evaluator);
    let solver = null;
    if (!useGreedy) {
        solver = new MCTSSolver({ simulations: simCount, weights });
    }

    const state = new GameState();
    state.deck = shuffleDeck(createDeck());
    const roundData = [];

    for (let round = 1; round <= 5; round++) {
        const numCards = (round === 1) ? 5 : 3;
        const remaining = state.getRemainingDeck();
        const shuffled = shuffleDeck(remaining);
        const dealt = shuffled.slice(0, numCards);
        state.round = round;

        let discarded = null;

        if (useGreedy) {
            // 纯贪心: 穷举动作, 用evaluator打分, 选最优
            if (round === 1) {
                rolloutEngine.expertPlace5(state, dealt);
            } else {
                rolloutEngine.expertPlace3(state, dealt);
                // 找出被弃的牌
                for (const c of dealt) {
                    const cid = cardId(c);
                    let found = false;
                    for (const r of [...state.top, ...state.middle, ...state.bottom]) {
                        if (cardId(r) === cid) { found = true; break; }
                    }
                    if (!found) { discarded = c; break; }
                }
            }
        } else {
            // MCTS模式
            let result;
            if (round === 1) {
                result = solver.solveRound1(state, dealt);
            } else {
                result = solver.solveRoundN(state, dealt, round);
            }
            if (!result) break;

            if (result.isRound1) {
                for (let i = 0; i < dealt.length; i++) {
                    state.placeCard(dealt[i], result.best[i]);
                }
            } else {
                discarded = dealt[result.best.discard];
                state.usedCards.add(cardId(discarded));
                for (let i = 0; i < result.best.kept.length; i++) {
                    state.placeCard(result.best.kept[i], result.best.placement[i]);
                }
            }
        }

        roundData.push({ round, dealt: dealt.map(c => ({...c})), discarded });
    }

    const finalScore = state.isComplete() ? state.getScore() : { foul: true, royalties: 0, fantasy: false };

    return {
        top: state.top.map(c => ({...c})),
        middle: state.middle.map(c => ({...c})),
        bottom: state.bottom.map(c => ({...c})),
        score: finalScore,
        rounds: roundData
    };
}

// ============================================================
// 3人对局模拟 (v4_more)
// ============================================================

function simulate3PlayerGame(weights) {
    const game = new ThreePlayerGame();
    const evaluator = new ExpertEvaluator(weights || DEFAULT_WEIGHTS);
    const engines = [
        new ExpertRollout(evaluator),
        new ExpertRollout(evaluator),
        new ExpertRollout(evaluator),
    ];

    for (let round = 1; round <= 5; round++) {
        const numCards = (round === 1) ? 5 : 3;

        // 1. 同时发牌给3人 (发牌在行动前)
        const hands = [];
        for (let p = 0; p < 3; p++) {
            hands.push(game.dealCards(p, numCards));
            game.players[p].round = round;
        }

        // 2. 记录每人行动前的可见牌快照
        //    P0行动时: 只看到上一轮结束时的场上牌
        //    P1行动时: 看到上一轮 + P0本轮放的牌
        //    P2行动时: 看到上一轮 + P0和P1本轮放的牌

        // 3. 按座位顺序依次行动
        for (let p = 0; p < 3; p++) {
            const dealt = hands[p];
            const state = game.players[p];

            // 该玩家的可见信息 = 场上所有已放的牌 + 自己弃过的牌
            // (不包括其他人的弃牌, 不包括还没行动的人本轮的放牌)
            const visibleExclude = new Set(game.visibleCards);
            for (const cid of state.usedCards) visibleExclude.add(cid);

            // 临时让 getRemainingDeck 返回基于可见信息的剩余牌
            const origGetRemaining = state.getRemainingDeck;
            state.getRemainingDeck = function() {
                return createDeck().filter(c => !visibleExclude.has(cardId(c)));
            };

            // 记录行动前的牌面
            const beforeTop = state.top.length;
            const beforeMid = state.middle.length;
            const beforeBot = state.bottom.length;

            if (round === 1) {
                engines[p].expertPlace5(state, dealt);
            } else {
                engines[p].expertPlace3(state, dealt);
            }

            // 恢复
            state.getRemainingDeck = origGetRemaining;

            // 更新可见牌: 只添加本轮新放的牌 (不含弃牌!)
            for (let i = beforeTop; i < state.top.length; i++) {
                game.visibleCards.add(cardId(state.top[i]));
            }
            for (let i = beforeMid; i < state.middle.length; i++) {
                game.visibleCards.add(cardId(state.middle[i]));
            }
            for (let i = beforeBot; i < state.bottom.length; i++) {
                game.visibleCards.add(cardId(state.bottom[i]));
            }

            // 弃牌: 标记到玩家自己的usedCards (只有自己知道)
            if (round > 1) {
                for (const c of dealt) {
                    const cid = cardId(c);
                    const placed = [...state.top, ...state.middle, ...state.bottom];
                    if (!placed.some(pc => cardId(pc) === cid)) {
                        state.usedCards.add(cid);
                    }
                }
            }
        }
    }

    // 返回3人结果
    const results = [];
    for (let p = 0; p < 3; p++) {
        const state = game.players[p];
        const score = state.isComplete() ? state.getScore() : { foul: true, royalties: 0, fantasy: false };
        results.push({
            player: p,
            top: state.top.map(c => ({...c})),
            middle: state.middle.map(c => ({...c})),
            bottom: state.bottom.map(c => ({...c})),
            score
        });
    }
    return { results, visibleCards: game.getVisibleCards() };
}
