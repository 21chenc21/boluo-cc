package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/boluo/v0-server/ofc"
)

// replay — TSV(created_at, dealt_json, layout_json, totalMs) 逐条重跑 ExpertPlaceFantasy, 对比 prod royalty
func replay(path string) {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var n, better, worse, same, lostRefan int
	var maxMs int64
	var totalMs int64
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 3 {
			continue
		}
		var dealtS []string
		var lay struct{ Top, Middle, Bottom []string }
		if json.Unmarshal([]byte(cols[1]), &dealtS) != nil || json.Unmarshal([]byte(cols[2]), &lay) != nil {
			continue
		}
		d := parse(strings.Join(dealtS, ","))
		old := ofc.ScoreHand(parse(strings.Join(lay.Top, ",")), parse(strings.Join(lay.Middle, ",")), parse(strings.Join(lay.Bottom, ",")))
		t0 := time.Now()
		r := ofc.ExpertPlaceFantasy(d, len(d)-13)
		ms := time.Since(t0).Milliseconds()
		totalMs += ms
		if ms > maxMs {
			maxMs = ms
		}
		n++
		if ms > 500 {
			fmt.Printf("SLOW %dms prod=%sms dealt=%s\n", ms, strings.TrimSpace(cols[len(cols)-1]), strings.Join(dealtS, ","))
		}
		if r == nil {
			fmt.Printf("NIL %s\n", cols[0])
			continue
		}
		oldRefan := !old.Foul && (old.TopEval.Type >= ofc.TypeThreeOfAKind || old.BotEval.Type >= ofc.TypeFourOfAKind)
		newRefan := !r.Sc.Foul && (r.Sc.TopEval.Type >= ofc.TypeThreeOfAKind || r.Sc.BotEval.Type >= ofc.TypeFourOfAKind)
		if oldRefan && !newRefan {
			lostRefan++
		}
		oldR := old.Royalties
		if old.Foul {
			oldR = -1
		}
		switch {
		case r.Sc.Royalties > oldR:
			better++
			fmt.Printf("BETTER %s %d→%d %dms dealt=%s\n", cols[0], oldR, r.Sc.Royalties, ms, strings.Join(dealtS, ","))
		case r.Sc.Royalties < oldR:
			worse++
			fmt.Printf("WORSE %s %d→%d %dms dealt=%s\n", cols[0], oldR, r.Sc.Royalties, ms, strings.Join(dealtS, ","))
		default:
			same++
		}
	}
	fmt.Printf("n=%d better=%d worse=%d same=%d lostRefan=%d avgMs=%d maxMs=%d\n", n, better, worse, same, lostRefan, totalMs/int64(max(n, 1)), maxMs)
}
