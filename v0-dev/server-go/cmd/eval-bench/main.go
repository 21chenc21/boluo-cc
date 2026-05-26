// 从 stdin 读 hands (每行一个 5-card 或 3-card hand, 空格分隔), 输出 type:value 行
// 用于 JS↔Go parity 测试
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/boluo/v0-server/ofc"
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		cards := make([]ofc.Card, 0, len(parts))
		for _, s := range parts {
			c, ok := ofc.ParseCard(s)
			if !ok {
				fmt.Fprintf(os.Stderr, "bad card: %s\n", s)
				os.Exit(1)
			}
			cards = append(cards, c)
		}
		hasJoker := false
		for _, c := range cards {
			if c.IsJoker() {
				hasJoker = true
				break
			}
		}
		var ev ofc.HandValue
		if len(cards) == 3 {
			if hasJoker {
				ev = ofc.Evaluate3Joker(cards)
			} else {
				ev = ofc.Evaluate3(cards)
			}
		} else if len(cards) == 5 {
			if hasJoker {
				ev = ofc.Evaluate5Joker(cards)
			} else {
				ev = ofc.Evaluate5(cards)
			}
		} else {
			fmt.Fprintf(os.Stderr, "want 3 or 5 cards, got %d\n", len(cards))
			os.Exit(1)
		}
		fmt.Printf("%d:%d\n", ev.Type, ev.Value)
	}
}
