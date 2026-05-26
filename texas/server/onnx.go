//go:build onnx

// Package server — in-process ONNX inference for the distilled Leduc policy NN.
//
// Build tag `onnx` gates the C dependency. Without the tag, the package falls
// back to a stub (see onnx_stub.go) so CI / non-deploy builds skip the C lib.
//
// Shared library lookup at runtime:
//  1. ONNXRUNTIME_LIB env var
//  2. third_party/libonnxruntime.{so,dylib} relative to walking up from cwd
//  3. /usr/local/lib/libonnxruntime.{so,dylib}
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	initOnce sync.Once
	initErr  error
)

func ensureInit() error {
	initOnce.Do(func() {
		path := findLibrary()
		if path == "" {
			initErr = fmt.Errorf(
				"onnx: libonnxruntime not found. Set ONNXRUNTIME_LIB or place at " +
					"third_party/libonnxruntime.{so,dylib}")
			return
		}
		ort.SetSharedLibraryPath(path)
		if err := ort.InitializeEnvironment(); err != nil {
			initErr = fmt.Errorf("onnx: InitializeEnvironment: %w", err)
		}
	})
	return initErr
}

func findLibrary() string {
	suffix := ".so"
	if runtime.GOOS == "darwin" {
		suffix = ".dylib"
	}
	libName := "libonnxruntime" + suffix

	if p := os.Getenv("ONNXRUNTIME_LIB"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Walk up looking for go.mod, then check third_party/.
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 8; i++ {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				candidate := filepath.Join(dir, "third_party", libName)
				if _, err := os.Stat(candidate); err == nil {
					return candidate
				}
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	for _, p := range []string{
		"third_party/" + libName,
		"/usr/local/lib/" + libName,
		"/opt/homebrew/lib/" + libName,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// PolicyModel — loaded ONNX policy NN bound to fixed-size 1×35 input → 1×3 output.
type PolicyModel struct {
	mu      sync.Mutex
	sess    *ort.AdvancedSession
	inputT  *ort.Tensor[float32]
	outputT *ort.Tensor[float32]
	featDim int
	outDim  int
}

// OpenPolicy loads the ONNX file with default 35-d input (Leduc).
// For other feature dims, use OpenPolicyDims.
func OpenPolicy(path string) (*PolicyModel, error) {
	return openPolicyNamed(path, "features", "logits", 35, 3)
}

// OpenPolicyDims — same as OpenPolicy but with explicit input/output dims.
// Required for HUNL push/fold (featDim=33).
func OpenPolicyDims(path string, featDim, outDim int) (*PolicyModel, error) {
	return openPolicyNamed(path, "features", "logits", featDim, outDim)
}

func openPolicyNamed(path, inputName, outputName string, featDim, outDim int) (*PolicyModel, error) {
	if err := ensureInit(); err != nil {
		return nil, err
	}
	inputT, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(featDim)))
	if err != nil {
		return nil, fmt.Errorf("alloc input tensor: %w", err)
	}
	outputT, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(outDim)))
	if err != nil {
		inputT.Destroy()
		return nil, fmt.Errorf("alloc output tensor: %w", err)
	}
	sess, err := ort.NewAdvancedSession(
		path,
		[]string{inputName}, []string{outputName},
		[]ort.Value{inputT}, []ort.Value{outputT},
		nil,
	)
	if err != nil {
		inputT.Destroy()
		outputT.Destroy()
		return nil, fmt.Errorf("create session %s: %w", path, err)
	}
	return &PolicyModel{
		sess:    sess,
		inputT:  inputT,
		outputT: outputT,
		featDim: featDim,
		outDim:  outDim,
	}, nil
}

// Forward — single-sample logits. Returned slice is freshly-allocated, caller
// owns it. Concurrent calls serialized via mutex; for parallel hot-path
// inference allocate one PolicyModel per goroutine.
func (m *PolicyModel) Forward(features []float32) ([]float32, error) {
	if len(features) != m.featDim {
		return nil, fmt.Errorf("forward: expected %d features, got %d", m.featDim, len(features))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copy(m.inputT.GetData(), features)
	if err := m.sess.Run(); err != nil {
		return nil, fmt.Errorf("session run: %w", err)
	}
	out := make([]float32, m.outDim)
	copy(out, m.outputT.GetData())
	return out, nil
}

// Close releases session + tensors.
func (m *PolicyModel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	if err := m.sess.Destroy(); err != nil && first == nil {
		first = err
	}
	if err := m.inputT.Destroy(); err != nil && first == nil {
		first = err
	}
	if err := m.outputT.Destroy(); err != nil && first == nil {
		first = err
	}
	return first
}
