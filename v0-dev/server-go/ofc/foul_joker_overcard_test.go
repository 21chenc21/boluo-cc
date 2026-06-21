package ofc

import "testing"

// 2026-06-14 TopExceedsMid TypeHighCard 漏判修复探针.
// prod 范局 (ypk 2026-06-14, 范内摆 foul): 顶 AKT / 中 A8764 / 底 club-flush(含鬼 X).
// 顶/中都 A 高, 但顶次高 K > 中次高 8 → 顶 > 中 = 冒顶 foul. 旧码只比 r0 (A==A) → 漏判.
// joker 路径 (IsFoulJoker) 专用, 0-joker 走 IsFoul 深比较无此问题.
func TestIsFoulJoker_AHighOvercard(t *testing.T) {
	top := mustCards("Kc", "Ac", "Th")
	mid := mustCards("7d", "4d", "Ah", "8d", "6s")
	bot := mustCards("6c", "7c", "9c", "Tc", "X")
	if !IsFoulJoker(top, mid, bot) {
		t.Fatal("顶AKT > 中A8764 应判 foul (次高 K>8), 漏判 = 范内摆 foul")
	}
}

func TestTopExceedsMid_HighCardKickers(t *testing.T) {
	// 顶 AKT vs 中 A8764: A==A, 次高 K>8 → true (修复目标)
	if !TopExceedsMid(Evaluate3(mustCards("Kc", "Ac", "Th")), Evaluate5(mustCards("7d", "4d", "Ah", "8d", "6s"))) {
		t.Fatal("AKT > A8764 (次高 K>8) 应 true")
	}
	// 顶 A82 vs 中 AK953: A==A, 次高 8<K → false (顶不超, 合法)
	if TopExceedsMid(Evaluate3(mustCards("Ac", "8h", "2d")), Evaluate5(mustCards("Ad", "Kc", "9s", "5h", "3c"))) {
		t.Fatal("A82 < AK953 (次高 8<K) 应 false")
	}
	// 前三高全等 → tie → false (相等不算冒顶, 防误判)
	if TopExceedsMid(Evaluate3(mustCards("Ac", "Kh", "Td")), Evaluate5(mustCards("As", "Kd", "Th", "5c", "3s"))) {
		t.Fatal("AKT == AKT(+5,3) 前三高相等应 false (tie 非 foul)")
	}
	// r0 不同仍正确: 顶 KQJ vs 中 A9752 → K<A → false
	if TopExceedsMid(Evaluate3(mustCards("Kc", "Qh", "Jd")), Evaluate5(mustCards("Ah", "9d", "7s", "5c", "2h"))) {
		t.Fatal("KQJ < A9752 (r0 K<A) 应 false")
	}
	// A==A, 次高==次高 (K), 第三 T>9 → true (走到 r2 比较)
	if !TopExceedsMid(Evaluate3(mustCards("Ac", "Kh", "Td")), Evaluate5(mustCards("As", "Kd", "9h", "5c", "3s"))) {
		t.Fatal("AKT > AK9(53) (r0/r1 等, 第三 T>9) 应 true")
	}
}
