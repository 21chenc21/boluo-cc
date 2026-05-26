// r1-trace-nn: dump R1 候选完整分数分解 (TrainedEval + 每个 penalty + 每个 bonus + hard rule filter)
// 复刻 expert_place.go ExpertPlace5 stage1 ranking 逻辑.
//
// stdin: {"dealt": ["Jh","5h","5c","9d","9h"], "jokers": 0, "weights": "path/to/ckpt.json"}
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

type request struct {
	Dealt        []string `json:"dealt"`
	Jokers       int      `json:"jokers"`
	Weights      string   `json:"weights"`
	TopK         int      `json:"topK"`
	Used         []string `json:"used"`        // 2026-05-22 加: phantom/opponent 已知 used cards
	FeatureDiff  []int    `json:"featureDiff"` // 2026-05-22: rank 编号列表, dump 它们的 147-d feature group-sum 对比
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

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "bad json: %v\n", err)
		os.Exit(1)
	}
	if req.TopK == 0 {
		req.TopK = 15
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
		c, ok := ofc.ParseCard(s)
		if !ok {
			fmt.Fprintf(os.Stderr, "parse %q failed\n", s)
			os.Exit(1)
		}
		dealt = append(dealt, c)
	}

	state := ofc.NewGameState(req.Jokers)
	state.Round = 1
	// 注入 used cards (phantom or opponent visible)
	for _, s := range req.Used {
		c, ok := ofc.ParseCard(s)
		if !ok {
			fmt.Fprintf(os.Stderr, "parse used %q failed\n", s)
			os.Exit(1)
		}
		state.UsedCards[c.ID()] = true
	}
	if len(req.Used) > 0 {
		fmt.Printf("[used cards: %v]\n", req.Used)
	}

	type rowInfo struct {
		placement ofc.Placement
		gs        *ofc.GameState
		te        float32
		// penalty 分解
		connector       float32
		fourInRow       float32
		incoherent      float32
		topNonAKX       float32
		jokerOnTopAA    float32
		foulImminent    float32
		sameSuitBonus   float32
		jokerAOnTopBon  float32
		singleAOnTopBon float32
		flushGroupBon   float32
		netPenalty      float32
		// final
		score      float32
		hardRuleOK bool
	}

	// 生成所有 dedup 候选
	actions := ofc.GenerateRound1Actions(dealt, state)
	uniq := make([]rowInfo, 0, len(actions))
	seen := make(map[string]bool)
	for _, p := range actions {
		gs := state.Clone()
		for i, c := range dealt {
			gs.PlaceCard(c, p[i])
		}
		// dedup by stateKey (要 export, 或用 placement 字符串)
		key := rowStr(gs.Top) + "|" + rowStr(gs.Middle) + "|" + rowStr(gs.Bottom)
		if seen[key] {
			continue
		}
		seen[key] = true

		te := ofc.TrainedEval(gs)
		if _, _, _, plogit, hasPolicy := ofc.TrainedEvalFull(gs); hasPolicy {
			te += ofc.PolicyBoost * plogit
		}

		info := rowInfo{placement: p, gs: gs, te: te}
		info.connector = ofc.ConnectorSplitPenalty(p, dealt)
		info.fourInRow = ofc.R1FourInRowPenalty(p, dealt)
		info.incoherent = ofc.R1IncoherentRowPenalty(p, dealt)
		info.topNonAKX = ofc.R1TopNonAKXPenalty(p, dealt, state)
		info.jokerOnTopAA = ofc.R1JokerOnTopWithAAPenalty(p, dealt)
		info.foulImminent = ofc.FoulImminentPenalty(gs)
		info.sameSuitBonus = ofc.R1SameSuitInRowBonus(p, dealt)
		info.jokerAOnTopBon = ofc.R1JokerWithAOnTopBonus(p, dealt)
		info.singleAOnTopBon = ofc.R1SingleAOnTopBonus(p, dealt)
		info.flushGroupBon = ofc.R1FlushGroupOnBotBonus(p, dealt)

		info.netPenalty = info.connector + info.fourInRow + info.incoherent +
			info.topNonAKX + info.jokerOnTopAA + info.foulImminent -
			info.sameSuitBonus - info.jokerAOnTopBon - info.singleAOnTopBon - info.flushGroupBon
		info.score = info.te - info.netPenalty
		info.hardRuleOK = true
		uniq = append(uniq, info)
	}

	// Hard rule filter
	if !ofc.HardRulesDisabled {
		r1c := make([]ofc.R1Cand, len(uniq))
		for i, u := range uniq {
			r1c[i] = ofc.R1Cand{Placement: u.placement, GS: u.gs}
		}
		kept := ofc.ApplyHardRulesR1(r1c, dealt, state)
		keepSet := make(map[string]bool, len(kept))
		for _, k := range kept {
			key := rowStr(k.GS.Top) + "|" + rowStr(k.GS.Middle) + "|" + rowStr(k.GS.Bottom)
			keepSet[key] = true
		}
		for i := range uniq {
			key := rowStr(uniq[i].gs.Top) + "|" + rowStr(uniq[i].gs.Middle) + "|" + rowStr(uniq[i].gs.Bottom)
			uniq[i].hardRuleOK = keepSet[key]
		}
	}

	// 按 final score 排
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].score > uniq[j].score })

	fmt.Printf("=== R1 trace: dealt=%v jokers=%d, total uniq candidates = %d ===\n",
		req.Dealt, req.Jokers, len(uniq))
	fmt.Printf("\nTop %d candidates by final score (= TE + plogit*PB - netPenalty)\n", req.TopK)
	fmt.Printf("%4s  %3s  %8s  %8s  %8s  %s\n", "rk", "HR", "TE", "penalty", "score", "placement (top|mid|bot)")
	for i, u := range uniq {
		if i >= req.TopK {
			break
		}
		hr := "OK"
		if !u.hardRuleOK {
			hr = "CUT"
		}
		fmt.Printf("%4d  %3s  %8.4f  %8.4f  %8.4f  %s | %s | %s\n",
			i+1, hr, u.te, u.netPenalty, u.score,
			rowStr(u.gs.Top), rowStr(u.gs.Middle), rowStr(u.gs.Bottom))
	}

	// Detail breakdown for top 5
	fmt.Printf("\n--- Penalty/Bonus breakdown for top 5 ---\n")
	fmt.Printf("%4s  %8s %8s %8s %8s %8s %8s | %8s %8s %8s %8s | %8s\n",
		"rk", "conn", "4row", "incoh", "topNAKX", "jOnTopAA", "FoulIM",
		"sameSuit", "jAOnTop", "singleA", "flGroup", "NET")
	for i, u := range uniq {
		if i >= 5 {
			break
		}
		fmt.Printf("%4d  %+8.2f %+8.2f %+8.2f %+8.2f %+8.2f %+8.2f | %+8.2f %+8.2f %+8.2f %+8.2f | %+8.2f\n",
			i+1, u.connector, u.fourInRow, u.incoherent, u.topNonAKX,
			u.jokerOnTopAA, u.foulImminent,
			u.sameSuitBonus, u.jokerAOnTopBon, u.singleAOnTopBon, u.flushGroupBon,
			u.netPenalty)
	}

	// 2026-05-22 加: feature diff for selected ranks
	if len(req.FeatureDiff) >= 2 {
		groups := []struct {
			name string
			lo   int
			hi   int
		}{
			{"A_boardState", 0, 8},
			{"B_handTiers", 8, 32},
			{"D_jokerState", 32, 40},
			{"E_suitDist", 40, 52},
			{"G_deckAware", 52, 69},
			{"X_probs", 69, 90},
			{"F_fantasyGran", 90, 94},
			{"Y_eRoyalty", 94, 97},
			{"Z_summary", 97, 102},
			{"U_pairRk", 102, 107},
			{"V_pairToTrips", 107, 112},
			{"T_topFanLocks", 112, 116},
			{"C_maxAchiev", 116, 119},
			{"R5_lastRd", 119, 121},
			{"Q_commit", 121, 125},
			{"M_foulMargin", 125, 128},
			{"S_slot", 128, 129},
			{"N_disc", 129, 131},
			{"L_crossRow", 131, 137},
			{"LR_lockDraw", 137, 145},
			{"N2_discExtra", 145, 147},
		}
		feats := make([][]float32, len(req.FeatureDiff))
		labels := make([]string, len(req.FeatureDiff))
		for i, rk := range req.FeatureDiff {
			if rk < 1 || rk > len(uniq) {
				fmt.Fprintf(os.Stderr, "featureDiff rank %d out of range\n", rk)
				continue
			}
			u := uniq[rk-1]
			feats[i] = ofc.BuildFeatures(u.gs, 147)
			labels[i] = fmt.Sprintf("rk%d[%s|%s|%s]", rk, rowStr(u.gs.Top), rowStr(u.gs.Middle), rowStr(u.gs.Bottom))
		}

		fmt.Printf("\n=== Feature group-sum diff (21 groups) ===\n")
		fmt.Printf("%-16s", "group")
		for _, l := range labels {
			fmt.Printf("  %26s", l)
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
				fmt.Printf("  %26.4f", s)
			}
			if len(feats) == 2 {
				fmt.Printf("  %+12.4f", sums[0]-sums[1])
			}
			fmt.Println()
		}

		// 找单 dim 最大差异 (top 10), 仅 2 候选时
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
			fmt.Printf("\n--- Top 10 单 dim |Δ| (1st - 2nd) ---\n")
			for i := 0; i < 10 && i < len(ds); i++ {
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
