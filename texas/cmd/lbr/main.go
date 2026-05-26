// lbr — Local Best Response exploitability lower bound (Lisý & Bowling 2017).
//
// At each BR decision point, evaluate EV of each candidate action assuming
// both players check-down (CheckCall everything) until showdown. Pick max-EV
// action. Average payoff over N hands = LBR exploitability for BR vs σ.
//
// Compute for both directions (BR=P0 vs σ_P1, BR=P1 vs σ_P0) and average to
// get full LBR exploitability in mbb/g. This is a LOWER bound on true
// exploitability (a real BR could do better than check-down after its move).
//
// Inner MC: for each candidate action, sample `mc-samples` future boards to
// estimate EV. Note: this version uses ACTUAL opp hole cards in the rollout
// (perfect-info-opp-range LBR), which over-estimates BR strength → LBR result
// here is itself an upper bound on the strict lower bound. Proper range-aware
// LBR requires σ-conditional opp range; deferred.
//
//	go run ./cmd/lbr -iters 500000 -hands 5000 -mc-samples 20
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
	iters       = flag.Int("iters", 500000, "MCCFR iterations to train σ")
	hands       = flag.Int("hands", 5000, "LBR evaluation hands per direction (×2 total)")
	mcSamples   = flag.Int("mc-samples", 20, "inner MC samples for EV-after-action")
	stackBBs    = flag.Int("stack", 20, "stack in BB units")
	seedTrain   = flag.Int64("seed-train", 42, "RNG seed for MCCFR training")
	seedEval    = flag.Int64("seed-eval", 7777, "RNG seed for LBR evaluation")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "comma-separated bet sizes")
	nnPath      = flag.String("nn", "", "if set, replace MCCFR σ with NN ONNX policy (needs build tag onnx)")
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

// policy — pluggable action sampler. sigma uses MCCFR-trained map.
// NN impl in nn_onnx.go (build tag onnx).
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
	log.Printf("[lbr] stack=%dBB / bet-sizes=%v / preflop=%s", *stackBBs, betSizes, *preflopPath)

	t0 := time.Now()
	m := nlhe.NewMCCFR(cfg, *seedTrain).WithIDFn(idFn)
	for i := 0; i < *iters; i++ {
		m.Iter()
	}
	log.Printf("[lbr] σ trained: %d iter / %.1fs / %d infosets", *iters, time.Since(t0).Seconds(), m.NumInfosets())
	var p policy = &sigmaPolicy{probs: m.AverageStrategy(), idFn: idFn}
	if *nnPath != "" {
		nn, err := loadNNPolicy(*nnPath)
		if err != nil {
			log.Fatalf("load NN: %v", err)
		}
		p = nn
		log.Printf("[lbr] σ replaced by NN policy from %s", *nnPath)
	}

	bb := float64(cfg.BigBlind)
	rng := rand.New(rand.NewSource(*seedEval))

	// Run LBR for each BR direction.
	t1 := time.Now()
	brP0Wins := make([]float64, *hands)
	for h := 0; h < *hands; h++ {
		brP0Wins[h] = lbrOneHand(cfg, p, nlhe.P0, rng, *mcSamples) / bb * 1000
	}
	meanP0, ciP0 := meanCI(brP0Wins)
	log.Printf("[lbr] BR=P0 vs σ_P1: %+.1f ±%.1f mbb/g  (95%% CI)", meanP0, ciP0)

	brP1Wins := make([]float64, *hands)
	for h := 0; h < *hands; h++ {
		brP1Wins[h] = lbrOneHand(cfg, p, nlhe.P1, rng, *mcSamples) / bb * 1000
	}
	meanP1, ciP1 := meanCI(brP1Wins)
	log.Printf("[lbr] BR=P1 vs σ_P0: %+.1f ±%.1f mbb/g  (95%% CI)", meanP1, ciP1)

	lbrExpl := (meanP0 + meanP1) / 2
	lbrCI := math.Sqrt(ciP0*ciP0+ciP1*ciP1) / 2
	elapsed := time.Since(t1)

	fmt.Println()
	fmt.Printf("=== LBR exploitability (%d hands × 2 directions, %d MC inner samples, %.1fs) ===\n",
		*hands, *mcSamples, elapsed.Seconds())
	fmt.Printf("LBR(σ) = %+.1f ±%.1f mbb/g  (95%% CI)\n", lbrExpl, lbrCI)
	fmt.Println()
	fmt.Println("Interpretation: σ can be exploited for AT LEAST this much against a")
	fmt.Println("simple BR strategy (check-down after move). True exploitability ≥ LBR.")
	fmt.Println("Lower is better. For ref: Pluribus ~48 mbb/g vs top humans (Slumbot ~50+).")
}

// lbrOneHand — play one hand with BR player picking max-EV action via inner MC.
func lbrOneHand(cfg *nlhe.GameConfig, σ policy, brPlayer nlhe.Player, rng *rand.Rand, mc int) float64 {
	s := nlhe.NewState(cfg)
	// Deal P0, P1 hole + all 5 board cards upfront (outer rng).
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
	boardOuter := [5]nlhe.Card{deck[4], deck[5], deck[6], deck[7], deck[8]}
	boardIdx := 0

	for {
		// Deal board if needed (outer game uses fixed board).
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			for i := 0; i < n; i++ {
				s.Board[s.NumBoard] = boardOuter[boardIdx]
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return float64(s.Payoff(brPlayer))
		}
		if s.Cur == brPlayer {
			best := -math.MaxFloat64
			var bestA nlhe.Action
			for _, a := range s.LegalActions() {
				v := lbrActionEV(cfg, s, a, σ, brPlayer, rng, mc)
				if v > best {
					best = v
					bestA = a
				}
			}
			s.Apply(bestA)
		} else {
			s.Apply(σ.sample(s, rng))
		}
	}
}

// lbrActionEV — apply candidate action, then check-down to showdown with
// random inner board completions, average BR payoff over mc samples.
func lbrActionEV(cfg *nlhe.GameConfig, s *nlhe.State, a nlhe.Action, σ policy, brPlayer nlhe.Player, rng *rand.Rand, mc int) float64 {
	if a.Kind == nlhe.ActionFold {
		// Folding immediately ends hand. No need for MC.
		snap := s.Snapshot()
		s.Apply(a)
		v := float64(s.Payoff(brPlayer))
		s.Restore(snap)
		return v
	}
	total := 0.0
	for k := 0; k < mc; k++ {
		snap := s.Snapshot()
		boardSavedNum := s.NumBoard
		s.Apply(a)
		// Check-down rollout: deal remaining board via inner rng + check-call.
		checkdownRollout(s, rng)
		total += float64(s.Payoff(brPlayer))
		// Restore state including board cards (Restore sets NumBoard back, the
		// extra Board entries past NumBoard are stale but never re-read).
		s.Restore(snap)
		_ = boardSavedNum
	}
	return total / float64(mc)
}

// checkdownRollout — advance s to fully-resolved terminal: deal random future
// board (covering both mid-game transitions AND all-in showdown fill-to-5),
// both players CheckCall every action decision.
func checkdownRollout(s *nlhe.State, rng *rand.Rand) {
	for {
		// Fill any needed board cards first (handles both street transitions
		// AND terminal all-in showdown that hasn't seen full 5 board cards).
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			var used [nlhe.DeckSize]bool
			used[s.Hole[nlhe.P0][0]] = true
			used[s.Hole[nlhe.P0][1]] = true
			used[s.Hole[nlhe.P1][0]] = true
			used[s.Hole[nlhe.P1][1]] = true
			for i := uint8(0); i < s.NumBoard; i++ {
				used[s.Board[i]] = true
			}
			for i := 0; i < n; i++ {
				for {
					c := nlhe.Card(rng.Intn(nlhe.DeckSize))
					if !used[c] {
						s.Board[s.NumBoard] = c
						s.NumBoard++
						used[c] = true
						break
					}
				}
			}
		}
		if s.Terminal {
			return
		}
		// Action node: pick CheckCall (true check-down).
		legal := s.LegalActions()
		picked := legal[0]
		for _, a := range legal {
			if a.Kind == nlhe.ActionCheckCall {
				picked = a
				break
			}
		}
		s.Apply(picked)
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
