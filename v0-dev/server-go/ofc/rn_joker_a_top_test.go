package ofc

import "testing"

// 2026-06-13 RnJokerAOnTopBonus — 本轮鬼+A 上顶锁 AA 范 → +10 (ypk-70123850-10 R2)
func TestRnJokerATop_Fire(t *testing.T) {
	// 顶[Kh] 本轮加 Ah+X → [Kh Ah X]=AA范锁
	g := st([]string{"Kh", "Ah", "X"}, []string{"2s", "3d"}, []string{"8c", "Ts"})
	a := &RoundNAction{Kept: []Card{mustParse("Ah"), mustParse("X")}, Placement: []Row{RowTop, RowTop}}
	if got := RnJokerAOnTopBonus(a, g); got != 10 {
		t.Fatalf("鬼+A 锁 AA 应 +10, got %v", got)
	}
}

func TestRnJokerATop_Skip_NoContribution(t *testing.T) {
	// 本轮 A/鬼 都没往 top 放 (放中) → 不奖
	g := st([]string{"Kh", "Ah", "X"}, []string{"2s", "3d"}, []string{"8c", "Ts"})
	a := &RoundNAction{Kept: []Card{mustParse("2c"), mustParse("3c")}, Placement: []Row{RowMiddle, RowBottom}}
	if got := RnJokerAOnTopBonus(a, g); got != 0 {
		t.Fatalf("本轮没往顶加鬼/A 应 0, got %v", got)
	}
}

func TestRnJokerATop_Skip_AAATrips(t *testing.T) {
	// 双鬼+A = AAA 三条 (jt=2) → 不归这条 (走 trips bonus)
	g := st([]string{"X", "X", "Ah"}, []string{"2s", "3d"}, []string{"8c", "Ts"})
	a := &RoundNAction{Kept: []Card{mustParse("X"), mustParse("Ah")}, Placement: []Row{RowTop, RowTop}}
	if got := RnJokerAOnTopBonus(a, g); got != 0 {
		t.Fatalf("双鬼+A=AAA 应 0 (非AA对), got %v", got)
	}
}

func TestRnJokerATop_Skip_MidFullNotTwoPair(t *testing.T) {
	// mid 满 KK (one pair < 两对) → top AA 托不住 → skip
	g := st([]string{"Kh", "Ah", "X"}, []string{"Ks", "Kd", "3h", "4s", "5h"}, []string{"9d", "Th", "Jc"})
	a := &RoundNAction{Kept: []Card{mustParse("Ah"), mustParse("X")}, Placement: []Row{RowTop, RowTop}}
	if got := RnJokerAOnTopBonus(a, g); got != 0 {
		t.Fatalf("mid满KK托不住AA 应 skip(0), got %v", got)
	}
}

func TestRnJokerATop_Skip_JokerPreExisting(t *testing.T) {
	// 实战16: 鬼已在顶 [X Qc], 本轮只加 A → [X Qc As], 该 AA进中不该追 → 不奖
	g := st([]string{"X", "Qc", "As"}, []string{"9s", "2h"}, []string{"Th", "Tc", "Ts"})
	a := &RoundNAction{Kept: []Card{mustParse("As"), mustParse("Ah")}, Placement: []Row{RowTop, RowMiddle}}
	if got := RnJokerAOnTopBonus(a, g); got != 0 {
		t.Fatalf("鬼已在顶+本轮只加A 应 skip(0), got %v", got)
	}
}
