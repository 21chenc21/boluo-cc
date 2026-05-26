package abstraction

import (
	"testing"

	"github.com/boluo/texas/engine/nlhe"
)

// TestStreetOCHSBuildSmoke — tiny build runs + produces valid struct.
func TestStreetOCHSBuildSmoke(t *testing.T) {
	bp := BuildStreetOCHS(3, 10, 5, 1000, 50, 42)
	if bp.K != 10 {
		t.Errorf("K=%d want 10", bp.K)
	}
	if bp.NumOppClusters != 5 {
		t.Errorf("NumOppClusters=%d want 5", bp.NumOppClusters)
	}
	if len(bp.Buckets) == 0 {
		t.Errorf("no buckets assigned")
	}
	if len(bp.Centers) != 10 {
		t.Errorf("Centers=%d want 10", len(bp.Centers))
	}
	for i, c := range bp.Centers {
		if len(c) != 5 {
			t.Errorf("Centers[%d] len=%d want 5", i, len(c))
		}
	}
}

// TestStreetOCHSProfileDistinguishesWeakVsStrong — 32o on K-Q-J-9-8 (plays
// board high card, weak vs any pair) vs AA on 2-7-K-3-9 (strong overpair)
// must have different OCHS profiles. Specifically 32o profile should drop
// vs stronger opp clusters (who pair the board) while AA stays high.
func TestStreetOCHSProfileDistinguishesWeakVsStrong(t *testing.T) {
	preEq := make([]float64, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		preEq[idx] = MCEquity(c1, c2, 5000, 42+int64(idx))
	}
	oppClusters, _ := KMeans1D(preEq, 5, 100)

	// 32o on K-Q-J-9-8 — hero plays board high card (KQJ98), weak vs any pair.
	weak := [2]nlhe.Card{nlhe.ParseCard("3c"), nlhe.ParseCard("2d")}
	boardKQJ98 := []nlhe.Card{
		nlhe.ParseCard("Ks"), nlhe.ParseCard("Qd"),
		nlhe.ParseCard("Jh"), nlhe.ParseCard("9c"), nlhe.ParseCard("8s"),
	}
	profileWeak := mcEquityProfileBoard(weak, boardKQJ98, oppClusters, 5, 2000, 1)

	// AA on 2-7-K-3-9 rainbow — strong overpair.
	aa := [2]nlhe.Card{nlhe.ParseCard("As"), nlhe.ParseCard("Ah")}
	boardLow := []nlhe.Card{
		nlhe.ParseCard("2c"), nlhe.ParseCard("7d"),
		nlhe.ParseCard("Kc"), nlhe.ParseCard("3s"), nlhe.ParseCard("9d"),
	}
	profileAA := mcEquityProfileBoard(aa, boardLow, oppClusters, 5, 2000, 2)

	t.Logf("32o-on-KQJ98 OCHS profile: %v", profileWeak)
	t.Logf("AA-on-27K39  OCHS profile: %v", profileAA)

	var diff float64
	for i := range profileWeak {
		d := profileAA[i] - profileWeak[i]
		if d < 0 {
			d = -d
		}
		diff += d
	}
	if diff < 1.0 {
		t.Errorf("OCHS profiles too similar (diff=%.3f) — 32o-plays-board and AA-overpair must clearly differ", diff)
	}
}
