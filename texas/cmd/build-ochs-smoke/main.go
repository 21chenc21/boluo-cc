package main
import (
  "fmt"
  "log"
  "time"
  "github.com/boluo/texas/engine/nlhe/abstraction"
)
func main() {
  for _, street := range []int{3,4,5} {
    name := []string{"","","", "flop", "turn", "river"}[street]
    t0 := time.Now()
    bp := abstraction.BuildStreetOCHS(street, 20, 5, 10000, 200, 42)
    fmt.Printf("%s OCHS K=20 opp=5 outer=10000 inner=200: %.1fs, %d unique, %d centers\n",
      name, time.Since(t0).Seconds(), len(bp.Buckets), len(bp.Centers))
    path := fmt.Sprintf("blueprints/%s-buckets-OCHS-K20.json", name)
    if err := bp.Save(path); err != nil { log.Fatalf("save %s: %v", path, err) }
  }
}
