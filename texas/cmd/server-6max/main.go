// server-6max — HTTP API serving NN policy for 6-max NLHE.
//
// Loads ONNX policy at startup. Per request: reconstructs state from
// (button, hole, board, history), runs 288-d feature encoding + NN forward,
// returns action probabilities masked by legal actions.
//
// Build tag `onnx` required (depends on libonnxruntime via server package).
//
//	go run -tags onnx ./cmd/server-6max -model distill/models/hunl6-150k.onnx -port 8080
//
// Test:
//
//	curl -X POST http://localhost:8080/api/v1/policy -d @example.json
//
// Example request body:
//
//	{
//	  "num_players": 6, "stack_bbs": 20,
//	  "bet_sizes": [0.5, 1.0, 2.0],
//	  "button": 0,
//	  "hole": ["As", "Ah"],
//	  "board": ["Kd", "7c", "2h"],
//	  "history": [
//	    {"kind": "bet", "size_idx": 1},
//	    {"kind": "checkcall"},
//	    ...
//	  ]
//	}
//
// Response:
//
//	{
//	  "legal_actions": ["fold", "checkcall", "bet0", "bet1", "bet2", "allin"],
//	  "probs": [0.0, 0.30, 0.10, 0.40, 0.10, 0.10],
//	  "cur_seat": 1,
//	  "model": "hunl6-150k.onnx"
//	}
//
// NOTE: this is a v0 skeleton. Production needs: rate limit, auth, request
// validation, structured logging, metrics. Not yet wired.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/boluo/texas/engine/nlhe"
	"github.com/boluo/texas/engine/nlhe6"
)

var (
	modelPath = flag.String("model", "", "path to ONNX policy model (required)")
	port      = flag.Int("port", 8080, "HTTP listen port")
)

// PolicyRequest — game state at decision time.
type PolicyRequest struct {
	NumPlayers int            `json:"num_players"`
	StackBBs   int            `json:"stack_bbs"`
	BetSizes   []float64      `json:"bet_sizes"`
	Button     int            `json:"button"`
	Hole       []string       `json:"hole"`
	Board      []string       `json:"board"`
	History    []HistoryEntry `json:"history"`
}

// HistoryEntry — single past action (actor seat derived from engine rotation).
type HistoryEntry struct {
	Kind    string `json:"kind"` // fold, checkcall, bet, allin
	SizeIdx int    `json:"size_idx,omitempty"`
}

// PolicyResponse — NN-predicted action probabilities.
type PolicyResponse struct {
	LegalActions []string  `json:"legal_actions"`
	Probs        []float64 `json:"probs"`
	CurSeat      int       `json:"cur_seat"`
	Model        string    `json:"model"`
	ServerMS     int64     `json:"server_ms"`
}

// ErrorResponse — error payload.
type ErrorResponse struct {
	Error string `json:"error"`
}

func parseAction(h HistoryEntry) (nlhe6.Action, error) {
	switch strings.ToLower(h.Kind) {
	case "fold":
		return nlhe6.Action{Kind: nlhe6.ActionFold}, nil
	case "checkcall", "check", "call":
		return nlhe6.Action{Kind: nlhe6.ActionCheckCall}, nil
	case "bet":
		return nlhe6.Action{Kind: nlhe6.ActionBet, SizeIdx: uint8(h.SizeIdx)}, nil
	case "allin", "all-in", "all_in":
		return nlhe6.Action{Kind: nlhe6.ActionAllIn}, nil
	}
	return nlhe6.Action{}, fmt.Errorf("unknown action kind: %q", h.Kind)
}

func actionLabel(a nlhe6.Action) string {
	switch a.Kind {
	case nlhe6.ActionFold:
		return "fold"
	case nlhe6.ActionCheckCall:
		return "checkcall"
	case nlhe6.ActionBet:
		return fmt.Sprintf("bet%d", a.SizeIdx)
	case nlhe6.ActionAllIn:
		return "allin"
	}
	return "?"
}

// reconstructState — build nlhe6.State from request by replaying action history.
// Sets hero's hole cards (other seats use placeholder cards distinct from hero+board).
func reconstructState(req *PolicyRequest) (*nlhe6.State, error) {
	if req.NumPlayers < 2 || req.NumPlayers > 6 {
		return nil, fmt.Errorf("num_players=%d out of range [2,6]", req.NumPlayers)
	}
	if len(req.Hole) != 2 {
		return nil, fmt.Errorf("hole must have 2 cards, got %d", len(req.Hole))
	}
	if len(req.Board) > 5 {
		return nil, fmt.Errorf("board must have ≤ 5 cards, got %d", len(req.Board))
	}
	cfg := &nlhe6.GameConfig{
		NumPlayers: req.NumPlayers,
		SmallBlind: 1,
		BigBlind:   2,
		StartStack: 2 * req.StackBBs,
		BetSizes:   req.BetSizes,
	}
	s := nlhe6.NewStateWithButton(cfg, nlhe6.Seat(req.Button))

	// Hero hole.
	hero := s.Cur // first to act (will change if history applied; that's OK we tag now)
	c1 := nlhe.ParseCard(req.Hole[0])
	c2 := nlhe.ParseCard(req.Hole[1])
	// Find an open slot: the seat that's first to act preflop = s.Cur. We set
	// hero's hole there. Other seats get placeholder cards.
	used := make(map[nlhe.Card]bool)
	used[c1] = true
	used[c2] = true
	s.SetHole(hero, c1, c2)

	// Placeholder holes for opponents (must be distinct from hero + board).
	for _, bc := range req.Board {
		used[nlhe.ParseCard(bc)] = true
	}
	var fillerIdx int
	getFiller := func() nlhe.Card {
		for fillerIdx < 52 {
			c := nlhe.Card(fillerIdx)
			fillerIdx++
			if !used[c] {
				used[c] = true
				return c
			}
		}
		return nlhe.Card(0) // shouldn't happen with ≥ 2 hero + ≤ 5 board + ≤ 10 opp_hole = 17 cards used
	}
	for i := 0; i < req.NumPlayers; i++ {
		if nlhe6.Seat(i) == hero {
			continue
		}
		s.SetHole(nlhe6.Seat(i), getFiller(), getFiller())
	}

	// Apply history actions in order. Server fills board between streets.
	for i, he := range req.History {
		// Fill board if street transition needs it (using request's board cards).
		boardIdx := int(s.NumBoard)
		for {
			n, needs := s.NeedsBoard()
			if !needs {
				break
			}
			if boardIdx+n > len(req.Board) {
				return nil, fmt.Errorf("history step %d: needs %d board cards but only %d in request", i, n, len(req.Board)-boardIdx)
			}
			for j := 0; j < n; j++ {
				s.Board[s.NumBoard] = nlhe.ParseCard(req.Board[boardIdx])
				s.NumBoard++
				boardIdx++
			}
		}
		if s.Terminal {
			return nil, fmt.Errorf("history step %d: state already terminal", i)
		}
		a, err := parseAction(he)
		if err != nil {
			return nil, fmt.Errorf("history step %d: %w", i, err)
		}
		s.Apply(a)
	}
	// Final board fill if needed.
	boardIdx := int(s.NumBoard)
	for {
		n, needs := s.NeedsBoard()
		if !needs {
			break
		}
		if boardIdx+n > len(req.Board) {
			return nil, fmt.Errorf("final board fill: needs %d but only %d in request", n, len(req.Board)-boardIdx)
		}
		for j := 0; j < n; j++ {
			s.Board[s.NumBoard] = nlhe.ParseCard(req.Board[boardIdx])
			s.NumBoard++
			boardIdx++
		}
	}

	return s, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	flag.Parse()
	if *modelPath == "" {
		log.Fatalf("-model required")
	}
	policy, err := loadModel(*modelPath)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer policy.Close()
	modelName := filepath.Base(*modelPath)
	log.Printf("[server] loaded NN model: %s", modelName)

	http.HandleFunc("/api/v1/policy", func(w http.ResponseWriter, r *http.Request) {
		t0 := time.Now()
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "POST only"})
			return
		}
		var req PolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "decode: " + err.Error()})
			return
		}
		s, err := reconstructState(&req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		if s.Terminal {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "state is terminal after history replay"})
			return
		}
		legal := s.LegalActions()
		probs, err := policy.Forward(s)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "inference: " + err.Error()})
			return
		}
		labels := make([]string, len(legal))
		for i, a := range legal {
			labels[i] = actionLabel(a)
		}
		writeJSON(w, http.StatusOK, PolicyResponse{
			LegalActions: labels,
			Probs:        probs,
			CurSeat:      int(s.Cur),
			Model:        modelName,
			ServerMS:     time.Since(t0).Milliseconds(),
		})
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("[server] listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
