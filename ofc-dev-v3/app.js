// ============================================================
// 大菠萝 (Pineapple OFC) - Application Logic
// ============================================================

let gameState = new GameState();
let solver = new MCTSSolver({ simulations: 3000 });
let gameHistory = [];
let currentDealt = [];
let autoMode = false;
let autoLogs = []; // 自动对局日志
let trainWorker = null;
let trainRunning = false;

// ============================================================
// UI Rendering
// ============================================================

function renderCard(card, small = false) {
    const color = SUIT_COLORS[card.suit];
    const cls = small ? 'card card-small' : 'card';
    return `<div class="${cls}" style="color:${color}" data-card="${cardId(card)}">
        <span class="card-rank">${RANK_DISPLAY[card.rank]}</span>
        <span class="card-suit">${SUIT_SYMBOLS[card.suit]}</span>
    </div>`;
}

function renderCardSlot() {
    return '<div class="card card-slot"></div>';
}

function renderRow(name, cards, maxCards, label, royalty = 0, foulWarning = false) {
    let html = `<div class="row-container">
        <div class="row-label">${label}`;
    if (royalty > 0 && !foulWarning) {
        html += ` <span class="royalty-badge">+${royalty}</span>`;
    } else if (foulWarning && name === 'top') {
        html += ` <span class="foul-warning-badge">犯规风险</span>`;
    }
    html += `</div><div class="row-cards" id="row-${name}">`;
    for (let i = 0; i < maxCards; i++) {
        if (i < cards.length) {
            html += renderCard(cards[i]);
        } else {
            html += renderCardSlot();
        }
    }
    html += '</div></div>';
    return html;
}

function updateBoard() {
    const complete = gameState.isComplete();
    const score = complete ? gameState.getScore() : null;

    // Check for in-progress foul warning
    let inProgressFoul = false;
    if (!complete) {
        inProgressFoul = checkPartialFoul(gameState);
    }

    // Only show royalties if complete and not foul
    const topR = score && !score.foul ? score.topRoyalty : 0;
    const midR = score && !score.foul ? score.midRoyalty : 0;
    const botR = score && !score.foul ? score.botRoyalty : 0;

    // Show preview royalty during play (but grayed out if potential foul)
    let previewTopR = 0;
    if (!complete && gameState.top.length === 3) {
        previewTopR = getTopRoyalty(gameState.top);
    }

    const boardHtml =
        renderRow('top', gameState.top, 3, '头道 (Top)', complete ? topR : previewTopR, inProgressFoul) +
        renderRow('middle', gameState.middle, 5, '中道 (Middle)', midR) +
        renderRow('bottom', gameState.bottom, 5, '底道 (Bottom)', botR);

    document.getElementById('board').innerHTML = boardHtml;

    // Update score display
    let scoreHtml = '';
    if (score) {
        if (score.foul) {
            scoreHtml = '<div class="score-foul">犯规(炸)!</div>';
        } else {
            scoreHtml = `<div class="score-info">
                <div>头道: ${score.topEval.name} ${topR > 0 ? `<span class="royalty-badge">+${topR}</span>` : ''}</div>
                <div>中道: ${score.midEval.name} ${midR > 0 ? `<span class="royalty-badge">+${midR}</span>` : ''}</div>
                <div>底道: ${score.botEval.name} ${botR > 0 ? `<span class="royalty-badge">+${botR}</span>` : ''}</div>
                <div class="score-total">总分: ${score.royalties}</div>
                ${score.fantasy ? '<div class="fantasy-badge">进入范特西!</div>' : ''}
            </div>`;
        }
    } else if (inProgressFoul) {
        scoreHtml = '<div class="score-foul" style="font-size:14px;">⚠ 当前牌型有犯规风险!</div>';
    }
    document.getElementById('score-display').innerHTML = scoreHtml;
}

// Check if partially placed cards already violate ordering
function checkPartialFoul(state) {
    const { top, middle, bottom } = state;

    // Check middle vs top (if both have enough cards to evaluate type)
    if (top.length === 3 && middle.length >= 3) {
        const topEval = evaluate3(top);
        // For partial middle, check if current cards can still beat top
        if (middle.length === 5) {
            const midEval = evaluate5(middle);
            if (midEval.type < topEval.type) return true;
            if (midEval.type === topEval.type && midEval.type === HAND_TYPE.PAIR) {
                const topRanks = top.map(c => rankIndex(c.rank));
                const topCounts = {};
                for (const r of topRanks) topCounts[r] = (topCounts[r] || 0) + 1;
                const topPair = parseInt(Object.entries(topCounts).find(([r,c]) => c===2)[0]);

                const midRanks = middle.map(c => rankIndex(c.rank));
                const midCounts = {};
                for (const r of midRanks) midCounts[r] = (midCounts[r] || 0) + 1;
                const midPairEntry = Object.entries(midCounts).find(([r,c]) => c>=2);
                if (midPairEntry) {
                    const midPair = parseInt(midPairEntry[0]);
                    if (midPair < topPair) return true;
                }
            }
            if (midEval.type === HAND_TYPE.HIGH_CARD && topEval.type >= HAND_TYPE.PAIR) return true;
        }
    }

    // Check middle vs bottom (both 5-card)
    if (middle.length === 5 && bottom.length === 5) {
        const midEval = evaluate5(middle);
        const botEval = evaluate5(bottom);
        if (midEval.value > botEval.value) return true;
    }

    return false;
}

function updateDealtCards(cards, discardIdx = -1) {
    let html = '<div class="dealt-title">发牌:</div><div class="dealt-cards">';
    for (let i = 0; i < cards.length; i++) {
        const cls = i === discardIdx ? 'card card-discarded' : 'card';
        const color = SUIT_COLORS[cards[i].suit];
        html += `<div class="${cls}" style="color:${color}">
            <span class="card-rank">${RANK_DISPLAY[cards[i].rank]}</span>
            <span class="card-suit">${SUIT_SYMBOLS[cards[i].suit]}</span>
        </div>`;
    }
    html += '</div>';
    document.getElementById('dealt-area').innerHTML = html;
}

function clearDealtCards() {
    document.getElementById('dealt-area').innerHTML = '';
}

function addHistoryEntry(round, dealt, discarded, placement, state) {
    const entry = { round, dealt, discarded, placement, state: state.clone() };
    gameHistory.push(entry);
    renderHistory();
}

function renderHistory() {
    let html = '';
    for (const entry of gameHistory) {
        html += `<div class="history-entry">
            <div class="history-round">第${entry.round}轮</div>
            <div class="history-detail">
                <span class="history-label">发牌:</span>
                ${entry.dealt.map(c => `<span class="history-card" style="color:${SUIT_COLORS[c.suit]}">${cardStr(c)}</span>`).join(' ')}
                ${entry.discarded ? `<span class="history-label">弃牌:</span><span class="history-card discarded-text">${cardStr(entry.discarded)}</span>` : ''}
            </div>
            <div class="history-state">
                <span class="history-label">头道:</span> [${entry.state.top.map(cardStr).join(',')}]
                <span class="history-label">中道:</span> [${entry.state.middle.map(cardStr).join(',')}]
                <span class="history-label">底道:</span> [${entry.state.bottom.map(cardStr).join(',')}]
            </div>
        </div>`;
    }
    document.getElementById('history').innerHTML = html;
}

// ============================================================
// Solver Integration
// ============================================================

async function runSolver(round, cards) {
    const statusEl = document.getElementById('solver-status');
    statusEl.innerHTML = '<div class="solving">求解中...</div>';
    const solveBtn = document.getElementById('btn-solve');
    if (solveBtn) solveBtn.disabled = true;

    // 降低模拟次数避免卡浏览器 (500次够用)
    const savedSims = solver.simulations;
    solver.simulations = Math.min(solver.simulations, 500);

    return new Promise(resolve => {
        setTimeout(() => {
            let result;
            if (round === 1) {
                result = solver.solveRound1(gameState, cards);
            } else {
                result = solver.solveRoundN(gameState, cards, round);
            }

            solver.simulations = savedSims;

            if (result) {
                displaySolverResult(result, cards, round);
            }

            statusEl.innerHTML = '<div class="solved">求解完成!</div>';
            if (solveBtn) solveBtn.disabled = false;
            resolve(result);
        }, 50);
    });
}

function displaySolverResult(result, cards, round) {
    let html = '<div class="solver-results">';
    html += `<div class="solver-header">MCTS分析 (共${result.totalActions}种方案, 展示前${Math.min(10, result.results.length)})</div>`;
    html += '<table class="solver-table"><tr><th>排名</th><th>方案</th><th>期望得分</th></tr>';

    for (let i = 0; i < result.results.length; i++) {
        const r = result.results[i];
        const isBest = i === 0;
        let desc;

        if (result.isRound1) {
            desc = cards.map((c, idx) => `${cardStr(c)}→${rowLabel(r.action[idx])}`).join(' ');
        } else {
            const discardCard = cards[r.action.discard];
            desc = `弃${cardStr(discardCard)} | ` +
                r.action.kept.map((c, idx) => `${cardStr(c)}→${rowLabel(r.action.placement[idx])}`).join(' ');
        }

        html += `<tr class="${isBest ? 'best-action' : ''}">
            <td>${i + 1}</td>
            <td>${desc}</td>
            <td>${r.avgScore.toFixed(1)}</td>
        </tr>`;
    }

    html += '</table></div>';
    document.getElementById('solver-output').innerHTML = html;
}

function rowLabel(row) {
    return { top: '头', middle: '中', bottom: '底' }[row] || row;
}

// ============================================================
// Game Control
// ============================================================

function newGame() {
    gameState = new GameState();
    gameHistory = [];
    currentDealt = [];
    gameState.deck = shuffleDeck(createDeck());
    gameState.round = 0;

    updateBoard();
    clearDealtCards();
    document.getElementById('history').innerHTML = '';
    document.getElementById('solver-output').innerHTML = '';
    document.getElementById('solver-status').innerHTML = '';
    document.getElementById('round-info').textContent = '新游戏 - 点击"发牌"开始';

    enableControls(true);
}

function dealCards() {
    if (gameState.isComplete()) {
        alert('游戏已结束!');
        return;
    }

    gameState.round++;
    const round = gameState.round;

    if (round > 5) {
        alert('已经是第5轮了!');
        return;
    }

    const numCards = (round === 1) ? 5 : 3;
    const remaining = gameState.getRemainingDeck();
    const shuffled = shuffleDeck(remaining);
    currentDealt = shuffled.slice(0, numCards);

    document.getElementById('round-info').textContent = `第${round}轮 - 发牌${numCards}张`;
    updateDealtCards(currentDealt);
    document.getElementById('solver-output').innerHTML = '';

    return currentDealt;
}

async function solveAndApply() {
    if (currentDealt.length === 0) {
        alert('请先发牌!');
        return;
    }

    const round = gameState.round;
    const result = await runSolver(round, currentDealt);

    if (!result) {
        alert('无法求解!');
        return;
    }

    // Apply best action
    let discarded = null;
    if (result.isRound1) {
        for (let i = 0; i < currentDealt.length; i++) {
            gameState.placeCard(currentDealt[i], result.best[i]);
        }
    } else {
        discarded = currentDealt[result.best.discard];
        gameState.usedCards.add(cardId(discarded));
        for (let i = 0; i < result.best.kept.length; i++) {
            gameState.placeCard(result.best.kept[i], result.best.placement[i]);
        }
        updateDealtCards(currentDealt, result.best.discard);
    }

    addHistoryEntry(round, currentDealt, discarded, result.best, gameState);
    updateBoard();
    currentDealt = [];

    if (gameState.isComplete()) {
        document.getElementById('round-info').textContent = '游戏结束!';
        const score = gameState.getScore();
        document.getElementById('solver-status').innerHTML = score.fantasy
            ? '<div class="fantasy-result">恭喜进入范特西!</div>'
            : `<div class="solved">最终得分: ${score.royalties}</div>`;
    }
}

async function autoPlay() {
    newGame();
    autoMode = true;

    // 用纯贪心模式(不跑MCTS), 秒级完成
    const evaluator = new ExpertEvaluator(solver.weights);
    const rollout = new ExpertRollout(evaluator);

    const logEntry = {
        id: autoLogs.length + 1,
        time: new Date().toLocaleString('zh-CN'),
        rounds: [],
        result: null
    };

    for (let round = 1; round <= 5; round++) {
        if (!autoMode) break;
        gameState.round = round;
        const numCards = (round === 1) ? 5 : 3;
        const remaining = gameState.getRemainingDeck();
        const shuffled = shuffleDeck(remaining);
        currentDealt = shuffled.slice(0, numCards);
        const dealtCopy = currentDealt.map(c => ({...c}));

        document.getElementById('round-info').textContent = `自动对局 - 第${round}轮`;
        updateDealtCards(currentDealt);

        // 贪心求解
        let discarded = null;
        if (round === 1) {
            rollout.expertPlace5(gameState, currentDealt);
        } else {
            rollout.expertPlace3(gameState, currentDealt);
            for (const c of currentDealt) {
                let found = false;
                for (const r of [...gameState.top, ...gameState.middle, ...gameState.bottom]) {
                    if (r.rank === c.rank && r.suit === c.suit) { found = true; break; }
                }
                if (!found) { discarded = c; break; }
            }
        }

        const roundLog = {
            round,
            dealt: dealtCopy,
            discarded,
            top: [...gameState.top],
            middle: [...gameState.middle],
            bottom: [...gameState.bottom]
        };
        logEntry.rounds.push(roundLog);

        addHistoryEntry(round, dealtCopy, discarded, null, gameState);
        updateBoard();
        if (discarded) {
            updateDealtCards(currentDealt, currentDealt.findIndex(c => c.rank === discarded.rank && c.suit === discarded.suit));
        }

        await new Promise(r => setTimeout(r, 150));
    }

    currentDealt = [];

    if (gameState.isComplete()) {
        logEntry.result = gameState.getScore();
        logEntry.top = [...gameState.top];
        logEntry.middle = [...gameState.middle];
        logEntry.bottom = [...gameState.bottom];

        const score = gameState.getScore();
        document.getElementById('round-info').textContent = '自动对局完成!';
        document.getElementById('solver-status').innerHTML = score.foul
            ? '<div class="score-foul">犯规!</div>'
            : score.fantasy
                ? '<div class="fantasy-result">进入范特西!</div>'
                : `<div class="solved">得分: ${score.royalties}</div>`;
    }

    autoLogs.push(logEntry);
    renderAutoLog();
    autoMode = false;
}

async function batchAutoPlay() {
    const count = parseInt(document.getElementById('batch-count').value) || 10;
    autoMode = true;
    document.getElementById('batch-status').textContent = `批量对局中... 0/${count}`;

    const evaluator = new ExpertEvaluator(solver.weights);
    const rolloutEng = new ExpertRollout(evaluator);

    for (let i = 0; i < count; i++) {
        if (!autoMode) break;

        const gs = new GameState();
        const logEntry = {
            id: autoLogs.length + 1,
            time: new Date().toLocaleString('zh-CN'),
            rounds: [],
            result: null
        };

        for (let round = 1; round <= 5; round++) {
            gs.round = round;
            const numCards = (round === 1) ? 5 : 3;
            const remaining = gs.getRemainingDeck();
            const shuffled = shuffleDeck(remaining);
            const dealt = shuffled.slice(0, numCards);

            let discarded = null;
            if (round === 1) {
                rolloutEng.expertPlace5(gs, dealt);
            } else {
                rolloutEng.expertPlace3(gs, dealt);
                for (const c of dealt) {
                    let found = false;
                    for (const r of [...gs.top, ...gs.middle, ...gs.bottom]) {
                        if (r.rank === c.rank && r.suit === c.suit) { found = true; break; }
                    }
                    if (!found) { discarded = c; break; }
                }
            }

            logEntry.rounds.push({
                round,
                dealt: dealt.map(c => ({...c})),
                discarded,
                top: [...gs.top],
                middle: [...gs.middle],
                bottom: [...gs.bottom]
            });
        }

        if (gs.isComplete()) {
            logEntry.result = gs.getScore();
            logEntry.top = [...gs.top];
            logEntry.middle = [...gs.middle];
            logEntry.bottom = [...gs.bottom];
        }

        autoLogs.push(logEntry);
        document.getElementById('batch-status').textContent = `批量对局中... ${i + 1}/${count}`;

        // Yield to UI every 5 games
        if (i % 5 === 0) await new Promise(r => setTimeout(r, 0));
    }

    updateBoard();
    renderAutoLog();
    document.getElementById('batch-status').textContent = autoMode ? `批量完成! ${count}局` : '已停止';
    autoMode = false;
}

function stopAuto() {
    autoMode = false;
}

function clearAutoLogs() {
    autoLogs = [];
    renderAutoLog();
}

function renderAutoLog() {
    const container = document.getElementById('auto-log');
    if (!container) return;

    // Statistics
    const total = autoLogs.length;
    const fouls = autoLogs.filter(l => l.result && l.result.foul).length;
    const fantasies = autoLogs.filter(l => l.result && l.result.fantasy).length;
    const valid = autoLogs.filter(l => l.result && !l.result.foul);
    const avgScore = valid.length > 0
        ? (valid.reduce((s, l) => s + l.result.royalties, 0) / valid.length).toFixed(1)
        : 0;

    let html = `<div class="log-stats">
        <span>总局数: <b>${total}</b></span>
        <span>犯规: <b class="stat-foul">${fouls}</b> (${total ? ((fouls/total)*100).toFixed(1) : 0}%)</span>
        <span>进范: <b class="stat-fantasy">${fantasies}</b> (${total ? ((fantasies/total)*100).toFixed(1) : 0}%)</span>
        <span>平均分: <b class="stat-score">${avgScore}</b></span>
    </div>`;

    // Log entries (most recent first, limit 50)
    const recent = [...autoLogs].reverse().slice(0, 50);
    html += '<div class="log-entries">';
    for (const log of recent) {
        const resultClass = !log.result ? 'log-incomplete'
            : log.result.foul ? 'log-foul'
            : log.result.fantasy ? 'log-fantasy' : 'log-normal';

        const resultText = !log.result ? '未完成'
            : log.result.foul ? '犯规'
            : log.result.fantasy ? `进范! (+${log.result.royalties})`
            : `得分: ${log.result.royalties}`;

        html += `<div class="log-entry ${resultClass}" onclick="expandLog(${log.id})">
            <span class="log-id">#${log.id}</span>
            <span class="log-time">${log.time}</span>
            <span class="log-result">${resultText}</span>
            ${log.top ? `<span class="log-hands">
                头[${log.top.map(cardStr).join(',')}]
                中[${log.middle.map(cardStr).join(',')}]
                底[${log.bottom.map(cardStr).join(',')}]
            </span>` : ''}
        </div>`;

        // Expandable detail
        html += `<div class="log-detail" id="log-detail-${log.id}" style="display:none;">`;
        for (const rd of log.rounds) {
            html += `<div class="log-round">
                R${rd.round}: 发[${rd.dealt.map(cardStr).join(',')}]
                ${rd.discarded ? ` 弃${cardStr(rd.discarded)}` : ''}
                → 头[${rd.top.map(cardStr).join(',')}] 中[${rd.middle.map(cardStr).join(',')}] 底[${rd.bottom.map(cardStr).join(',')}]
            </div>`;
        }
        html += '</div>';
    }
    html += '</div>';

    container.innerHTML = html;
}

function expandLog(id) {
    const el = document.getElementById(`log-detail-${id}`);
    if (el) {
        el.style.display = el.style.display === 'none' ? 'block' : 'none';
    }
}

function exportLogs() {
    const data = JSON.stringify(autoLogs, (key, val) => {
        if (key === 'result' && val && val.topEval) {
            return { foul: val.foul, fantasy: val.fantasy, royalties: val.royalties, score: val.score };
        }
        return val;
    }, 2);
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `pineapple-ofc-log-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(url);
}

// ============================================================
// Test Data
// ============================================================

function loadTestData() {
    gameState = new GameState();
    gameHistory = [];
    currentDealt = [];
    gameState.round = 0;

    document.getElementById('history').innerHTML = '';
    document.getElementById('solver-output').innerHTML = '';
    document.getElementById('solver-status').innerHTML = '';

    const testData = [
        {
            round: 1,
            dealt: ['Jc','9h','8h','6c','3d'],
            placements: [
                { card: '8h', row: 'top' },
                { card: '6c', row: 'middle' },
                { card: '3d', row: 'middle' },
                { card: 'Jc', row: 'bottom' },
                { card: '9h', row: 'bottom' }
            ]
        },
        {
            round: 2,
            dealt: ['Ad','5h','4s'],
            discard: '4s',
            placements: [
                { card: 'Ad', row: 'middle' },
                { card: '5h', row: 'middle' }
            ]
        },
        {
            round: 3,
            dealt: ['Qc','7h','3h'],
            discard: 'Qc',
            placements: [
                { card: '3h', row: 'middle' },
                { card: '7h', row: 'bottom' }
            ]
        },
        {
            round: 4,
            dealt: ['8d','6h','5d'],
            discard: '6h',
            placements: [
                { card: '5d', row: 'top' },
                { card: '8d', row: 'bottom' }
            ]
        },
        {
            round: 5,
            dealt: ['Qh','4h','3c'],
            discard: '4h',
            placements: [
                { card: '3c', row: 'top' },
                { card: 'Qh', row: 'bottom' }
            ]
        }
    ];

    runTestData(testData);
}

async function runTestData(testData) {
    for (const round of testData) {
        gameState.round = round.round;
        const dealt = round.dealt.map(parseCard);

        // Apply placements
        let discarded = null;
        if (round.discard) {
            discarded = parseCard(round.discard);
            gameState.usedCards.add(cardId(discarded));
        }

        for (const p of round.placements) {
            const card = parseCard(p.card);
            gameState.placeCard(card, p.row);
        }

        addHistoryEntry(round.round, dealt, discarded, null, gameState);
        updateBoard();
        updateDealtCards(dealt, round.discard ? round.dealt.indexOf(round.discard) : -1);

        document.getElementById('round-info').textContent = `测试数据 - 第${round.round}轮`;

        await new Promise(r => setTimeout(r, 500));
    }

    document.getElementById('round-info').textContent = '测试数据回放完成';

    // Now run solver analysis on the final state
    const score = gameState.getScore();
    let analysis = '<div class="test-analysis">';
    analysis += '<h3>测试数据分析</h3>';

    if (score.foul) {
        analysis += '<div class="score-foul">结果: 犯规!</div>';
    } else {
        analysis += `<div class="score-info">
            <div>头道: [${gameState.top.map(cardStr).join(', ')}] - ${score.topEval.name} ${score.topRoyalty > 0 ? `(+${score.topRoyalty})` : ''}</div>
            <div>中道: [${gameState.middle.map(cardStr).join(', ')}] - ${score.midEval.name} ${score.midRoyalty > 0 ? `(+${score.midRoyalty})` : ''}</div>
            <div>底道: [${gameState.bottom.map(cardStr).join(', ')}] - ${score.botEval.name} ${score.botRoyalty > 0 ? `(+${score.botRoyalty})` : ''}</div>
            <div class="score-total">总Royalty: ${score.royalties}</div>
            <div>${score.fantasy ? '<span class="fantasy-badge">进入范特西!</span>' : '未进范特西'}</div>
        </div>`;
    }

    // Compare with solver recommendation
    analysis += '<h3>MCTS推荐对比</h3>';
    analysis += '<div class="compare-note">正在为每轮生成MCTS推荐...</div>';
    analysis += '</div>';

    document.getElementById('solver-output').innerHTML = analysis;

    // Run solver for each round of test data
    await runTestComparison(testData);
}

async function runTestComparison(testData) {
    const tempState = new GameState();
    let html = '<div class="test-analysis"><h3>MCTS逐轮推荐对比</h3>';

    for (const round of testData) {
        const dealt = round.dealt.map(parseCard);

        // Get solver recommendation
        const result = round.round === 1
            ? solver.solveRound1(tempState, dealt)
            : solver.solveRoundN(tempState, dealt, round.round);

        html += `<div class="compare-round">`;
        html += `<div class="compare-round-title">第${round.round}轮 发牌: ${dealt.map(cardStr).join(' ')}</div>`;

        if (result && result.results.length > 0) {
            const best = result.results[0];
            let recDesc;
            if (result.isRound1) {
                recDesc = dealt.map((c, i) => `${cardStr(c)}→${rowLabel(best.action[i])}`).join(' ');
            } else {
                const discardCard = dealt[best.action.discard];
                recDesc = `弃${cardStr(discardCard)} | ` +
                    best.action.kept.map((c, i) => `${cardStr(c)}→${rowLabel(best.action.placement[i])}`).join(' ');
            }
            html += `<div class="compare-rec">MCTS推荐: ${recDesc} (期望分: ${best.avgScore.toFixed(1)})</div>`;
        }

        // What was actually played
        let actualDesc;
        if (round.round === 1) {
            actualDesc = round.placements.map(p => {
                const c = parseCard(p.card);
                return `${cardStr(c)}→${rowLabel(p.row)}`;
            }).join(' ');
        } else {
            actualDesc = `弃${cardStr(parseCard(round.discard))} | ` +
                round.placements.map(p => {
                    const c = parseCard(p.card);
                    return `${cardStr(c)}→${rowLabel(p.row)}`;
                }).join(' ');
        }
        html += `<div class="compare-actual">实际操作: ${actualDesc}</div>`;
        html += '</div>';

        // Apply to temp state
        if (round.discard) {
            tempState.usedCards.add(cardId(parseCard(round.discard)));
        }
        for (const p of round.placements) {
            const card = parseCard(p.card);
            tempState.placeCard(card, p.row);
        }
        tempState.round = round.round;
    }

    html += '</div>';
    document.getElementById('solver-output').innerHTML = html;
}

// ============================================================
// Custom Test Input
// ============================================================

function parseCustomTest() {
    const input = document.getElementById('custom-test-input').value.trim();
    if (!input) {
        alert('请输入测试数据!');
        return;
    }

    try {
        const testData = parseTestInput(input);
        gameState = new GameState();
        gameHistory = [];
        currentDealt = [];
        document.getElementById('history').innerHTML = '';
        document.getElementById('solver-output').innerHTML = '';
        runTestData(testData);
    } catch (e) {
        alert('解析错误: ' + e.message);
    }
}

function parseTestInput(text) {
    const lines = text.split('\n').filter(l => l.trim());
    const rounds = [];
    let currentRound = null;

    for (const line of lines) {
        const dealMatch = line.match(/第(\d+)轮\s*发牌\s*\[([^\]]+)\]/);
        const discardMatch = line.match(/弃牌\s*\[([^\]]+)\]/);

        if (dealMatch) {
            const roundNum = parseInt(dealMatch[1]);
            const cards = dealMatch[2].split(',').map(s => s.trim());

            if (currentRound && currentRound.round === roundNum) {
                // Same round, add discard info
                if (discardMatch) {
                    currentRound.discard = discardMatch[1].trim();
                }
            } else {
                currentRound = {
                    round: roundNum,
                    dealt: cards,
                    placements: []
                };
                if (discardMatch) {
                    currentRound.discard = discardMatch[1].trim();
                }
                rounds.push(currentRound);
            }
        }

        // Parse state lines like: 第1轮 [8h] [6c,3d] [Jc,9h]
        const stateMatch = line.match(/^第(\d+)轮\s+\[([^\]]*)\]\s+\[([^\]]*)\]\s+\[([^\]]*)\]/);
        if (stateMatch && !dealMatch) {
            const roundNum = parseInt(stateMatch[1]);
            const round = rounds.find(r => r.round === roundNum);
            if (round) {
                const topCards = stateMatch[2] ? stateMatch[2].split(',').map(s => s.trim()).filter(Boolean) : [];
                const midCards = stateMatch[3] ? stateMatch[3].split(',').map(s => s.trim()).filter(Boolean) : [];
                const botCards = stateMatch[4] ? stateMatch[4].split(',').map(s => s.trim()).filter(Boolean) : [];

                // Determine placements based on what's new
                round.placements = [];
                const prevRound = rounds.find(r => r.round === roundNum - 1);

                const getPrevCards = (row) => {
                    if (!prevRound) return [];
                    // Find the state line for previous round
                    return []; // Will be computed differently
                };

                // For simplicity, compute placements from dealt cards + state
                const allPlaced = [...topCards, ...midCards, ...botCards];
                const dealtSet = new Set(round.dealt);
                const discardSet = round.discard ? new Set([round.discard]) : new Set();

                for (const c of round.dealt) {
                    if (discardSet.has(c)) continue;
                    if (topCards.includes(c)) {
                        round.placements.push({ card: c, row: 'top' });
                    } else if (midCards.includes(c)) {
                        round.placements.push({ card: c, row: 'middle' });
                    } else if (botCards.includes(c)) {
                        round.placements.push({ card: c, row: 'bottom' });
                    }
                }
            }
        }
    }

    return rounds;
}

// ============================================================
// Settings
// ============================================================

function updateSimulations() {
    const val = parseInt(document.getElementById('sim-count').value);
    if (val > 0) {
        solver.simulations = val;
    }
}

function enableControls(enabled) {
    document.querySelectorAll('.ctrl-btn').forEach(btn => {
        btn.disabled = !enabled;
    });
}

// ============================================================
// 训练系统
// ============================================================

function startTraining() {
    if (trainRunning) { alert('训练已在运行!'); return; }

    const generations = parseInt(document.getElementById('train-generations').value) || 50;
    const popSize = parseInt(document.getElementById('train-popsize').value) || 8;
    const gamesPerEval = parseInt(document.getElementById('train-games').value) || 30;
    const simCount = parseInt(document.getElementById('train-simcount').value) || 150;
    const targetMinutes = parseInt(document.getElementById('train-minutes').value) || 240;

    trainWorker = new Worker('worker.js');
    trainRunning = true;

    document.getElementById('train-btn-start').disabled = true;
    document.getElementById('train-btn-stop').disabled = false;
    document.getElementById('train-log').innerHTML = '<div class="train-msg">训练启动中...</div>';

    trainWorker.onmessage = function(e) {
        const { type, data } = e.data;

        switch (type) {
            case 'train_status':
                appendTrainLog(data.message);
                break;

            case 'train_progress':
                updateTrainProgress(data);
                break;

            case 'train_done':
                onTrainDone(data);
                break;

            case 'batch_progress':
                updateBatchProgress(data);
                break;

            case 'batch_done':
                onBatchDone(data);
                break;
        }
    };

    trainWorker.postMessage({
        type: 'train',
        data: { generations, populationSize: popSize, gamesPerEval, simCount, targetMinutes }
    });
}

function stopTraining() {
    if (trainWorker) {
        trainWorker.postMessage({ type: 'stop' });
        trainRunning = false;
        document.getElementById('train-btn-start').disabled = false;
        document.getElementById('train-btn-stop').disabled = true;
        appendTrainLog('训练已停止');
    }
}

function startBatchWorker() {
    if (trainRunning) { alert('训练/批量已在运行!'); return; }

    const count = parseInt(document.getElementById('batch-count').value) || 10;
    const simCount = parseInt(document.getElementById('sim-count').value) || 3000;

    trainWorker = new Worker('worker.js');
    trainRunning = true;
    document.getElementById('batch-status').textContent = '批量对局中... 0/' + count;

    trainWorker.onmessage = function(e) {
        const { type, data } = e.data;
        if (type === 'batch_progress') {
            updateBatchProgress(data);
        } else if (type === 'batch_done') {
            onBatchDone(data);
        }
    };

    trainWorker.postMessage({
        type: 'batch',
        data: { count, simCount, weights: solver.weights }
    });
}

function updateTrainProgress(data) {
    const elapsed = data.elapsed;
    const hours = Math.floor(elapsed / 3600000);
    const mins = Math.floor((elapsed % 3600000) / 60000);
    const secs = Math.floor((elapsed % 60000) / 1000);
    const timeStr = `${hours}h${String(mins).padStart(2,'0')}m${String(secs).padStart(2,'0')}s`;

    const stats = data.currentStats;
    const sigma = data.adaptiveSigma ? data.adaptiveSigma.toFixed(3) : '-';

    let html = `<div class="train-stats-grid">
        <div class="train-stat">
            <div class="train-stat-label">训练代数</div>
            <div class="train-stat-value">${data.generation}</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">运行时间</div>
            <div class="train-stat-value">${timeStr}</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">犯规率</div>
            <div class="train-stat-value ${stats.foulRate > 0.2 ? 'stat-bad' : 'stat-good'}">${(stats.foulRate * 100).toFixed(1)}%</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">进范率</div>
            <div class="train-stat-value ${stats.fantasyRate > 0.1 ? 'stat-great' : ''}">${(stats.fantasyRate * 100).toFixed(1)}%</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">平均分</div>
            <div class="train-stat-value">${stats.avgScore.toFixed(1)}</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">最佳适应度</div>
            <div class="train-stat-value">${data.bestFitness.toFixed(1)}</div>
        </div>
        <div class="train-stat">
            <div class="train-stat-label">噪声系数</div>
            <div class="train-stat-value">${sigma}</div>
        </div>
    </div>`;

    // 训练曲线 (文本图表)
    if (data.history && data.history.length > 1) {
        html += renderTextChart(data.history);
    }

    document.getElementById('train-progress').innerHTML = html;
}

function renderTextChart(history) {
    const width = 60;
    const height = 12;
    const foulRates = history.map(h => h.foulRate);
    const fantasyRates = history.map(h => h.fantasyRate);
    const scores = history.map(h => h.avgScore || 0);

    let html = '<div class="train-chart">';
    html += '<div class="chart-title">训练曲线 (犯规率↓ / 进范率↑ / 平均分)</div>';

    // Simple sparkline using block chars
    const sparkline = (values, label, invert = false) => {
        const min = Math.min(...values);
        const max = Math.max(...values) || 1;
        const range = max - min || 1;
        const blocks = '▁▂▃▄▅▆▇█';

        let line = values.slice(-width).map(v => {
            let norm = (v - min) / range;
            if (invert) norm = 1 - norm;
            const idx = Math.min(Math.floor(norm * 8), 7);
            return blocks[idx];
        }).join('');

        const latest = values[values.length - 1];
        return `<div class="sparkline"><span class="spark-label">${label}</span><span class="spark-data">${line}</span><span class="spark-value">${typeof latest === 'number' ? (latest * (label.includes('率') ? 100 : 1)).toFixed(1) + (label.includes('率') ? '%' : '') : ''}</span></div>`;
    };

    html += sparkline(foulRates, '犯规率', true);
    html += sparkline(fantasyRates, '进范率');
    html += sparkline(scores, '平均分');
    html += '</div>';
    return html;
}

function appendTrainLog(msg) {
    const el = document.getElementById('train-log');
    if (el) {
        const time = new Date().toLocaleTimeString('zh-CN');
        el.innerHTML += `<div class="train-msg"><span class="train-time">${time}</span> ${msg}</div>`;
        el.scrollTop = el.scrollHeight;
    }
}

function onTrainDone(data) {
    trainRunning = false;
    document.getElementById('train-btn-start').disabled = false;
    document.getElementById('train-btn-stop').disabled = true;

    const hours = Math.floor(data.totalTime / 3600000);
    const mins = Math.floor((data.totalTime % 3600000) / 60000);

    appendTrainLog(`训练完成! 共${data.totalGenerations}代, 耗时${hours}h${mins}m`);
    appendTrainLog(`最终: 犯规率${(data.finalStats.foulRate * 100).toFixed(1)}% | 进范率${(data.finalStats.fantasyRate * 100).toFixed(1)}% | 平均分${data.finalStats.avgScore.toFixed(1)}`);

    // Apply best weights to solver
    solver.updateWeights(data.bestWeights);
    appendTrainLog('最优权重已应用到求解器!');

    // Show export option
    const weightsJson = JSON.stringify(data.bestWeights, null, 2);
    document.getElementById('trained-weights').value = weightsJson;
    document.getElementById('trained-weights-section').style.display = 'block';
}

function applyTrainedWeights() {
    try {
        const weights = JSON.parse(document.getElementById('trained-weights').value);
        solver.updateWeights(weights);
        alert('权重已应用!');
    } catch(e) {
        alert('JSON解析错误: ' + e.message);
    }
}

function exportTrainedWeights() {
    const data = document.getElementById('trained-weights').value;
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `ofc-weights-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(url);
}

function updateBatchProgress(data) {
    document.getElementById('batch-status').textContent =
        `批量对局中... ${data.completed}/${data.total} | 犯规${(data.stats.foulRate*100).toFixed(0)}% 进范${(data.stats.fantasyRate*100).toFixed(0)}%`;

    // Add to auto logs
    if (data.latestGame) {
        const log = {
            id: autoLogs.length + 1,
            time: new Date().toLocaleString('zh-CN'),
            rounds: data.latestGame.rounds.map((rd, idx) => ({
                round: rd.round,
                dealt: rd.dealt,
                discarded: rd.discarded,
                top: idx === data.latestGame.rounds.length - 1 ? data.latestGame.top : [],
                middle: idx === data.latestGame.rounds.length - 1 ? data.latestGame.middle : [],
                bottom: idx === data.latestGame.rounds.length - 1 ? data.latestGame.bottom : [],
            })),
            result: data.latestGame.score,
            top: data.latestGame.top,
            middle: data.latestGame.middle,
            bottom: data.latestGame.bottom
        };
        autoLogs.push(log);
    }
}

function onBatchDone(data) {
    trainRunning = false;
    document.getElementById('batch-status').textContent =
        `完成! 共${data.stats.games}局 | 犯规${(data.stats.foulRate*100).toFixed(1)}% | 进范${(data.stats.fantasyRate*100).toFixed(1)}% | 平均分${data.stats.avgScore.toFixed(1)}`;
    renderAutoLog();
    if (trainWorker) { trainWorker.terminate(); trainWorker = null; }
}

// ============================================================
// Initialize
// ============================================================

// 训练好的权重 (v2 - 5h进化策略, 18.5万局训练)
const TRAINED_WEIGHTS_V2 = {
    fantasyBonus: 601.1,
    foulPenalty: -1494.4,
    topPairBase: 4.23,
    topPairPerRank: 3.79,
    topTripsBonus: 319.6,
    royaltyWeight: 5.74,
    botStrengthWeight: 13.61,
    midStrengthWeight: 1.73,
    topStrengthWeight: 0.3,
    orderMarginWeight: 13.75,
    flushDrawWeight: 15.11,
    straightDrawWeight: 4.14,
    pairDrawWeight: 4.44,
    fantasyChaseWeight: 87.6,
    uselessDiscardBonus: 5,
};

document.addEventListener('DOMContentLoaded', () => {
    // 默认使用训练好的权重
    solver.updateWeights(TRAINED_WEIGHTS_V2);
    console.log('已加载训练权重 v2');

    // localStorage的权重优先级更高
    const saved = localStorage.getItem('ofc-weights');
    if (saved) {
        try {
            const weights = JSON.parse(saved);
            solver.updateWeights(weights);
            console.log('已加载localStorage权重(覆盖)');
        } catch(e) {}
    }

    newGame();
});
