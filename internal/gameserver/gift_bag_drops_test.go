package gameserver

import (
	"path/filepath"
	"testing"
	"time"

	"rf4server/internal/store"
)

// 福袋咬钩抽签与确定性发放：100% 概率必中、开关关闭不发、
// 每日上限生效、同一尾鱼重放得到同一个袋子、未携带标记不发袋。
func TestGiftBagBiteRollAndDeterministicGrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "giftbag-bite.db")
	s, db := newReceiptTestServer(t, path)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json)
		VALUES(910001,'gift_bag_ny23_3',73,'{"obifdihmcfn":30}')`); err != nil {
		t.Fatal(err)
	}
	setTuns := func(enabled, chance, daily float64) {
		s.tunMu.Lock()
		s.tunCache = map[string]float64{
			store.GiftBagDropEnabledKey: enabled,
			store.GiftBagDropChanceKey:  chance,
			store.GiftBagDropDailyKey:   daily,
		}
		s.tunStrCache = map[string]string{}
		s.tunAt, s.tunStrAt = time.Now(), time.Now()
		s.tunMu.Unlock()
	}

	// 关闭开关：不抽签。
	setTuns(0, 1, 0)
	if id := s.rollGiftBagBite(ctx, uid); id != "" {
		t.Fatalf("开关关闭仍抽中福袋: %q", id)
	}

	// 100% 概率、不限每日：必中且落在白名单年份目录内。
	setTuns(1, 1, 0)
	id := s.rollGiftBagBite(ctx, uid)
	if id != "gift_bag_ny23_3" {
		t.Fatalf("100%% 概率应抽中唯一福袋: %q", id)
	}

	// 确定性发放：构造的袋子 GUID 由鱼 GUID 派生，重放不变。
	f := &RolledFish{FishGUID: "fish-guid-a", Species: "perch", GiftBagID: id}
	bag, err := s.prepareCatchGiftBag(ctx, uid, f)
	if err != nil || bag == nil {
		t.Fatalf("携带福袋的鱼应构造袋子: %v %v", bag, err)
	}
	again, err := s.prepareCatchGiftBag(ctx, uid, f)
	if err != nil || again == nil || again.GUID != bag.GUID {
		t.Fatalf("重放 GUID 不稳定: %+v %v %v", again, err, bag.GUID)
	}

	// 未携带标记的鱼不发袋。
	plain := &RolledFish{FishGUID: "fish-guid-b", Species: "perch"}
	if bag, err := s.prepareCatchGiftBag(ctx, uid, plain); err != nil || bag != nil {
		t.Fatalf("普通鱼不应发袋: %+v %v", bag, err)
	}

	// 每日上限：已有 1 个今日袋子、上限 1 时不再抽中。
	if _, err := s.CreateItem(ctx, uid, "gift_bag_ny23_3", "backpack", 1); err != nil {
		t.Fatal(err)
	}
	setTuns(1, 1, 1)
	if id := s.rollGiftBagBite(ctx, uid); id != "" {
		t.Fatalf("超过每日上限仍抽中福袋: %q", id)
	}
}
