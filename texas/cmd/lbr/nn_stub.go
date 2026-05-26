//go:build !onnx

package main

import "fmt"

func loadNNPolicy(path string) (policy, error) {
	return nil, fmt.Errorf("NN policy requires build tag `onnx`: rebuild with `go build -tags onnx ./cmd/lbr`")
}
