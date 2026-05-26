//go:build onnx

package main

import (
	"math"

	"github.com/boluo/texas/engine/nlhe6"
	"github.com/boluo/texas/server"
)

type modelPolicy struct {
	model *server.PolicyModel
}

func loadModel(path string) (*modelPolicy, error) {
	m, err := server.OpenPolicyDims(path, nlhe6.FeatureDimMultiStreet, 6)
	if err != nil {
		return nil, err
	}
	return &modelPolicy{model: m}, nil
}

func (m *modelPolicy) Close() error {
	return m.model.Close()
}

// Forward — compute legal-action probabilities for hero at s.Cur via NN
// inference + softmax over legal mask.
func (m *modelPolicy) Forward(s *nlhe6.State) ([]float64, error) {
	feat := nlhe6.FeatureVecMultiStreet(s)
	logits, err := m.model.Forward(feat[:])
	if err != nil {
		return nil, err
	}
	const numActions = 6
	legal := s.LegalActions()
	mask := make([]bool, numActions)
	idxToLegal := make([]int, numActions)
	for i := range idxToLegal {
		idxToLegal[i] = -1
	}
	for li, a := range legal {
		var idx int
		switch a.Kind {
		case nlhe6.ActionFold:
			idx = 0
		case nlhe6.ActionCheckCall:
			idx = 1
		case nlhe6.ActionBet:
			idx = 2 + int(a.SizeIdx)
		case nlhe6.ActionAllIn:
			idx = 5
		default:
			continue
		}
		mask[idx] = true
		idxToLegal[idx] = li
	}
	var maxL float32 = -1e9
	for i := 0; i < numActions; i++ {
		if mask[i] && logits[i] > maxL {
			maxL = logits[i]
		}
	}
	var sum float64
	rawProbs := make([]float64, numActions)
	for i := 0; i < numActions; i++ {
		if mask[i] {
			rawProbs[i] = math.Exp(float64(logits[i] - maxL))
			sum += rawProbs[i]
		}
	}
	out := make([]float64, len(legal))
	if sum <= 0 {
		u := 1.0 / float64(len(legal))
		for i := range out {
			out[i] = u
		}
		return out, nil
	}
	for i := 0; i < numActions; i++ {
		if mask[i] {
			out[idxToLegal[i]] = rawProbs[i] / sum
		}
	}
	return out, nil
}
