package cfr

import (
	"github.com/boluo/texas/engine/leduc"
)

// Feature dimensions for Leduc infoset → NN input (POC encoding).
//
//	[0:3]      priv rank one-hot  (J=0, Q=1, K=2)
//	[3:7]      pub rank one-hot   (J=0, Q=1, K=2, none=3)
//	[7]        round indicator    (0 or 1)
//	[8:20]     r1 history         (4 positions × 3 one-hot {Fold,CheckCall,BetRaise})
//	[20:32]    r2 history         (same encoding)
//	[32:35]    legal-action mask  (0/1 per action)
//
// Total = 35 dims. Last 3 are the legal-action mask so NN can learn to set
// illegal action prob ≈ 0 (or downstream masking can use it).
const FeatureDim = 35

// FeatureVec builds the 35-d feature vector for the CURRENT actor's view.
// Caller is responsible for the state being at a decision point (not terminal,
// not awaiting public card).
func FeatureVec(s *leduc.State) [FeatureDim]float32 {
	var f [FeatureDim]float32

	// Priv rank one-hot (always known to actor).
	f[s.Priv[s.Cur].Rank()] = 1

	// Pub rank one-hot, slot 6 = "no public yet".
	if s.HasPub {
		f[3+s.Pub.Rank()] = 1
	} else {
		f[3+3] = 1
	}

	// Round indicator.
	f[7] = float32(s.Round)

	// History per round, 4 positions max, one-hot per action.
	for i, a := range s.Hist[0] {
		f[8+i*3+int(a)] = 1
	}
	for i, a := range s.Hist[1] {
		f[20+i*3+int(a)] = 1
	}

	// Legal-action mask.
	if !s.Terminal {
		for _, a := range s.LegalActions() {
			f[32+int(a)] = 1
		}
	}

	return f
}
