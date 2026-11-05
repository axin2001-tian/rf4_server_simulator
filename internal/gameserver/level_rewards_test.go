package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
)

// 升级奖励：从 1 级连升到 5 级，应发到 2~5 级的银币和物品，且物品有独立槽位。
func TestGrantLevelUpsGrantsItemsAndSilver(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const uid = int64(2301)
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	start, serr := db.GetProfile(context.Background(), uid)
	if serr != nil {
		t.Fatal(serr)
	}
	c := &Ctx{Log: logging.Discard()}
	items, silver := srv.grantLevelUps(context.Background(), c, uid, 1, 5)

	if silver <= 0 {
		t.Fatal("升级奖励未发放银币")
	}
	if items <= 0 {
		t.Fatal("升级奖励未发放物品")
	}

	var total, distinct int
	if err := db.SQL.QueryRow(
		`SELECT COUNT(*), COUNT(DISTINCT slot) FROM item WHERE uid=? AND container='backpack'`,
		uid).Scan(&total, &distinct); err != nil {
		t.Fatal(err)
	}
	if total != items {
		t.Fatalf("背包物品数=%d，期望 %d", total, items)
	}
	if total != distinct {
		t.Fatalf("槽位有重复：物品 %d 件，不同槽位 %d 个", total, distinct)
	}

	p, err := db.GetProfile(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Silver != start.Silver+silver {
		t.Fatalf("银币=%d，期望 %d", p.Silver, start.Silver+silver)
	}
}

// 没有配置奖励的等级区间不应发放任何东西。
func TestGrantLevelUpsSkipsUnconfiguredLevels(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	const uid = int64(2302)
	if _, err := db.EnsureProfile(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	c := &Ctx{Log: logging.Discard()}
	items, silver := srv.grantLevelUps(context.Background(), c, uid, 40, 41)
	if items != 0 || silver != 0 {
		t.Fatalf("未配置的等级不应发放奖励，实得 items=%d silver=%d", items, silver)
	}
}
