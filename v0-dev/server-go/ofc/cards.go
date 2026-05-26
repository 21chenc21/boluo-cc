package ofc

// Card 表示一张牌. 用 packed uint8 表示:
//   bits 0-3: rank (0=2, 1=3, ..., 11=K, 12=A, 13=joker)
//   bits 4-5: 非鬼: suit (0=s, 1=h, 2=d, 3=c); 鬼: jid (0-3)
//   bit  6:   joker flag (1 = joker)
// 多张鬼牌通过 jid 区分 (与 JS card.jid 一致, 用于 cardId/usedCards 唯一性)
type Card uint8

const (
	JokerRank uint8 = 13
)

// 显式 rank/suit 索引
const (
	Rank2 = iota
	Rank3
	Rank4
	Rank5
	Rank6
	Rank7
	Rank8
	Rank9
	RankT
	RankJ
	RankQ
	RankK
	RankA
)

const (
	SuitS = iota
	SuitH
	SuitD
	SuitC
)

// MakeCard 构造非鬼牌
func MakeCard(rank, suit uint8) Card {
	return Card(rank | (suit << 4))
}

// MakeJoker 构造 jid=0 的鬼牌
func MakeJoker() Card {
	return MakeJokerWithJID(0)
}

// MakeJokerWithJID 构造指定 jid 的鬼牌 (jid 0-3)
func MakeJokerWithJID(jid uint8) Card {
	return Card(JokerRank | ((jid & 0x03) << 4) | (1 << 6))
}

// Rank 返回 rank 索引 (0-12), 鬼牌返回 13
func (c Card) Rank() uint8 { return uint8(c) & 0x0F }

// Suit 返回 suit 索引 (0-3); 鬼牌结果不应使用
func (c Card) Suit() uint8 { return (uint8(c) >> 4) & 0x03 }

// JID 返回鬼牌 id (0-3); 非鬼牌返回 0
func (c Card) JID() uint8 {
	if !c.IsJoker() {
		return 0
	}
	return (uint8(c) >> 4) & 0x03
}

// IsJoker 判断是否鬼牌
func (c Card) IsJoker() bool { return uint8(c)&(1<<6) != 0 }

// String 返回 'Kc' 等. 鬼牌返回 'X'.
func (c Card) String() string {
	if c.IsJoker() {
		return "X"
	}
	return string([]byte{rankChar(c.Rank()), suitChar(c.Suit())})
}

// ID 返回 cardId (与 JS cardId 一致): joker 返回 'Xj{jid}', 非 joker 返回 'RankSuit'
func (c Card) ID() string {
	if c.IsJoker() {
		return "Xj" + string('0'+c.JID())
	}
	return c.String()
}

func rankChar(r uint8) byte {
	switch r {
	case 8:
		return 'T'
	case 9:
		return 'J'
	case 10:
		return 'Q'
	case 11:
		return 'K'
	case 12:
		return 'A'
	}
	return '2' + r
}

func suitChar(s uint8) byte {
	return []byte{'s', 'h', 'd', 'c'}[s]
}

// ParseCard 解析 'Kc'/'X' 等字符串
func ParseCard(s string) (Card, bool) {
	if s == "X" || s == "x" {
		return MakeJoker(), true
	}
	// "Xj0" "Xj1" 等 — Card.ID() 用的多鬼牌格式, 与 JS cardId 对齐.
	// 1.0.0 漏修: ID() 输出 "Xj0" 但 ParseCard 不认 -> 多鬼场景 round-trip fail.
	if len(s) == 3 && (s[0] == 'X' || s[0] == 'x') && s[1] == 'j' {
		jid := s[2] - '0'
		if jid <= 3 {
			return MakeJokerWithJID(jid), true
		}
	}
	if len(s) != 2 {
		return 0, false
	}
	r, ok := rankFromChar(s[0])
	if !ok {
		return 0, false
	}
	su, ok := suitFromChar(s[1])
	if !ok {
		return 0, false
	}
	return MakeCard(r, su), true
}

func rankFromChar(c byte) (uint8, bool) {
	switch c {
	case '2':
		return 0, true
	case '3':
		return 1, true
	case '4':
		return 2, true
	case '5':
		return 3, true
	case '6':
		return 4, true
	case '7':
		return 5, true
	case '8':
		return 6, true
	case '9':
		return 7, true
	case 'T':
		return 8, true
	case 'J':
		return 9, true
	case 'Q':
		return 10, true
	case 'K':
		return 11, true
	case 'A':
		return 12, true
	}
	return 0, false
}

func suitFromChar(c byte) (uint8, bool) {
	switch c {
	case 's':
		return 0, true
	case 'h':
		return 1, true
	case 'd':
		return 2, true
	case 'c':
		return 3, true
	}
	return 0, false
}

// MakeDeck 构造 52 + numJokers 张牌. 鬼牌 jid 顺序 0..numJokers-1
// 顺序与 JS createDeck 一致: SUITS=['c','d','h','s'], RANKS=['2'..'A']
// (suit 0=s, 1=h, 2=d, 3=c, 所以 JS 顺序对应 idx 3, 2, 1, 0)
func MakeDeck(numJokers int) []Card {
	out := make([]Card, 0, 52+numJokers)
	suitOrder := []uint8{3, 2, 1, 0} // c, d, h, s (与 JS SUITS 一致)
	for _, s := range suitOrder {
		for r := uint8(0); r < 13; r++ {
			out = append(out, MakeCard(r, s))
		}
	}
	for i := 0; i < numJokers; i++ {
		out = append(out, MakeJokerWithJID(uint8(i)))
	}
	return out
}
