// stdin: 每行 "top|mid|bot" 用 ' ' 分隔 cards
// stdout: "foul:score:royalties:fantasy" (foul/fantasy 是 0/1)
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/boluo/v0-server/ofc"
)

func parseRow(s string) []ofc.Card {
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	out := make([]ofc.Card, 0, len(parts))
	for _, p := range parts {
		c, ok := ofc.ParseCard(p)
		if !ok {
			fmt.Fprintf(os.Stderr, "bad card: %s\n", p)
			os.Exit(1)
		}
		out = append(out, c)
	}
	return out
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			fmt.Fprintf(os.Stderr, "want 3 sections, got %d\n", len(parts))
			os.Exit(1)
		}
		top := parseRow(parts[0])
		mid := parseRow(parts[1])
		bot := parseRow(parts[2])
		r := ofc.ScoreHand(top, mid, bot)
		f := 0
		if r.Foul {
			f = 1
		}
		fan := 0
		if r.Fantasy {
			fan = 1
		}
		fmt.Printf("%d:%d:%d:%d\n", f, r.Score, r.Royalties, fan)
	}
}
