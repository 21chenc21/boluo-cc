// dump-training-data — convert blueprint JSON to JSONL training data for Python.
//
// Each line of the output is one (infoset, feature_vec, target_probs) record.
// Pad target probs to length 3 with 0 for illegal actions.
//
//	go run ./cmd/dump-training-data \
//	  -in blueprints/leduc-vanilla-30k.json \
//	  -out distill/data/leduc-train.jsonl
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/engine/leduc"
)

var (
	in  = flag.String("in", "blueprints/leduc-vanilla-30k.json", "input blueprint JSON")
	out = flag.String("out", "distill/data/leduc-train.jsonl", "output JSONL training data")
)

// TrainingRecord — one (infoset, features, target) tuple for distillation.
type TrainingRecord struct {
	ID       uint64    `json:"id"`
	Label    string    `json:"label"`
	Features []float32 `json:"features"` // length 35 (cfr.FeatureDim)
	Probs    []float32 `json:"probs"`    // length 3, padded with 0 for illegal
	Legal    []float32 `json:"legal"`    // length 3, 1 if action legal
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	sigma, meta, err := cfr.LoadBlueprint(*in)
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	log.Printf("[dump] loaded %s: %d infosets, expl=%.6f", *in, meta.NumInfosets, meta.Exploitability)

	if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	// Enumerate the game tree to build (state, infoset_id) and emit training records.
	written := make(map[uint64]bool)
	count := 0

	var dfs func(s *leduc.State)
	dfs = func(s *leduc.State) {
		if s.Terminal {
			return
		}
		if s.NeedsPublicCard() {
			for c := leduc.Card(0); c < leduc.DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl)
			}
			return
		}
		id := s.InfosetID()
		if !written[id] {
			written[id] = true
			probs, ok := sigma[id]
			if !ok {
				log.Printf("[dump] WARN: id=%d (%s) missing in blueprint", id, s.InfosetKey())
				return
			}
			feat := cfr.FeatureVec(s)
			featSlice := make([]float32, len(feat))
			copy(featSlice, feat[:])

			legal := s.LegalActions()
			padded := make([]float32, 3)
			legalMask := make([]float32, 3)
			for i, a := range legal {
				padded[int(a)] = float32(probs[i])
				legalMask[int(a)] = 1
			}

			rec := TrainingRecord{
				ID:       id,
				Label:    s.InfosetKey(),
				Features: featSlice,
				Probs:    padded,
				Legal:    legalMask,
			}
			if err := enc.Encode(&rec); err != nil {
				log.Fatalf("encode id=%d: %v", id, err)
			}
			count++
		}
		for _, a := range s.LegalActions() {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl)
		}
	}

	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(leduc.NewState(p0, p1))
		}
	}
	log.Printf("[dump] wrote %d training records to %s", count, *out)
	if count != 288 {
		log.Fatalf("expected 288, got %d", count)
	}
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
