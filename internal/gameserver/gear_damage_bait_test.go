package gameserver

import (
	"context"
	"testing"
)

// TestLineBreakKeepsUnusedBaitStack 锁定：整包装填的饵堆在断线时只损失钩上那一份，
// 其余必须退回背包。此前终端组件是整行删除，整堆饵会一起消失。
func TestLineBreakKeepsUnusedBaitStack(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "tele_10180_4", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_15095", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := srv.CreateItem(ctx, uid, "clas_20163", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	bait, err := srv.CreateItem(ctx, uid, "worm_01", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.setStackCount(ctx, bait, 6)

	sess := newProbeSess(srv)
	for _, guid := range []string{line.GUID, hook.GUID, bait.GUID} {
		mountGearForTest(t, srv, sess, rod.GUID, guid)
	}

	comps := rigSlotGUIDs(t, srv, rod.GUID)
	lineGUID := comps[catLine]
	if lineGUID == "" {
		t.Fatal("主线未挂上")
	}
	if _, err := srv.breakLineOrRod(ctx, uid, rod.GUID, lineGUID); err != nil {
		t.Fatalf("断线失败: %v", err)
	}

	var total int64
	var rows int
	if err := srv.DB.SQL.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(qty),0) FROM item
		 WHERE uid=? AND container='backpack' AND item_id=?`, uid, "worm_01").
		Scan(&rows, &total); err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("断线后背包应剩 5 份红虫（6 减钩上 1 份），实得 rows=%d total=%d", rows, total)
	}
}

// TestLineBreakSpendsOnlyOneBaitWhenStackInBag 锁定：饵还在背包里（普通拆分挂载）时，
// 断线同样只扣一份。
func TestLineBreakSpendsOnlyOneBaitWhenStackInBag(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)

	rod, err := srv.CreateItem(ctx, uid, "tele_10180_4", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	line, err := srv.CreateItem(ctx, uid, "mono_15095", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	hook, err := srv.CreateItem(ctx, uid, "clas_20163", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	bait, err := srv.CreateItem(ctx, uid, "worm_01", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	srv.setStackCount(ctx, bait, 6)

	sess := newProbeSess(srv)
	for _, guid := range []string{line.GUID, hook.GUID} {
		mountGearForTest(t, srv, sess, rod.GUID, guid)
	}
	// 单独拆一份挂在钩上，背包保留整堆。
	child := srv.splitMountableChild(ctx, uid, rod.GUID, bait.GUID)
	mountGearForTest(t, srv, sess, rod.GUID, child)

	comps := rigSlotGUIDs(t, srv, rod.GUID)
	lineGUID := comps[catLine]
	if lineGUID == "" {
		t.Fatal("主线未挂上")
	}
	if _, err := srv.breakLineOrRod(ctx, uid, rod.GUID, lineGUID); err != nil {
		t.Fatalf("断线失败: %v", err)
	}
	var total int64
	if err := srv.DB.SQL.QueryRow(
		`SELECT COALESCE(SUM(qty),0) FROM item
		 WHERE uid=? AND container='backpack' AND item_id=?`, uid, "worm_01").
		Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("拆一份挂在钩上时，断线后背包应剩 5 份，实得 %d", total)
	}
}
