//go:build !onnx

// Stub for when the `onnx` build tag is not set. Lets non-deploy code paths
// build/test without the C shared library.
package server

import "errors"

type PolicyModel struct{}

func OpenPolicy(path string) (*PolicyModel, error) {
	return nil, errors.New("server: built without `onnx` tag; rebuild with `go build -tags onnx`")
}

func OpenPolicyDims(path string, featDim, outDim int) (*PolicyModel, error) {
	return nil, errors.New("server: built without `onnx` tag; rebuild with `go build -tags onnx`")
}

func (m *PolicyModel) Forward([]float32) ([]float32, error) {
	return nil, errors.New("server: stub PolicyModel")
}

func (m *PolicyModel) Close() error { return nil }
