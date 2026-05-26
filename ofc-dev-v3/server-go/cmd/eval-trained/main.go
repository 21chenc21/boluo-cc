// stdin: 每行 "top|mid|bot" (用 ' ' 分隔 cards, 空 row 允许)
// stdout: trainedEval 浮点输出, 每行一个
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
		gs := ofc.NewGameState(0)
		for _, c := range parseRow(parts[0]) {
			gs.PlaceCard(c, ofc.RowTop)
		}
		for _, c := range parseRow(parts[1]) {
			gs.PlaceCard(c, ofc.RowMiddle)
		}
		for _, c := range parseRow(parts[2]) {
			gs.PlaceCard(c, ofc.RowBottom)
		}
		score := ofc.TrainedEval(gs)
		fmt.Printf("%.6f\n", score)
	}
}
