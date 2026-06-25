package ofc

import "testing"

// 2026-06-25 用户 spec: 顺draw紧密度档位 (细化权重). 4=rank2,5=rank3,6=rank4,7=rank5,8=rank6.
func TestStraightDrawTier_Spec(t *testing.T) {
	r := map[string]int{"4": 2, "5": 3, "6": 4, "7": 5, "8": 6}
	cases := []struct {
		name string
		rs   []int
		want int
	}{
		{"45连张", []int{r["4"], r["5"]}, 1},
		{"46", []int{r["4"], r["6"]}, 2},
		{"47", []int{r["4"], r["7"]}, 3},
		{"48", []int{r["4"], r["8"]}, 4},
		{"456", []int{r["4"], r["5"], r["6"]}, 1},
		{"467", []int{r["4"], r["6"], r["7"]}, 2},
		{"468", []int{r["4"], r["6"], r["8"]}, 3},
		{"4567", []int{r["4"], r["5"], r["6"], r["7"]}, 1},
		{"4578", []int{r["4"], r["5"], r["7"], r["8"]}, 2},
	}
	for _, c := range cases {
		if got := straightDrawTier(c.rs); got != c.want {
			t.Errorf("%s: 期望 %d档, 得 %d", c.name, c.want, got)
		}
	}
}

// 边界: 单张/塞不进5窗 → 0; A当低 A2345
func TestStraightDrawTier_Edge(t *testing.T) {
	if straightDrawTier([]int{2}) != 0 {
		t.Error("单张应0")
	}
	if straightDrawTier([]int{2, 7}) != 0 { // 4-9 span5 塞不进
		t.Error("4-9 span>4 应0")
	}
	// A,2,3 = A当低 -1,0,1 → span2 count3 → tier1
	if got := straightDrawTier([]int{int(RankA), 0, 1}); got != 1 {
		t.Errorf("A23(A低) 应1档, 得%d", got)
	}
}
