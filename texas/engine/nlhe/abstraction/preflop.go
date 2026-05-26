package abstraction

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/boluo/texas/engine/nlhe"
)

// PreflopBuckets — bucket assignment for all 169 canonical preflop hand types.
//
// Bucket order is by ascending equity (bucket 0 = trashiest, K-1 = nuts).
//
// Two modes:
//   - "EHS"  (default Build): 1-D K-means on E[HS] equity. Fast but mis-clusters
//     strategically different hands with similar mean equity (see Phase 1 sweep:
//     K=50 stuck at 92.1% because AKs/T2o get bucketed by wrong metric).
//   - "OCHS" (BuildOCHS):     multi-D K-means on equity-per-opp-cluster profile.
//     Captures variance vs different opp ranges; better fix for AKs/T2o issues.
type PreflopBuckets struct {
	Mode       string    `json:"mode"` // "EHS" or "OCHS"
	K          int       `json:"k"`
	Equities   []float64 `json:"equities"`            // len 169 (1-D, present in both modes)
	Buckets    []int     `json:"buckets"`             // len 169, bucket idx per hand type
	Centers    []float64 `json:"centers,omitempty"`   // EHS-only: 1-D K-means centers
	MCSamples  int       `json:"mc_samples"`
	Seed       int64     `json:"seed"`
	BuildLabel string    `json:"build_label"`

	// OCHS-only fields (omitted in EHS mode).
	NumOppClusters int         `json:"num_opp_clusters,omitempty"`
	OchsEquities   [][]float64 `json:"ochs_equities,omitempty"`   // 169 × NumOppClusters
	OppClusterFor  []int       `json:"opp_cluster_for,omitempty"` // 169 → opp cluster idx
	CentersND      [][]float64 `json:"centers_nd,omitempty"`      // OCHS K-means centers
}

// BuildLossless — K=169 identity mapping: each canonical hand type gets its
// own bucket. No clustering, by-construction no dragdown (AA/KK/QQ never share
// strategy). MC equities still computed for sort-order + downstream use.
//
// Use case: cure the K=20 "preflop bucket 19 = AA-TT" dragdown that forces
// premium pairs into the limp-heavy averaged strategy. Trade-off: 169 vs 20
// preflop buckets ↑ infoset count ~8.5x at preflop layer (still small fraction
// of total multi-street infoset table).
func BuildLossless(mcSamples int, seed int64) *PreflopBuckets {
	eq := make([]float64, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		eq[idx] = MCEquity(c1, c2, mcSamples, seed+int64(idx))
	}
	buckets := make([]int, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		buckets[idx] = idx
	}
	return &PreflopBuckets{
		Mode:       "lossless",
		K:          NumPreflopHandTypes,
		Equities:   eq,
		Buckets:    buckets,
		Centers:    append([]float64(nil), eq...), // each "center" is the hand's own equity
		MCSamples:  mcSamples,
		Seed:       seed,
		BuildLabel: fmt.Sprintf("preflop-lossless-K%d-S%d", NumPreflopHandTypes, mcSamples),
	}
}

// Build — E[HS] mode (1-D K-means on equity vs random opp).
// Kept for backward compatibility and fast baseline.
func Build(K, mcSamples int, seed int64) *PreflopBuckets {
	eq := make([]float64, NumPreflopHandTypes)
	for idx := 0; idx < NumPreflopHandTypes; idx++ {
		c1, c2 := CanonicalRepresentative(idx)
		eq[idx] = MCEquity(c1, c2, mcSamples, seed+int64(idx))
	}
	assigns, centers := KMeans1D(eq, K, 100)
	return &PreflopBuckets{
		Mode:       "EHS",
		K:          K,
		Equities:   eq,
		Buckets:    assigns,
		Centers:    centers,
		MCSamples:  mcSamples,
		Seed:       seed,
		BuildLabel: fmt.Sprintf("preflop-EHS-K%d-S%d", K, mcSamples),
	}
}

// BuildOCHS — Opponent Cluster Hand Strength bucketing.
//   - K: target bucket count
//   - numOppClusters: opp range partition (5-8 typical)
//   - mcSamples: per-hand MC iters (~50k gives reliable per-cluster equity)
func BuildOCHS(K, numOppClusters, mcSamples int, seed int64) *PreflopBuckets {
	ochs, oppClusters, eq := ComputeOCHS(numOppClusters, mcSamples, seed)
	assigns, centersND := KMeansND(ochs, K, 100, seed)
	return &PreflopBuckets{
		Mode:           "OCHS",
		K:              K,
		Equities:       eq,
		Buckets:        assigns,
		MCSamples:      mcSamples,
		Seed:           seed,
		BuildLabel:     fmt.Sprintf("preflop-OCHS-K%d-N%d-S%d", K, numOppClusters, mcSamples),
		NumOppClusters: numOppClusters,
		OchsEquities:   ochs,
		OppClusterFor:  oppClusters,
		CentersND:      centersND,
	}
}

// For — bucket index for the two hole cards (suit-collapsed).
func (b *PreflopBuckets) For(c1, c2 nlhe.Card) int {
	return b.Buckets[HandTypeIdx(c1, c2)]
}

// EquityOf — MC-estimated equity of the canonical type containing (c1, c2).
func (b *PreflopBuckets) EquityOf(c1, c2 nlhe.Card) float64 {
	return b.Equities[HandTypeIdx(c1, c2)]
}

// HandsInBucket — list canonical hand-type labels in a given bucket (debug).
func (b *PreflopBuckets) HandsInBucket(bucketID int) []string {
	var out []string
	for idx, bid := range b.Buckets {
		if bid == bucketID {
			out = append(out, HandTypeLabel(idx))
		}
	}
	return out
}

// Save — JSON-serialize to file.
func (b *PreflopBuckets) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

// LoadPreflopBuckets — read back from disk.
func LoadPreflopBuckets(path string) (*PreflopBuckets, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b PreflopBuckets
	if err := json.NewDecoder(f).Decode(&b); err != nil {
		return nil, err
	}
	if len(b.Buckets) != NumPreflopHandTypes {
		return nil, fmt.Errorf("invalid bucket file: %d buckets, want %d", len(b.Buckets), NumPreflopHandTypes)
	}
	return &b, nil
}
