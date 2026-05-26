// ============================================================
// 大菠萝训练 Web Worker
// 后台进化策略优化 + 批量对局
// ============================================================

importScripts('game.js', 'solver.js');

let running = false;

self.onmessage = function(e) {
    const { type, data } = e.data;

    switch (type) {
        case 'train':
            running = true;
            runTraining(data);
            break;
        case 'batch':
            running = true;
            runBatch(data);
            break;
        case 'stop':
            running = false;
            break;
    }
};

// ============================================================
// 批量对局 (用指定权重跑N局)
// ============================================================

function runBatch(config) {
    const { count, weights, simCount } = config;
    const results = [];

    for (let i = 0; i < count; i++) {
        if (!running) break;

        const result = simulateOneGame(weights || DEFAULT_WEIGHTS, simCount || 200);
        results.push(result);

        if ((i + 1) % 5 === 0 || i === count - 1) {
            self.postMessage({
                type: 'batch_progress',
                data: {
                    completed: i + 1,
                    total: count,
                    stats: computeStats(results),
                    latestGame: result
                }
            });
        }
    }

    self.postMessage({
        type: 'batch_done',
        data: {
            results,
            stats: computeStats(results)
        }
    });
    running = false;
}

// ============================================================
// 进化策略训练
// ============================================================

function runTraining(config) {
    const {
        generations = 50,
        populationSize = 8,
        gamesPerEval = 30,
        simCount = 150,
        learningRate = 0.15,
        noiseSigma = 0.2,
        targetMinutes = 240,  // 目标训练时长(分钟)
    } = config;

    let currentWeights = { ...DEFAULT_WEIGHTS };
    let bestWeights = { ...currentWeights };
    let bestFitness = -Infinity;
    const history = [];
    const startTime = Date.now();
    const targetMs = targetMinutes * 60 * 1000;

    // 先评估基线
    self.postMessage({ type: 'train_status', data: { message: '评估基线权重...' } });
    const baselineStats = evaluateWeights(currentWeights, gamesPerEval, simCount);
    const baselineFitness = fitness(baselineStats);
    bestFitness = baselineFitness;

    self.postMessage({
        type: 'train_progress',
        data: {
            generation: 0,
            totalGenerations: generations,
            currentStats: baselineStats,
            bestStats: baselineStats,
            bestFitness,
            weights: currentWeights,
            elapsed: Date.now() - startTime,
            history: [{ gen: 0, fitness: baselineFitness, ...baselineStats }]
        }
    });

    // 可调参数列表
    const tunable = [
        'fantasyBonus', 'foulPenalty', 'topPairBase', 'topPairPerRank',
        'topTripsBonus', 'royaltyWeight', 'botStrengthWeight', 'midStrengthWeight',
        'topStrengthWeight', 'orderMarginWeight', 'flushDrawWeight', 'straightDrawWeight',
        'pairDrawWeight', 'fantasyChaseWeight', 'uselessDiscardBonus'
    ];

    let gen = 0;
    while (running) {
        gen++;
        const elapsed = Date.now() - startTime;

        // 动态调整: 如果时间还多, 增加评估精度
        const timeLeft = targetMs - elapsed;
        if (timeLeft <= 0 && gen > generations) break;
        if (gen > generations * 3) break; // 硬上限

        // 自适应噪声: 随训练进行逐渐减小
        const adaptiveSigma = noiseSigma * Math.max(0.3, 1 - gen / (generations * 2));

        // 生成扰动
        const perturbations = [];
        for (let p = 0; p < populationSize; p++) {
            const noise = {};
            const perturbed = { ...currentWeights };
            for (const key of tunable) {
                const n = gaussianRandom() * adaptiveSigma;
                noise[key] = n;
                perturbed[key] = currentWeights[key] * (1 + n);
                // 约束
                if (key === 'foulPenalty') perturbed[key] = Math.min(perturbed[key], -100);
                if (key === 'fantasyBonus') perturbed[key] = Math.max(perturbed[key], 100);
            }
            perturbations.push({ noise, weights: perturbed });
        }

        // 评估每个扰动
        const fitnesses = [];
        for (let p = 0; p < populationSize; p++) {
            if (!running) break;
            const stats = evaluateWeights(perturbations[p].weights, gamesPerEval, simCount);
            const f = fitness(stats);
            fitnesses.push(f);

            if (f > bestFitness) {
                bestFitness = f;
                bestWeights = { ...perturbations[p].weights };
            }
        }

        if (!running) break;

        // 更新权重: 加权平均方向
        const meanFit = fitnesses.reduce((a, b) => a + b, 0) / fitnesses.length;
        const stdFit = Math.sqrt(fitnesses.reduce((a, f) => a + (f - meanFit) ** 2, 0) / fitnesses.length) || 1;

        for (const key of tunable) {
            let gradient = 0;
            for (let p = 0; p < populationSize; p++) {
                const advantage = (fitnesses[p] - meanFit) / stdFit;
                gradient += advantage * perturbations[p].noise[key];
            }
            gradient /= populationSize;
            currentWeights[key] += learningRate * currentWeights[key] * gradient;
        }

        // 约束修正
        currentWeights.foulPenalty = Math.min(currentWeights.foulPenalty, -100);
        currentWeights.fantasyBonus = Math.max(currentWeights.fantasyBonus, 100);

        // 评估当前权重
        const currentStats = evaluateWeights(currentWeights, gamesPerEval, simCount);
        const currentFitness = fitness(currentStats);

        if (currentFitness > bestFitness) {
            bestFitness = currentFitness;
            bestWeights = { ...currentWeights };
        }

        const entry = { gen, fitness: currentFitness, ...currentStats };
        history.push(entry);

        self.postMessage({
            type: 'train_progress',
            data: {
                generation: gen,
                totalGenerations: Math.max(generations, gen),
                currentStats,
                bestStats: evaluateWeights(bestWeights, 5, simCount),
                bestFitness,
                weights: currentWeights,
                bestWeights,
                elapsed: Date.now() - startTime,
                history,
                adaptiveSigma
            }
        });
    }

    // 最终评估best weights (更多局数)
    self.postMessage({ type: 'train_status', data: { message: '最终评估最优权重...' } });
    const finalStats = evaluateWeights(bestWeights, Math.max(gamesPerEval * 2, 50), simCount);

    self.postMessage({
        type: 'train_done',
        data: {
            bestWeights,
            finalStats,
            bestFitness,
            history,
            totalGenerations: gen,
            totalTime: Date.now() - startTime
        }
    });
    running = false;
}

// ============================================================
// 评估函数
// ============================================================

function evaluateWeights(weights, numGames, simCount) {
    let fouls = 0, fantasies = 0, totalRoyalties = 0, totalScore = 0;
    const scores = [];

    for (let i = 0; i < numGames; i++) {
        if (!running) break;
        const result = simulateOneGame(weights, simCount);
        if (result.score.foul) {
            fouls++;
            scores.push(-20);
        } else {
            if (result.score.fantasy) fantasies++;
            totalRoyalties += result.score.royalties;
            totalScore += result.score.royalties + (result.score.fantasy ? 15 : 0);
            scores.push(result.score.royalties + (result.score.fantasy ? 15 : 0));
        }
    }

    const n = scores.length || 1;
    return {
        games: numGames,
        foulRate: fouls / numGames,
        fantasyRate: fantasies / numGames,
        avgRoyalties: totalRoyalties / Math.max(numGames - fouls, 1),
        avgScore: scores.reduce((a, b) => a + b, 0) / n,
        fouls,
        fantasies
    };
}

function fitness(stats) {
    // 综合适应度: 进范率权重最高, 其次不犯规, 再次得分
    return stats.fantasyRate * 500
        + (1 - stats.foulRate) * 200
        + stats.avgScore * 2
        + stats.avgRoyalties;
}

// ============================================================
// 工具函数
// ============================================================

function gaussianRandom() {
    let u = 0, v = 0;
    while (u === 0) u = Math.random();
    while (v === 0) v = Math.random();
    return Math.sqrt(-2.0 * Math.log(u)) * Math.cos(2.0 * Math.PI * v);
}

function computeStats(results) {
    const n = results.length;
    if (n === 0) return { games: 0, foulRate: 0, fantasyRate: 0, avgRoyalties: 0, avgScore: 0 };
    let fouls = 0, fantasies = 0, totalR = 0;
    for (const r of results) {
        if (r.score.foul) fouls++;
        else {
            if (r.score.fantasy) fantasies++;
            totalR += r.score.royalties;
        }
    }
    return {
        games: n,
        foulRate: fouls / n,
        fantasyRate: fantasies / n,
        avgRoyalties: totalR / Math.max(n - fouls, 1),
        avgScore: totalR / n,
        fouls,
        fantasies
    };
}
