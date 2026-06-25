package main

import (
	"fmt"

	"github.com/boluo/v0-server/ofc"
)

func main() {
	lcg := ofc.NewLCG(1234)
	fmt.Println("Go LCG (seed=1234):")
	for i := 0; i < 8; i++ {
		f := lcg.NextFloat()
		fmt.Printf("  state=%d float=%.8f intn(10)=%d\n", lcg.State, f, int(f*10))
	}
}
