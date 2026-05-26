// build-preflop-buckets — Monte-Carlo equity table for 169 preflop hand types,
// then K-means cluster into K buckets. Save as JSON for engine consumption.
//
//	go run ./cmd/build-preflop-buckets -k 20 -samples 100000 \
//	    -out blueprints/preflop-buckets-K20.json
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
	k              = flag.Int("k", 20, "number of buckets")
	samples        = flag.Int("samples", 100000, "MC samples per hand type")
	seed           = flag.Int64("seed", 42, "RNG seed")
	mode           = flag.String("mode", "EHS", "bucketing mode: EHS or OCHS")
	numOppClusters = flag.Int("opp-clusters", 5, "(OCHS only) opp range partition count")
	out            = flag.String("out", "", "output JSON path (default: derived from mode)")
	verbose        = flag.Bool("v", false, "print bucket contents")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	if *out == "" {
		if *mode == "OCHS" {
			*out = fmt.Sprintf("blueprints/preflop-buckets-OCHS-K%d.json", *k)
		} else {
			*out = fmt.Sprintf("blueprints/preflop-buckets-K%d.json", *k)
		}
	}
	log.Printf("[build] preflop buckets: mode=%s K=%d samples=%d seed=%d", *mode, *k, *samples, *seed)

	t0 := time.Now()
	var bp *abstraction.PreflopBuckets
	switch *mode {
	case "EHS":
		bp = abstraction.Build(*k, *samples, *seed)
	case "OCHS":
		log.Printf("[build] OCHS with %d opp clusters", *numOppClusters)
		bp = abstraction.BuildOCHS(*k, *numOppClusters, *samples, *seed)
	case "lossless":
		log.Printf("[build] lossless 169-bucket identity mapping (ignoring -k)")
		bp = abstraction.BuildLossless(*samples, *seed)
	default:
		log.Fatalf("unknown mode %q (want EHS, OCHS, or lossless)", *mode)
	}
	log.Printf("[build] done in %.1fs", time.Since(t0).Seconds())

	// Pretty-print bucket distribution.
	fmt.Println()
	fmt.Println("=== Bucket contents (low → high equity) ===")
	for bid := 0; bid < bp.K; bid++ {
		hands := bp.HandsInBucket(bid)
		// Center summary: 1-D for EHS, sum/mean for OCHS.
		var centerStr string
		if bp.Mode == "OCHS" && bid < len(bp.CentersND) {
			var sum float64
			for _, v := range bp.CentersND[bid] {
				sum += v
			}
			centerStr = fmt.Sprintf("center mean=%.4f", sum/float64(len(bp.CentersND[bid])))
		} else if bid < len(bp.Centers) {
			centerStr = fmt.Sprintf("center eq=%.4f", bp.Centers[bid])
		}
		showN := 8
		if len(hands) < showN {
			showN = len(hands)
		}
		fmt.Printf("Bucket %2d (%s, %d hands): %v\n", bid, centerStr, len(hands), hands[:showN])
		if len(hands) > 8 && !*verbose {
			fmt.Printf("    ... +%d more\n", len(hands)-8)
		} else if *verbose {
			for _, h := range hands[8:] {
				fmt.Printf("    %s\n", h)
			}
		}
	}
	fmt.Println()

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := bp.Save(*out); err != nil {
		log.Fatalf("save: %v", err)
	}
	st, _ := os.Stat(*out)
	log.Printf("[build] saved %s (%d bytes)", *out, st.Size())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
