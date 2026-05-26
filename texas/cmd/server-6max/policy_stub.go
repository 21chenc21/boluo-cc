//go:build !onnx

package main

import (
	"fmt"

	"github.com/boluo/texas/engine/nlhe6"
)

type modelPolicy struct{}

func loadModel(path string) (*modelPolicy, error) {
	return nil, fmt.Errorf("NN ONNX requires build tag `onnx`: go build -tags onnx ./cmd/server-6max")
}

func (*modelPolicy) Close() error                      { return nil }
func (*modelPolicy) Forward(s *nlhe6.State) ([]float64, error) {
	return nil, fmt.Errorf("stub")
}
