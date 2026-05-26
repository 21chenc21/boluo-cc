//go:build onnx

// onnx-validate — Go end-to-end ONNX validation:
//  1. Load the distilled ONNX model in Go via onnxruntime_go.
//  2. Walk Leduc game tree, for each of 288 infosets:
//     - Build 35-d feature vector using cfr.FeatureVec
//     - Run Go ONNX forward → logits
//     - Apply legal mask + softmax → action probs
//  3. Write strategy in BlueprintFile format.
//  4. Run cfr.Exploitability + GameValue on it.
//  5. Optionally compare with Python-side NN strategy JSON.
//
// This validates the GO inference path matches the Python one — without it,
// "POC PASS" only covers Python deploy; Go deploy could still fail.
//
//	go run -tags onnx ./cmd/onnx-validate
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"os"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/engine/leduc"
	"github.com/boluo/texas/server"
)

var (
	modelPath  = flag.String("model", "distill/models/leduc-policy.onnx", "ONNX model")
	tabularBP  = flag.String("tabular", "blueprints/leduc-vanilla-30k.json", "tabular blueprint")
	out        = flag.String("out", "distill/models/leduc-go-nn-strategy.json", "Go-side NN strategy output")
	compareTo  = flag.String("compare-to", "distill/models/leduc-nn-strategy.json", "compare against Python-side NN strategy (optional)")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	m, err := server.OpenPolicy(*modelPath)
	if err != nil {
		log.Fatalf("open model: %v", err)
	}
	defer m.Close()
	log.Printf("[validate] loaded %s", *modelPath)

	// Walk Leduc tree, build strategy from Go ONNX inference.
	sigma := make(cfr.Strategy)
	labels := make(map[uint64]string)
	visited := make(map[uint64]bool)
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
		if !visited[id] {
			visited[id] = true
			feat := cfr.FeatureVec(s)
			logits, err := m.Forward(feat[:])
			if err != nil {
				log.Fatalf("forward at infoset %s: %v", s.InfosetKey(), err)
			}
			legal := s.LegalActions()
			// Mask + softmax over legal actions only.
			probs := softmaxLegal(logits, legal)
			sigma[id] = probs
			labels[id] = s.InfosetKey()
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
	log.Printf("[validate] built strategy: %d infosets", len(sigma))
	if len(sigma) != 288 {
		log.Fatalf("expected 288 infosets, got %d", len(sigma))
	}

	// Compute metrics.
	gv0 := cfr.GameValue(sigma, leduc.P0)
	expl := cfr.Exploitability(sigma)
	log.Printf("[validate] Go-NN expl=%.6f  gv(P0)=%+.6f", expl, gv0)

	// Save as BlueprintFile-format JSON for compare-blueprints.
	bp := cfr.BlueprintFile{
		GameName:       "leduc-holdem",
		Iters:          0,
		GameValueP0:    gv0,
		Exploitability: expl,
		NumInfosets:    len(sigma),
		Strategy:       make([]cfr.BlueprintInfoset, 0, len(sigma)),
	}
	for id, probs := range sigma {
		bp.Strategy = append(bp.Strategy, cfr.BlueprintInfoset{
			ID:    id,
			Label: labels[id],
			Probs: probs,
		})
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("create out: %v", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bp); err != nil {
		log.Fatalf("encode: %v", err)
	}
	f.Close()
	log.Printf("[validate] wrote %s", *out)

	// Compare with Python-side NN strategy if available.
	if *compareTo != "" {
		py, _, err := cfr.LoadBlueprint(*compareTo)
		if err != nil {
			log.Printf("[validate] skip Python comparison (%s not loadable: %v)", *compareTo, err)
			return
		}
		log.Printf("")
		log.Printf("=== Go ONNX vs Python ONNX (same model) ===")
		var maxDiff, sumDiff float64
		count := 0
		for id, goProbs := range sigma {
			pyProbs, ok := py[id]
			if !ok {
				continue
			}
			for i := range goProbs {
				diff := math.Abs(goProbs[i] - pyProbs[i])
				if diff > maxDiff {
					maxDiff = diff
				}
				sumDiff += diff
				count++
			}
		}
		log.Printf("per-action prob diff: max=%.2e  avg=%.2e  (expect both < 1e-5)", maxDiff, sumDiff/float64(count))
		if maxDiff > 1e-4 {
			log.Fatalf("Go ↔ Python ONNX divergence too large; investigate inference path")
		}
		log.Printf("✓ Go ONNX path matches Python ONNX path within numerical precision")
	}

	log.Printf("")
	log.Printf("Compare with tabular: go run ./cmd/compare-blueprints -nn %s", *out)
}

// softmaxLegal — softmax over legal actions only. Returns probs sized to len(legal).
func softmaxLegal(logits []float32, legal []leduc.Action) []float64 {
	n := len(legal)
	out := make([]float64, n)
	// Find max for numerical stability.
	var maxL float32 = -1e30
	for _, a := range legal {
		if logits[a] > maxL {
			maxL = logits[a]
		}
	}
	var sum float64
	for i, a := range legal {
		out[i] = math.Exp(float64(logits[a] - maxL))
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}
