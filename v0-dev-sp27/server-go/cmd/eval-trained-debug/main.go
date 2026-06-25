// 同 eval-trained 但同时输出 56-d feature 数组 (JSON line)
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
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
	weights := flag.String("weights", "", "path to weights JSON")
	flag.Parse()
	if *weights != "" {
		if err := ofc.LoadWeightsFromFile(*weights); err != nil {
			log.Fatalf("load weights: %v", err)
		}
		fmt.Fprintf(os.Stderr, "[eval-trained-debug] loaded weights from %s\n", *weights)
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// parts: top | mid | bot | used (used optional)
		parts := strings.Split(line, "|")
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
		if len(parts) >= 4 {
			for _, c := range parseRow(parts[3]) {
				gs.UsedCards[c.ID()] = true
			}
		}
		f := ofc.BuildFeaturesForDebug(gs)
		score := ofc.TrainedEval(gs)
		out, _ := json.Marshal(map[string]interface{}{"score": score, "f": f})
		fmt.Println(string(out))
	}
}
