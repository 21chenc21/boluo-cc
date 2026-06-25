// Go OFC solver — single-binary server (compute + API gateway + static + DB)
//
// 全 Go 化后这一个进程负责所有事:
//   - POST /api/solve   游戏决策 (兼容旧 Node /api/solve 形状)
//   - POST /solve       同上但用 raw rolloutConfig (parity test 用)
//   - GET  /api/health  健康检查 (前端用)
//   - GET  /health      简化版健康 (兼容老 client)
//   - POST /api/games   保存对局
//   - GET  /api/games   列出对局
//   - GET  /api/games/:id  对局详情
//   - GET  /cache/clear   清 LRU
//   - GET  /             静态文件 (v7_fan/*.html, *.js, *.css)
//
// 启动 flag:
//   -addr       :8001       (TCP)
//   -unix       /path/sock  (Unix socket, 覆盖 -addr)
//   -static     ../         (前端静态文件根; 默认相对 binary 位置)
//   -db         games.db    (sqlite 文件路径)
//
// 环境变量:
//   SOLVE_CACHE_SIZE  Go 进程内 LRU 容量 (默认 2000, 0 关闭)
//   DEFAULT_LEVEL     low / medium / high (默认 medium)
//   HIGH_MULT MEDIUM_MULT LOW_MULT  覆盖各档 r1Mult
//   SOLVE_LOG         off / sample / on (默认 off)
//   SOLVE_LOG_RATE    sample 模式采样率 (默认 0.1)
//   SOLVE_LOG_RETAIN  保留最近 N 条 (默认 50000)
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/boluo/v0-server/ofc"
)

var (
	totalSolved atomic.Int64
	totalMs     atomic.Int64
	solveCache  *ofc.SolveCache
	mctsSims    int     // MCTS_SIMS env, 0=disabled (legacy ExpertPlace)
	mctsCPuct   float32 // MCTS_CPUCT env, default 1.5
	gameDB      *ofc.DB
)

// === Levels ===
type levelConfig struct {
	R1Mult float32 `json:"r1Mult"`
}

var (
	levels       map[string]levelConfig
	defaultLevel string

	solveLogMode   string  // off / sample / on
	solveLogRate   float64 // sample 模式采样率
	solveLogRetain int
)

func initLevelsAndLogging() {
	hi := envFloat("HIGH_MULT", 1.0)
	md := envFloat("MEDIUM_MULT", 0.5)
	lo := envFloat("LOW_MULT", 0.25)
	levels = map[string]levelConfig{
		"high":   {R1Mult: hi},
		"medium": {R1Mult: md},
		"low":    {R1Mult: lo},
	}
	// MCTS at inference (Phase 2): 0=off (legacy), >0=sims per decision
	mctsSims = int(envFloat("MCTS_SIMS", 0))
	mctsCPuct = envFloat("MCTS_CPUCT", 1.5)
	if mctsSims > 0 {
		log.Printf("[server] MCTS_SIMS=%d MCTS_CPUCT=%.2f (chance-node PUCT MCTS at inference; legacy ExpertPlace 不用)", mctsSims, mctsCPuct)
	}
	if lr := envStr("MCTS_LEAF_ROLLOUTS", ""); lr != "" {
		v, _ := strconv.Atoi(lr)
		if v > 0 {
			ofc.MctsLeafRollouts = v
			log.Printf("[server] MCTS_LEAF_ROLLOUTS=%d (per-leaf rollout avg, 降单次噪声)", v)
		}
	}
	if in := envStr("MCTS_INIT_ROLLOUTS", ""); in != "" {
		v, _ := strconv.Atoi(in)
		if v > 0 {
			ofc.MctsInitRollouts = v
			log.Printf("[server] MCTS_INIT_ROLLOUTS=%d (init rollouts per candidate, PUCT 解锁)", v)
		}
	}

	defaultLevel = strings.ToLower(envStr("DEFAULT_LEVEL", "medium"))
	if _, ok := levels[defaultLevel]; !ok {
		defaultLevel = "medium"
	}

	solveLogMode = strings.ToLower(envStr("SOLVE_LOG", "off"))
	solveLogRate = envFloat64("SOLVE_LOG_RATE", 0.1)
	if solveLogRate < 0 {
		solveLogRate = 0
	}
	if solveLogRate > 1 {
		solveLogRate = 1
	}
	solveLogRetain, _ = strconv.Atoi(envStr("SOLVE_LOG_RETAIN", "50000"))
	if solveLogRetain <= 0 {
		solveLogRetain = 50000
	}
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envFloat(k string, def float32) float32 {
	if v := os.Getenv(k); v != "" {
		f, err := strconv.ParseFloat(v, 32)
		if err == nil {
			return float32(f)
		}
	}
	return def
}

func envFloat64(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return def
}

func resolveLevel(name string) levelConfig {
	if name == "" {
		return levels[defaultLevel]
	}
	if l, ok := levels[strings.ToLower(name)]; ok {
		return l
	}
	return levels[defaultLevel]
}

func shouldLogSolve() bool {
	switch solveLogMode {
	case "on":
		return true
	case "sample":
		return mrand.Float64() < solveLogRate
	}
	return false
}

// ============================================================
// /solve and /api/solve handlers
// ============================================================

type stateData struct {
	Top       []string `json:"top"`
	Middle    []string `json:"middle"`
	Bottom    []string `json:"bottom"`
	UsedCards []string `json:"usedCards"`
	Round     int      `json:"round"`
}

// rawSolveRequest — POST /solve (parity 测试用, 不暴露 level)
type rawSolveRequest struct {
	State         stateData `json:"state"`
	Dealt         []string  `json:"dealt"`
	DiscardCount  int       `json:"discardCount"`
	Mode          string    `json:"mode"`
	JokerCount    int       `json:"jokerCount"`
	RolloutConfig *struct {
		R1Mult float32 `json:"r1Mult"`
	} `json:"rolloutConfig"`
}

// apiSolveRequest — POST /api/solve (前端用, 含 level/r1Mult 直传)
type apiSolveRequest struct {
	Player       *int        `json:"player"`
	Round        int         `json:"round"`
	State        stateData   `json:"state"`
	Dealt        []string    `json:"dealt"`
	DiscardCount int         `json:"discardCount"`
	Mode         string      `json:"mode"`
	GameID       *string     `json:"gameId"`
	ExtGameID    string      `json:"game_id"`     // 2026-05-26: 外部 game id (e.g. "112565853-0") — log 关联用, 跟内部 gameId 区分
	UID          string      `json:"uid"`         // 2026-05-26: 用户 id — log 关联用户行为
	SeatNumber   *int        `json:"seat_number"` // 2026-05-26: 座位号 (0-2 for 3 player) — log 关联座位
	Level        string      `json:"level"`
	R1Mult       *float32    `json:"r1Mult"`
	PureMLP      bool        `json:"pureMLP"`    // 2026-05-22: per-request 跳 MCTS, 纯 MLP top-1
	JokerCount   int         `json:"jokerCount"` // 2026-05-22: 本局总鬼数 (0/2/4). 跨 fork 漏迁移 fix.
	TopK         int         `json:"topK"`       // 2026-05-23: AI 难度. 1=最强 top-1 deterministic, 2=中等 top-2 sample, 3=简单 top-3 sample. 只 R1 用 sample, R2-R5 永远 top-1 保 endgame.
	Profile      interface{} `json:"profile"`
}

type apiSolveResponse struct {
	Layout    map[string][]string `json:"layout"`
	Discards  []string            `json:"discards"`
	ElapsedMs int64               `json:"elapsedMs"`
	TotalMs   int64               `json:"totalMs"`
	Level     string              `json:"level"`             // pureMLP=true 时返 "pureMLP", 否则 low/medium/high/custom
	R1Mult    float32             `json:"r1Mult,omitempty"`  // pureMLP=true 时省略 (MCTS 缩放参数不生效)
	TopK      int                 `json:"topK,omitempty"`    // 2026-05-31: 回显 AI 难度 (1=最强 / 2-3=sample)
	Cached    bool                `json:"cached"`
}

type rawSolveResponse struct {
	OK        bool                `json:"ok"`
	Layout    map[string][]string `json:"layout,omitempty"`
	Discards  []string            `json:"discards,omitempty"`
	ElapsedMs int64               `json:"elapsedMs"`
	Cached    bool                `json:"cached,omitempty"`
	Error     string              `json:"error,omitempty"`
}

// solveCore — 共用 solve 逻辑. 输入已经规范化的字段, 返回 layout / discards / elapsed / cached / err.
type solveOut struct {
	Layout    map[string][]string
	Discards  []string
	ElapsedMs int64
	Cached    bool
}

func solveCore(
	stateTop, stateMid, stateBot, usedCards []string,
	dealtRaw []string,
	round, discardCount int,
	mode string,
	cfg ofc.RolloutConfig,
	jokerCount int, // 2026-05-22: 本局总鬼数 (0/2/4), 传 NewGameState.
) (*solveOut, error) {
	t0 := time.Now()

	// 解析 cards
	tCards, err := mustParseCards(stateTop)
	if err != nil {
		return nil, err
	}
	mCards, err := mustParseCards(stateMid)
	if err != nil {
		return nil, err
	}
	bCards, err := mustParseCards(stateBot)
	if err != nil {
		return nil, err
	}
	dealt, err := mustParseCards(dealtRaw)
	if err != nil {
		return nil, err
	}

	// fallback round
	if round == 0 {
		if len(dealt) == 5 {
			round = 1
		} else {
			round = 2
		}
	}
	if mode == "" {
		mode = "normal"
	}

	// === Cache 查询 ===
	var cacheKey string
	if solveCache != nil {
		cacheKey = ofc.BuildSolveKey(
			stateTop, stateMid, stateBot, usedCards, round, dealtRaw,
			discardCount, mode, cfg.R1Mult, jokerCount, cfg.TopKSampleR1,
		)
		if v, ok := solveCache.Get(cacheKey); ok {
			elapsed := time.Since(t0).Milliseconds()
			totalSolved.Add(1)
			totalMs.Add(elapsed)
			return &solveOut{Layout: v.Layout, Discards: v.Discards, ElapsedMs: elapsed, Cached: true}, nil
		}
	}

	// 构 GameState — 2026-05-22 fix: 用真实 jokerCount, 不再写死 0
	state := ofc.NewGameState(jokerCount)
	state.Round = round
	for _, c := range tCards {
		state.PlaceCard(c, ofc.RowTop)
	}
	for _, c := range mCards {
		state.PlaceCard(c, ofc.RowMiddle)
	}
	for _, c := range bCards {
		state.PlaceCard(c, ofc.RowBottom)
	}
	for _, cid := range usedCards {
		state.UsedCards[cid] = true
	}

	// === fantasy mode ===
	if mode == "fantasy" {
		fr := ofc.ExpertPlaceFantasy(dealt, discardCount)
		if fr == nil {
			return nil, fmt.Errorf("fantasy: no layout (Go all phases failed)")
		}
		placedSet := make(map[ofc.Card]int)
		for _, c := range fr.Layout.Top {
			placedSet[c]++
		}
		for _, c := range fr.Layout.Middle {
			placedSet[c]++
		}
		for _, c := range fr.Layout.Bottom {
			placedSet[c]++
		}
		discards := make([]string, 0)
		for _, c := range dealt {
			if placedSet[c] > 0 {
				placedSet[c]--
			} else {
				discards = append(discards, c.String())
			}
		}
		layout := map[string][]string{
			"top":    cardStrs(fr.Layout.Top),
			"middle": cardStrs(fr.Layout.Middle),
			"bottom": cardStrs(fr.Layout.Bottom),
		}
		if cacheKey != "" {
			solveCache.Set(cacheKey, layout, discards)
		}
		elapsed := time.Since(t0).Milliseconds()
		totalSolved.Add(1)
		totalMs.Add(elapsed)
		return &solveOut{Layout: layout, Discards: discards, ElapsedMs: elapsed}, nil
	}

	// === normal mode ===
	// Deterministic mode: always create fresh RNG (don't put back, force pool.New() each call)
	var rng *mrand.Rand
	if rngSeedBase != 0 {
		rng = rngPool.New().(*mrand.Rand)
	} else {
		rng = rngPool.Get().(*mrand.Rand)
		defer rngPool.Put(rng)
	}

	beforeTop := append([]ofc.Card(nil), state.Top...)
	beforeMid := append([]ofc.Card(nil), state.Middle...)
	beforeBot := append([]ofc.Card(nil), state.Bottom...)

	// MCTS at inference if MCTS_SIMS env set; otherwise fall back to legacy ExpertPlace
	if mctsSims > 0 {
		mctsCfg := ofc.MCTSConfig{
			Sims:       mctsSims,
			CPuct:      mctsCPuct,
			UseValue:   true,
			RolloutCfg: &cfg,
			Rng:        rng,
		}
		action, _ := ofc.MCTSSearch(state, dealt, round, mctsCfg)
		ofc.ApplyMCTSAction(state, dealt, action)
	} else {
		er := &ofc.ExpertRollout{Rng: rng, Cfg: cfg}
		if round == 1 || len(dealt) == 5 {
			er.ExpertPlace5(state, dealt)
		} else {
			er.ExpertPlace3(state, dealt)
		}
	}

	addedTop := diffCards(beforeTop, state.Top)
	addedMid := diffCards(beforeMid, state.Middle)
	addedBot := diffCards(beforeBot, state.Bottom)
	placedSet := make(map[string]bool)
	for _, c := range addedTop {
		placedSet[c.ID()] = true
	}
	for _, c := range addedMid {
		placedSet[c.ID()] = true
	}
	for _, c := range addedBot {
		placedSet[c.ID()] = true
	}
	discards := make([]string, 0)
	for _, c := range dealt {
		if !placedSet[c.ID()] {
			discards = append(discards, c.String())
		}
	}

	layout := map[string][]string{
		"top":    cardStrs(addedTop),
		"middle": cardStrs(addedMid),
		"bottom": cardStrs(addedBot),
	}
	if cacheKey != "" {
		solveCache.Set(cacheKey, layout, discards)
	}
	elapsed := time.Since(t0).Milliseconds()
	totalSolved.Add(1)
	totalMs.Add(elapsed)
	return &solveOut{Layout: layout, Discards: discards, ElapsedMs: elapsed}, nil
}

func handleSolveRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req rawSolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, "bad request: "+err.Error())
		return
	}
	defer r.Body.Close()

	cfg := ofc.DefaultRolloutConfig
	if req.RolloutConfig != nil {
		if req.RolloutConfig.R1Mult > 0 {
			cfg.R1Mult = req.RolloutConfig.R1Mult
		}
	}

	out, err := solveCore(
		req.State.Top, req.State.Middle, req.State.Bottom,
		req.State.UsedCards, req.Dealt,
		req.State.Round, req.DiscardCount, req.Mode, cfg,
		req.JokerCount,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, rawSolveResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, rawSolveResponse{
		OK: true, Layout: out.Layout, Discards: out.Discards,
		ElapsedMs: out.ElapsedMs, Cached: out.Cached,
	})
}

func handleAPISolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	tStart := time.Now()
	var req apiSolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	if len(req.Dealt) == 0 {
		http.Error(w, "dealt required", http.StatusBadRequest)
		return
	}

	// 解析 level / r1Mult
	hasDirect := req.R1Mult != nil && *req.R1Mult > 0
	cfg := ofc.DefaultRolloutConfig
	if hasDirect {
		base := levels[defaultLevel]
		if req.Level != "" {
			base = resolveLevel(req.Level)
		}
		cfg.R1Mult = base.R1Mult
		if req.R1Mult != nil && *req.R1Mult > 0 {
			cfg.R1Mult = *req.R1Mult
		}
	} else {
		l := resolveLevel(req.Level)
		cfg.R1Mult = l.R1Mult
	}
	// 2026-05-22: per-request pureMLP override — 跳 MCTS, 直接 prerank top-1
	if req.PureMLP {
		cfg.PureMLP = true
	}
	// 2026-05-23: per-request topK (AI 难度). 1=top-1 deterministic 最强, 2/3=top-K sample 较弱.
	if req.TopK >= 2 {
		cfg.TopKSampleR1 = req.TopK
	}

	round := req.Round
	if round == 0 {
		round = req.State.Round
	}
	out, err := solveCore(
		req.State.Top, req.State.Middle, req.State.Bottom,
		req.State.UsedCards, req.Dealt,
		round, req.DiscardCount, req.Mode, cfg,
		req.JokerCount,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	levelLabel := strings.ToLower(req.Level)
	if cfg.PureMLP {
		levelLabel = "pureMLP" // 跳 MCTS, level/r1Mult 占位
	} else if hasDirect {
		levelLabel = "custom"
	} else if levelLabel == "" {
		levelLabel = defaultLevel
	}

	resp := apiSolveResponse{
		Layout:    out.Layout,
		Discards:  out.Discards,
		ElapsedMs: out.ElapsedMs,
		TotalMs:   time.Since(tStart).Milliseconds(),
		Level:     levelLabel,
		TopK:      req.TopK, // 0=default(top-1), 2/3=R1 sample
		Cached:    out.Cached,
	}
	// 仅 MCTS path 时返 r1Mult (pureMLP 时占位无意义, 省略)
	if !cfg.PureMLP {
		resp.R1Mult = cfg.R1Mult
	}

	// solve_log (sample / on)
	if shouldLogSolve() && gameDB != nil {
		go func(in apiSolveRequest, response apiSolveResponse, elapsed int64) {
			mode := in.Mode
			if mode == "" {
				mode = "normal"
			}
			req := map[string]interface{}{
				"state": in.State, "dealt": in.Dealt, "discardCount": in.DiscardCount, "mode": mode,
			}
			// 2026-06-01 fix: 之前只存 6 字段, 漏 jokerCount/pureMLP/topK 致 ypk case bug 分析误判
			// (用户报 "前端传了 jokerCount", 但 solve_log 不显示, 因为不存)
			req["jokerCount"] = in.JokerCount
			req["pureMLP"] = in.PureMLP
			if in.TopK > 0 {
				req["topK"] = in.TopK
			}
			if in.Level != "" {
				req["level"] = in.Level
			}
			// 2026-05-26: 外部 game_id / uid / seat_number 一起写进 request_json
			if in.ExtGameID != "" {
				req["game_id"] = in.ExtGameID
			}
			if in.UID != "" {
				req["uid"] = in.UID
			}
			if in.SeatNumber != nil {
				req["seat_number"] = *in.SeatNumber
			}
			gameDB.LogSolve(ofc.LogSolveInput{
				GameID: in.GameID, Player: in.Player, Round: &round, Mode: &mode,
				Request: req, Response: response, ElapsedMs: elapsed,
			}, time.Now().UnixMilli())
		}(req, resp, out.ElapsedMs)
	}

	writeJSON(w, resp)
}

// ============================================================
// /api/health and /health
// ============================================================

func buildHealthBody() map[string]interface{} {
	n := totalSolved.Load()
	avg := int64(0)
	if n > 0 {
		avg = totalMs.Load() / n
	}
	out := map[string]interface{}{
		"ok":           true,
		"totalSolved":  n,
		"avgElapsedMs": avg,
		"levels":       levels,
		"defaultLevel": defaultLevel,
	}
	if solveCache != nil {
		size, max, hits, misses := solveCache.Stats()
		hitRate := 0.0
		if total := hits + misses; total > 0 {
			hitRate = float64(hits) / float64(total)
		}
		out["cache"] = map[string]interface{}{
			"enabled": true,
			"size":    size,
			"max":     max,
			"hits":    hits,
			"misses":  misses,
			"hitRate": hitRate,
		}
	} else {
		out["cache"] = map[string]interface{}{"enabled": false}
	}
	if gameDB != nil {
		c, _ := gameDB.SolveLogCount()
		out["solveLog"] = map[string]interface{}{
			"mode": solveLogMode, "rate": solveLogRate, "retain": solveLogRetain, "count": c,
		}
	}
	return out
}

func handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildHealthBody())
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	// 简化版 (老 client / parity 测试)
	body := buildHealthBody()
	body["ok"] = true
	writeJSON(w, body)
}

// ============================================================
// /api/games endpoints
// ============================================================

func handleAPIGames(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleSaveGame(w, r)
	case http.MethodGet:
		handleListGames(w, r)
	default:
		http.Error(w, "POST or GET", http.StatusMethodNotAllowed)
	}
}

type saveGameRequest struct {
	ID         string      `json:"id"`
	JokerCount int         `json:"jokerCount"`
	Players    interface{} `json:"players"`
	Rounds     interface{} `json:"rounds"`
	Scores     interface{} `json:"scores"`
	Meta       interface{} `json:"meta"`
}

func handleSaveGame(w http.ResponseWriter, r *http.Request) {
	if gameDB == nil {
		http.Error(w, "db disabled", http.StatusServiceUnavailable)
		return
	}
	var req saveGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	if req.ID == "" {
		req.ID = randomHex(8)
	}
	if err := gameDB.SaveGame(ofc.SaveGameInput{
		ID: req.ID, JokerCount: req.JokerCount,
		Players: req.Players, Rounds: req.Rounds, Scores: req.Scores, Meta: req.Meta,
	}, time.Now().UnixMilli()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"id": req.ID, "ok": true})
}

func handleListGames(w http.ResponseWriter, r *http.Request) {
	if gameDB == nil {
		http.Error(w, "db disabled", http.StatusServiceUnavailable)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	games, err := gameDB.ListGames(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"games": games})
}

func handleGetGame(w http.ResponseWriter, r *http.Request) {
	if gameDB == nil {
		http.Error(w, "db disabled", http.StatusServiceUnavailable)
		return
	}
	// path = /api/games/<id>
	id := strings.TrimPrefix(r.URL.Path, "/api/games/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	g, err := gameDB.GetGame(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if g == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, g)
}

// /api/games 路由器: /api/games (no trailing slash → list/save), /api/games/<id> (detail)
func gamesRouter(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/games")
	if rest == "" {
		handleAPIGames(w, r)
		return
	}
	if strings.HasPrefix(rest, "/") {
		handleGetGame(w, r)
		return
	}
	http.NotFound(w, r)
}

// ============================================================
// 杂项
// ============================================================

func handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if solveCache == nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "cache disabled"})
		return
	}
	solveCache.Clear()
	writeJSON(w, map[string]interface{}{"ok": true})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, rawSolveResponse{OK: false, Error: msg})
}

func mustParseCards(strs []string) ([]ofc.Card, error) {
	out := make([]ofc.Card, 0, len(strs))
	for _, s := range strs {
		c, ok := ofc.ParseCard(s)
		if !ok {
			return nil, fmt.Errorf("bad card: %s", s)
		}
		out = append(out, c)
	}
	return out, nil
}

func diffCards(before, after []ofc.Card) []ofc.Card {
	beforeSet := make(map[string]int)
	for _, c := range before {
		beforeSet[c.ID()]++
	}
	out := make([]ofc.Card, 0)
	for _, c := range after {
		if beforeSet[c.ID()] > 0 {
			beforeSet[c.ID()]--
		} else {
			out = append(out, c)
		}
	}
	return out
}

func cardStrs(cards []ofc.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.String()
	}
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// Determinism via SEED env: single shared RNG + counter. 牺牲并发性能换 reproducibility.
var rngSeedBase int64
var rngSeedCounter atomic.Int64

var rngPool = sync.Pool{
	New: func() interface{} {
		if rngSeedBase != 0 {
			return mrand.New(mrand.NewSource(rngSeedBase + rngSeedCounter.Add(1)))
		}
		return mrand.New(mrand.NewSource(time.Now().UnixNano()))
	},
}

// 在 main 早期调用; 若 SEED 设置, 同时禁掉 sync.Pool 复用 (确保每次 Get 新创建)
func initRngFromEnv() {
	if s := envStr("SEED", ""); s != "" {
		base, _ := strconv.ParseInt(s, 10, 64)
		if base == 0 {
			base = 42
		}
		rngSeedBase = base
		log.Printf("[server] SEED=%d, RNG deterministic mode", base)
	}
}

// ============================================================
// main
// ============================================================

func main() {
	addr := flag.String("addr", ":8001", "TCP listen addr")
	unixSock := flag.String("unix", "", "Unix socket path (overrides -addr)")
	staticDir := flag.String("static", "", "static frontend dir (default: ../<binary-dir>/)")
	dbPath := flag.String("db", "games.db", "sqlite db file path")
	weightsFile := flag.String("weights", "", "override embedded weights with a JSON file (ckpt or server-go schema)")
	flag.Parse()

	// 观测日志: stdlib log 同时落进按日 dated 文件 (/tmp/ofc-8002-YYYY-MM-DD.log),
	// 启动日志持久化 + 不被部署 redirect 覆盖 + 保留 7 天. 见 obslog.go.
	log.SetOutput(io.MultiWriter(os.Stderr, obsLogWriter{}))
	obsLogf("[boot] server starting (obslog enabled, retain=%dd)", obsLogRetainDays)

	initRngFromEnv()
	if envStr("DISABLE_HARD_RULES", "") != "" {
		ofc.HardRulesDisabled = true
		log.Print("[server] DISABLE_HARD_RULES set; bypass all hard rule filters")
	}
	if pb := envStr("POLICY_BOOST", ""); pb != "" {
		v, _ := strconv.ParseFloat(pb, 32)
		ofc.PolicyBoost = float32(v)
		log.Printf("[server] POLICY_BOOST=%.2f (head3 policy logit bias in prerank)", v)
	}
	if envStr("DISABLE_MCTS", "") != "" {
		ofc.MctsDisabled = true
		log.Print("[server] DISABLE_MCTS set; ExpertPlace5/3 跳 rollout, 仅 prerank top-1 (纯MLP value head)")
	}
	if sm := envStr("MCTS_SIMS_MULT", ""); sm != "" {
		v, _ := strconv.ParseFloat(sm, 32)
		ofc.MctsSimsMult = float32(v)
		log.Printf("[server] MCTS_SIMS_MULT=%.2f (MCTS stage sims 倍率)", v)
	}
	if pw := envStr("MCTS_PRERANK_W", ""); pw != "" {
		v, _ := strconv.ParseFloat(pw, 32)
		ofc.MctsPrerankW = float32(v)
		log.Printf("[server] MCTS_PRERANK_W=%.2f (stage1 = w*prerank + (1-w)*rollout_mean; 1=skip rollout)", v)
	}

	if *weightsFile != "" {
		if err := ofc.LoadWeightsFromFile(*weightsFile); err != nil {
			log.Fatalf("[ofc-go] -weights load failed: %v", err)
		}
		log.Printf("[ofc-go] loaded weights from %s", *weightsFile)
	}

	initLevelsAndLogging()

	// === Cache ===
	cacheSize := 2000
	if env := os.Getenv("SOLVE_CACHE_SIZE"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n >= 0 {
			cacheSize = n
		}
	}
	if cacheSize > 0 {
		solveCache = ofc.NewSolveCache(cacheSize)
		log.Printf("[ofc-go] solve cache enabled (max=%d)", cacheSize)
	} else {
		log.Print("[ofc-go] solve cache disabled (SOLVE_CACHE_SIZE=0)")
	}

	// === DB ===
	if *dbPath != "" {
		db, err := ofc.OpenDB(*dbPath, solveLogRetain)
		if err != nil {
			log.Fatalf("[ofc-go] open db %s: %v", *dbPath, err)
		}
		gameDB = db
		log.Printf("[ofc-go] db opened: %s (solveLog=%s, retain=%d)", *dbPath, solveLogMode, solveLogRetain)
	}

	// === 静态 ===
	staticPath := *staticDir
	if staticPath == "" {
		// 默认: binary 同级或父级
		exe, err := os.Executable()
		if err == nil {
			binDir := filepath.Dir(exe)
			// 优先 binDir/.. (兼容 v7_fan/server-go/ofc-go)
			parent := filepath.Dir(binDir)
			if _, err := os.Stat(filepath.Join(parent, "3player.html")); err == nil {
				staticPath = parent
			} else if _, err := os.Stat(filepath.Join(binDir, "3player.html")); err == nil {
				staticPath = binDir
			}
		}
	}
	if staticPath != "" {
		log.Printf("[ofc-go] static dir: %s", staticPath)
	}

	mux := http.NewServeMux()
	// obsWrap: verbose=true (solve 类) 写 [recv]/[done] + 抓 no-response; 全部兜 panic.
	mux.HandleFunc("/solve", obsWrap("solveRaw", true, handleSolveRaw))
	mux.HandleFunc("/health", obsWrap("health", false, handleHealth))
	mux.HandleFunc("/cache/clear", obsWrap("cacheClear", false, handleCacheClear))
	mux.HandleFunc("/api/solve", obsWrap("apiSolve", true, handleAPISolve))
	mux.HandleFunc("/api/health", obsWrap("apiHealth", false, handleAPIHealth))
	mux.HandleFunc("/api/games", obsWrap("games", false, gamesRouter))
	mux.HandleFunc("/api/games/", obsWrap("games", false, gamesRouter))
	if staticPath != "" {
		mux.Handle("/", http.FileServer(http.Dir(staticPath)))
	}

	var ln net.Listener
	var err error
	if *unixSock != "" {
		os.Remove(*unixSock)
		ln, err = net.Listen("unix", *unixSock)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[ofc-go] listening on unix:%s", *unixSock)
	} else {
		ln, err = net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[ofc-go] listening on %s", *addr)
	}
	srv := &http.Server{Handler: mux}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Print("[ofc-go] shutting down...")
		srv.Close()
		if gameDB != nil {
			gameDB.Close()
		}
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
