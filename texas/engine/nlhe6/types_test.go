package nlhe6

import "testing"

// TestPositionForHU — HU convention: button seat = SB, other = BB.
func TestPositionForHU(t *testing.T) {
	tests := []struct {
		seat, button Seat
		want         Position
	}{
		{0, 0, PosSB},
		{1, 0, PosBB},
		{0, 1, PosBB},
		{1, 1, PosSB},
	}
	for _, tt := range tests {
		got := PositionFor(tt.seat, tt.button, 2)
		if got != tt.want {
			t.Errorf("PositionFor(seat=%d, button=%d, n=2) = %v, want %v",
				tt.seat, tt.button, got, tt.want)
		}
	}
}

// TestPositionFor6Max — 6-max standard offsets from button.
func TestPositionFor6Max(t *testing.T) {
	// Button at seat 0; positions clockwise.
	tests := []struct {
		seat Seat
		want Position
	}{
		{0, PosBTN},
		{1, PosSB},
		{2, PosBB},
		{3, PosUTG},
		{4, PosMP},
		{5, PosCO},
	}
	for _, tt := range tests {
		got := PositionFor(tt.seat, 0, 6)
		if got != tt.want {
			t.Errorf("PositionFor(seat=%d, button=0, n=6) = %v, want %v",
				tt.seat, got, tt.want)
		}
	}
	// Button at seat 3; positions shift.
	got := PositionFor(3, 3, 6)
	if got != PosBTN {
		t.Errorf("PositionFor(seat=3, button=3, n=6) = %v, want BTN", got)
	}
	got = PositionFor(0, 3, 6)
	if got != PosUTG {
		t.Errorf("PositionFor(seat=0, button=3, n=6) = %v, want UTG", got)
	}
}

// TestFirstToActPreflopHU — SB acts first preflop in HU.
func TestFirstToActPreflopHU(t *testing.T) {
	// Button=0 (SB=0, BB=1). SB acts first.
	if FirstToActPreflop(0, 2) != 0 {
		t.Errorf("HU button=0 preflop first should be seat 0 (SB)")
	}
	if FirstToActPreflop(1, 2) != 1 {
		t.Errorf("HU button=1 preflop first should be seat 1 (SB)")
	}
}

// TestFirstToActPostflopHU — BB acts first postflop in HU.
func TestFirstToActPostflopHU(t *testing.T) {
	if FirstToActPostflop(0, 2) != 1 {
		t.Errorf("HU button=0 postflop first should be seat 1 (BB)")
	}
	if FirstToActPostflop(1, 2) != 0 {
		t.Errorf("HU button=1 postflop first should be seat 0 (BB)")
	}
}

// TestFirstToActPreflop6Max — UTG (button+3) acts first.
func TestFirstToActPreflop6Max(t *testing.T) {
	if FirstToActPreflop(0, 6) != 3 {
		t.Errorf("6-max button=0 preflop first should be seat 3 (UTG)")
	}
	if FirstToActPreflop(5, 6) != 2 {
		t.Errorf("6-max button=5 preflop first should be seat 2 (UTG)")
	}
}

// TestFirstToActPostflop6Max — SB (button+1) acts first.
func TestFirstToActPostflop6Max(t *testing.T) {
	if FirstToActPostflop(0, 6) != 1 {
		t.Errorf("6-max button=0 postflop first should be seat 1 (SB)")
	}
	if FirstToActPostflop(5, 6) != 0 {
		t.Errorf("6-max button=5 postflop first should be seat 0 (SB)")
	}
}

// TestNextSeat — clockwise rotation.
func TestNextSeat(t *testing.T) {
	if NextSeat(0, 6) != 1 {
		t.Errorf("NextSeat(0, 6) want 1")
	}
	if NextSeat(5, 6) != 0 {
		t.Errorf("NextSeat(5, 6) wrap to 0")
	}
	if NextSeat(0, 2) != 1 {
		t.Errorf("NextSeat(0, 2) want 1")
	}
	if NextSeat(1, 2) != 0 {
		t.Errorf("NextSeat(1, 2) wrap to 0")
	}
}
