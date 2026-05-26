package nlhe

// Features for HUNL push/fold NN distillation.
//
// 33-d compact encoding (rank-canonical, suit-aware for flush draws later):
//
//	[0:13]    low rank one-hot   (canonical min(rank1, rank2))
//	[13:26]   high rank one-hot  (canonical max(rank1, rank2))
//	[26]      pair indicator     (rank1 == rank2)
//	[27]      suited indicator   (suit1 == suit2)
//	[28]      position           (0 = SB / P0, 1 = BB / P1)
//	[29]      facing shove       (opp committed full stack already)
//	[30:33]   legal-action mask  (Fold, CheckCall, AllIn — push/fold action set)
//
// For full HUNL (multi-street) we'd extend with board cards and street index;
// kept compact for push/fold POC.
const FeatureDimPushFold = 33

// FeatureVecPushFold builds the 33-d feature vector for the current actor.
// Only valid in push/fold mode (PushFoldOnly=true). Caller responsible for
// state being at a decision point.
func FeatureVecPushFold(s *State) [FeatureDimPushFold]float32 {
	var f [FeatureDimPushFold]float32

	cur := s.Cur
	c1 := s.Hole[cur][0]
	c2 := s.Hole[cur][1]
	r1, r2 := c1.Rank(), c2.Rank()
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	f[r1] = 1                                       // low rank
	f[13+r2] = 1                                    // high rank
	if c1.Rank() == c2.Rank() {
		f[26] = 1
	}
	if c1.Suit() == c2.Suit() {
		f[27] = 1
	}
	f[28] = float32(cur)

	// "Facing shove" = opponent is all-in.
	if s.AllIn[cur.Other()] {
		f[29] = 1
	}

	// Legal action mask.
	if !s.Terminal {
		for _, a := range s.LegalActions() {
			switch a.Kind {
			case ActionFold:
				f[30] = 1
			case ActionCheckCall:
				f[31] = 1
			case ActionAllIn:
				f[32] = 1
			}
		}
	}

	return f
}
