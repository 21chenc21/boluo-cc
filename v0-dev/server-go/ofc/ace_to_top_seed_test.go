package ofc
import "testing"

// 2026-06-18 局56 R3: 顶[Qd]非范级, As→顶 seed AA追范 → +8
func TestAceToTopSeed_Fire(t *testing.T) {
	pre := st([]string{"Qd"}, []string{"Ks", "3c"}, []string{"3d", "4s", "6d", "5h"})
	pre.Round = 3
	post := st([]string{"Qd", "As"}, []string{"Ks", "3c"}, []string{"3d", "4s", "6d", "5h", "7d"})
	post.Round = 3
	if got := RnAceToTopSeedBonus(post, pre); got != 8 {
		t.Fatalf("As→非范顶 seed 应+8, got %v", got)
	}
}

// R5 不加 (std-50: 终局A→顶若AA>中foul→鬼压低浪费A)
func TestAceToTopSeed_SkipR5(t *testing.T) {
	pre := st([]string{"Qd"}, []string{"Ks", "3c"}, []string{"3d", "4s", "6d", "5h"})
	pre.Round = 5
	post := st([]string{"Qd", "As"}, []string{"Ks", "3c"}, []string{"3d", "4s", "6d", "5h", "7d"})
	post.Round = 5
	if got := RnAceToTopSeedBonus(post, pre); got != 0 {
		t.Fatalf("R5终局 不加, got %v", got)
	}
}

// 顶已范级对(QQ) 不加 — 已锁范不需seed
func TestAceToTopSeed_SkipFanTop(t *testing.T) {
	pre := st([]string{"Qd", "Qs"}, []string{"Ks", "3c"}, []string{"3d", "4s"})
	pre.Round = 3
	post := st([]string{"Qd", "Qs", "As"}, []string{"Ks", "3c"}, []string{"3d", "4s"})
	post.Round = 3
	if got := RnAceToTopSeedBonus(post, pre); got != 0 {
		t.Fatalf("顶已QQ范 不需seed, got %v", got)
	}
}
