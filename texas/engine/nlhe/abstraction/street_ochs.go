package abstraction

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"

	"github.com/boluo/texas/engine/nlhe"
)

// StreetOCHSBuckets — postflop OCHS bucketing. N-d equity profile per (hole,
// board) canonical class clustered into K buckets via N-d K-means.
//
// vs StreetBuckets (EHS): 1-D average equity per class → 1-D K-means. Loses
// shape info — "T2o on AKQ plays board straight (50% vs everyone)" looks
// identical to "AA on dry low (70% vs everyone)" in flat-equity terms.
//
// OCHS: vector of equity-vs-each-opp-cluster. T2o-on-AKQ profile is "0.50,
// 0.50, 0.45, 0.40, 0.35" (board-straight against all); AA-on-low profile is
// "0.95, 0.85, 0.70, 0.55, 0.40" (crushes weak, splits with sets). N-d K-means
// groups by SHAPE.
type StreetOCHSBuckets struct {
	Street          int                  `json:"street"`
	Mode            string               `json:"mode"`
	K               int                  `json:"k"`
	NumOppClusters  int                  `json:"num_opp_clusters"`
	OppClusters     []int                `json:"opp_clusters"` // 169-d: preflop hand type idx → cluster id
	Buckets         map[string]int       `json:"buckets"`      // canonical (hole, board) key → bucket id
	Profiles        map[string][]float64 `json:"profiles"`     // canonical key → N-d equity vector
	Centers         [][]float64          `json:"centers"`      // K cluster centers, N-d each
	Visits          map[string]int       `json:"visits"`
	OuterSamples    int                  `json:"outer_samples"`
	InnerSamples    int                  `json:"inner_samples"`
	Seed            int64                `json:"seed"`
	BuildLabel      string               `json:"build_label"`
}

// BuildStreetOCHS — sample-based postflop OCHS bucket build.
//
//	street: 3 (flop) / 4 (turn) / 5 (river)
//	K: bucket count
//	numOppClusters: 5-8 typical (preflop range partition)
//	outerSamples: # of (hole, board) draws
//	innerSamples: per-class MC equity vs opp samples
func BuildStreetOCHS(street, K, numOppClusters, outerSamples, innerSamples int, seed int64) *StreetOCHSBuckets {
	if street < 3 || street > 5 {
		panic(fmt.Sprintf("BuildStreetOCHS: street=%d, expect 3/4/5", street))
	}
	streetName := []string{"", "", "", "flop", "turn", "river"}[street]
	rng := rand.New(rand.NewSource(seed))

	// 1. Compute 1-D preflop equities + cluster 169 hand types into numOppClusters.
	preEq := make([]float64, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		preEq[idx] = MCEquity(c1, c2, 10000, seed+int64(idx))
	}
	oppClusters, _ := KMeans1D(preEq, numOppClusters, 100)

	bp := &StreetOCHSBuckets{
		Street:         street,
		Mode:           fmt.Sprintf("OCHS-%s", streetName),
		K:              K,
		NumOppClusters: numOppClusters,
		OppClusters:    oppClusters,
		Buckets:        make(map[string]int),
		Profiles:       make(map[string][]float64),
		Visits:         make(map[string]int),
		OuterSamples:   outerSamples,
		InnerSamples:   innerSamples,
		Seed:           seed,
		BuildLabel:     fmt.Sprintf("%s-OCHS-K%d-opp%d-out%d-in%d", streetName, K, numOppClusters, outerSamples, innerSamples),
	}

	nCards := 2 + street
	profileSum := make(map[string][]float64)
	profileCnt := make(map[string][]int)

	for trial := 0; trial < outerSamples; trial++ {
		var cards [7]nlhe.Card
		var used [nlhe.DeckSize]bool
		for i := 0; i < nCards; i++ {
			for {
				c := nlhe.Card(rng.Intn(nlhe.DeckSize))
				if !used[c] {
					used[c] = true
					cards[i] = c
					break
				}
			}
		}
		hole := [2]nlhe.Card{cards[0], cards[1]}
		board := cards[2:nCards]
		key := CanonicalHoleBoardKey(hole, board)

		profile := mcEquityProfileBoard(hole, board, oppClusters, numOppClusters, innerSamples, seed+int64(trial)+1)

		if _, ok := profileSum[key]; !ok {
			profileSum[key] = make([]float64, numOppClusters)
			profileCnt[key] = make([]int, numOppClusters)
		}
		for cl := 0; cl < numOppClusters; cl++ {
			profileSum[key][cl] += profile[cl]
			profileCnt[key][cl]++ // count visits per cluster (always 1 here)
		}
		bp.Visits[key]++
	}

	// Average profiles per class.
	for key, sum := range profileSum {
		avg := make([]float64, numOppClusters)
		for cl := 0; cl < numOppClusters; cl++ {
			if profileCnt[key][cl] > 0 {
				avg[cl] = sum[cl] / float64(profileCnt[key][cl])
			} else {
				avg[cl] = 0.5
			}
		}
		bp.Profiles[key] = avg
	}

	// N-d K-means on profiles.
	keys := make([]string, 0, len(bp.Profiles))
	for k := range bp.Profiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data := make([][]float64, len(keys))
	for i, k := range keys {
		data[i] = bp.Profiles[k]
	}
	assigns, centers := KMeansND(data, K, 50, seed)
	for i, k := range keys {
		bp.Buckets[k] = assigns[i]
	}
	bp.Centers = centers
	return bp
}

// mcEquityProfileBoard — for a given (hole, board), MC sample opp + remaining
// board cards; classify opp by preflop type → cluster; record equity per cluster.
// Returns numOppClusters-d profile.
func mcEquityProfileBoard(hole [2]nlhe.Card, board []nlhe.Card, oppClusters []int, numClusters, samples int, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))

	var used [nlhe.DeckSize]bool
	used[hole[0]] = true
	used[hole[1]] = true
	for _, c := range board {
		used[c] = true
	}
	deck := make([]nlhe.Card, 0, nlhe.DeckSize-2-len(board))
	for c := nlhe.Card(0); c < nlhe.DeckSize; c++ {
		if !used[c] {
			deck = append(deck, c)
		}
	}

	needBoard := 5 - len(board)
	sumEq := make([]float64, numClusters)
	count := make([]int, numClusters)

	for trial := 0; trial < samples; trial++ {
		// Need opp(2) + completeBoard(needBoard) distinct from deck.
		need := 2 + needBoard
		for i := 0; i < need; i++ {
			j := i + rng.Intn(len(deck)-i)
			deck[i], deck[j] = deck[j], deck[i]
		}
		oppType := HandTypeIdx(deck[0], deck[1])
		cl := oppClusters[oppType]

		var my [7]nlhe.Card
		var op [7]nlhe.Card
		my[0] = hole[0]
		my[1] = hole[1]
		op[0] = deck[0]
		op[1] = deck[1]
		for k := 0; k < len(board); k++ {
			my[2+k] = board[k]
			op[2+k] = board[k]
		}
		for k := 0; k < needBoard; k++ {
			my[2+len(board)+k] = deck[2+k]
			op[2+len(board)+k] = deck[2+k]
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
			out[cl] = 0.5
		}
	}
	return out
}

// For — bucket id for (hole, board). -1 if class not seen.
func (b *StreetOCHSBuckets) For(hole [2]nlhe.Card, board []nlhe.Card) int {
	key := CanonicalHoleBoardKey(hole, board)
	if bid, ok := b.Buckets[key]; ok {
		return bid
	}
	return -1
}

// ForOrFallback — bucket id, run inner MC + nearest N-d center for unseen.
func (b *StreetOCHSBuckets) ForOrFallback(hole [2]nlhe.Card, board []nlhe.Card, mcSamples int, seed int64) int {
	if bid := b.For(hole, board); bid >= 0 {
		return bid
	}
	profile := mcEquityProfileBoard(hole, board, b.OppClusters, b.NumOppClusters, mcSamples, seed)
	best := 0
	bestD := sqDist(profile, b.Centers[0])
	for j := 1; j < len(b.Centers); j++ {
		d := sqDist(profile, b.Centers[j])
		if d < bestD {
			bestD = d
			best = j
		}
	}
	return best
}

func sqDist(a, b []float64) float64 {
	var s float64
	for i := range a {
		d := a[i] - b[i]
		s += d * d
	}
	return s
}

func (b *StreetOCHSBuckets) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

func LoadStreetOCHSBuckets(path string) (*StreetOCHSBuckets, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b StreetOCHSBuckets
	if err := json.NewDecoder(f).Decode(&b); err != nil {
		return nil, err
	}
	return &b, nil
}
