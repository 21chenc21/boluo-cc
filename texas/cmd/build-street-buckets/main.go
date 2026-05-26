// build-street-buckets — generic flop/turn/river bucket builder.
//
//	go run ./cmd/build-street-buckets -street flop  -k 100 -outer 100000 -inner 500
//	go run ./cmd/build-street-buckets -street turn  -k 100 -outer 200000 -inner 500
//	go run ./cmd/build-street-buckets -street river -k 100 -outer 200000 -inner 200
//
// Theoretical canonical class counts (suit-isomorphic):
//
//	flop  ~ 1,286,792
//	turn  ~ 13,960,050
//	river ~ 123,156,254
//
// Sampling can cover only a fraction. ForOrFallback handles unseen classes.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/boluo/texas/engine/nlhe/abstraction"
)

var (
	street = flag.String("street", "flop", "street: flop|turn|river")
	k      = flag.Int("k", 100, "number of buckets")
	outer  = flag.Int("outer", 100000, "outer samples")
	inner  = flag.Int("inner", 500, "inner MC samples per equity estimate")
	seed   = flag.Int64("seed", 42, "RNG seed")
	out    = flag.String("out", "", "output JSON path (default: derived from street + K)")
)

func main() {
	flag.Parse()
	log.SetFlags(0)
	streetIdx := map[string]int{"flop": 3, "turn": 4, "river": 5}[*street]
	if streetIdx == 0 {
		log.Fatalf("invalid -street %q (flop|turn|river)", *street)
	}
	if *out == "" {
		*out = fmt.Sprintf("blueprints/%s-buckets-K%d.json", *street, *k)
	}
	log.Printf("[build] %s buckets: K=%d outer=%d inner=%d seed=%d",
		*street, *k, *outer, *inner, *seed)

	t0 := time.Now()
	bp := abstraction.BuildStreet(streetIdx, *k, *outer, *inner, *seed)
	log.Printf("[build] done in %.1fs", time.Since(t0).Seconds())
	log.Printf("[build] unique canonical classes: %d, coverage %.2f%%",
		len(bp.Buckets), bp.CoveragePct())

	// Bucket size distribution.
	bucketCounts := make([]int, *k)
	for _, bid := range bp.Buckets {
		bucketCounts[bid]++
	}
	fmt.Println()
	fmt.Println("=== Bucket sizes (low → high equity) ===")
	for i, n := range bucketCounts {
		fmt.Printf("  bucket %3d  center eq=%.4f  classes=%d\n", i, bp.Centers[i], n)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := bp.Save(*out); err != nil {
		log.Fatalf("save: %v", err)
	}
	st, _ := os.Stat(*out)
	log.Printf("[build] saved %s (%.1f MB)", *out, float64(st.Size())/(1024*1024))
}
