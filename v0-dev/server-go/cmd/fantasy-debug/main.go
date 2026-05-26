// fantasy-debug — 给 ExpertPlaceFantasy 喂 sample 14-card hand, 打印摆法 + 计分细节
package main

import (
	"fmt"
	"math/rand"

	"github.com/boluo/v0-server/ofc"
)

func cardsToStr(cs []ofc.Card) string {
	out := ""
	for _, c := range cs {
		out += c.String() + " "
	}
	return out
}

func handTypeName(t int) string {
	names := []string{"HighCard", "Pair", "TwoPair", "Trips", "Straight", "Flush", "FullHouse", "Quads", "SF", "RoyalF"}
	if t < 0 || t >= len(names) {
		return "?"
	}
	return names[t]
}

func placeWithRecover(dealt []ofc.Card, discardCount int) (r *ofc.FantasyResult, panicked bool) {
	defer func() {
		if rec := recover(); rec != nil {
			panicked = true
		}
	}()
	r = ofc.ExpertPlaceFantasy(dealt, discardCount)
	return
}

func main() {
	rng := rand.New(rand.NewSource(42))
	jokers := 2

	fmt.Printf("=== Fantasy placement debug (5 random 14-card hands, 2 jokers in deck) ===\n\n")

	for i := 0; i < 5; i++ {
		deck := ofc.MakeDeck(jokers)
		for j := len(deck) - 1; j > 0; j-- {
			k := rng.Intn(j + 1)
			deck[j], deck[k] = deck[k], deck[j]
		}
		dealt := deck[:14]
		fmt.Printf("--- sample %d ---\n", i+1)
		fmt.Printf("发 14 张: %s\n", cardsToStr(dealt))

		r, panicked := placeWithRecover(dealt, 1)
		if panicked {
			fmt.Printf("  ⚠ PANIC in solver\n\n")
			continue
		}
		if r == nil {
			fmt.Printf("  ⚠ nil result (no valid placement found)\n\n")
			continue
		}
		fmt.Printf("  top: %s   [%s]\n", cardsToStr(r.Layout.Top), handTypeName(r.Sc.TopEval.Type))
		fmt.Printf("  mid: %s   [%s]\n", cardsToStr(r.Layout.Middle), handTypeName(r.Sc.MidEval.Type))
		fmt.Printf("  bot: %s   [%s]\n", cardsToStr(r.Layout.Bottom), handTypeName(r.Sc.BotEval.Type))
		fmt.Printf("  royalty: top=%d mid=%d bot=%d  total=%d\n",
			r.Sc.TopRoyalty, r.Sc.MidRoyalty, r.Sc.BotRoyalty, r.Sc.Royalties)
		fmt.Printf("  foul=%v  fantasy=%v\n", r.Sc.Foul, r.Sc.Fantasy)
		refan := r.Sc.TopEval.Type >= ofc.TypeThreeOfAKind || r.Sc.BotEval.Type >= ofc.TypeFourOfAKind
		fmt.Printf("  refan=%v (top>=trips OR bot>=quads)\n\n", refan)
	}
}
