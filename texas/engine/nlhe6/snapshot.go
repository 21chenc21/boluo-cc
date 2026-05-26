package nlhe6

// Snapshot — captures mutable state for O(1) restore during MCCFR walk.
// Hole cards immutable per game (not snapshotted). Board mutations tracked via
// NumBoard; entries past NumBoard may be garbage but never read.
type Snapshot struct {
	Stacks        [MaxPlayers]int
	Wagered       [MaxPlayers]int
	BetThisStreet [MaxPlayers]int
	HasActed      [MaxPlayers]bool
	Folded        [MaxPlayers]bool
	AllIn         [MaxPlayers]bool

	NumBoard      uint8
	Street        Street
	Button        Seat
	Cur           Seat
	HistLen       [4]int

	LastBetAmount int
	LastRaiseSize int

	Terminal   bool
	FoldWinner Seat
}

func (s *State) Snapshot() Snapshot {
	return Snapshot{
		Stacks:        s.Stacks,
		Wagered:       s.Wagered,
		BetThisStreet: s.BetThisStreet,
		HasActed:      s.HasActed,
		Folded:        s.Folded,
		AllIn:         s.AllIn,
		NumBoard:      s.NumBoard,
		Street:        s.Street,
		Button:        s.Button,
		Cur:           s.Cur,
		HistLen: [4]int{
			len(s.Hist[0]), len(s.Hist[1]), len(s.Hist[2]), len(s.Hist[3]),
		},
		LastBetAmount: s.LastBetAmount,
		LastRaiseSize: s.LastRaiseSize,
		Terminal:      s.Terminal,
		FoldWinner:    s.FoldWinner,
	}
}

func (s *State) Restore(snap Snapshot) {
	s.Stacks = snap.Stacks
	s.Wagered = snap.Wagered
	s.BetThisStreet = snap.BetThisStreet
	s.HasActed = snap.HasActed
	s.Folded = snap.Folded
	s.AllIn = snap.AllIn
	s.NumBoard = snap.NumBoard
	s.Street = snap.Street
	s.Button = snap.Button
	s.Cur = snap.Cur
	s.Hist[0] = s.Hist[0][:snap.HistLen[0]]
	s.Hist[1] = s.Hist[1][:snap.HistLen[1]]
	s.Hist[2] = s.Hist[2][:snap.HistLen[2]]
	s.Hist[3] = s.Hist[3][:snap.HistLen[3]]
	s.LastBetAmount = snap.LastBetAmount
	s.LastRaiseSize = snap.LastRaiseSize
	s.Terminal = snap.Terminal
	s.FoldWinner = snap.FoldWinner
}
