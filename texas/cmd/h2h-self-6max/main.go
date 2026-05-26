// h2h-self-6max — 6-max head-to-head metric.
//
// Setup: σ_A occupies one "target" seat, σ_B occupies the other N-1 seats.
// Rotate target seat through all N positions for variance reduction. Report
// σ_A's per-hand return in mbb/g (95% CI).
//
// If A == B (or NN distillation preserves σ), expected return ≈ 0.
// If A beats B, positive return (A exploits B's flaws).
//
// Without AIVAT initially — variance high in 6-max; need many hands.
//
//	go run ./cmd/h2h-self-6max -iters-a 100000 -iters-b 20000 \
//	    -hands 10000 -stack 20 -players 6
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/boluo/texas/engine/nlhe/abstraction"
	"github.com/boluo/texas/engine/nlhe6"
)

var (
	itersA       = flag.Int("iters-a", 100000, "MCCFR iter for A")
	itersB       = flag.Int("iters-b", 20000, "MCCFR iter for B")
	hands        = flag.Int("hands", 10000, "h2h hands per direction")
	stackBBs     = flag.Int("stack", 20, "stack in BB units")
	numPlayers   = flag.Int("players", 6, "table size")
	seedTrain    = flag.Int64("seed-train", 42, "MCCFR seed")
	seedDeal     = flag.Int64("seed-deal", 7777, "dealing seed")
	preflopA     = flag.String("preflop-a", "blueprints/preflop-buckets-K20.json", "A: preflop bucket")
	preflopB     = flag.String("preflop-b", "", "B: preflop bucket (defaults to A)")
	flopPath     = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket")
	turnPath     = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket")
	riverPath    = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket")
	betFracs     = flag.String("bet-frac", "0.5,1.0,2.0", "bet sizes")
	nnPolicyA    = flag.String("nn-a", "", "if set, A uses NN ONNX (needs onnx tag)")
	nnPolicyB    = flag.String("nn-b", "", "if set, B uses NN ONNX")
	useAIVAT     = flag.Bool("aivat", false, "AIVAT-style control variate using σ-self-play baselines")
)

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

// policy — pluggable. sigmaPolicy uses MCCFR avg strategy map; nnPolicy (in
// nn_onnx.go, build tag) uses ONNX inference.
type policy interface {
	sample(s *nlhe6.State, rng *rand.Rand) nlhe6.Action
}

type sigmaPolicy struct {
	probs map[uint64][]float64
	idFn  func(*nlhe6.State) uint64
}

func (p *sigmaPolicy) sample(s *nlhe6.State, rng *rand.Rand) nlhe6.Action {
	legal := s.LegalActions()
	id := p.idFn(s)
	pr, ok := p.probs[id]
	// Hash collision safety: if cached probs length doesn't match current
	// legal-action count, fall back to uniform (treat as missing).
	if !ok || len(pr) != len(legal) {
		return legal[rng.Intn(len(legal))]
	}
	r := rng.Float64()
	var cum float64
	for i, p := range pr {
		cum += p
		if r < cum {
			return legal[i]
		}
	}
	return legal[len(legal)-1]
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	if *preflopB == "" {
		*preflopB = *preflopA
	}
	loadBuckets := func(prePath string) *abstraction.MultiStreetBuckets {
		pre, err := abstraction.LoadPreflopBuckets(prePath)
		if err != nil {
			log.Fatalf("load preflop %s: %v", prePath, err)
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
		return &abstraction.MultiStreetBuckets{
			Preflop: pre, Flop: flop, Turn: turn, River: river,
			FallbackSeed: *seedTrain,
		}
	}
	bA := loadBuckets(*preflopA)
	bB := loadBuckets(*preflopB)
	idFnA := nlhe6.MultiStreetIDFn(bA)
	idFnB := nlhe6.MultiStreetIDFn(bB)

	betSizes := parseBetSizes(*betFracs)
	cfg := nlhe6.DefaultConfigN(*numPlayers)
	cfg.StartStack = 2 * (*stackBBs)
	cfg.BetSizes = betSizes
	log.Printf("[h2h6] players=%d stack=%dBB / bet-sizes=%v", *numPlayers, *stackBBs, betSizes)

	t0 := time.Now()
	var pA, pB policy
	if *nnPolicyA != "" {
		nn, err := loadNNPolicy(*nnPolicyA)
		if err != nil {
			log.Fatalf("load NN A: %v", err)
		}
		pA = nn
		log.Printf("[h2h6] A: NN policy from %s", *nnPolicyA)
	} else {
		mA := nlhe6.NewMCCFR(cfg, *seedTrain).WithIDFn(idFnA)
		for i := 0; i < *itersA; i++ {
			mA.Iter()
		}
		log.Printf("[h2h6] A trained: %d iter / %.1fs / %d infosets",
			*itersA, time.Since(t0).Seconds(), mA.NumInfosets())
		pA = &sigmaPolicy{probs: mA.AverageStrategy(), idFn: idFnA}
	}

	t1 := time.Now()
	if *nnPolicyB != "" {
		nn, err := loadNNPolicy(*nnPolicyB)
		if err != nil {
			log.Fatalf("load NN B: %v", err)
		}
		pB = nn
		log.Printf("[h2h6] B: NN policy from %s", *nnPolicyB)
	} else {
		mB := nlhe6.NewMCCFR(cfg, *seedTrain+1).WithIDFn(idFnB)
		for i := 0; i < *itersB; i++ {
			mB.Iter()
		}
		log.Printf("[h2h6] B trained: %d iter / %.1fs / %d infosets",
			*itersB, time.Since(t1).Seconds(), mB.NumInfosets())
		pB = &sigmaPolicy{probs: mB.AverageStrategy(), idFn: idFnB}
	}

	bb := float64(cfg.BigBlind)
	rng := rand.New(rand.NewSource(*seedDeal))
	tStart := time.Now()
	results := make([]float64, *hands)
	baselineA := make([]float64, *hands) // σ_A self-play at target seat (E=0 by symmetry)
	baselineB := make([]float64, *hands) // σ_B self-play at target seat
	for h := 0; h < *hands; h++ {
		target := nlhe6.Seat(h % *numPlayers)
		// Generate deal once, reuse for h2h + baselines.
		deal := newDeal(cfg, rng)
		pay := playOneHandDeal(cfg, target, deal, pA, pB)
		results[h] = pay / bb * 1000
		if *useAIVAT {
			// σ_A all 6 seats (target same), σ_B all 6 seats.
			baselineA[h] = playOneHandDeal(cfg, target, deal, pA, pA) / bb * 1000
			baselineB[h] = playOneHandDeal(cfg, target, deal, pB, pB) / bb * 1000
		}
	}
	elapsed := time.Since(tStart)

	mean, ci := meanCI(results)
	fmt.Println()
	fmt.Printf("=== h2h-6max (A at 1 target seat, B at %d others, %d hands rotating target, %.1fs) ===\n",
		*numPlayers-1, *hands, elapsed.Seconds())
	fmt.Printf("Raw        : %+.1f mbb/g  ±%.1f  (95%% CI [%+.1f, %+.1f])\n",
		mean, ci, mean-ci, mean+ci)

	if *useAIVAT {
		alphaA, alphaB, aivatMean, aivatCI := controlVariate2(results, baselineA, baselineB)
		varRedux := 1 - (aivatCI*aivatCI)/(ci*ci)
		fmt.Printf("AIVAT (α_A=%.3f, α_B=%.3f): %+.1f mbb/g  ±%.1f  (95%% CI [%+.1f, %+.1f])  variance ↓ %.0f%%\n",
			alphaA, alphaB, aivatMean, aivatCI, aivatMean-aivatCI, aivatMean+aivatCI, 100*varRedux)
		mean = aivatMean
		ci = aivatCI
	}

	if mean-ci > 0 {
		fmt.Println("       → A statistically beats B")
	} else if mean+ci < 0 {
		fmt.Println("       → B statistically beats A")
	} else {
		fmt.Println("       → no significant difference")
	}
}

// dealRecord — fixed cards for a hand (button, hole pairs, full board).
type dealRecord struct {
	button nlhe6.Seat
	holes  [][2]nlhe6.Card
	board  [5]nlhe6.Card
}

func newDeal(cfg *nlhe6.GameConfig, rng *rand.Rand) dealRecord {
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
	d := dealRecord{button: nlhe6.Seat(rng.Intn(n)), holes: make([][2]nlhe6.Card, n)}
	for i := 0; i < n; i++ {
		d.holes[i] = [2]nlhe6.Card{deck[2*i], deck[2*i+1]}
	}
	for i := 0; i < 5; i++ {
		d.board[i] = deck[2*n+i]
	}
	return d
}

func playOneHandDeal(cfg *nlhe6.GameConfig, target nlhe6.Seat, d dealRecord, pTarget, pOthers policy) float64 {
	s := nlhe6.NewStateWithButton(cfg, d.button)
	for i := 0; i < cfg.NumPlayers; i++ {
		s.SetHole(nlhe6.Seat(i), d.holes[i][0], d.holes[i][1])
	}
	boardIdx := 0
	// Local RNG for action sampling so deal stays deterministic across multiple
	// games using same deal.
	rng := rand.New(rand.NewSource(int64(d.button)*7919 + int64(d.holes[0][0])))
	for {
		for {
			nNeed, needs := s.NeedsBoard()
			if !needs {
				break
			}
			for i := 0; i < nNeed; i++ {
				s.Board[s.NumBoard] = d.board[boardIdx]
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return float64(s.Payoff(target))
		}
		var p policy
		if s.Cur == target {
			p = pTarget
		} else {
			p = pOthers
		}
		s.Apply(p.sample(s, rng))
	}
}

// controlVariate2 — minimum-variance 2-variate control variate.
func controlVariate2(x, bA, bB []float64) (alphaA, alphaB, mean, ci float64) {
	n := len(x)
	var mx, mbA, mbB float64
	for i := 0; i < n; i++ {
		mx += x[i]
		mbA += bA[i]
		mbB += bB[i]
	}
	mx /= float64(n)
	mbA /= float64(n)
	mbB /= float64(n)
	var covA, covB, varA, varB, covAB float64
	for i := 0; i < n; i++ {
		dx := x[i] - mx
		dA := bA[i] - mbA
		dB := bB[i] - mbB
		covA += dx * dA
		covB += dx * dB
		varA += dA * dA
		varB += dB * dB
		covAB += dA * dB
	}
	det := varA*varB - covAB*covAB
	if det < 1e-12 {
		return 0, 0, mx, ci
	}
	alphaA = (varB*covA - covAB*covB) / det
	alphaB = (varA*covB - covAB*covA) / det
	adj := make([]float64, n)
	for i := 0; i < n; i++ {
		adj[i] = x[i] - alphaA*(bA[i]-mbA) - alphaB*(bB[i]-mbB)
	}
	m, c := meanCI(adj)
	return alphaA, alphaB, m, c
}

func meanCI(xs []float64) (mean, ci float64) {
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var m2 float64
	for _, x := range xs {
		d := x - mean
		m2 += d * d
	}
	std := math.Sqrt(m2 / float64(len(xs)-1))
	se := std / math.Sqrt(float64(len(xs)))
	return mean, 1.96 * se
}
