// dump-pushfold-data — train HUNL push/fold MCCFR blueprint, then enumerate
// all reachable infosets and emit (feature_vec, target_probs) JSONL for Python.
//
// Total records: 2652 (= 1326 SB-opening + 1326 BB-facing-shove infosets).
//
//	go run ./cmd/dump-pushfold-data -iters 3000000 -stack 10 \
//	    -out distill/data/hunl-pushfold-train.jsonl \
//	    -blueprint blueprints/hunl-pushfold-10bb.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	iters    = flag.Int("iters", 3000000, "MCCFR iterations")
	stackBBs = flag.Int("stack", 10, "stack in BB units")
	seed     = flag.Int64("seed", 42, "RNG seed")
	outPath  = flag.String("out", "distill/data/hunl-pushfold-train.jsonl", "training data JSONL output")
	bpPath   = flag.String("blueprint", "blueprints/hunl-pushfold-10bb.json", "save tabular blueprint JSON")
	// Abstract mode: lookup strategy via PreflopID instead of lossless InfosetID.
	// When set, dump iterates lossless infosets but writes ABSTRACT strategy (shared
	// across hands in same bucket). JSONL `id` still lossless for downstream tools.
	abstractPath = flag.String("abstract", "", "if set, train abstract MCCFR using this bucket file")
)

type trainRec struct {
	ID       uint64    `json:"id"`
	Label    string    `json:"label"`
	Features []float32 `json:"features"`
	Probs    []float32 `json:"probs"` // length 3, padded with 0 for illegal
	Legal    []float32 `json:"legal"` // length 3
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	cfg := nlhe.PushFoldConfig(*stackBBs)
	mode := "lossless"
	var buckets *abstraction.PreflopBuckets
	if *abstractPath != "" {
		var err error
		buckets, err = abstraction.LoadPreflopBuckets(*abstractPath)
		if err != nil {
			log.Fatalf("load abstract buckets: %v", err)
		}
		mode = fmt.Sprintf("abstract (K=%d, %s)", buckets.K, buckets.Mode)
	}
	log.Printf("[dump] training MCCFR push/fold @ %dbb, %d iters, mode=%s, seed=%d",
		*stackBBs, *iters, mode, *seed)

	m := nlhe.NewMCCFR(cfg, *seed)
	if buckets != nil {
		m.WithIDFn(buckets.PreflopID)
	}
	t0 := time.Now()
	logEvery := *iters / 10
	if logEvery < 1 {
		logEvery = 1
	}
	for i := 1; i <= *iters; i++ {
		m.Iter()
		if i%logEvery == 0 {
			log.Printf("[dump] iter %d/%d  %.1fs  infosets=%d",
				i, *iters, time.Since(t0).Seconds(), m.NumInfosets())
		}
	}
	avg := m.AverageStrategy()
	log.Printf("[dump] trained: %d infosets in %.1fs", len(avg), time.Since(t0).Seconds())

	// Enumerate ALL 2652 infosets directly: 1326 SB-opening + 1326 BB-facing-shove.
	// For each: build state, record features + strategy.
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		log.Fatalf("mkdir out: %v", err)
	}
	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create out: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	if err := os.MkdirAll(filepath.Dir(*bpPath), 0o755); err != nil {
		log.Fatalf("mkdir blueprint: %v", err)
	}
	bp, err := os.Create(*bpPath)
	if err != nil {
		log.Fatalf("create blueprint: %v", err)
	}
	defer bp.Close()

	type bpRec struct {
		ID    uint64    `json:"id"`
		Label string    `json:"label"`
		Probs []float64 `json:"probs"`
	}
	type bpFile struct {
		Game        string  `json:"game"`
		Iters       int     `json:"iters"`
		NumInfosets int     `json:"num_infosets"`
		Strategy    []bpRec `json:"strategy"`
		StackBBs    int     `json:"stack_bbs"`
	}

	bpData := bpFile{Game: "hunl-pushfold", Iters: *iters, StackBBs: *stackBBs}
	missing := 0
	for actorPerspective := 0; actorPerspective < 2; actorPerspective++ {
		// actorPerspective=0 → SB-opening
		// actorPerspective=1 → BB facing SB shove
		for c1 := nlhe.Card(0); c1 < nlhe.DeckSize; c1++ {
			for c2 := c1 + 1; c2 < nlhe.DeckSize; c2++ {
				oppA, oppB := pickOpp(c1, c2)
				s := nlhe.NewState(cfg)
				if actorPerspective == 0 {
					s.SetHole(nlhe.P0, c1, c2)
					s.SetHole(nlhe.P1, oppA, oppB)
				} else {
					s.SetHole(nlhe.P0, oppA, oppB)
					s.SetHole(nlhe.P1, c1, c2)
					s.Apply(nlhe.Action{Kind: nlhe.ActionAllIn})
				}
				if s.Terminal {
					continue
				}
				id := s.InfosetID()
				lookupID := id
				if buckets != nil {
					lookupID = buckets.PreflopID(s)
				}
				probs, ok := avg[lookupID]
				if !ok {
					missing++
					continue
				}
				feat := nlhe.FeatureVecPushFold(s)
				legal := s.LegalActions()

				// Pad probs to length 3 (Fold, CheckCall, AllIn).
				padded := make([]float32, 3)
				mask := make([]float32, 3)
				for i, a := range legal {
					var idx int
					switch a.Kind {
					case nlhe.ActionFold:
						idx = 0
					case nlhe.ActionCheckCall:
						idx = 1
					case nlhe.ActionAllIn:
						idx = 2
					}
					padded[idx] = float32(probs[i])
					mask[idx] = 1
				}
				rec := trainRec{
					ID:       id,
					Label:    s.InfosetLabel(),
					Features: feat[:],
					Probs:    padded,
					Legal:    mask,
				}
				if err := enc.Encode(&rec); err != nil {
					log.Fatalf("encode: %v", err)
				}
				probs64 := make([]float64, len(probs))
				copy(probs64, probs)
				bpData.Strategy = append(bpData.Strategy, bpRec{
					ID:    id,
					Label: s.InfosetLabel(),
					Probs: probs64,
				})
			}
		}
	}
	bpData.NumInfosets = len(bpData.Strategy)
	enc2 := json.NewEncoder(bp)
	enc2.SetIndent("", "  ")
	if err := enc2.Encode(&bpData); err != nil {
		log.Fatalf("encode blueprint: %v", err)
	}

	st, _ := os.Stat(*outPath)
	bpSt, _ := os.Stat(*bpPath)
	log.Printf("[dump] wrote %d training records → %s (%d bytes)", bpData.NumInfosets, *outPath, st.Size())
	log.Printf("[dump] wrote %s (%d bytes)", *bpPath, bpSt.Size())
	if missing > 0 {
		log.Printf("[dump] WARN: %d infosets not visited by MCCFR (uniform-fill in NN)", missing)
	}
	fmt.Println()
	fmt.Println("Next step (Week 2-style distillation, but for HUNL):")
	fmt.Printf("  python3 distill/train.py --data %s --out distill/models/hunl-policy.pt\n", *outPath)
}

func pickOpp(c1, c2 nlhe.Card) (nlhe.Card, nlhe.Card) {
	var pick [2]nlhe.Card
	n := 0
	for cd := nlhe.Card(0); cd < nlhe.DeckSize && n < 2; cd++ {
		if cd != c1 && cd != c2 && (n == 0 || cd != pick[0]) {
			pick[n] = cd
			n++
		}
	}
	return pick[0], pick[1]
}
