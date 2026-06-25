package ofc

import "testing"

// 2026-06-12: fan-bonus scale 存进 ckpt + 加载时对齐. 向后兼容: 无字段 → default (太子行为不变).

func resetFanBonus() { SetFanBonusScale(nil, nil, nil, nil, nil) }

func TestFanBonusScale_DefaultAndOverride(t *testing.T) {
	defer resetFanBonus()
	resetFanBonus()
	if V3FanBonusAA != 100 || V3FanBonusTrips != 120 || V3FoulCost != 6 {
		t.Fatalf("default 应 100/120/6, got AA=%v trips=%v foul=%v", V3FanBonusAA, V3FanBonusTrips, V3FoulCost)
	}
	aa, tr := 120.0, 180.0
	SetFanBonusScale(nil, nil, &aa, &tr, nil)
	if V3FanBonusAA != 120 || V3FanBonusTrips != 180 {
		t.Fatalf("override 应 120/180, got AA=%v trips=%v", V3FanBonusAA, V3FanBonusTrips)
	}
	if V3FanBonusQQ != 20 || V3FoulCost != 6 {
		t.Fatalf("没传的字段应回 default, got QQ=%v foul=%v", V3FanBonusQQ, V3FoulCost)
	}
	// 再加载无字段 → 必须 reset 回 default (防 scale 残留)
	resetFanBonus()
	if V3FanBonusAA != 100 || V3FanBonusTrips != 120 {
		t.Fatalf("无字段加载应 reset 回 default, got AA=%v trips=%v", V3FanBonusAA, V3FanBonusTrips)
	}
}

// minimal valid 2-hidden ckpt JSON, 可选带 fan-bonus 字段
func miniCkpt(extra string) []byte {
	base := `"inDim":2,"means":[0,0],"stds":[1,1],"w1":[[0,0],[0,0]],"b1":[0,0],"w2":[[0,0],[0,0]],"b2":[0,0],"w3":[[0,0]],"b3":[0],"yStd":1,"yMean":0`
	return []byte("{" + base + extra + "}")
}

func TestLoadCkpt_FanBonusFields(t *testing.T) {
	defer resetFanBonus()
	// 带字段 → 覆盖
	if err := loadWeightsFromBytes(miniCkpt(`,"fanBonusAA":120,"fanBonusTrips":180,"foulCost":12`)); err != nil {
		t.Fatalf("load with fields: %v", err)
	}
	if V3FanBonusAA != 120 || V3FanBonusTrips != 180 || V3FoulCost != 12 {
		t.Fatalf("带字段 ckpt 应覆盖 120/180/12, got AA=%v trips=%v foul=%v", V3FanBonusAA, V3FanBonusTrips, V3FoulCost)
	}
	// 无字段 (太子) → reset 回 default (关键向后兼容)
	if err := loadWeightsFromBytes(miniCkpt("")); err != nil {
		t.Fatalf("load without fields: %v", err)
	}
	if V3FanBonusAA != 100 || V3FanBonusTrips != 120 || V3FoulCost != 6 {
		t.Fatalf("无字段 ckpt (太子) 应回 default 100/120/6, got AA=%v trips=%v foul=%v", V3FanBonusAA, V3FanBonusTrips, V3FoulCost)
	}
}
