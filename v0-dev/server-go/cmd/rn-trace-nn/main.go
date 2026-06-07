// rn-trace-nn: dump R2-R5 候选完整分数分解 (TrainedEval + 每个 RN penalty/bonus + hard rule filter)
//
// stdin JSON (与 r1-trace-nn 类似, 加 round 字段):
// {
//   "round": 4,
//   "dealt": ["Ad","X","Ts"],
//   "state": {"top":["As","X"], "middle":[...], "bottom":[...]},
//   "used": [...optional opp visible/discards...],
//   "jokers": 2,
//   "weights": "/path/best.json",
//   "topK": 15
// }
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

type stateJSON struct {
	Top    []string `json:"top"`
	Middle []string `json:"middle"`
	Bottom []string `json:"bottom"`
}

type request struct {
	Round       int       `json:"round"`
	Dealt       []string  `json:"dealt"`
	State       stateJSON `json:"state"`
	Used        []string  `json:"used"`
	Jokers      int       `json:"jokers"`
	Weights     string    `json:"weights"`
	TopK        int       `json:"topK"`
	FeatureDiff []int     `json:"featureDiff"` // 候选排名 (1-based), 对比 147-d feature
}

func parseCard(s string) ofc.Card {
	c, ok := ofc.ParseCard(s)
	if !ok {
		fmt.Fprintf(os.Stderr, "parse %q failed\n", s)
		os.Exit(1)
	}
	return c
}

func rowStr(cs []ofc.Card) string {
	if len(cs) == 0 {
		return "[]"
	}
	out := "["
	for i, c := range cs {
		if i > 0 {
			out += " "
		}
		out += c.ID()
	}
	return out + "]"
}

func placementStr(gs *ofc.GameState) string {
	return fmt.Sprintf("top=%s mid=%s bot=%s", rowStr(gs.Top), rowStr(gs.Middle), rowStr(gs.Bottom))
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "bad json: %v\n", err)
		os.Exit(1)
	}
	if req.TopK == 0 {
		req.TopK = 15
	}
	if req.Round < 2 || req.Round > 5 {
		fmt.Fprintf(os.Stderr, "round must be 2..5, got %d\n", req.Round)
		os.Exit(1)
	}
	if req.Weights != "" {
		if err := ofc.LoadWeightsFromFile(req.Weights); err != nil {
			fmt.Fprintf(os.Stderr, "load weights: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[loaded %s]\n", req.Weights)
	}

	dealt := make([]ofc.Card, 0, len(req.Dealt))
	for _, s := range req.Dealt {
		dealt = append(dealt, parseCard(s))
	}

	state := ofc.NewGameState(req.Jokers)
	state.Round = req.Round
	for _, s := range req.State.Top {
		c := parseCard(s)
		state.PlaceCard(c, ofc.RowTop)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range req.State.Middle {
		c := parseCard(s)
		state.PlaceCard(c, ofc.RowMiddle)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range req.State.Bottom {
		c := parseCard(s)
		state.PlaceCard(c, ofc.RowBottom)
		state.UsedCards[c.ID()] = true
	}
	for _, s := range req.Used {
		// 2026-06-05: 用 raw "X" (不 canonical) 对齐 prod server (main.go:311) + 前端.
		// 之前用 parseCard(s).ID() canonical → joker 不 double-count → 跟 prod 决策对不上
		// (ypk-32571722-17: canonical 显示 ace→top, prod raw 是 AA→mid). prod/bench 才是真相.
		state.UsedCards[s] = true
	}

	fmt.Printf("[round=%d, jokers=%d, dealt=%v]\n", req.Round, req.Jokers, req.Dealt)
	fmt.Printf("[state: %s]\n\n", placementStr(state))

	// 枚举所有 RoundN actions
	actions := ofc.GenerateRoundNActions(dealt, state)

	type cand struct {
		action     ofc.RoundNAction
		gs         *ofc.GameState
		te         float32
		plogit     float32
		hasPol     bool
		foul       float32
		kkOnMid    float32
		jokerHigh  float32
		singleA    float32
		topCap     float32
		score      float32
		hardRuleOK bool
		discardStr string
	}

	cands := make([]cand, 0, len(actions))
	for i := range actions {
		a := &actions[i]
		gs := state.Clone()
		gs.UsedCards[dealt[a.DiscardIdx].ID()] = true
		gs.SetDiscard(dealt[a.DiscardIdx])
		for k, c := range a.Kept {
			gs.PlaceCard(c, a.Placement[k])
		}

		c := cand{action: *a, gs: gs, discardStr: dealt[a.DiscardIdx].ID()}
		c.te = ofc.TrainedEval(gs)
		if _, _, _, pl, has := ofc.TrainedEvalFull(gs); has {
			c.plogit = pl
			c.hasPol = true
		}
		c.foul = ofc.FoulImminentPenalty(gs)
		c.kkOnMid = ofc.RnKKOnMidPenalty(a, dealt, state)
		c.singleA = ofc.RnSingleAOnTopBonus(a, gs, c.foul)

		c.score = c.te + ofc.PolicyBoost*c.plogit - c.foul - c.kkOnMid + c.singleA - ofc.RnJokersSameRowPenalty(a, gs) + ofc.RnSingleJokerTopChaseABonus(gs, state) - ofc.RnLoneAceMidJokerTopPenalty(gs, state)
		c.hardRuleOK = true
		cands = append(cands, c)
	}

	// Hard rule filter
	if !ofc.HardRulesDisabled {
		rnc := make([]ofc.RNCand, len(cands))
		for i := range cands {
			rnc[i] = ofc.RNCand{Action: &cands[i].action, GS: cands[i].gs}
		}
		kept := ofc.ApplyHardRulesRN(rnc, dealt, state)
		keepSet := make(map[string]bool, len(kept))
		for _, c := range kept {
			key := fmt.Sprintf("%s|%s|%s|%s", c.GS.Top, c.GS.Middle, c.GS.Bottom, dealt[c.Action.DiscardIdx].ID())
			keepSet[key] = true
		}
		for i := range cands {
			key := fmt.Sprintf("%s|%s|%s|%s", cands[i].gs.Top, cands[i].gs.Middle, cands[i].gs.Bottom, cands[i].discardStr)
			cands[i].hardRuleOK = keepSet[key]
		}
	}

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })

	if req.TopK > len(cands) {
		req.TopK = len(cands)
	}

	fmt.Printf("Total candidates: %d (hard-rule kept: %d)\n", len(cands),
		func() int { n := 0; for _, c := range cands { if c.hardRuleOK { n++ } }; return n }())
	fmt.Printf("\nTop %d by final score (= TE + PB*plogit - foul - kkMid + jokerHigh + singleA - topCap)\n", req.TopK)
	fmt.Printf("%-4s %-4s %-50s %-6s %8s %8s %8s\n", "#", "HR", "placement (弃)", "score", "te", "plogit", "foul")
	for i := 0; i < req.TopK; i++ {
		c := cands[i]
		hr := "✓"
		if !c.hardRuleOK {
			hr = "✗"
		}
		pl := "-"
		if c.hasPol {
			pl = fmt.Sprintf("%+.3f", c.plogit)
		}
		fmt.Printf("[%2d] %-4s %-50s %+7.3f %+8.3f %8s %+8.3f\n",
			i+1, hr,
			fmt.Sprintf("%s 弃%s", placementStr(c.gs), c.discardStr),
			c.score, c.te, pl, c.foul)
	}

	fmt.Printf("\n--- Soft rule breakdown for top %d ---\n", req.TopK)
	fmt.Printf("%-4s %-50s %8s %8s %8s %8s %8s %8s\n",
		"#", "placement", "foul-", "kkMid-", "jkHi+", "sgA+", "topCp-", "net")
	for i := 0; i < req.TopK; i++ {
		c := cands[i]
		net := -c.foul - c.kkOnMid + c.jokerHigh + c.singleA - c.topCap
		fmt.Printf("[%2d] %-50s %+8.3f %+8.3f %+8.3f %+8.3f %+8.3f %+8.3f\n",
			i+1,
			fmt.Sprintf("%s 弃%s", placementStr(c.gs), c.discardStr),
			-c.foul, -c.kkOnMid, +c.jokerHigh, +c.singleA, -c.topCap, net)
	}

	// === Feature diff (147-d, 21 groups) ===
	if len(req.FeatureDiff) >= 2 {
		groups := []struct {
			name string
			lo   int
			hi   int
		}{
			{"A_boardState", 0, 8}, {"B_handTiers", 8, 32}, {"D_jokerState", 32, 40},
			{"E_suitDist", 40, 52}, {"G_deckAware", 52, 69}, {"X_probs", 69, 90},
			{"F_fantasyGran", 90, 94}, {"Y_eRoyalty", 94, 97}, {"Z_summary", 97, 102},
			{"U_pairRk", 102, 107}, {"V_pairToTrips", 107, 112}, {"T_topFanLocks", 112, 116},
			{"C_maxAchiev", 116, 119}, {"R5_lastRd", 119, 121}, {"Q_commit", 121, 125},
			{"M_foulMargin", 125, 128}, {"S_slot", 128, 129}, {"N_disc", 129, 131},
			{"L_crossRow", 131, 137}, {"LR_lockDraw", 137, 145}, {"N2_discExtra", 145, 147},
		}
		feats := make([][]float32, len(req.FeatureDiff))
		labels := make([]string, len(req.FeatureDiff))
		for i, rk := range req.FeatureDiff {
			if rk < 1 || rk > len(cands) {
				fmt.Fprintf(os.Stderr, "featureDiff rank %d out of range\n", rk)
				continue
			}
			c := cands[rk-1]
			feats[i] = ofc.BuildFeatures(c.gs, 147)
			labels[i] = fmt.Sprintf("rk%d[%s|%s|%s]",
				rk, rowStr(c.gs.Top), rowStr(c.gs.Middle), rowStr(c.gs.Bottom))
		}

		fmt.Printf("\n=== Feature group-sum diff (21 groups) ===\n")
		fmt.Printf("%-16s", "group")
		for _, l := range labels {
			fmt.Printf("  %32s", l)
		}
		if len(feats) == 2 {
			fmt.Printf("  %12s", "Δ (1st-2nd)")
		}
		fmt.Println()
		for _, g := range groups {
			fmt.Printf("%-16s", g.name)
			sums := make([]float32, len(feats))
			for i, f := range feats {
				var s float32
				for k := g.lo; k < g.hi; k++ {
					s += f[k]
				}
				sums[i] = s
				fmt.Printf("  %32.4f", s)
			}
			if len(feats) == 2 {
				fmt.Printf("  %+12.4f", sums[0]-sums[1])
			}
			fmt.Println()
		}

		if len(feats) == 2 {
			type ddim struct {
				idx int
				d   float32
			}
			ds := make([]ddim, 147)
			for i := 0; i < 147; i++ {
				ds[i] = ddim{i, feats[0][i] - feats[1][i]}
			}
			sort.SliceStable(ds, func(i, j int) bool { return absF(ds[i].d) > absF(ds[j].d) })
			fmt.Printf("\n--- Top 15 单 dim |Δ| (1st - 2nd) ---\n")
			for i := 0; i < 15 && i < len(ds); i++ {
				idx := ds[i].idx
				grpName := ""
				for _, g := range groups {
					if idx >= g.lo && idx < g.hi {
						grpName = g.name
						break
					}
				}
				fmt.Printf("  dim %3d (%-16s)  v1=%+8.4f  v2=%+8.4f  Δ=%+8.4f\n",
					idx, grpName, feats[0][idx], feats[1][idx], ds[i].d)
			}
		}
	}
}

func absF(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
