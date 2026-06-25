package ofc

// Row 表示行位置
type Row int

const (
	RowTop    Row = 0
	RowMiddle Row = 1
	RowBottom Row = 2
)

func (r Row) String() string {
	switch r {
	case RowTop:
		return "top"
	case RowMiddle:
		return "middle"
	case RowBottom:
		return "bottom"
	}
	return "?"
}

// GameState — 单玩家局面 (与 JS GameState 对齐)
type GameState struct {
	Top       []Card
	Middle    []Card
	Bottom    []Card
	UsedCards map[string]bool // cardId 集合 (含已放 + 已弃 + 已知对手可见)
	Round     int
	NumJokers int

	// LastDiscard / HasLastDiscard — R2-R5 决策后的弃牌信号 (V3 features N 组 + Tier 3 用).
	// R1 没弃牌; HasLastDiscard=false. R2-R5 candidate 构建时 *必须* 设置, 否则 V3 N 组特征丢失.
	LastDiscard    Card
	HasLastDiscard bool
}

// NewGameState 构造空 state
func NewGameState(numJokers int) *GameState {
	return &GameState{
		Top:       make([]Card, 0, 3),
		Middle:    make([]Card, 0, 5),
		Bottom:    make([]Card, 0, 5),
		UsedCards: make(map[string]bool),
		NumJokers: numJokers,
	}
}

// SetDiscard — 设置 R2-R5 候选的弃牌信号 (供 V3 features N/N2 用).
// 在候选 postState 构建处调用, 紧跟 UsedCards 标记之后. R1 不调用.
func (gs *GameState) SetDiscard(c Card) {
	gs.LastDiscard = c
	gs.HasLastDiscard = true
}

// Clone — 深拷贝
func (gs *GameState) Clone() *GameState {
	out := &GameState{
		Top:            append(make([]Card, 0, 3), gs.Top...),
		Middle:         append(make([]Card, 0, 5), gs.Middle...),
		Bottom:         append(make([]Card, 0, 5), gs.Bottom...),
		UsedCards:      make(map[string]bool, len(gs.UsedCards)),
		Round:          gs.Round,
		NumJokers:      gs.NumJokers,
		LastDiscard:    gs.LastDiscard,
		HasLastDiscard: gs.HasLastDiscard,
	}
	for k, v := range gs.UsedCards {
		out.UsedCards[k] = v
	}
	return out
}

func (gs *GameState) TopSlots() int    { return 3 - len(gs.Top) }
func (gs *GameState) MidSlots() int    { return 5 - len(gs.Middle) }
func (gs *GameState) BotSlots() int    { return 5 - len(gs.Bottom) }
func (gs *GameState) TotalSlots() int  { return gs.TopSlots() + gs.MidSlots() + gs.BotSlots() }
func (gs *GameState) IsComplete() bool { return len(gs.Top) == 3 && len(gs.Middle) == 5 && len(gs.Bottom) == 5 }

// CanPlace — row 是否还有空位
func (gs *GameState) CanPlace(row Row) bool {
	switch row {
	case RowTop:
		return gs.TopSlots() > 0
	case RowMiddle:
		return gs.MidSlots() > 0
	case RowBottom:
		return gs.BotSlots() > 0
	}
	return false
}

// PlaceCard — 放牌到指定行 (会同时加到 usedCards). 满了静默忽略 (与 JS 一致)
func (gs *GameState) PlaceCard(c Card, row Row) {
	switch row {
	case RowTop:
		if gs.TopSlots() > 0 {
			gs.Top = append(gs.Top, c)
		}
	case RowMiddle:
		if gs.MidSlots() > 0 {
			gs.Middle = append(gs.Middle, c)
		}
	case RowBottom:
		if gs.BotSlots() > 0 {
			gs.Bottom = append(gs.Bottom, c)
		}
	}
	gs.UsedCards[c.ID()] = true
}

// AddUsed — 把卡 ID 直接加到 usedCards (不放牌, 用于对手可见牌等)
func (gs *GameState) AddUsed(cid string) {
	gs.UsedCards[cid] = true
}

// GetRemainingDeck — 返回还可发的牌 (Deck \ UsedCards)
func (gs *GameState) GetRemainingDeck() []Card {
	deck := MakeDeck(gs.NumJokers)
	out := make([]Card, 0, len(deck))
	for _, c := range deck {
		if !gs.UsedCards[c.ID()] {
			out = append(out, c)
		}
	}
	return out
}

// Score — 返回评分结果 (foul/score/royalty/fantasy)
func (gs *GameState) Score() ScoreResult {
	return ScoreHand(gs.Top, gs.Middle, gs.Bottom)
}
