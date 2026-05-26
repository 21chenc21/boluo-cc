// h2h-self — head-to-head play between two MCCFR checkpoints with
// duplicate-hands variance reduction.
//
// Industry-standard "daily smoke" metric (per LBR paper + Pluribus methodology):
// train two strategies (e.g. candidate iters_a vs older iters_b under same
// config), play N random deals, swap who has which hole cards on a "duplicate"
// rerun, average the payoff. Variance cut roughly 50% per the ACPC duplicate
// matching scheme.
//
// Reports mbb/g (milli-big-blinds per game) for player A relative to B with
// 95% confidence interval.
//
//	go run ./cmd/h2h-self -iters-a 1000000 -iters-b 200000 -hands 5000
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

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	itersA      = flag.Int("iters-a", 1000000, "MCCFR iterations for player A (candidate)")
	itersB      = flag.Int("iters-b", 200000, "MCCFR iterations for player B (baseline)")
	hands       = flag.Int("hands", 5000, "number of unique deals (each played twice with hole swap = 2*hands games)")
	stackBBs    = flag.Int("stack", 20, "stack in BB units")
	seedTrain   = flag.Int64("seed-train", 42, "RNG seed for MCCFR training")
	seedDeal    = flag.Int64("seed-deal", 12345, "RNG seed for h2h dealing (independent of training)")
	preflopA    = flag.String("preflop-a", "blueprints/preflop-buckets-lossless.json", "A: preflop bucket JSON")
	preflopB    = flag.String("preflop-b", "", "B: preflop bucket JSON (defaults to -preflop-a)")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket JSON (shared)")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket JSON (shared)")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket JSON (shared)")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "comma-separated bet sizes")
	useAIVAT    = flag.Bool("aivat", false, "enable simple AIVAT-style control-variate variance reduction")
	nnPolicyA   = flag.String("nn-a", "", "if set, load NN ONNX as policy A (overrides MCCFR training; needs build tag onnx)")
	nnPolicyB   = flag.String("nn-b", "", "if set, load NN ONNX as policy B")
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

// policy — pluggable action sampler. Implementations: sigmaPolicy (lookup
// MCCFR avg strategy by abstract ID) and nnPolicy (ONNX forward + softmax over
// legal mask). NN impl in nn_onnx.go (build tag onnx).
type policy interface {
	sample(s *nlhe.State, rng *rand.Rand) nlhe.Action
}

type sigmaPolicy struct {
	probs map[uint64][]float64
	idFn  func(*nlhe.State) uint64
}

func (p *sigmaPolicy) sample(s *nlhe.State, rng *rand.Rand) nlhe.Action {
	legal := s.LegalActions()
	id := p.idFn(s)
	pr, ok := p.probs[id]
	if !ok {
		idx := rng.Intn(len(legal))
		return legal[idx]
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
	loadBuckets := func(preflopPath string) *abstraction.MultiStreetBuckets {
		pre, err := abstraction.LoadPreflopBuckets(preflopPath)
		if err != nil {
			log.Fatalf("load preflop %s: %v", preflopPath, err)
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
	idFnA := func(s *nlhe.State) uint64 { return bA.ID(s) }
	idFnB := func(s *nlhe.State) uint64 { return bB.ID(s) }

	betSizes := parseBetSizes(*betFracs)
	cfg := &nlhe.GameConfig{
		SmallBlind: 1, BigBlind: 2,
		StartStack: 2 * (*stackBBs),
		BetSizes:   betSizes,
	}
	log.Printf("[h2h] stack=%dBB / bet-sizes=%v", *stackBBs, betSizes)
	log.Printf("[h2h] A preflop: %s (K=%d)", *preflopA, bA.Preflop.K)
	log.Printf("[h2h] B preflop: %s (K=%d)", *preflopB, bB.Preflop.K)

	// Train A.
	t0 := time.Now()
	mA := nlhe.NewMCCFR(cfg, *seedTrain).WithIDFn(idFnA)
	for i := 0; i < *itersA; i++ {
		mA.Iter()
	}
	log.Printf("[h2h] A trained: %d iter / %.1fs / %d infosets", *itersA, time.Since(t0).Seconds(), mA.NumInfosets())

	// Train B (different seed to avoid identical-noise correlation).
	t1 := time.Now()
	mB := nlhe.NewMCCFR(cfg, *seedTrain+1).WithIDFn(idFnB)
	for i := 0; i < *itersB; i++ {
		mB.Iter()
	}
	log.Printf("[h2h] B trained: %d iter / %.1fs / %d infosets", *itersB, time.Since(t1).Seconds(), mB.NumInfosets())

	var pA, pB policy = &sigmaPolicy{probs: mA.AverageStrategy(), idFn: idFnA}, &sigmaPolicy{probs: mB.AverageStrategy(), idFn: idFnB}
	// NN override: if -nn-a / -nn-b set, load ONNX-backed NN policy.
	// Requires build tag `onnx`. Stub returns error otherwise.
	if *nnPolicyA != "" {
		nn, err := loadNNPolicy(*nnPolicyA)
		if err != nil {
			log.Fatalf("load NN policy A: %v", err)
		}
		pA = nn
		log.Printf("[h2h] A: NN policy from %s", *nnPolicyA)
	}
	if *nnPolicyB != "" {
		nn, err := loadNNPolicy(*nnPolicyB)
		if err != nil {
			log.Fatalf("load NN policy B: %v", err)
		}
		pB = nn
		log.Printf("[h2h] B: NN policy from %s", *nnPolicyB)
	}

	rng := rand.New(rand.NewSource(*seedDeal))
	bb := float64(cfg.BigBlind)

	// Self-play baseline for AIVAT control variate: σ_A vs σ_A on same cards
	// has zero expected payoff by position symmetry, but realized payoff
	// reveals card-luck. Stronger correlation with real game than uniform-
	// random baseline (matches σ's dynamics).

	// Each "hand" = a fresh deal played twice (A as P0, then A as P1 with same cards).
	// Returns mean A-side chips/hand averaged over the duplicate pair.
	tStart := time.Now()
	gameResults := make([]float64, *hands)
	baselineA := make([]float64, *hands) // σ_A self-play on same cards
	baselineB := make([]float64, *hands) // σ_B self-play on same cards
	for h := 0; h < *hands; h++ {
		// Deal 4 distinct cards (P0 + P1 hole) + 5 board cards.
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
		hP0a := [2]nlhe.Card{deck[0], deck[1]}
		hP0b := [2]nlhe.Card{deck[2], deck[3]}
		boardCards := [5]nlhe.Card{deck[4], deck[5], deck[6], deck[7], deck[8]}

		// Game 1: A = P0, B = P1.
		payA1 := playOneGame(cfg, hP0a, hP0b, boardCards, pA, pB, rng)
		// Game 2 (duplicate): A = P1, B = P0, with hole cards swapped to match (so A holds hP0b).
		payA2 := playOneGame(cfg, hP0b, hP0a, boardCards, pB, pA, rng)
		paySum := payA1 + (-payA2)
		gameResults[h] = paySum / 2 / bb * 1000

		if *useAIVAT {
			// Self-play baselines: σ_A vs σ_A, σ_B vs σ_B on same cards.
			// E[both] = 0 by position symmetry; per-deal realized value
			// reveals card-luck → control variates.
			a1 := playOneGame(cfg, hP0a, hP0b, boardCards, pA, pA, rng)
			a2 := playOneGame(cfg, hP0b, hP0a, boardCards, pA, pA, rng)
			baselineA[h] = (a1 + (-a2)) / 2 / bb * 1000
			bb1 := playOneGame(cfg, hP0a, hP0b, boardCards, pB, pB, rng)
			bb2 := playOneGame(cfg, hP0b, hP0a, boardCards, pB, pB, rng)
			baselineB[h] = (bb1 + (-bb2)) / 2 / bb * 1000
		}
	}
	elapsed := time.Since(tStart)

	// Stats: mean, std, 95% CI.
	mean, ci95 := meanCI(gameResults)

	fmt.Println()
	fmt.Printf("=== h2h-self result (%d duplicate-hand pairs, %.1fs) ===\n", *hands, elapsed.Seconds())
	fmt.Printf("Raw        : %+.1f mbb/g  ±%.1f  (95%% CI [%+.1f, %+.1f])\n",
		mean, ci95, mean-ci95, mean+ci95)

	if *useAIVAT {
		// Two-baseline control variate: AIVAT_x = x - α_A*(b_A - mb_A) - α_B*(b_B - mb_B).
		// α* solves min Var via 2x2 normal equations.
		alphaA, alphaB, aivatMean, aivatCI := controlVariate2(gameResults, baselineA, baselineB)
		varianceReduction := 1 - (aivatCI*aivatCI)/(ci95*ci95)
		fmt.Printf("AIVAT (α_A=%.3f, α_B=%.3f): %+.1f mbb/g  ±%.1f  (95%% CI [%+.1f, %+.1f])  variance ↓ %.0f%%\n",
			alphaA, alphaB, aivatMean, aivatCI, aivatMean-aivatCI, aivatMean+aivatCI, 100*varianceReduction)
		mean = aivatMean
		ci95 = aivatCI
	}

	fmt.Printf("       (A = %d iter, B = %d iter)\n", *itersA, *itersB)
	if mean-ci95 > 0 {
		fmt.Println("       → A is statistically better than B")
	} else if mean+ci95 < 0 {
		fmt.Println("       → B is statistically better than A")
	} else {
		fmt.Println("       → no statistically significant difference")
	}
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

// controlVariate2 — two zero-mean control variates (σ_A and σ_B self-play).
// Solves 2x2 normal equations:
//   [varA  covAB] [αA]   [covA]
//   [covAB  varB] [αB] = [covB]
// where covA = Cov(x, bA), covB = Cov(x, bB), varA = Var(bA), varB = Var(bB),
// covAB = Cov(bA, bB). Returns optimal (αA, αB), adjusted mean, 95% CI.
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
		return 0, 0, mx, ciFromVar(x, mx)
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

func ciFromVar(xs []float64, mean float64) float64 {
	var m2 float64
	for _, x := range xs {
		d := x - mean
		m2 += d * d
	}
	std := math.Sqrt(m2 / float64(len(xs)-1))
	return 1.96 * std / math.Sqrt(float64(len(xs)))
}

// playOneGame — play a single hand using pre-determined cards + policies.
// Cards are forced (not sampled) so duplicate-hand pairs are deterministic in
// the dealing dimension. Policy sampling is the only stochasticity.
//
// Returns P0's net chip payoff (positive = P0 won, negative = P1 won).
func playOneGame(cfg *nlhe.GameConfig, holeP0, holeP1 [2]nlhe.Card, board [5]nlhe.Card,
	policyP0, policyP1 policy, rng *rand.Rand) float64 {
	s := nlhe.NewState(cfg)
	s.SetHole(nlhe.P0, holeP0[0], holeP0[1])
	s.SetHole(nlhe.P1, holeP1[0], holeP1[1])
	boardIdx := 0
	for {
		// Fill board if street transition needs it.
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			for i := 0; i < n; i++ {
				s.Board[s.NumBoard] = board[boardIdx]
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return float64(s.Payoff(nlhe.P0))
		}
		var p policy
		if s.Cur == nlhe.P0 {
			p = policyP0
		} else {
			p = policyP1
		}
		s.Apply(p.sample(s, rng))
	}
}
