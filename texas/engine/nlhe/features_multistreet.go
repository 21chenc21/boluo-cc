package nlhe

import "sort"

// Features for HUNL multi-street NN distillation.
//
// === 288-d schema (6-max-friendly + action history) ===
//
// 同 schema works for HU (2 player) and 6-max (6 player). HU uses 1 of 5
// opponent slots; 6-max uses all 5. AlphaHoldem-inspired action history.
//
// Layout (6-max migration notes inline):
//
// HERO / BOARD (113 dims)
//	[ 0: 28]   hero hole — low rank + high rank + pair + suited
//	[28:113]   board 5 slots × 17 (rank 13 + suit 4), sorted desc rank
//
// STATE (44 dims)
//	[113:117]  street one-hot (preflop, flop, turn, river)
//	[117:123]  hero position one-hot, 6 slots [SB, BB, UTG, MP, CO, BTN]
//	           HU: slot 0 (SB / P0) or slot 1 (BB / P1).
//	[123:127]  hero (stack, bet-this-street, pot, last-raise) ratios
//	[127:147]  5 opp slots × 4 dims (stack, bet, pos offset, all-in)
//	           HU fills slot 0; 6-max fills 0-4 in canonical clockwise order.
//	[147:153]  legal action mask (Fold, CheckCall, Bet0, Bet1, Bet2, AllIn)
//	[153:157]  board structural (pairs, max-suit, rank-span, distinct ranks)
//
// DERIVED SCALARS (3 dims) — added per AlphaHoldem-era research recommendation:
//	[157]      pot odds (to_call / (pot + to_call))
//	[158]      SPR (hero_stack / pot, clamped to 10.0)
//	[159]      effective stack (min(hero_stack, max_opp_stack) / StartStack)
//
// ACTION HISTORY BLOCK (128 dims) — critical for distinguishing equal-pot but
// different-range scenarios (e.g. "limp→3bet→call" vs "open→3bet→call").
//	[160:288]  4 streets × 4 action slots × 8 dims/slot:
//	             dim 0: actor bit (0 = P0, 1 = P1)
//	             dim 1-6: action one-hot (Fold, CheckCall, Bet0, Bet1, Bet2, AllIn)
//	             dim 7: exists flag (1 = slot filled, 0 = no action here yet)
//	           Each street's actions written from slot 0 onwards. Overflow past
//	           slot 3 silently dropped (max 4 actions per street; rare in HU at
//	           20BB with our bet abstraction).
//
// === HU-specific assumptions (need rework for 6-max) ===
//
// Marked inline with "// HU:" comments. Migration to 6-max requires:
//   1. Hero position: HU uses slots 0/1. 6-max rotates through all 6.
//   2. Per-opponent slots: HU fills 1; 6-max fills 5 in canonical order.
//   3. Pot normalization: HU divides by 2×StartStack. 6-max needs NumPlayers×StartStack.
//   4. Opp position offset: HU writes 1.0; 6-max writes (opp_seat - hero_seat) mod N / N.
//   5. Action history actor bit (dim 0): HU writes raw P0/P1. 6-max would write
//      seat-relative-to-hero (0-5 normalized).
const FeatureDimMultiStreet = 288

func FeatureVecMultiStreet(s *State) [FeatureDimMultiStreet]float32 {
	var f [FeatureDimMultiStreet]float32

	cur := s.Cur
	c1 := s.Hole[cur][0]
	c2 := s.Hole[cur][1]
	r1, r2 := c1.Rank(), c2.Rank()
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	f[r1] = 1
	f[13+r2] = 1
	if c1.Rank() == c2.Rank() {
		f[26] = 1
	}
	if c1.Suit() == c2.Suit() {
		f[27] = 1
	}

	// Board cards sorted by descending rank for canonical order.
	type bc struct {
		rank, suit uint8
	}
	bcs := make([]bc, s.NumBoard)
	for i := uint8(0); i < s.NumBoard; i++ {
		bcs[i] = bc{rank: uint8(s.Board[i].Rank()), suit: uint8(s.Board[i].Suit())}
	}
	sort.Slice(bcs, func(i, j int) bool { return bcs[i].rank > bcs[j].rank })
	for i, b := range bcs {
		base := 28 + i*17
		f[base+int(b.rank)] = 1
		f[base+13+int(b.suit)] = 1
	}

	// Street one-hot.
	f[113+int(s.Street)] = 1

	// HU: hero position in 6-slot canonical layout.
	switch cur {
	case P0:
		f[117] = 1 // SB
	case P1:
		f[118] = 1 // BB
	}

	startStack := float32(s.Cfg.StartStack)
	pot := float32(s.Pot())
	if startStack > 0 {
		// HU: pot normalized by 2×StartStack (2-player total).
		f[123] = float32(s.Stacks[cur]) / startStack
		f[124] = float32(s.BetThisStreet[cur]) / startStack
		f[125] = pot / (2 * startStack)
		f[126] = float32(s.LastRaiseSize) / startStack
	}

	// HU: 1 opp slot filled (slot 0). 6-max fills 5.
	opp := cur.Other()
	const oppSlotsBase = 127
	if startStack > 0 {
		f[oppSlotsBase+0] = float32(s.Stacks[opp]) / startStack
		f[oppSlotsBase+1] = float32(s.BetThisStreet[opp]) / startStack
	}
	// HU: position offset hard-coded 1.0. 6-max computes from seat diff.
	f[oppSlotsBase+2] = 1.0
	if s.AllIn[opp] {
		f[oppSlotsBase+3] = 1
	}

	// Legal action mask.
	const maskBase = 147
	if !s.Terminal {
		for _, a := range s.LegalActions() {
			switch a.Kind {
			case ActionFold:
				f[maskBase+0] = 1
			case ActionCheckCall:
				f[maskBase+1] = 1
			case ActionBet:
				switch a.SizeIdx {
				case 0:
					f[maskBase+2] = 1
				case 1:
					f[maskBase+3] = 1
				case 2:
					f[maskBase+4] = 1
				}
			case ActionAllIn:
				f[maskBase+5] = 1
			}
		}
	}

	// Board structural features.
	if s.NumBoard > 0 {
		var rankCount [13]int
		var suitCount [4]int
		for i := uint8(0); i < s.NumBoard; i++ {
			rankCount[s.Board[i].Rank()]++
			suitCount[s.Board[i].Suit()]++
		}
		var pairs, distinct, minRank, maxRank int
		minRank = 13
		maxRank = -1
		for r, c := range rankCount {
			if c >= 2 {
				pairs++
			}
			if c >= 1 {
				distinct++
				if r < minRank {
					minRank = r
				}
				if r > maxRank {
					maxRank = r
				}
			}
		}
		var maxSuit int
		for _, c := range suitCount {
			if c > maxSuit {
				maxSuit = c
			}
		}
		f[153] = float32(pairs) / 2
		f[154] = float32(maxSuit) / 5
		f[155] = float32(maxRank-minRank) / 12
		f[156] = float32(distinct) / 5
	}

	// === Derived scalars ===
	toCall := float32(s.BetThisStreet[opp] - s.BetThisStreet[cur])
	if toCall < 0 {
		toCall = 0
	}
	if toCall+pot > 0 {
		f[157] = toCall / (toCall + pot)
	}
	if pot > 0 {
		spr := float32(s.Stacks[cur]) / pot
		if spr > 10 {
			spr = 10
		}
		f[158] = spr / 10
	}
	maxOpp := float32(s.Stacks[opp])
	heroStk := float32(s.Stacks[cur])
	eff := heroStk
	if maxOpp < eff {
		eff = maxOpp
	}
	if startStack > 0 {
		f[159] = eff / startStack
	}

	// === Action history block ===
	// Layout: 4 streets × 4 slots × 8 dims (actor 1 + action_onehot 6 + exists 1).
	const histBase = 160
	const slotDims = 8
	const slotsPerStreet = 4
	for st := 0; st < 4; st++ {
		hist := s.Hist[st]
		streetBase := histBase + st*slotsPerStreet*slotDims
		for i, a := range hist {
			if i >= slotsPerStreet {
				break
			}
			slotBase := streetBase + i*slotDims
			// dim 0: actor bit. HU: P0/P1 raw. 6-max migration: encode (actor - hero) mod N / N.
			// For HU we approximate "who acted" using street parity: at street st with idx i,
			// the actor depends on who opened. For preflop: P0 acts first (SB). Post-preflop:
			// P1 (BB) acts first. We use the engine's recorded action sequence — actor is
			// implicit from state.Hist[st] ordering + first-actor-on-street rules.
			// Encode the action ITSELF; actor derivable from (street, slot_idx).
			// dim 0: actor bit by parity. preflop: P0 starts even slots. flop+: P1 starts.
			var firstActor Player
			if st == 0 {
				firstActor = P0
			} else {
				firstActor = P1
			}
			actor := firstActor
			if i%2 == 1 {
				actor = actor.Other()
			}
			f[slotBase+0] = float32(actor)
			// dim 1-6: action one-hot (Fold/CheckCall/Bet0/Bet1/Bet2/AllIn).
			switch a.Kind {
			case ActionFold:
				f[slotBase+1] = 1
			case ActionCheckCall:
				f[slotBase+2] = 1
			case ActionBet:
				switch a.SizeIdx {
				case 0:
					f[slotBase+3] = 1
				case 1:
					f[slotBase+4] = 1
				case 2:
					f[slotBase+5] = 1
				}
			case ActionAllIn:
				f[slotBase+6] = 1
			}
			// dim 7: exists flag.
			f[slotBase+7] = 1
		}
	}

	return f
}
