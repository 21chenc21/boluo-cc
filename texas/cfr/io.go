package cfr

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/boluo/texas/engine/leduc"
)

// BlueprintFile — on-disk format for a converged strategy.
type BlueprintFile struct {
	GameName       string             `json:"game"`
	Iters          int                `json:"iters"`
	GameValueP0    float64            `json:"gv_p0"`
	Exploitability float64            `json:"exploitability"`
	NumInfosets    int                `json:"num_infosets"`
	Strategy       []BlueprintInfoset `json:"strategy"`
}

// BlueprintInfoset — one infoset entry. Key is uint64 InfosetID; also store
// a human-readable label for debugging.
type BlueprintInfoset struct {
	ID    uint64    `json:"id"`
	Label string    `json:"label"` // human-readable, e.g. "K/J/rrc/"
	Probs []float64 `json:"probs"` // P over [Fold, CheckCall, BetRaise] (BetRaise only if legal)
}

// SaveBlueprint writes σ to JSON with metadata. Labels reconstructed by walking
// the tree and matching InfosetID.
func SaveBlueprint(sigma Strategy, path string, iters int) error {
	gv0 := GameValue(sigma, leduc.P0)
	expl := Exploitability(sigma)
	labels := infosetLabels()

	infosets := make([]BlueprintInfoset, 0, len(sigma))
	for id, probs := range sigma {
		infosets = append(infosets, BlueprintInfoset{
			ID:    id,
			Label: labels[id],
			Probs: append([]float64(nil), probs...),
		})
	}

	bp := BlueprintFile{
		GameName:       "leduc-holdem",
		Iters:          iters,
		GameValueP0:    gv0,
		Exploitability: expl,
		NumInfosets:    len(infosets),
		Strategy:       infosets,
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(bp)
}

// LoadBlueprint reads back a saved file. Returns sigma + metadata.
func LoadBlueprint(path string) (Strategy, *BlueprintFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var bp BlueprintFile
	if err := json.NewDecoder(f).Decode(&bp); err != nil {
		return nil, nil, err
	}
	sigma := make(Strategy, len(bp.Strategy))
	for _, e := range bp.Strategy {
		sigma[e.ID] = e.Probs
	}
	return sigma, &bp, nil
}

// infosetLabels — walks tree once, builds id → InfosetKey() string map.
// Used to attach human-readable labels to saved blueprint entries.
func infosetLabels() map[uint64]string {
	out := make(map[uint64]string)
	var dfs func(s *leduc.State)
	dfs = func(s *leduc.State) {
		if s.Terminal {
			return
		}
		if s.NeedsPublicCard() {
			for c := leduc.Card(0); c < leduc.DeckSize; c++ {
				if c == s.Priv[0] || c == s.Priv[1] {
					continue
				}
				cl := s.Clone()
				cl.SetPublic(c)
				dfs(cl)
			}
			return
		}
		id := s.InfosetID()
		if _, seen := out[id]; !seen {
			out[id] = s.InfosetKey()
		}
		for _, a := range s.LegalActions() {
			cl := s.Clone()
			cl.Apply(a)
			dfs(cl)
		}
	}
	for p0 := leduc.Card(0); p0 < leduc.DeckSize; p0++ {
		for p1 := leduc.Card(0); p1 < leduc.DeckSize; p1++ {
			if p0 == p1 {
				continue
			}
			dfs(leduc.NewState(p0, p1))
		}
	}
	return out
}

func sanityCheckBlueprint(sigma Strategy) error {
	if len(sigma) != 288 {
		return fmt.Errorf("blueprint coverage %d, want 288", len(sigma))
	}
	for id, probs := range sigma {
		var sum float64
		for _, p := range probs {
			if p < 0 || p > 1.0001 {
				return fmt.Errorf("infoset %d: invalid prob %v", id, p)
			}
			sum += p
		}
		if sum < 0.9999 || sum > 1.0001 {
			return fmt.Errorf("infoset %d: probs sum %v, want 1.0", id, sum)
		}
	}
	return nil
}
