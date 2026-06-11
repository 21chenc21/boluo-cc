package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readTodayObsLog() string {
	p := filepath.Join(obsLogDir, obsLogPrefix+time.Now().Format("2006-01-02")+".log")
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestObsWrap_PanicRecovered(t *testing.T) {
	h := obsWrap("panicTest", true, func(w http.ResponseWriter, r *http.Request) {
		panic("boom-xyz")
	})
	req := httptest.NewRequest("POST", "/api/solve", strings.NewReader(`{"round":4,"game_id":"g1","uid":"u1","dealt":["X","Ts","3c"]}`))
	rw := httptest.NewRecorder()
	h(rw, req) // 不应 panic 出来
	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("panic 后应回 500, got %d", rw.Code)
	}
	log := readTodayObsLog()
	for _, want := range []string{"[PANIC]", "boom-xyz", "[recv]", "game=g1", "uid=u1", "[done]"} {
		if !strings.Contains(log, want) {
			t.Errorf("日志缺 %q", want)
		}
	}
}

func TestObsWrap_NormalRecvDone(t *testing.T) {
	h := obsWrap("okTest", true, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`ok`))
	})
	req := httptest.NewRequest("POST", "/api/solve", strings.NewReader(`{"round":2,"game_id":"gOK","dealt":["X"]}`))
	rw := httptest.NewRecorder()
	h(rw, req)
	if rw.Code != 200 {
		t.Fatalf("got %d", rw.Code)
	}
	log := readTodayObsLog()
	if !strings.Contains(log, "game=gOK") || !strings.Contains(log, "status=200") {
		t.Errorf("缺 recv/done 标记, log尾:\n%s", log[max0(len(log)-400):])
	}
}

func max0(x int) int { if x < 0 { return 0 }; return x }

func TestObsCleanup_Removes8DayOld(t *testing.T) {
	oldP := filepath.Join(obsLogDir, obsLogPrefix+"2000-01-01.log")
	if err := os.WriteFile(oldP, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -8)
	os.Chtimes(oldP, old, old)
	freshP := filepath.Join(obsLogDir, obsLogPrefix+"2000-01-02.log") // 内容旧但 mtime 新 → 保留
	os.WriteFile(freshP, []byte("fresh"), 0644)
	obsMu.Lock()
	obsCleanupOldLocked()
	obsMu.Unlock()
	if _, err := os.Stat(oldP); !os.IsNotExist(err) {
		t.Errorf(">7天的日志应被删, 还在: %s", oldP)
	}
	if _, err := os.Stat(freshP); err != nil {
		t.Errorf("新 mtime 的日志不该删: %v", err)
	}
	os.Remove(freshP)
}
