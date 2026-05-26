package abstraction

import (
	"math/rand"

	"github.com/boluo/texas/engine/nlhe"
)

// OCHS — Opponent Cluster Hand Strength.
//
// Algorithm:
//  1. Compute 1-D E[HS] for all 169 canonical hand types (existing logic).
//  2. K-means cluster the 169 types into NumOppClusters opponent groups
//     (typically 5-8), by 1-D equity.
//  3. For each of 169 self-hands, run Monte Carlo: sample (opp_hand, board);
//     classify opp_hand into its cluster; record equity outcome. After many
//     samples, you have a per-cluster equity vector for each self-hand.
//  4. K-means in NumOppClusters-dimensional space → final K buckets.
//
// Why this fixes E[HS]:
//   - Two hands with same E[HS] (vs random opp) can have different Nash strategy
//     because Nash depends on equity vs OPP'S ACTUAL RANGE (e.g. shove range),
//     not vs random.
//   - OCHS profile = equity-vs-each-opp-cluster — captures the distribution
//     shape that drives Nash. AKs has high equity vs strong & vs weak (shows
//     up at top of every cluster). T2o has low equity across clusters.
//     Pocket pairs have asymmetric profile (high vs trash, lower vs premium).
//   - N-d K-means groups hands by profile SHAPE, not just average.

// ComputeOCHS — run the OCHS pipeline. Returns matrices ready for K-means.
//
//	numOppClusters: 5-8 typical (5 → trash/weak/med/strong/premium)
//	samples: per self-hand, ~50k gives ~10k per cluster (low variance)
func ComputeOCHS(numOppClusters, samples int, seed int64) ([][]float64, []int, []float64) {
	// 1. Compute 1-D equities (used for opp clustering and as side data).
	eq := make([]float64, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		eq[idx] = MCEquity(c1, c2, samples, seed+int64(idx))
	}

	// 2. Cluster opp range by 1-D equity.
	oppClusters, _ := KMeans1D(eq, numOppClusters, 100)

	// 3. Per self-hand, MC equity per opp cluster.
	ochs := make([][]float64, NumPreflopHandTypes)
	for myIdx := 0; myIdx < NumPreflopHandTypes; myIdx++ {
		my1, my2 := CanonicalRepresentative(myIdx)
		ochs[myIdx] = mcEquityVsClusters(my1, my2, oppClusters, numOppClusters,
			samples, seed+int64(NumPreflopHandTypes)+int64(myIdx))
	}
	return ochs, oppClusters, eq
}

// mcEquityVsClusters — for given hole cards, sample (opp_hand, board) trials,
// classify opp_hand into its cluster, record equity outcome per cluster.
func mcEquityVsClusters(my1, my2 nlhe.Card, oppClusters []int, numClusters, samples int, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))

	deck := make([]nlhe.Card, 0, nlhe.DeckSize-2)
	for c := nlhe.Card(0); c < nlhe.DeckSize; c++ {
		if c != my1 && c != my2 {
			deck = append(deck, c)
		}
	}

	sumEq := make([]float64, numClusters)
	count := make([]int, numClusters)

	for trial := 0; trial < samples; trial++ {
		// Partial Fisher-Yates: shuffle 7 (2 opp + 5 board).
		for i := 0; i < 7; i++ {
			j := i + rng.Intn(len(deck)-i)
			deck[i], deck[j] = deck[j], deck[i]
		}
		oppType := HandTypeIdx(deck[0], deck[1])
		cl := oppClusters[oppType]

		var my [7]nlhe.Card
		my[0] = my1
		my[1] = my2
		var op [7]nlhe.Card
		op[0] = deck[0]
		op[1] = deck[1]
		for k := 0; k < 5; k++ {
			my[2+k] = deck[2+k]
			op[2+k] = deck[2+k]
		}
		myR := nlhe.Evaluate7(my)
		opR := nlhe.Evaluate7(op)
		var equity float64
		switch {
		case myR > opR:
			equity = 1.0
		case myR == opR:
			equity = 0.5
		}
		sumEq[cl] += equity
		count[cl]++
	}

	out := make([]float64, numClusters)
	for cl := 0; cl < numClusters; cl++ {
		if count[cl] > 0 {
			out[cl] = sumEq[cl] / float64(count[cl])
		} else {
			out[cl] = 0.5 // default when no samples landed in this cluster
		}
	}
	return out
}
