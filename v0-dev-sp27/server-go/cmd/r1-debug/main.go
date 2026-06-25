// r1-debug: dump R1 ExpertPlace5 各候选每阶段评分 (simpleEval / stage1 rollout mean / stage2 / stage3).
//
// 用法:
//   echo '{"dealt":["2s","5s","3s","Js","Ac"],"jokers":2}' | ./r1-debug
//
// 输出: 每候选一行
//   [stage] simple=X stage1=Y(n=N) stage2=Z(n=M) stage3=W(n=K) anchor=fy/fl/none top=... mid=... bot=...
//
// 用于诊断为何模型选 X 而非 Y (例如 UR9 4-spade-bot 候选去哪了).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/boluo/v0-server/ofc"
)

type request struct {
	Dealt   []string `json:"dealt"`
	Jokers  int      `json:"jokers"`
	TopK    int      `json:"topK"`    // 显示 top K 候选 (default 30)
	Sims    int      `json:"sims"`    // stage1 sims per cand (default 30)
	Weights string   `json:"weights"` // optional: load this ckpt as inference engine (vs embed)
}

type candInfo struct {
	placement   ofc.Placement
	simpleScore float32
	stage1Mean  float32
	stage1Sims  int
	stage2Mean  float32
	stage2Sims  int
	stage3Mean  float32
	stage3Sims  int
	anchor      string // "fy" / "fl" / "fy+fl" / "none"
	gs          *ofc.GameState
}

func anchorTag(gs *ofc.GameState) string {
	fy := ofc.IsFantasyAnchorR1(gs)
	fl := ofc.IsFlushDrawAnchorR1(gs)
	switch {
	case fy && fl:
		return "fy+fl"
	case fy:
		return "fy"
	case fl:
		return "fl"
	default:
		return "none"
	}
}

func fmtRow(cards []ofc.Card) string {
	if len(cards) == 0 {
		return "[]"
	}
	out := "["
	for i, c := range cards {
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
		req.TopK = 30
	}
	if req.Sims == 0 {
		req.Sims = 30
	}
	if req.Jokers == 0 {
		req.Jokers = 2
	}
	if req.Weights != "" {
		if err := ofc.LoadWeightsFromFile(req.Weights); err != nil {
			fmt.Fprintf(os.Stderr, "load weights %q failed: %v\n", req.Weights, err)
			os.Exit(1)
		}
		fmt.Printf("[loaded weights from %s]\n", req.Weights)
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

	// 生成候选 + simpleEval
	actions := ofc.GenerateRound1Actions(dealt, state)
	cands := make([]candInfo, 0, len(actions))
	seen := make(map[string]bool)
	for _, p := range actions {
		gs := state.Clone()
		for i, c := range dealt {
			gs.PlaceCard(c, p[i])
		}
		// dedup
		key := stateKey(gs)
		if seen[key] {
			continue
		}
		seen[key] = true
		simple := ofc.SimpleEval(gs)
		cands = append(cands, candInfo{
			placement:   p,
			simpleScore: simple,
			anchor:      anchorTag(gs),
			gs:          gs,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].simpleScore > cands[j].simpleScore })

	fmt.Printf("=== R1 debug: dealt=%v jokers=%d (total %d unique candidates) ===\n",
		req.Dealt, req.Jokers, len(cands))

	// 跑 stage1 rollout (top K + 漏在 K 外的 anchor 候选)
	er := ofc.NewExpertRollout()
	er.Cfg = ofc.DefaultRolloutConfig
	stage1Pool := make([]int, 0, req.TopK*2)
	for i := 0; i < req.TopK && i < len(cands); i++ {
		stage1Pool = append(stage1Pool, i)
	}
	for i := req.TopK; i < len(cands); i++ {
		if cands[i].anchor != "none" {
			stage1Pool = append(stage1Pool, i)
		}
	}

	for _, idx := range stage1Pool {
		c := &cands[idx]
		var sum float32
		for s := 0; s < req.Sims; s++ {
			sum += er.QuickRollout(c.gs, 1)
		}
		c.stage1Mean = sum / float32(req.Sims)
		c.stage1Sims = req.Sims
	}

	// 重新按 stage1 + anchor boost +5 排
	type ranked struct {
		idx int
	}
	pool := make([]ranked, 0, len(stage1Pool))
	for _, idx := range stage1Pool {
		pool = append(pool, ranked{idx})
	}
	sort.SliceStable(pool, func(i, j int) bool {
		si := cands[pool[i].idx].stage1Mean + ofc.AnchorBoost(cands[pool[i].idx].gs)
		sj := cands[pool[j].idx].stage1Mean + ofc.AnchorBoost(cands[pool[j].idx].gs)
		return si > sj
	})

	fmt.Printf("\n--- All candidates that ran stage1 (sorted by stage1+anchor boost) ---\n")
	fmt.Printf("%4s  %7s  %6s  %5s  %s\n", "rank", "simple", "stage1", "anchor", "placement")
	for r, p := range pool {
		c := cands[p.idx]
		fmt.Printf("%4d  %7.2f  %6.2f  %5s  top=%s mid=%s bot=%s\n",
			r+1, c.simpleScore, c.stage1Mean, c.anchor,
			fmtRow(c.gs.Top), fmtRow(c.gs.Middle), fmtRow(c.gs.Bottom))
	}

	// stage2: top 8 (跟 ExpertPlace5 默认配置一致)
	stage2K := 8
	if stage2K > len(pool) {
		stage2K = len(pool)
	}
	stage2Sims := req.Sims * 2
	for i := 0; i < stage2K; i++ {
		c := &cands[pool[i].idx]
		var sum float32
		for s := 0; s < stage2Sims; s++ {
			sum += er.QuickRollout(c.gs, 1)
		}
		c.stage2Mean = sum / float32(stage2Sims)
		c.stage2Sims = stage2Sims
	}
	sort.SliceStable(pool[:stage2K], func(i, j int) bool {
		si := cands[pool[i].idx].stage2Mean + ofc.AnchorBoost(cands[pool[i].idx].gs)
		sj := cands[pool[j].idx].stage2Mean + ofc.AnchorBoost(cands[pool[j].idx].gs)
		return si > sj
	})

	fmt.Printf("\n--- Stage 2 (top %d by stage1+boost, sims=%d) ---\n", stage2K, stage2Sims)
	fmt.Printf("%4s  %6s  %6s  %5s  %s\n", "rank", "stage1", "stage2", "anchor", "placement")
	for i := 0; i < stage2K; i++ {
		c := cands[pool[i].idx]
		fmt.Printf("%4d  %6.2f  %6.2f  %5s  top=%s mid=%s bot=%s\n",
			i+1, c.stage1Mean, c.stage2Mean, c.anchor,
			fmtRow(c.gs.Top), fmtRow(c.gs.Middle), fmtRow(c.gs.Bottom))
	}

	// stage3: top 3 (默认)
	stage3K := 3
	if stage3K > stage2K {
		stage3K = stage2K
	}
	stage3Sims := req.Sims * 5
	for i := 0; i < stage3K; i++ {
		c := &cands[pool[i].idx]
		var sum float32
		for s := 0; s < stage3Sims; s++ {
			sum += er.QuickRollout(c.gs, 1)
		}
		c.stage3Mean = sum / float32(stage3Sims)
		c.stage3Sims = stage3Sims
	}

	fmt.Printf("\n--- Stage 3 final (top %d, sims=%d) ===\n", stage3K, stage3Sims)
	fmt.Printf("%4s  %6s  %6s  %6s  %6s  %4s  %5s  %s\n", "rank", "stage1", "stage2", "stage3", "comb", "mono", "anchor", "placement")
	type s3 struct {
		idx     int
		comb    float32
		mono    int
	}
	results := make([]s3, 0, stage3K)
	for i := 0; i < stage3K; i++ {
		c := cands[pool[i].idx]
		mono := ofc.MonoSplitBadness(c.gs)
		// 复刻 ExpertPlace5 stage3: combined = stage3Mean + simpleEval*0.3 + AnchorBoost
		// (mono badness 不进 combined, 进 tie-break)
		combined := c.stage3Mean + c.simpleScore*0.3 + ofc.AnchorBoost(c.gs)
		results = append(results, s3{pool[i].idx, combined, mono})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].comb > results[j].comb })
	for i, r := range results {
		c := cands[r.idx]
		fmt.Printf("%4d  %6.2f  %6.2f  %6.2f  %6.2f  %4d  %5s  top=%s mid=%s bot=%s\n",
			i+1, c.stage1Mean, c.stage2Mean, c.stage3Mean, r.comb, r.mono, c.anchor,
			fmtRow(c.gs.Top), fmtRow(c.gs.Middle), fmtRow(c.gs.Bottom))
	}
	// Lex sort (跟 ExpertPlace5 一致): badness asc → combined desc
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].mono != results[j].mono {
			return results[i].mono < results[j].mono
		}
		return results[i].comb > results[j].comb
	})
	winner := cands[results[0].idx]
	fmt.Printf("\n>> WINNER: top=%s mid=%s bot=%s  (combined=%.2f, mono=%d)\n",
		fmtRow(winner.gs.Top), fmtRow(winner.gs.Middle), fmtRow(winner.gs.Bottom),
		results[0].comb, results[0].mono)
}

// stateKey copy 自 expert_place.go (内部函数, 复刻一下)
func stateKey(gs *ofc.GameState) string {
	t := cardIDs(gs.Top)
	m := cardIDs(gs.Middle)
	b := cardIDs(gs.Bottom)
	sortStrs(t)
	sortStrs(m)
	sortStrs(b)
	return joinIDs(t) + "|" + joinIDs(m) + "|" + joinIDs(b)
}

func cardIDs(cards []ofc.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID()
	}
	return out
}

func sortStrs(s []string) {
	sort.Strings(s)
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}
