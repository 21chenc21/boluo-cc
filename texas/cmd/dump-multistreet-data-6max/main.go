// dump-multistreet-data-6max — 6-max NLHE training data dump. Mirrors HU
// cmd/dump-multistreet-data but for nlhe6 engine + N-player self-play sampling.
//
// Outputs JSONL records: 288-d feature + 6-d probs + 6-d legal mask. Format
// compatible with HU pipeline (distill/train.py works on both).
//
//	go run ./cmd/dump-multistreet-data-6max \
//	    -iters 500000 -games 50000 -stack 20 -players 6 \
//	    -out distill/data/hunl6-train.jsonl
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

	"github.com/boluo/texas/engine/nlhe/abstraction"
	"github.com/boluo/texas/engine/nlhe6"
)

var (
	iters       = flag.Int("iters", 500000, "MCCFR iterations to train σ")
	games       = flag.Int("games", 50000, "self-play games to sample")
	numPlayers  = flag.Int("players", 6, "table size (2-6)")
	stackBBs    = flag.Int("stack", 20, "stack in BB units")
	seedTrain   = flag.Int64("seed-train", 42, "MCCFR training seed")
	seedSample  = flag.Int64("seed-sample", 7777, "self-play sampling seed")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "comma-separated bet sizes")
	outPath     = flag.String("out", "distill/data/hunl6-train.jsonl", "output JSONL")
)

type trainRec struct {
	Features []float32 `json:"features"`
	Probs    []float32 `json:"probs"`
	Legal    []float32 `json:"legal"`
}

const numActions = 6

func actionIdx(a nlhe6.Action) int {
	switch a.Kind {
	case nlhe6.ActionFold:
		return 0
	case nlhe6.ActionCheckCall:
		return 1
	case nlhe6.ActionBet:
		return 2 + int(a.SizeIdx)
	case nlhe6.ActionAllIn:
		return 5
	}
	panic(fmt.Sprintf("unknown action: %v", a.Kind))
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

	cfg := nlhe6.DefaultConfigN(*numPlayers)
	cfg.StartStack = 2 * (*stackBBs)
	cfg.BetSizes = parseBetSizes(*betFracs)
	log.Printf("[dump] players=%d stack=%dBB / bet-sizes=%v", *numPlayers, *stackBBs, cfg.BetSizes)

	t0 := time.Now()
	m := nlhe6.NewMCCFR(cfg, *seedTrain).WithIDFn(nlhe6.MultiStreetIDFn(b))
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
	idFn := nlhe6.MultiStreetIDFn(b)
	var nRecords, nMissing int
	t1 := time.Now()
	logGame := *games / 10
	if logGame < 1 {
		logGame = 1
	}
	for g := 0; g < *games; g++ {
		nRecords += playOneGame(cfg, idFn, avg, rng, enc, &nMissing)
		if (g+1)%logGame == 0 {
			log.Printf("[dump] sample game %d/%d  records=%d  missing=%d  %.1fs",
				g+1, *games, nRecords, nMissing, time.Since(t1).Seconds())
		}
	}
	missingRate := 0.0
	if nRecords+nMissing > 0 {
		missingRate = 100 * float64(nMissing) / float64(nRecords+nMissing)
	}
	log.Printf("[dump] done: %d records, %d missing (%.2f%%) in %.1fs",
		nRecords, nMissing, missingRate, time.Since(t1).Seconds())

	st, _ := os.Stat(*outPath)
	log.Printf("[dump] saved %s (%.1f MB)", *outPath, float64(st.Size())/(1024*1024))
}

func playOneGame(cfg *nlhe6.GameConfig, idFn func(*nlhe6.State) uint64,
	avg map[uint64][]float64, rng *rand.Rand, enc *json.Encoder, nMissing *int) int {
	n := cfg.NumPlayers
	need := 2*n + 5
	var used [52]bool
	deck := make([]nlhe6.Card, 0, need)
	for i := 0; i < need; i++ {
		for {
			c := nlhe6.Card(rng.Intn(52))
			if !used[c] {
				used[c] = true
				deck = append(deck, c)
				break
			}
		}
	}
	s := nlhe6.NewStateWithButton(cfg, nlhe6.Seat(rng.Intn(n)))
	for i := 0; i < n; i++ {
		s.SetHole(nlhe6.Seat(i), deck[2*i], deck[2*i+1])
	}
	board := deck[2*n:]
	boardIdx := 0

	records := 0
	for {
		// Fill board if needed (covers terminal showdown fill too).
		for {
			nNeed, needs := s.NeedsBoard()
			if !needs {
				break
			}
			for i := 0; i < nNeed; i++ {
				s.Board[s.NumBoard] = board[boardIdx]
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return records
		}

		legal := s.LegalActions()
		id := idFn(s)
		probs, ok := avg[id]
		// Hash collision: cached probs length mismatches current legal count.
		// Treat as missing.
		if !ok || len(probs) != len(legal) {
			*nMissing++
			s.Apply(legal[rng.Intn(len(legal))])
			continue
		}
		feat := nlhe6.FeatureVecMultiStreet(s)
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
