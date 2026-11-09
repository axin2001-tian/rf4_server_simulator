package gameserver

// 新年福袋（物品类别 73）开袋逻辑。
//
// 客户端里礼物袋只是活动任务道具，没有任何“开袋给奖励”的现成数据，双击使用时
// 走的是 4/8 通用分支，原来只会回一份物品快照，所以表现为“解不开”。这里接管
// 4/8：消耗 1 个袋子，按后台奖池（kv: gift_bag_reward_pool）随机发一份奖励。
//
// 注意：袋子在背包里的 wire 类别沿用礼包 DTO（items.go 里 73 → coacllgmnml），
// 所以客户端表现仍按礼包处理；如果实测背包渲染异常，优先回看那里的映射。

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

const giftBagCategory = 73

// useGiftBag 处理 4/8：打开自己背包里的福袋。非福袋返回 handled=false 交回通用分支。
func (s *Server) useGiftBag(ctx *Ctx) ([]byte, bool) {
	if !s.requireDB(ctx) {
		return nil, false
	}
	storage, hasStorage := ctx.ArgInt(0)
	if ctx.Tree == nil || ctx.Tree.Kind != proto.KindArgs || len(ctx.Tree.Items) != 2 {
		return nil, false
	}
	arg := ctx.Tree.ArgItem(1)
	if arg == nil || arg.Kind != proto.KindGuid || !validRigGUID(arg.Hex) {
		return nil, false
	}

	var tx *sql.Tx
	fail := func(message string) ([]byte, bool) {
		if tx != nil {
			_ = tx.Rollback()
		}
		ctx.ReplyFailure("System.InvalidOperationException", message)
		return nil, true
	}
	failed := func(err error) ([]byte, bool) {
		ctx.Log.Warn("打开福袋未完成", "err", err)
		return fail("打开福袋未完成，请重试")
	}

	cctx, uid := ctx.Sess.pushCtx(), ctx.Sess.Userid
	row, err := s.DB.GetItemByGUID(cctx, arg.Hex)
	if errors.Is(err, sql.ErrNoRows) || row == nil {
		return nil, false
	}
	if err != nil {
		return failed(err)
	}
	def, err := s.DB.GetItemDef(cctx, row.NumericID)
	if errors.Is(err, sql.ErrNoRows) || def == nil {
		return nil, false
	}
	if err != nil {
		return failed(err)
	}
	if def.Category != giftBagCategory {
		return nil, false
	}
	bagClass := categoryWireClass[giftBagCategory]
	if ctx.Sess.Closed() {
		return fail("会话已关闭，请重新连接")
	}
	if !hasStorage || storage != 0 || row.UID != uid || row.Container != "backpack" || row.RigGUID != "" {
		return fail("只能打开自己背包中的福袋")
	}
	if row.WireClass != bagClass.Class || row.TypeID != bagClass.TypeID {
		return fail("福袋状态无效，请刷新背包")
	}

	year, mask, no := store.GiftBagYearOf(def.ItemID)
	candidates, err := s.giftBagRewardCandidates(cctx, mask)
	if err != nil {
		return failed(err)
	}
	if len(candidates) == 0 {
		return fail("福袋奖池还没有配置可发放的物品，请联系管理员")
	}
	chosen, ok := rollGiftBagReward(candidates)
	if !ok {
		return fail("福袋奖池权重无效，请联系管理员")
	}
	reward, err := s.prepareNewItem(cctx, uid, chosen.ItemID, "backpack", chosen.Qty, "")
	if err != nil {
		ctx.Log.Warn("福袋奖励构造失败", "item", chosen.ItemID, "err", err)
		return fail("福袋奖励物品无效： " + chosen.ItemID)
	}

	// 必须在开启写事务之前构建物品树：store 只维护一个物理连接，事务打开期间
	// 任何走 s.DB 的查询（ItemToTree 内部的 GetItemDef 等）都会阻塞，等 ctx 超时
	// 后 database/sql 会自动回滚事务，后续 tx 操作报 ErrTxDone。
	rewardTree, err := s.ItemToTree(cctx, reward)
	if err != nil {
		return failed(err)
	}

	tx, err = beginEconomyTx(cctx, s.DB)
	if err != nil {
		return failed(err)
	}
	defer func() { _ = tx.Rollback() }()

	// 事务内重新取一次，避免读快照后被并发改动。
	cur, err := getItemTx(cctx, tx, row.GUID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail("福袋已不在背包中，请刷新背包")
	}
	if err != nil {
		return failed(err)
	}
	if cur.UID != uid || cur.Container != "backpack" || cur.NumericID != row.NumericID {
		return fail("福袋已不在背包中，请刷新背包")
	}
	if _, err := consumeItemQtyTx(cctx, tx, cur, 1); err != nil {
		return failed(err)
	}
	if err := insertItemTx(cctx, tx, reward); err != nil {
		return failed(err)
	}

	var updated []*proto.Value
	var removed []string
	if cur.Qty == 0 {
		removed = []string{cur.GUID}
	} else {
		fields, err := FieldsFromJSON(cur.FieldsJSON)
		if err != nil {
			return failed(err)
		}
		tree := proto.Obj(cur.WireClass, uint32(cur.TypeID), fields...)
		stateFlagsZeroFill(cur.WireClass, tree, def)
		updated = []*proto.Value{tree}
	}
	// 客户端对类别 73 的 4/8 响应会走“开袋”分支（deghjmihlcf.acgahdbhelm）：
	// 回包类型必须是 clghnpicpeh（806，ldkingipcpa），f0 = 获得的物品，
	// 客户端据此弹出“找到物品”对话框（顶部物品格 + 中间 3D 预览 + 确定[Space]）。
	// f1/f2/f3 与 800 快照同义（新增/更新/移除），保证背包状态同步。
	f2 := proto.Null()
	if len(updated) > 0 {
		f2 = proto.Args(proto.S("61"), updated...)
	}
	f3 := proto.Null()
	if len(removed) > 0 {
		f3 = proto.GuidArr(removed)
	}
	body, err := proto.EncodeValue(wiregen.MustMakeObj("clghnpicpeh", map[int]*proto.Value{
		0: rewardTree,
		1: proto.Args(proto.S("61")),
		2: f2,
		3: f3,
	}), ctx.Sess.Table())
	if err != nil {
		return failed(err)
	}
	if err := tx.Commit(); err != nil {
		return failed(err)
	}

	change := baitChange{Removed: removed}
	if len(updated) > 0 {
		change.Updated = []string{cur.GUID}
	}
	rewardGUID := reward.GUID
	ctx.AfterResponse(func() {
		ctx.Sess.pushBaitChange(change)
		ctx.Sess.Push4_22ItemGUIDs([]string{rewardGUID})
	})
	ctx.Log.Info("4/8 打开福袋", "bag", def.ItemID, "year", year, "no", no,
		"reward", chosen.ItemID, "qty", chosen.Qty)
	return body, true
}

// giftBagRewardCandidates 过滤出适用于该年份、物品定义存在且权重大于 0 的奖池条目。
func (s *Server) giftBagRewardCandidates(ctx context.Context, mask int64) ([]store.GiftBagReward, error) {
	pool, err := s.DB.GiftBagRewards(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.GiftBagReward, 0, len(pool))
	for _, it := range pool {
		if it.Weight <= 0 || it.Qty <= 0 || it.ItemID == "" {
			continue
		}
		if it.Mask != 0 && it.Mask&mask == 0 {
			continue
		}
		def, err := s.DB.GetItemDefByItemID(ctx, it.ItemID)
		if err != nil || def == nil {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// rollGiftBagReward 按权重随机抽一条，使用系统随机源，避免可预测。
func rollGiftBagReward(list []store.GiftBagReward) (store.GiftBagReward, bool) {
	var total float64
	for _, it := range list {
		if it.Weight > 0 {
			total += it.Weight
		}
	}
	if total <= 0 {
		return store.GiftBagReward{}, false
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return store.GiftBagReward{}, false
	}
	r := float64(binary.LittleEndian.Uint64(buf[:])>>11) / float64(uint64(1)<<53) * total
	for _, it := range list {
		if it.Weight <= 0 {
			continue
		}
		r -= it.Weight
		if r <= 0 {
			return it, true
		}
	}
	return list[len(list)-1], true
}
