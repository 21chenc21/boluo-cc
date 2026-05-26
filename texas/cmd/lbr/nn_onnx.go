//go:build onnx

package main

import (
	"math"
	"math/rand"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/server"
)

type nnPolicy struct {
	model *server.PolicyModel
}

func loadNNPolicy(path string) (policy, error) {
	m, err := server.OpenPolicyDims(path, nlhe.FeatureDimMultiStreet, 6)
	if err != nil {
		return nil, err
	}
	return &nnPolicy{model: m}, nil
}

func (p *nnPolicy) sample(s *nlhe.State, rng *rand.Rand) nlhe.Action {
	legal := s.LegalActions()
	feat := nlhe.FeatureVecMultiStreet(s)
	logits, err := p.model.Forward(feat[:])
	if err != nil {
		return legal[rng.Intn(len(legal))]
	}
	const numActions = 6
	var mask [numActions]bool
	idxToLegal := make([]int, numActions)
	for i := range idxToLegal {
		idxToLegal[i] = -1
	}
	for li, a := range legal {
		var idx int
		switch a.Kind {
		case nlhe.ActionFold:
			idx = 0
		case nlhe.ActionCheckCall:
			idx = 1
		case nlhe.ActionBet:
			idx = 2 + int(a.SizeIdx)
		case nlhe.ActionAllIn:
			idx = 5
		default:
			continue
		}
		mask[idx] = true
		idxToLegal[idx] = li
	}
	var max float32 = -1e9
	for i := 0; i < numActions; i++ {
		if mask[i] && logits[i] > max {
			max = logits[i]
		}
	}
	var sum float64
	var probs [numActions]float64
	for i := 0; i < numActions; i++ {
		if mask[i] {
			probs[i] = math.Exp(float64(logits[i] - max))
			sum += probs[i]
		}
	}
	if sum <= 0 {
		return legal[rng.Intn(len(legal))]
	}
	r := rng.Float64() * sum
	var cum float64
	for i := 0; i < numActions; i++ {
		if mask[i] {
			cum += probs[i]
			if r < cum {
				return legal[idxToLegal[i]]
			}
		}
	}
	return legal[len(legal)-1]
}
