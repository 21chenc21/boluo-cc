// lbr-6max — Local Best Response exploitability lower bound for 6-max NLHE.
//
// Per LBR (Lisý & Bowling 2017): at each BR decision, evaluate EV of each
// candidate action assuming all remaining decisions are check-down. Pick
// max-EV action. Run for each of N seats as BR, average → LBR(σ).
//
// vs HU LBR: same algorithm, scaled to N players. Inner MC samples future
// board cards. Caveat: uses ACTUAL opp hole cards (perfect-info-opp-range),
// over-estimates BR strength → absolute LBR number inflated. Useful for
// relative comparison.
//
//	go run ./cmd/lbr-6max -iters 50000 -hands 2000 -mc-samples 20
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
	iters       = flag.Int("iters", 50000, "MCCFR iter to train σ")
	hands       = flag.Int("hands", 2000, "LBR eval hands per BR direction (×N total)")
	mcSamples   = flag.Int("mc-samples", 20, "inner MC samples for EV-after-action")
	numPlayers  = flag.Int("players", 6, "table size")
	stackBBs    = flag.Int("stack", 20, "stack in BB")
	seedTrain   = flag.Int64("seed-train", 42, "MCCFR seed")
	seedEval    = flag.Int64("seed-eval", 9999, "LBR eval seed")
	preflopPath = flag.String("preflop", "blueprints/preflop-buckets-K20.json", "preflop bucket")
	flopPath    = flag.String("flop", "blueprints/flop-buckets-K50.json", "flop bucket")
	turnPath    = flag.String("turn", "blueprints/turn-buckets-K50.json", "turn bucket")
	riverPath   = flag.String("river", "blueprints/river-buckets-K50.json", "river bucket")
	betFracs    = flag.String("bet-frac", "0.5,1.0,2.0", "bet sizes")
	nnPath      = flag.String("nn", "", "if set, σ replaced by NN ONNX (build tag onnx)")
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
	idFn := nlhe6.MultiStreetIDFn(b)

	cfg := nlhe6.DefaultConfigN(*numPlayers)
	cfg.StartStack = 2 * (*stackBBs)
	cfg.BetSizes = parseBetSizes(*betFracs)
	log.Printf("[lbr6] players=%d stack=%dBB / bet-sizes=%v", *numPlayers, *stackBBs, cfg.BetSizes)

	t0 := time.Now()
	var p policy
	if *nnPath != "" {
		nn, err := loadNNPolicy(*nnPath)
		if err != nil {
			log.Fatalf("load NN: %v", err)
		}
		p = nn
		log.Printf("[lbr6] σ replaced by NN policy from %s", *nnPath)
	} else {
		m := nlhe6.NewMCCFR(cfg, *seedTrain).WithIDFn(idFn)
		for i := 0; i < *iters; i++ {
			m.Iter()
		}
		log.Printf("[lbr6] σ trained: %d iter / %.1fs / %d infosets",
			*iters, time.Since(t0).Seconds(), m.NumInfosets())
		p = &sigmaPolicy{probs: m.AverageStrategy(), idFn: idFn}
	}

	bb := float64(cfg.BigBlind)
	rng := rand.New(rand.NewSource(*seedEval))

	t1 := time.Now()
	var allWins []float64
	for brSeat := 0; brSeat < *numPlayers; brSeat++ {
		wins := make([]float64, *hands)
		for h := 0; h < *hands; h++ {
			wins[h] = lbrOneHand(cfg, p, nlhe6.Seat(brSeat), rng, *mcSamples) / bb * 1000
		}
		mean, ci := meanCI(wins)
		log.Printf("[lbr6] BR=seat %d vs σ_others: %+.1f ±%.1f mbb/g", brSeat, mean, ci)
		allWins = append(allWins, wins...)
	}
	combMean, combCI := meanCI(allWins)
	elapsed := time.Since(t1)

	fmt.Println()
	fmt.Printf("=== LBR-6max (BR rotation × %d seats, %d hands each, %d MC inner, %.1fs) ===\n",
		*numPlayers, *hands, *mcSamples, elapsed.Seconds())
	fmt.Printf("LBR(σ) = %+.1f ±%.1f mbb/g (95%% CI)\n", combMean, combCI)
	fmt.Println()
	fmt.Println("Interpretation: σ can be exploited for at least this much against a")
	fmt.Println("simple BR strategy (check-down after move). True exploitability ≥ LBR.")
	fmt.Println("Caveat: uses ACTUAL opp hole cards → inflates by 2-5x vs range-aware LBR.")
}

// lbrOneHand — one BR direction. Random deal + BR picks max-EV at each
// decision; other seats sample from σ.
func lbrOneHand(cfg *nlhe6.GameConfig, σ policy, brSeat nlhe6.Seat, rng *rand.Rand, mc int) float64 {
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

	for {
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
			return float64(s.Payoff(brSeat))
		}
		if s.Cur == brSeat {
			best := -math.MaxFloat64
			var bestA nlhe6.Action
			for _, a := range s.LegalActions() {
				v := lbrActionEV(s, a, brSeat, rng, mc)
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

// lbrActionEV — apply candidate action, then check-down to terminal with
// random inner board, average BR payoff over `mc` samples.
//
// Multi-way Fold: BR's payoff after fold is fixed at -Wagered (BR can no
// longer win), regardless of how the rest of game plays out. Skip rollout.
func lbrActionEV(s *nlhe6.State, a nlhe6.Action, brSeat nlhe6.Seat, rng *rand.Rand, mc int) float64 {
	if a.Kind == nlhe6.ActionFold {
		// BR loses their wagered chips on fold. No rollout needed.
		return -float64(s.Wagered[brSeat])
	}
	total := 0.0
	for k := 0; k < mc; k++ {
		snap := s.Snapshot()
		s.Apply(a)
		checkdownRollout(s, rng)
		total += float64(s.Payoff(brSeat))
		s.Restore(snap)
	}
	return total / float64(mc)
}

// checkdownRollout — advance to terminal: deal random future board + everyone
// CheckCalls every decision.
func checkdownRollout(s *nlhe6.State, rng *rand.Rand) {
	n := s.Cfg.NumPlayers
	for {
		for {
			nNeed, needs := s.NeedsBoard()
			if !needs {
				break
			}
			var used [52]bool
			for i := 0; i < n; i++ {
				used[s.Hole[i][0]] = true
				used[s.Hole[i][1]] = true
			}
			for i := uint8(0); i < s.NumBoard; i++ {
				used[s.Board[i]] = true
			}
			for i := 0; i < nNeed; i++ {
				for {
					c := nlhe6.Card(rng.Intn(52))
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
		legal := s.LegalActions()
		picked := legal[0]
		for _, a := range legal {
			if a.Kind == nlhe6.ActionCheckCall {
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
