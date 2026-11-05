package gameserver

import (
	"context"
	"strings"
	"testing"

	"rf4server/internal/proto"
)

// TestBuildHookTreeByGUIDPicksRequestedHook 锁定 14/11 的钩同步必须按本尾鱼
// 实际中钩的 GUID 取实例，而不是无条件取"第一个挂载钩"。
// 多钩/多拟饵钓组取错钩时，客户端按 14/14.f23 找不到钩实例，钩强状态缺失，
// 表现就是海竿力度条恒空、提杆被钳制。
func TestBuildHookTreeByGUIDPicksRequestedHook(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	ctx := context.Background()
	uid := int64(3101)
	seedEconomyItemDef(t, db, 9501, "hook_test", 12) // 单钩（agedjipcnpe）
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	first, err := srv.CreateItem(ctx, uid, "hook_test", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := srv.CreateItem(ctx, uid, "hook_test", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.GUID == second.GUID {
		t.Fatal("两枚钩应是不同实例")
	}

	tree := srv.buildHookTreeByGUID(ctx, uid, "", second.GUID)
	if tree == nil {
		t.Fatal("按 GUID 取钩实例失败")
	}
	if got := guidOfTree(tree); !strings.EqualFold(got, second.GUID) {
		t.Fatalf("应取本次中钩 %s，实得 %s", second.GUID, got)
	}

	// 目标 GUID 不存在时回退到挂载钩；没有挂载竿时返回空树而不是乱取一个钩。
	if fallback := srv.buildHookTreeByGUID(ctx, uid, "", "ffffffffffffffffffffffffffffffff"); fallback != nil {
		if got := guidOfTree(fallback); strings.EqualFold(got, second.GUID) || strings.EqualFold(got, first.GUID) {
			t.Fatalf("未知 GUID 不应回退到无关钩实例，实得 %s", got)
		}
	}
}

// guidOfTree 返回实例树里的 GUID16 字段值。
func guidOfTree(v *proto.Value) string {
	if v == nil {
		return ""
	}
	for i := range v.Fields {
		if v.Fields[i].Op == "GUID16" && v.Fields[i].Hex != "" {
			return v.Fields[i].Hex
		}
	}
	return ""
}
