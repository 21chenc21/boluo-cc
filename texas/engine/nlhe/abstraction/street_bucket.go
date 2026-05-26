package abstraction

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"

	"github.com/boluo/texas/engine/nlhe"
)

// StreetBuckets — generic per-street bucket assignment for (hole, board) classes.
// Replaces FlopBuckets with a Street field so the same code handles flop/turn/river.
//
// Strategy: sample (hole, board) combos uniformly from valid raw configurations,
// derive canonical key, accumulate equity. K-means cluster all unique canonical
// keys → K buckets.
//
// Coverage tradeoff: outerSamples controls coverage of theoretical class count.
// Unseen classes at runtime use `ForOrFallback` to compute equity + nearest center.
type StreetBuckets struct {
	Street       int                `json:"street"` // 3 (flop), 4 (turn), 5 (river)
	Mode         string             `json:"mode"`   // e.g. "EHS-flop", "EHS-turn"
	K            int                `json:"k"`
	Buckets      map[string]int     `json:"buckets"`  // canonical_key → bucket_id
	Equities     map[string]float64 `json:"equities"` // canonical_key → avg equity from build
	Centers      []float64          `json:"centers"`  // K cluster centers, ascending
	Visits       map[string]int     `json:"visits"`   // canonical_key → MC sample count
	MCSamples    int                `json:"mc_samples"`
	OuterSamples int                `json:"outer_samples"`
	Seed         int64              `json:"seed"`
	BuildLabel   string             `json:"build_label"`
}

// BuildStreet — sample-based build of street buckets.
//
//	street: number of board cards (3 = flop, 4 = turn, 5 = river)
//	K: bucket count
//	outerSamples: # of raw (hole, board) draws (each derives canonical key)
//	innerSamples: per-equity MC sample count (~500-1000 for K=50-200)
func BuildStreet(street, K, outerSamples, innerSamples int, seed int64) *StreetBuckets {
	if street < 3 || street > 5 {
		panic(fmt.Sprintf("BuildStreet: street=%d, expect 3/4/5", street))
	}
	streetName := []string{"", "", "", "flop", "turn", "river"}[street]
	rng := rand.New(rand.NewSource(seed))

	bp := &StreetBuckets{
		Street:       street,
		Mode:         fmt.Sprintf("EHS-%s", streetName),
		K:            K,
		Buckets:      make(map[string]int),
		Equities:     make(map[string]float64),
		Visits:       make(map[string]int),
		MCSamples:    innerSamples,
		OuterSamples: outerSamples,
		Seed:         seed,
		BuildLabel:   fmt.Sprintf("%s-EHS-K%d-out%d-in%d", streetName, K, outerSamples, innerSamples),
	}

	nCards := 2 + street // hole + board cards
	eqSum := make(map[string]float64)

	for trial := 0; trial < outerSamples; trial++ {
		// Sample nCards distinct cards from deck.
		var cards [7]nlhe.Card // max = hole(2) + river(5)
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
		eq := MCEquityBoard(hole, board, innerSamples, seed+int64(trial))
		eqSum[key] += eq
		bp.Visits[key]++
	}

	// Average equity per key.
	for k, v := range bp.Visits {
		bp.Equities[k] = eqSum[k] / float64(v)
	}

	// K-means on unique keys' equities (1-D).
	keys := make([]string, 0, len(bp.Equities))
	for k := range bp.Equities {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	eqArr := make([]float64, len(keys))
	for i, k := range keys {
		eqArr[i] = bp.Equities[k]
	}
	assigns, centers := KMeans1D(eqArr, K, 100)
	for i, k := range keys {
		bp.Buckets[k] = assigns[i]
	}
	bp.Centers = centers
	return bp
}

// For — bucket id for (hole, board). -1 if class not seen in build.
func (b *StreetBuckets) For(hole [2]nlhe.Card, board []nlhe.Card) int {
	key := CanonicalHoleBoardKey(hole, board)
	if bid, ok := b.Buckets[key]; ok {
		return bid
	}
	return -1
}

// ForOrFallback — bucket id, compute equity + nearest-center for unseen class.
func (b *StreetBuckets) ForOrFallback(hole [2]nlhe.Card, board []nlhe.Card, mcSamples int, seed int64) int {
	if bid := b.For(hole, board); bid >= 0 {
		return bid
	}
	eq := MCEquityBoard(hole, board, mcSamples, seed)
	best := 0
	bestD := abs(eq - b.Centers[0])
	for j := 1; j < len(b.Centers); j++ {
		d := abs(eq - b.Centers[j])
		if d < bestD {
			bestD = d
			best = j
		}
	}
	return best
}

// CoveragePct — fraction of theoretical canonical class count covered.
// Theoretical (suit-isomorphic count):
//
//	flop  ~ 1,286,792
//	turn  ~ 13,960,050
//	river ~ 123,156,254
func (b *StreetBuckets) CoveragePct() float64 {
	theoretical := map[int]int{3: 1_286_792, 4: 13_960_050, 5: 123_156_254}
	t, ok := theoretical[b.Street]
	if !ok {
		return 0
	}
	return float64(len(b.Buckets)) / float64(t) * 100
}

func (b *StreetBuckets) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func LoadStreetBuckets(path string) (*StreetBuckets, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b StreetBuckets
	if err := json.NewDecoder(f).Decode(&b); err != nil {
		return nil, err
	}
	return &b, nil
}
