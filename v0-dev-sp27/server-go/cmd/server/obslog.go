// obslog.go — 持久观测日志, 抓"请求 prod 时无响应"的场景.
// 2026-06-11 加 (用户报怀疑有无响应). 三件事:
//   1. 按日 dated append 日志 /tmp/ofc-8002-YYYY-MM-DD.log (不被部署覆盖), 保留 7 天.
//   2. panic-recovery 中间件: 单请求 panic 时写 [PANIC] + req body + stack 到持久日志
//      + 写一行 solve_log (mode=PANIC) 方便 SQL 查; 并回 500 (客户端拿到响应而非 silent drop).
//   3. solve 类请求入口写 [recv]、响应后写 [done]; 有 recv 无对应 done = 抓到无响应.
// 设计: 全部 best-effort, 日志失败绝不影响服务. stdlib log 也 MultiWriter 进同一 dated 文件 (启动日志持久化).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boluo/v0-server/ofc"
)

const (
	obsLogDir        = "/tmp"
	obsLogPrefix     = "ofc-8002-"
	obsLogRetainDays = 7
)

var (
	obsMu      sync.Mutex
	obsFile    *os.File
	obsFileDay string
	obsReqSeq  atomic.Int64
)

// obsAppend — 把 raw bytes 追加到当日 dated 文件, 按日 rotate, rotate 时清 >7 天. 持锁. best-effort.
func obsAppend(p []byte) {
	day := time.Now().Format("2006-01-02")
	obsMu.Lock()
	defer obsMu.Unlock()
	if obsFile == nil || obsFileDay != day {
		if obsFile != nil {
			obsFile.Close()
		}
		path := filepath.Join(obsLogDir, obsLogPrefix+day+".log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return // 日志打不开就放弃, 不影响服务
		}
		obsFile = f
		obsFileDay = day
		obsCleanupOldLocked() // 每次 rotate 顺手清旧
	}
	obsFile.Write(p)
}

// obsLogf — 写一行带时间戳的观测日志.
func obsLogf(format string, args ...interface{}) {
	line := time.Now().Format("2006-01-02 15:04:05.000") + " " + fmt.Sprintf(format, args...) + "\n"
	obsAppend([]byte(line))
}

// obsCleanupOldLocked — 删 >7 天的 ofc-8002-*.log (调用方已持锁).
func obsCleanupOldLocked() {
	matches, err := filepath.Glob(filepath.Join(obsLogDir, obsLogPrefix+"*.log"))
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -obsLogRetainDays)
	for _, p := range matches {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().Before(cutoff) {
			os.Remove(p)
		}
	}
}

// obsLogWriter — io.Writer, 让 stdlib log 也落进同一 dated 文件 (启动日志/log.Fatal 持久化).
type obsLogWriter struct{}

func (obsLogWriter) Write(p []byte) (int, error) {
	obsAppend(p)
	return len(p), nil
}

// obsRespWriter — 包 ResponseWriter 记录 status code + 是否已写 header (panic 后判断能否再 500).
type obsRespWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *obsRespWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *obsRespWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

// obsWrap — 观测中间件.
//   verbose=true (solve 类): 抓 body, 写 [recv]/[done]; panic 时连 body 一起 dump.
//   verbose=false (health/games 等): 只兜 panic, 不写 recv/done (避免高频 poll 刷屏).
// 任意 handler panic → 写 [PANIC]+stack 到持久日志 + solve_log(mode=PANIC) + 回 500.
func obsWrap(name string, verbose bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := obsReqSeq.Add(1)
		start := time.Now()
		var body []byte
		if verbose && r.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 64*1024))
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			obsLogf("[recv] #%d %s ip=%s %s", id, name, clientIP(r), obsSummary(body))
		}
		ow := &obsRespWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				obsLogf("[PANIC] #%d %s err=%v ip=%s\nreq=%s\nstack=%s",
					id, name, rec, clientIP(r), string(body), debug.Stack())
				obsRecordPanicRow(name, body, rec)
				// 已恢复, 尽量回 500 (header 没写过才行); 包 recover 防二次 panic.
				func() {
					defer func() { _ = recover() }()
					if !ow.wroteHeader {
						http.Error(ow, "internal error", http.StatusInternalServerError)
					}
				}()
			}
			if verbose {
				obsLogf("[done] #%d %s status=%d %dms", id, name, ow.status, time.Since(start).Milliseconds())
			}
		}()
		h(ow, r)
	}
}

// obsRecordPanicRow — panic 时写一行 solve_log (mode=PANIC), 方便 SQL 查历史 panic. best-effort.
func obsRecordPanicRow(name string, body []byte, rec interface{}) {
	if gameDB == nil {
		return
	}
	defer func() { _ = recover() }()
	mode := "PANIC"
	gameDB.LogSolve(ofc.LogSolveInput{
		Mode:    &mode,
		Request: map[string]interface{}{"handler": name, "raw": string(body)},
		Response: map[string]interface{}{
			"error":  "PANIC",
			"detail": fmt.Sprint(rec),
		},
	}, time.Now().UnixMilli())
}

// obsSummary — solve body 关键字段摘要 (round/game_id/uid/dealt 长度), recv 量大不全存.
func obsSummary(body []byte) string {
	if len(body) == 0 {
		return "body=0B"
	}
	var m struct {
		Round   int      `json:"round"`
		GameID  string   `json:"game_id"`
		UID     string   `json:"uid"`
		Dealt   []string `json:"dealt"`
		PureMLP bool     `json:"pureMLP"`
	}
	if json.Unmarshal(body, &m) != nil {
		return fmt.Sprintf("body=%dB(unparsed)", len(body))
	}
	return fmt.Sprintf("round=%d game=%s uid=%s dealt=%d pureMLP=%v", m.Round, m.GameID, m.UID, len(m.Dealt), m.PureMLP)
}

// clientIP — 取真实客户端 IP (优先 X-Forwarded-For / X-Real-IP, 否则 RemoteAddr).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	return r.RemoteAddr
}
