//go:build onnx

// hunl-onnx-validate — load HUNL push/fold ONNX, walk 2652 infosets, emit
// NN strategy in HUNL blueprint JSON format. Then compute case-bench-style
// metrics against the tabular blueprint.
//
//	go run -tags onnx ./cmd/hunl-onnx-validate \
//	    -model distill/models/hunl-policy.onnx \
//	    -tabular blueprints/hunl-pushfold-10bb.json \
//	    -out distill/models/hunl-nn-strategy.json
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"os"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/server"
)

var (
	modelPath   = flag.String("model", "distill/models/hunl-policy.onnx", "ONNX model")
	tabularPath = flag.String("tabular", "blueprints/hunl-pushfold-10bb.json", "tabular blueprint JSON (HUNL push/fold)")
	outPath     = flag.String("out", "distill/models/hunl-nn-strategy.json", "Go-side NN strategy output")
	stackBBs    = flag.Int("stack", 10, "stack in BB units (must match blueprint)")
)

type hunlRec struct {
	ID    uint64    `json:"id"`
	Label string    `json:"label"`
	Probs []float64 `json:"probs"`
}

type hunlBP struct {
	Game        string    `json:"game"`
	Iters       int       `json:"iters"`
	NumInfosets int       `json:"num_infosets"`
	Strategy    []hunlRec `json:"strategy"`
	StackBBs    int       `json:"stack_bbs"`
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	// Load tabular reference.
	tabFile, err := os.Open(*tabularPath)
	if err != nil {
		log.Fatalf("open tabular: %v", err)
	}
	var tab hunlBP
	if err := json.NewDecoder(tabFile).Decode(&tab); err != nil {
		log.Fatalf("decode tabular: %v", err)
	}
	tabFile.Close()
	tabStrat := make(map[uint64][]float64)
	for _, r := range tab.Strategy {
		tabStrat[r.ID] = r.Probs
	}
	log.Printf("[validate] tabular: %d infosets from %s", len(tabStrat), *tabularPath)

	// Load NN — HUNL push/fold is 33-d in, 3-d out.
	m, err := server.OpenPolicyDims(*modelPath, nlhe.FeatureDimPushFold, 3)
	if err != nil {
		log.Fatalf("open model: %v", err)
	}
	defer m.Close()
	log.Printf("[validate] loaded NN: %s", *modelPath)

	// Walk all 2652 infosets and build NN strategy.
	cfg := nlhe.PushFoldConfig(*stackBBs)
	nnStrat := make(map[uint64][]float64)
	labels := make(map[uint64]string)

	for perspective := 0; perspective < 2; perspective++ {
		for c1 := nlhe.Card(0); c1 < nlhe.DeckSize; c1++ {
			for c2 := c1 + 1; c2 < nlhe.DeckSize; c2++ {
				oppA, oppB := pickOpp(c1, c2)
				s := nlhe.NewState(cfg)
				if perspective == 0 {
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
				if _, dup := nnStrat[id]; dup {
					continue
				}
				feat := nlhe.FeatureVecPushFold(s)
				logits, err := m.Forward(feat[:])
				if err != nil {
					log.Fatalf("forward: %v", err)
				}
				legal := s.LegalActions()
				probs := softmaxLegal(logits, legal)
				nnStrat[id] = probs
				labels[id] = s.InfosetLabel()
			}
		}
	}
	log.Printf("[validate] NN strategy: %d infosets", len(nnStrat))

	if len(nnStrat) != len(tabStrat) {
		log.Printf("[validate] WARN: infoset count mismatch nn=%d tab=%d", len(nnStrat), len(tabStrat))
	}

	// Per-infoset diff stats.
	var maxDiff, sumDiff float64
	var worstID uint64
	var worstTab, worstNN []float64
	cnt := 0
	missing := 0
	for id, nnP := range nnStrat {
		tabP, ok := tabStrat[id]
		if !ok {
			missing++
			continue
		}
		gap := 0.0
		for i := 0; i < len(nnP) && i < len(tabP); i++ {
			d := math.Abs(nnP[i] - tabP[i])
			if d > gap {
				gap = d
			}
		}
		sumDiff += gap
		cnt++
		if gap > maxDiff {
			maxDiff = gap
			worstID = id
			worstTab = tabP
			worstNN = nnP
		}
	}
	log.Printf("")
	log.Printf("=== NN vs tabular blueprint diff ===")
	log.Printf("  infosets compared: %d", cnt)
	if missing > 0 {
		log.Printf("  missing in tab: %d", missing)
	}
	log.Printf("  max |Δ| per infoset: %.4f", maxDiff)
	log.Printf("  avg |Δ| per infoset: %.4f", sumDiff/float64(cnt))
	log.Printf("  worst infoset id=%d (%s)", worstID, labels[worstID])
	log.Printf("    tab probs: %v", worstTab)
	log.Printf("    nn  probs: %v", worstNN)

	// Save NN strategy.
	out := hunlBP{
		Game:        "hunl-pushfold-nn",
		StackBBs:    *stackBBs,
		NumInfosets: len(nnStrat),
		Strategy:    make([]hunlRec, 0, len(nnStrat)),
	}
	for id, probs := range nnStrat {
		out.Strategy = append(out.Strategy, hunlRec{ID: id, Label: labels[id], Probs: probs})
	}
	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create out: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&out); err != nil {
		log.Fatalf("encode: %v", err)
	}
	log.Printf("[validate] saved %s", *outPath)
}

func softmaxLegal(logits []float32, legal []nlhe.Action) []float64 {
	n := len(legal)
	out := make([]float64, n)
	var maxL float32 = -1e30
	for _, a := range legal {
		idx := actionIdx(a.Kind)
		if logits[idx] > maxL {
			maxL = logits[idx]
		}
	}
	var sum float64
	for i, a := range legal {
		idx := actionIdx(a.Kind)
		out[i] = math.Exp(float64(logits[idx] - maxL))
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func actionIdx(k nlhe.ActionKind) int {
	switch k {
	case nlhe.ActionFold:
		return 0
	case nlhe.ActionCheckCall:
		return 1
	case nlhe.ActionAllIn:
		return 2
	}
	return 0
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
