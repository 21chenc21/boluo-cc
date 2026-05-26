//go:build onnx

// onnx-bench — measure Go-side ONNX inference latency on the distilled Leduc
// policy NN. Counterpart to distill/bench_latency.py — should match Python's
// 14 µs/query, validating Go deployment path.
//
//	go run -tags onnx ./cmd/onnx-bench
package main

import (
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/server"
)

var (
	modelPath = flag.String("model", "distill/models/leduc-policy.onnx", "ONNX model path")
	warmup    = flag.Int("warmup", 100, "warmup iterations")
	trials    = flag.Int("trials", 100000, "measurement iterations")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	m, err := server.OpenPolicy(*modelPath)
	if err != nil {
		log.Fatalf("open model: %v", err)
	}
	defer m.Close()
	log.Printf("[bench] loaded %s", *modelPath)

	rng := rand.New(rand.NewSource(42))
	feat := make([]float32, cfr.FeatureDim)

	randFeat := func() {
		for i := range feat {
			feat[i] = rng.Float32()
		}
	}

	// Warmup
	for i := 0; i < *warmup; i++ {
		randFeat()
		if _, err := m.Forward(feat); err != nil {
			log.Fatalf("warmup: %v", err)
		}
	}

	// Measure single-query latency.
	t0 := time.Now()
	for i := 0; i < *trials; i++ {
		randFeat()
		if _, err := m.Forward(feat); err != nil {
			log.Fatalf("forward: %v", err)
		}
	}
	elapsed := time.Since(t0)
	perCall := elapsed.Seconds() / float64(*trials)
	log.Printf("[bench] single-query Go: %d trials, %.3f µs/call, %.0f qps",
		*trials, perCall*1e6, 1.0/perCall)
	log.Printf("[bench] (POC #4 threshold: < 10 ms = 10000 µs)")
}
