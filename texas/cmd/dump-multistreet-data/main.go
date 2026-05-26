// dump-multistreet-data — train multi-street MCCFR with MultiStreetBuckets,
// then sample game states via σ self-play, emit (134-d feature, 6-d probs)
// JSONL for Python distillation.
//
// vs push/fold dump (enumerate all 2652 infosets): multi-street lossless
// infoset count is ~10^14, impossible to enumerate. Instead self-play under σ
// to get a state distribution proportional to σ's reach probabilities — which
// is exactly what the NN should learn to handle at inference time.
//
//	go run ./cmd/dump-multistreet-data \
//	    -iters 500000 -games 20000 -stack 20 \
//	    -out distill/data/hunl-multistreet-train.jsonl
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	iters       = flag.Int("iters", 500000, "MCCFR iterations to train σ")
	games       = flag.Int("games", 20000, "self-play games to sample training data from")
	stackBBs    = flag.Int("stack", 20, "stack in BB units")
	seedTrain   = flag.Int64("seed-train", 42, "MCCFR training seed")
	seedSample  = flag.Int64("seed-sample", 7777, "self-play sampling seed")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "comma-separated bet sizes")
	outPath     = flag.String("out", "distill/data/hunl-multistreet-train.jsonl", "JSONL output path")
)

// trainRec — 134-d feature, 6-d action probs (Fold, CheckCall, Bet0, Bet1, Bet2, AllIn).
type trainRec struct {
	Features []float32 `json:"features"` // 134
	Probs    []float32 `json:"probs"`    // 6 (padded with 0 for illegal)
	Legal    []float32 `json:"legal"`    // 6 (1 for legal, 0 for not)
}

const numActions = 6 // Fold, CheckCall, Bet0, Bet1, Bet2, AllIn

// actionIdx — canonical action index for output vector layout.
func actionIdx(a nlhe.Action) int {
	switch a.Kind {
	case nlhe.ActionFold:
		return 0
	case nlhe.ActionCheckCall:
		return 1
	case nlhe.ActionBet:
		return 2 + int(a.SizeIdx) // Bet0/Bet1/Bet2 → 2/3/4
	case nlhe.ActionAllIn:
		return 5
	}
	panic(fmt.Sprintf("unknown action kind: %v", a.Kind))
}

func parseBetSizes(s string) []float64 {
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			log.Fatalf("bet size %q: %v", p, err)
		}
		out = append(out, f)
	}
	return out
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	pre, err := abstraction.LoadPreflopBuckets(*preflopPath)
	if err != nil {
		log.Fatalf("load preflop: %v", err)
	}
	flop, err := abstraction.LoadStreetBuckets(*flopPath)
	if err != nil {
		log.Fatalf("load flop: %v", err)
	}
	turn, err := abstraction.LoadStreetBuckets(*turnPath)
	if err != nil {
		log.Fatalf("load turn: %v", err)
	}
	river, err := abstraction.LoadStreetBuckets(*riverPath)
	if err != nil {
		log.Fatalf("load river: %v", err)
	}
	b := &abstraction.MultiStreetBuckets{
		Preflop: pre, Flop: flop, Turn: turn, River: river,
		FallbackSeed: *seedTrain,
	}
	idFn := func(s *nlhe.State) uint64 { return b.ID(s) }

	betSizes := parseBetSizes(*betFracs)
	cfg := &nlhe.GameConfig{
		SmallBlind: 1, BigBlind: 2,
		StartStack: 2 * (*stackBBs),
		BetSizes:   betSizes,
	}
	log.Printf("[dump] stack=%dBB / bet-sizes=%v / preflop=%s", *stackBBs, betSizes, *preflopPath)

	// Train σ.
	t0 := time.Now()
	m := nlhe.NewMCCFR(cfg, *seedTrain).WithIDFn(idFn)
	logEvery := *iters / 5
	if logEvery < 1 {
		logEvery = 1
	}
	for i := 1; i <= *iters; i++ {
		m.Iter()
		if i%logEvery == 0 {
			log.Printf("[dump] train iter %d/%d  %.1fs  infosets=%d",
				i, *iters, time.Since(t0).Seconds(), m.NumInfosets())
		}
	}
	avg := m.AverageStrategy()
	log.Printf("[dump] trained: %d abstract infosets in %.1fs", len(avg), time.Since(t0).Seconds())

	// Self-play sample. At each decision, dump (feature, probs, legal).
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create %s: %v", *outPath, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	rng := rand.New(rand.NewSource(*seedSample))
	var nRecords, nMissing int
	t1 := time.Now()
	for g := 0; g < *games; g++ {
		nRecords += playOneGame(cfg, idFn, avg, rng, enc, &nMissing)
		if (g+1)%(*games/10) == 0 {
			log.Printf("[dump] sample game %d/%d  records=%d  missing=%d  %.1fs",
				g+1, *games, nRecords, nMissing, time.Since(t1).Seconds())
		}
	}
	log.Printf("[dump] done: %d records, %d missing (%.2f%%) in %.1fs",
		nRecords, nMissing, 100*float64(nMissing)/float64(nRecords+nMissing), time.Since(t1).Seconds())

	st, _ := os.Stat(*outPath)
	log.Printf("[dump] saved %s (%.1f MB)", *outPath, float64(st.Size())/(1024*1024))
}

// playOneGame — random deal + σ self-play. Emit one record per decision point.
// Returns number of records emitted. nMissing incremented for unvisited infosets.
func playOneGame(cfg *nlhe.GameConfig, idFn func(*nlhe.State) uint64,
	avg map[uint64][]float64, rng *rand.Rand, enc *json.Encoder, nMissing *int) int {
	s := nlhe.NewState(cfg)
	var used [nlhe.DeckSize]bool
	var deck [9]nlhe.Card
	for i := 0; i < 9; i++ {
		for {
			c := nlhe.Card(rng.Intn(nlhe.DeckSize))
			if !used[c] {
				used[c] = true
				deck[i] = c
				break
			}
		}
	}
	s.SetHole(nlhe.P0, deck[0], deck[1])
	s.SetHole(nlhe.P1, deck[2], deck[3])
	boardIdx := 0
	boardCards := [5]nlhe.Card{deck[4], deck[5], deck[6], deck[7], deck[8]}

	records := 0
	for {
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			for i := 0; i < n; i++ {
				s.Board[s.NumBoard] = boardCards[boardIdx]
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return records
		}

		// Dump record at this decision point.
		legal := s.LegalActions()
		id := idFn(s)
		probs, ok := avg[id]
		if !ok {
			*nMissing++
			// Skip this decision (use uniform random for game continuation).
			s.Apply(legal[rng.Intn(len(legal))])
			continue
		}
		feat := nlhe.FeatureVecMultiStreet(s)
		paddedProbs := make([]float32, numActions)
		legalMask := make([]float32, numActions)
		for i, a := range legal {
			idx := actionIdx(a)
			paddedProbs[idx] = float32(probs[i])
			legalMask[idx] = 1
		}
		rec := trainRec{
			Features: feat[:],
			Probs:    paddedProbs,
			Legal:    legalMask,
		}
		if err := enc.Encode(&rec); err != nil {
			log.Fatalf("encode: %v", err)
		}
		records++

		// Sample action from σ for game continuation.
		r := rng.Float64()
		var cum float64
		picked := legal[len(legal)-1]
		for i, p := range probs {
			cum += p
			if r < cum {
				picked = legal[i]
				break
			}
		}
		s.Apply(picked)
	}
}
