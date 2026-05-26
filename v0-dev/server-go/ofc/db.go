package ofc

// SQLite 简单封装 — 与 server/db.js 兼容 (同 schema, 同 column 名)
// 用 modernc.org/sqlite (纯 Go, 不要 CGO).
//
// 公开 API:
//   OpenDB(path) → *DB
//   db.SaveGame, db.ListGames, db.GetGame, db.LogSolve, db.SolveLogCount, db.Close

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn      *sql.DB
	writeCnt  atomic.Int64 // 累计 logSolve 次数, 用于平摊 prune
	retainMax int
}

const dbSchema = `
CREATE TABLE IF NOT EXISTS games (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    joker_count INTEGER NOT NULL DEFAULT 0,
    players_json TEXT NOT NULL,
    rounds_json TEXT NOT NULL,
    scores_json TEXT NOT NULL,
    meta_json TEXT
);
CREATE INDEX IF NOT EXISTS games_created_idx ON games(created_at DESC);

CREATE TABLE IF NOT EXISTS solve_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id TEXT,
    player INTEGER,
    round INTEGER,
    mode TEXT,
    request_json TEXT NOT NULL,
    response_json TEXT NOT NULL,
    elapsed_ms INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS solve_log_game_idx ON solve_log(game_id);
CREATE INDEX IF NOT EXISTS solve_log_created_idx ON solve_log(created_at DESC);
`

func OpenDB(path string, retainMax int) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(dbSchema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &DB{conn: conn, retainMax: retainMax}, nil
}

func (d *DB) Close() error {
	if d == nil || d.conn == nil {
		return nil
	}
	return d.conn.Close()
}

// SaveGameInput — 与 JS server/db.js saveGame 参数一致
type SaveGameInput struct {
	ID         string
	JokerCount int
	Players    interface{}
	Rounds     interface{}
	Scores     interface{}
	Meta       interface{}
}

func (d *DB) SaveGame(in SaveGameInput, nowMs int64) error {
	pj, _ := json.Marshal(in.Players)
	rj, _ := json.Marshal(in.Rounds)
	sj, _ := json.Marshal(in.Scores)
	var mj sql.NullString
	if in.Meta != nil {
		b, _ := json.Marshal(in.Meta)
		mj = sql.NullString{String: string(b), Valid: true}
	}
	_, err := d.conn.Exec(
		`INSERT INTO games(id, created_at, joker_count, players_json, rounds_json, scores_json, meta_json)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		in.ID, nowMs, in.JokerCount, string(pj), string(rj), string(sj), mj,
	)
	return err
}

type GameSummary struct {
	ID         string          `json:"id"`
	CreatedAt  int64           `json:"created_at"`
	JokerCount int             `json:"joker_count"`
	Scores     json.RawMessage `json:"scores"`
}

func (d *DB) ListGames(limit int) ([]GameSummary, error) {
	if limit > 200 {
		limit = 200
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.conn.Query(
		`SELECT id, created_at, joker_count, scores_json FROM games ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GameSummary, 0)
	for rows.Next() {
		var g GameSummary
		var sc string
		if err := rows.Scan(&g.ID, &g.CreatedAt, &g.JokerCount, &sc); err != nil {
			return nil, err
		}
		g.Scores = json.RawMessage(sc)
		out = append(out, g)
	}
	return out, rows.Err()
}

type GameDetail struct {
	ID         string          `json:"id"`
	CreatedAt  int64           `json:"created_at"`
	JokerCount int             `json:"joker_count"`
	Players    json.RawMessage `json:"players"`
	Rounds     json.RawMessage `json:"rounds"`
	Scores     json.RawMessage `json:"scores"`
	Meta       json.RawMessage `json:"meta,omitempty"`
}

func (d *DB) GetGame(id string) (*GameDetail, error) {
	row := d.conn.QueryRow(`SELECT id, created_at, joker_count, players_json, rounds_json, scores_json, meta_json FROM games WHERE id = ?`, id)
	var g GameDetail
	var pj, rj, sj string
	var mj sql.NullString
	err := row.Scan(&g.ID, &g.CreatedAt, &g.JokerCount, &pj, &rj, &sj, &mj)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.Players = json.RawMessage(pj)
	g.Rounds = json.RawMessage(rj)
	g.Scores = json.RawMessage(sj)
	if mj.Valid {
		g.Meta = json.RawMessage(mj.String)
	}
	return &g, nil
}

type LogSolveInput struct {
	GameID    *string
	Player    *int
	Round     *int
	Mode      *string
	Request   interface{}
	Response  interface{}
	ElapsedMs int64
}

// LogSolve — 写一条 solve 日志, 累计写到 retainMax * 1.001 时触发 prune (与 JS 行为一致, 每 1000 次平摊)
func (d *DB) LogSolve(in LogSolveInput, nowMs int64) error {
	rj, _ := json.Marshal(in.Request)
	resj, _ := json.Marshal(in.Response)
	var gid sql.NullString
	if in.GameID != nil {
		gid = sql.NullString{String: *in.GameID, Valid: true}
	}
	var pl, rd sql.NullInt64
	var md sql.NullString
	if in.Player != nil {
		pl = sql.NullInt64{Int64: int64(*in.Player), Valid: true}
	}
	if in.Round != nil {
		rd = sql.NullInt64{Int64: int64(*in.Round), Valid: true}
	}
	if in.Mode != nil {
		md = sql.NullString{String: *in.Mode, Valid: true}
	}
	_, err := d.conn.Exec(
		`INSERT INTO solve_log(game_id, player, round, mode, request_json, response_json, elapsed_ms, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		gid, pl, rd, md, string(rj), string(resj), in.ElapsedMs, nowMs,
	)
	if err != nil {
		return err
	}
	w := d.writeCnt.Add(1)
	if d.retainMax > 0 && w%1000 == 0 {
		var n int
		if err := d.conn.QueryRow(`SELECT COUNT(*) FROM solve_log`).Scan(&n); err == nil {
			if n > d.retainMax {
				_, _ = d.conn.Exec(
					`DELETE FROM solve_log WHERE id IN (SELECT id FROM solve_log ORDER BY id ASC LIMIT ?)`,
					n-d.retainMax,
				)
			}
		}
	}
	return nil
}

func (d *DB) SolveLogCount() (int, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM solve_log`).Scan(&n)
	return n, err
}
