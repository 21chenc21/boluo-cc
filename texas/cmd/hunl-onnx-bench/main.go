//go:build onnx

// hunl-onnx-bench — measure HUNL push/fold ONNX inference latency in Go.
//
//	go run -tags onnx ./cmd/hunl-onnx-bench
package main

import (
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/server"
)

var (
	modelPath = flag.String("model", "distill/models/hunl-policy.onnx", "ONNX model")
	warmup    = flag.Int("warmup", 100, "warmup iterations")
	trials    = flag.Int("trials", 100000, "measurement iterations")
)

func main() {
	flag.Parse()
	log.SetFlags(0)
	m, err := server.OpenPolicyDims(*modelPath, nlhe.FeatureDimPushFold, 3)
	if err != nil {
		log.Fatalf("open model: %v", err)
	}
	defer m.Close()

	rng := rand.New(rand.NewSource(42))
	feat := make([]float32, nlhe.FeatureDimPushFold)
	for i := 0; i < *warmup; i++ {
		for j := range feat {
			feat[j] = rng.Float32()
		}
		_, _ = m.Forward(feat)
	}
	t0 := time.Now()
	for i := 0; i < *trials; i++ {
		for j := range feat {
			feat[j] = rng.Float32()
		}
		_, _ = m.Forward(feat)
	}
	elapsed := time.Since(t0).Seconds()
	perCall := elapsed / float64(*trials) * 1e6
	log.Printf("[bench] HUNL push/fold ONNX (Go): %d trials, %.2f µs/call, %.0f qps",
		*trials, perCall, 1.0/(elapsed/float64(*trials)))
}
