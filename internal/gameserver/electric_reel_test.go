package gameserver

import (
	"testing"

	"rf4server/internal/proto"
)

// 电动轮的电机装在渔轮 f14（dfjpnambjcj）扩展槽，wire 类必须是
// kbpmmclfipg/1470（DTO odlknjlbooh）。槽为空时客户端不启用电动收线。
func TestElectricReelMotorMaterialized(t *testing.T) {
	srv, _, ctx := newMiscProbe(t)
	it, err := srv.CreateItem(ctx, 1000001, "elcon_6003", "backpack", 1)
	if err != nil || it == nil {
		t.Fatalf("创建电动轮失败: %v", err)
	}
	if it.WireClass != "dgjinoebdop" || it.TypeID != 149 {
		t.Fatalf("渔轮 wire=%s/%d", it.WireClass, it.TypeID)
	}
	fields, err := FieldsFromJSON(it.FieldsJSON)
	if err != nil {
		t.Fatal(err)
	}
	idx := fieldIdxByCS(it.WireClass, "dfjpnambjcj")
	if idx < 0 || idx >= len(fields) {
		t.Fatal("渔轮缺少 f14 电机槽")
	}
	motor := fields[idx].Val
	if motor == nil || motor.Kind != proto.KindObj {
		t.Fatalf("f14 电机槽为空: %#v", motor)
	}
	if motor.Class != "kbpmmclfipg" || motor.TypeID != 1470 {
		t.Fatalf("f14 电机类=%s/%d，期望 kbpmmclfipg/1470", motor.Class, motor.TypeID)
	}
}
