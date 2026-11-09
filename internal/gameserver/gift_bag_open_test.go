package gameserver

import (
	"path/filepath"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
)

func TestUseGiftBagConsumesAndGrantsReward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "giftbag.db")
	s, db := newReceiptTestServer(t, path)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil {
		t.Fatal(err)
	}
	for _, def := range []struct {
		num   int64
		id    string
		cat   int64
		attrs string
	}{
		{910001, "gift_bag_ny23_3", 73, `{"obifdihmcfn":30}`},
		{910002, "bloodworm", 27, `{}`},
	} {
		if _, err := db.SQL.Exec(`INSERT INTO item_def(numeric_id,item_id,category,attrs_json)
			VALUES(?,?,?,?)`, def.num, def.id, def.cat, def.attrs); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetGiftBagRewards(ctx, []store.GiftBagReward{
		{ItemID: "bloodworm", Qty: 1, Weight: 1},
	}); err != nil {
		t.Fatal(err)
	}
	bag, err := s.CreateItem(ctx, 1000001, "gift_bag_ny23_3", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if bag.WireClass == "" {
		t.Fatal("福袋没有 wire 类别")
	}
	sess := newProbeSess(s)
	sess.Token = "giftbag-test"
	tree := proto.Args524(proto.U32(0), proto.Guid(bag.GUID))
	body, err := proto.EncodeValue(tree, s.Table())
	if err != nil {
		t.Fatal(err)
	}
	call := &Ctx{Sess: sess, Main: 4, Sub: 8, CallID: 1, Tree: tree, Body: body,
		DecodeOK: true, ExpectResponse: true, Log: logging.Discard(), replyCapture: &ctxReplyCapture{}}
	out, handled := s.useGiftBag(call)
	if !handled || len(out) == 0 {
		_, present, _, failure := call.replyCapture.snapshot()
		t.Fatalf("handled=%v len=%d present=%v failure=%v", handled, len(out), present, failure)
	}
	// 806 必须过契约守卫；被拒发时客户端收不到回包，只能一直转圈。
	if reason := s.dispatch.checkRegisteredResponse(4, 8, out, s.Table()); reason != "" {
		t.Fatalf("福袋 806 回包被契约守卫拒绝: %s", reason)
	}
	if row, err := db.GetItemByGUID(ctx, bag.GUID); err == nil && row != nil {
		t.Fatalf("福袋未被消耗: qty=%d", row.Qty)
	}
	var got int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=1000001 AND item_id='bloodworm'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("奖励未发放, count=%d", got)
	}
}
