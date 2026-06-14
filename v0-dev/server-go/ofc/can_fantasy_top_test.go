package ofc
import "testing"
// 2026-06-14 canFantasyTopFinal joker-aware 修 (实战19 双鬼 bug)
func TestCanFantasyTop(t *testing.T) {
	tc := []struct {
		name string
		cards []string
		want bool
	}{
		{"双鬼+3=333三条", []string{"X", "X", "3c"}, true},
		{"鬼+低对33=333", []string{"X", "3c", "3d"}, true},
		{"鬼+Q=QQ对范", []string{"X", "Qc", "5h"}, true},
		{"双鬼+2=222三条", []string{"X", "X", "2c"}, true},
		{"QQ对范", []string{"Qc", "Qd", "5h"}, true},
		{"AK2高张非范", []string{"Ah", "Kc", "2s"}, false},
		{"A67高张非范", []string{"Ac", "6h", "7d"}, false},
		{"鬼+低张J非范", []string{"X", "Jc", "5h"}, false}, // 鬼+J=JJ<QQ, 非对范
		{"未满留余地", []string{"Ac"}, true},
	}
	for _, c := range tc {
		cards := make([]Card, len(c.cards))
		for i, s := range c.cards {
			cards[i] = mustCard(s)
		}
		if got := canFantasyTopFinal(cards); got != c.want {
			t.Errorf("%s: canFantasyTopFinal(%v) = %v, want %v", c.name, c.cards, got, c.want)
		}
	}
}
