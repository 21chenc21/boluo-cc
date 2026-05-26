// train-blueprint — runs vanilla CFR on Leduc, saves converged strategy to JSON.
// Used as the source for Week 2 distillation POC.
//
//	go run ./cmd/train-blueprint -iters 30000 -out blueprints/leduc-vanilla-30k.json
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/boluo/texas/cfr"
	"github.com/boluo/texas/engine/leduc"
)

var (
	iters = flag.Int("iters", 30000, "vanilla CFR iterations")
	out   = flag.String("out", "blueprints/leduc-vanilla.json", "output path")
)

func main() {
	flag.Parse()
	log.SetFlags(0)

	log.Printf("[bp] vanilla CFR on Leduc, %d iters", *iters)
	c := cfr.New()
	t0 := time.Now()
	logEvery := *iters / 10
	if logEvery < 1 {
		logEvery = 1
	}
	for c.Iters() < *iters {
		c.Iter()
		if c.Iters()%logEvery == 0 {
			avg := c.AverageStrategy()
			expl := cfr.Exploitability(avg)
			gv := cfr.GameValue(avg, leduc.P0)
			log.Printf("[bp] iter %d/%d  %.1fs  expl=%.6f  gv(P0)=%+.6f",
				c.Iters(), *iters, time.Since(t0).Seconds(), expl, gv)
		}
	}

	avg := c.AverageStrategy()
	expl := cfr.Exploitability(avg)
	gv := cfr.GameValue(avg, leduc.P0)
	log.Printf("[bp] DONE: %d iters, %.1fs, expl=%.6f, gv(P0)=%+.6f, infosets=%d",
		c.Iters(), time.Since(t0).Seconds(), expl, gv, c.NumInfosets())

	if err := cfr.SaveBlueprint(avg, *out, c.Iters()); err != nil {
		log.Fatalf("save: %v", err)
	}
	st, _ := os.Stat(*out)
	log.Printf("[bp] saved %s (%d bytes)", *out, st.Size())

	// Round-trip verify.
	loaded, meta, err := cfr.LoadBlueprint(*out)
	if err != nil {
		log.Fatalf("verify load: %v", err)
	}
	loadedExpl := cfr.Exploitability(loaded)
	loadedGv := cfr.GameValue(loaded, leduc.P0)
	log.Printf("[bp] verify load: expl=%.6f gv(P0)=%+.6f (meta says expl=%.6f gv=%+.6f)",
		loadedExpl, loadedGv, meta.Exploitability, meta.GameValueP0)
	if abs(loadedExpl-expl) > 1e-9 || abs(loadedGv-gv) > 1e-9 {
		fmt.Println("FAIL: round-trip mismatch")
		os.Exit(1)
	}
	log.Printf("[bp] round-trip OK")
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
