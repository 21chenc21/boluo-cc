package nlhe6

import "sort"

// Features for 6-max NLHE NN distillation.
//
// Same 288-d schema as engine/nlhe FeatureVecMultiStreet (designed 6-max-friendly
// from the start in Phase 2j). HU uses 1 opp slot + 1 of 2 position slots;
// 6-max fills up to 5 opp slots + 1 of 6 position slots. All offsets match HU
// schema so a single Python train.py works on both data sources.
//
// vs engine/nlhe FeatureVecMultiStreet:
//   - Hero position one-hot: uses canonical PositionFor(seat, button, n) →
//     6 slots populated correctly for n ∈ [2, 6]
//   - Pot normalization: divides by N×StartStack (was 2×StartStack)
//   - Opp slots: fills slot i = clockwise opp at (hero+1+i) mod n, for i in [0, N-2]
//   - Action history actor encoding: uses HistEntry.Seat → relative-to-button
//     normalized [0,1), more general than HU's (street, slot_idx) parity
const FeatureDimMultiStreet = 288

// FeatureVecMultiStreet — builds 288-d feature for 6-max state. Caller
// responsible for state being at a non-terminal, non-chance node.
func FeatureVecMultiStreet(s *State) [FeatureDimMultiStreet]float32 {
	var f [FeatureDimMultiStreet]float32
	n := s.Cfg.NumPlayers
	hero := s.Cur

	// Hero hole.
	c1 := s.Hole[hero][0]
	c2 := s.Hole[hero][1]
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

	// Board sorted desc rank.
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

	// Hero position one-hot — uses canonical PositionFor (HU SB/BB, 6-max all 6).
	heroPos := PositionFor(hero, s.Button, n)
	f[117+int(heroPos)] = 1

	// Hero state ratios. Pot normalized by N×StartStack (6-max correct).
	startStack := float32(s.Cfg.StartStack)
	pot := float32(s.Pot())
	if startStack > 0 {
		f[123] = float32(s.Stacks[hero]) / startStack
		f[124] = float32(s.BetThisStreet[hero]) / startStack
		f[125] = pot / (float32(n) * startStack)
		f[126] = float32(s.LastRaiseSize) / startStack
	}

	// Opp slots: fill clockwise from hero+1, up to N-1 slots (max 5 for 6-max).
	const oppSlotsBase = 127
	for i := 0; i < n-1; i++ {
		if i >= 5 {
			break // schema has only 5 opp slots
		}
		opp := Seat((int(hero) + 1 + i) % n)
		slot := oppSlotsBase + i*4
		if startStack > 0 {
			f[slot+0] = float32(s.Stacks[opp]) / startStack
			f[slot+1] = float32(s.BetThisStreet[opp]) / startStack
		}
		// Position offset normalized in [0, 1).
		f[slot+2] = float32(i+1) / float32(n)
		if s.AllIn[opp] {
			f[slot+3] = 1
		}
		// Note: folded opps still encoded; NN can detect via stack ratio + position.
		// If desired, add a "folded" flag dim; current schema fixed at 4 dims/slot.
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

	// Derived scalars.
	// to_call: max BetThisStreet across non-folded - hero's BetThisStreet.
	maxBet := 0
	for i := 0; i < n; i++ {
		if !s.Folded[i] && s.BetThisStreet[i] > maxBet {
			maxBet = s.BetThisStreet[i]
		}
	}
	toCall := float32(maxBet - s.BetThisStreet[hero])
	if toCall < 0 {
		toCall = 0
	}
	if toCall+pot > 0 {
		f[157] = toCall / (toCall + pot)
	}
	if pot > 0 {
		spr := float32(s.Stacks[hero]) / pot
		if spr > 10 {
			spr = 10
		}
		f[158] = spr / 10
	}
	// effective stack: min hero stack across all active opp stacks.
	heroStk := float32(s.Stacks[hero])
	eff := heroStk
	for i := 0; i < n; i++ {
		if i == int(hero) || s.Folded[i] {
			continue
		}
		opStk := float32(s.Stacks[i])
		if opStk < eff {
			eff = opStk
		}
	}
	if startStack > 0 {
		f[159] = eff / startStack
	}

	// Action history block: 4 streets × 4 slots × 8 dims.
	const histBase = 160
	const slotDims = 8
	const slotsPerStreet = 4
	for st := 0; st < 4; st++ {
		hist := s.Hist[st]
		streetBase := histBase + st*slotsPerStreet*slotDims
		for i, e := range hist {
			if i >= slotsPerStreet {
				break
			}
			slotBase := streetBase + i*slotDims
			// dim 0: actor seat relative to button, normalized [0, 1).
			// 6-max: encodes "who acted" in canonical position frame.
			rel := (int(e.Seat) - int(s.Button) + n) % n
			f[slotBase+0] = float32(rel) / float32(n)
			// dim 1-6: action one-hot.
			switch e.Action.Kind {
			case ActionFold:
				f[slotBase+1] = 1
			case ActionCheckCall:
				f[slotBase+2] = 1
			case ActionBet:
				switch e.Action.SizeIdx {
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
