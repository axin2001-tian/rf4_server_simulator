package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

// 历史竿架在补齐类别映射前以兜底类型 140（饵料）落库。启动修复应把它改写成
// 竿架实例类 abmokghdbhf/11500，玩家不必删除重买；属性保持不变。
func TestRepairLegacyRodPodReclassifies(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	def, err := db.GetItemDefByItemID(ctx, "rod_pod_03al")
	if err != nil {
		t.Fatal(err)
	}
	obj := wiregen.MustMakeObj(fallbackWireClass, nil)
	guid := NewItemGUID()
	obj.Fields[fieldIdxByCS(fallbackWireClass, csGUID)].Hex = guid
	obj.Fields[fieldIdxByCS(fallbackWireClass, csNumericID)].Num = ptrU64(uint64(def.NumericID))
	fields, err := FieldsToJSON(obj.Fields)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &store.Item{
		GUID: guid, UID: 1000001, ItemID: def.ItemID, NumericID: def.NumericID,
		WireClass: fallbackWireClass, TypeID: fallbackTypeID, Container: "backpack",
		Qty: 1, Durability: 0.8, Workmanship: 0.6, FieldsJSON: fields,
	}
	if err := db.InsertItem(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := srv.RepairItemPhysics(ctx); err != nil {
		t.Fatal(err)
	}
	fixed := mustItem(t, db, guid)
	if fixed.WireClass != "abmokghdbhf" || fixed.TypeID != 11500 {
		t.Fatalf("历史竿架未改写为竿架实例类: %s/%d", fixed.WireClass, fixed.TypeID)
	}
	if fixed.Durability != 0.8 || fixed.Workmanship != 0.6 || fixed.Qty != 1 {
		t.Fatalf("改写改动了物品属性: %+v", fixed)
	}
	if err := srv.RepairItemPhysics(ctx); err != nil {
		t.Fatal(err)
	}
	again := mustItem(t, db, guid)
	if *again != *fixed {
		t.Fatal("重复修复改动了竿架")
	}
}
