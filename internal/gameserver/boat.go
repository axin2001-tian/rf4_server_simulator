package gameserver

// 3/7 player/check_ticket —— 上船验票 + 开始租船。
//
// 客户端在码头靠近船只时由 BoatWatchDog 驱动：
//  1. 先发 [站点, null guid]（marker 0x17）问"这个码头能用哪张票"；
//  2. 选票后再发 [站点, 票 GUID] 验票，并把响应强转成 ckdficiaaji
//     （wire 类 bplepbfgpmk / type_id 162）；
//  3. 用票构造租用记录（jfnimfgkepk）。票里 knpoijanngk（开关值）为 0 时
//     客户端认为"未租用"，记录构造失败，看门狗执行 "teleport from boat..."，
//     表现为"选中票也上不了船"。
//
// 因此带 GUID 的验票除了回票对象，还要写入租用状态：
//   - knpoijanngk = 1（开关值，客户端据此计算剩余时间）
//   - oieepfcgjgn = 租期分钟数
//   - bkmnbppciao = mbplhipcojh 时效对象 {类型 1, 起始分钟, 时长分钟}

import (
	"fmt"
	"strings"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

const (
	// boatTicketTypeID 是船票的线上 type_id（wire 类 bplepbfgpmk）。
	boatTicketTypeID = 162
	// boatTicketInUseFlag 是 bhhaopagglk.pmdfjipkbmn：物品"使用中"状态位。
	boatTicketInUseFlag = 1048576
	// boatTicketStationAttr 是船票定义里的站点枚举（0..5）。
	boatTicketStationAttr = "algmimlfbdj"
	// boatTicketDurationAttr 是船票定义里的租用时长（分钟）。
	boatTicketDurationAttr = "gloegohmand"
)

func init() { RegisterExtra(registerBoatHandlers) }

// registerBoatHandlers 注册载具/船票相关命令。
//
// 3/10 boat_use 由 timeline.go 的 hBoatUseLog 处理（仅记录请求体，客户端上船后
// 单向通知，不需要回包），这里不重复注册，避免注册顺序漂移。
func registerBoatHandlers(d *Dispatch) {
	d.Register(3, 7, hCheckTicket)
	// 3/18 player/warnings 由 timeline.go 的 hPlayerWarnings 处理（上游 e125cb6），
	// 这里不再重复注册，避免重复定义与注册顺序漂移。
	// main=3 族（player/*）全兜底处理器：客户端会发大量 player/* 命令，
	// 我们不可能逐条注册；兜底器通过 fallbackFor 根据 command_contract.json
	// 里的形状表（kind=none/null/args/dto）返回正确的空响应，避免客户端
	// 因收到 null （0x00）而在内部抛 NullReferenceException 导致 BoatWatchDog
	// 状态机崩溃（表现为上船黑屏 / 反复传回岸边）。
	d.RegisterFamily(3, hFamilyFallbackPlayer)
}

// hFamilyFallbackPlayer main=3 族兜底：按合同表返回空形状。
func hFamilyFallbackPlayer(ctx *Ctx) ([]byte, bool) {
	srv := ctx.Sess.Srv
	if srv == nil {
		return []byte{0x00}, true
	}
	body, _ := srv.fallbackFor(ctx.Main, ctx.Sub)
	if traceCmds {
		ctx.Log.Info("3/" + fmt.Sprintf("%d", ctx.Sub) + " 兜底回包", "len", len(body))
	}
	return body, true
}

// hCheckTicket 处理 3/7：请求参数为 [站点枚举, 船票 GUID]。
func hCheckTicket(ctx *Ctx) ([]byte, bool) {
	// 上船验票前，先清掉"已消耗"的票：到期后我们把 knpoijanngk 置 0 并保留
	// 租用记录作为标记，这里连同租用记录一起删除并把 4/21 推给客户端。
	cleanupConsumedTickets(ctx)

	srv := ctx.Sess.Srv
	if !srv.requireDB(ctx) {
		return nil, false
	}
	cctx := ctx.Sess.pushCtx()
	guids := deepGUIDs(ctx.Tree)
	ints := treeIntsDeep(ctx.Tree)

	var station uint64
	if len(ints) > 0 {
		station = ints[0]
	}

	// null guid 表示"按站点问可用票"，不能当成真实票号去查库。
	var realGUIDs []string
	for _, g := range guids {
		if !isZeroGUID(g) {
			realGUIDs = append(realGUIDs, g)
		}
	}
	if len(realGUIDs) == 0 {
		// 不带 GUID 表示"这个码头现在能用哪张票"。
		// 若已有仍在有效期内的激活票，直接复用它——否则玩家每次上船都会被
		// 追问一次票。没有激活票时才回 null，让客户端弹选票框，由玩家手动
		// 选票后客户端再发 [站点, 票 GUID] 走下方验票分支。
		// （实测 2026-11-03：null-guid 时若自动选票激活并回票对象，客户端
		// 看门狗不认账，会以约 17ms 间隔死循环重发 3/7，表现为按 E 即黑屏。）
		if tree := srv.activeRentalTicket(ctx, station); tree != nil {
			body, err := proto.EncodeValue(tree, ctx.Sess.Table())
			if err == nil {
				ctx.Log.Info("3/7 复用有效期内的船票", "station", station)
				return body, true
			}
			ctx.Log.Warn("3/7 复用船票编码失败", "err", err)
		}
		ctx.Log.Info("3/7 无可复用的票，回 null 交由客户端弹选票框", "station", station)
		return []byte{0x00}, true
	}

	for _, g := range realGUIDs {
		row, err := srv.DB.GetItemByGUID(cctx, g)
		if err != nil || row == nil || row.UID != ctx.Sess.Userid {
			continue
		}
		if row.TypeID != boatTicketTypeID {
			ctx.Log.Info("3/7 该物品不是船票", "guid", guidShort(g), "type", row.TypeID)
			continue
		}
		def, derr := srv.DB.GetItemDef(cctx, row.NumericID)
		if derr != nil {
			def = nil
		}
		if def != nil {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && station > 0 && st != station {
				ctx.Log.Warn("3/7 票站点与请求站点不一致，仍然放行",
					"ticket_station", st, "request_station", station, "guid", guidShort(g))
			}
		}
		// 选中票即开始租用：写入 knpoijanngk / 租期 / 时效对象并落库。
		// 注意：只在状态真正变化时才补推 remove+add（见下方 push 处说明）。
		rented := srv.startTicketRent(ctx, row, def)
		if rented {
			ctx.Log.Info("3/7 船票开始租用", "guid", guidShort(g), "item", row.ItemID)
		}
		tree, terr := srv.ItemToTree(cctx, row)
		if terr != nil {
			ctx.Log.Warn("3/7 船票物化失败", "guid", guidShort(g), "err", terr)
			continue
		}
		body, berr := proto.EncodeValue(tree, ctx.Sess.Table())
		if berr != nil {
			ctx.Log.Warn("3/7 编码失败", "guid", guidShort(g), "err", berr)
			continue
		}
		// [调试] 打印票对象完整字段，定位客户端 _activateTicket 失败的原因。
		// 只在第一次租用时打印一次，避免循环刷屏。
		if rented {
			var dbg string
			for i, f := range tree.Fields {
				label := fmtField(f)
				if dbg != "" {
					dbg += " | "
				}
				dbg += fmt.Sprintf("%d:%s=%s", i, fieldNameByIdx(row.WireClass, i), label)
			}
			ctx.Log.Info("3/7 票对象字段 dump", "guid", guidShort(g), "item", row.ItemID, "fields", dbg)
		}
		// 客户端构造租用记录读的是本地票对象，必须在回包前把"租用中"推给它，
		// 否则它用旧的（未租用）票构造记录，看门狗判定失败并传回岸边。
		// 但只在租用状态发生变化时才推：看门狗验票失败会以极短间隔反复重发
		// 3/7，若每次都推 4/21（移除）+4/22（重加），会把客户端刚构造好的
		// 租用记录连带票对象一起打掉，造成无限验票循环（表现为上船黑屏）。
		if rented {
			ctx.Sess.Push(4, 21, proto.Args524(proto.GuidArr([]string{row.GUID})))
			ctx.Sess.Push(4, 22, proto.Args(proto.S("61"), tree))
		}
		ctx.Log.Info("3/7 验票通过", "guid", guidShort(g), "item", row.ItemID, "station", station)
		return body, true
	}

	ctx.Log.Info("3/7 没有可用船票，回 null", "station", station, "guids", len(realGUIDs))
	return []byte{0x00}, true
}

// startTicketRent 写入"已租用"状态并落库；返回是否发生改动。
//
// 租期默认取票定义里的 gloegohmand（分钟），可用运行参数 boat_rent_minutes
// 覆盖（管理后台 /api/tunables，用于短租期测试）。
func (s *Server) startTicketRent(ctx *Ctx, row *store.Item, def *store.ItemDef) bool {
	minutes := uint64(1440)
	if def != nil {
		if v, ok := defAttrU64(def, boatTicketDurationAttr); ok && v > 0 {
			minutes = v
		}
	}
	if v := s.tunableF("boat_rent_minutes", 0); v > 0 {
		minutes = uint64(v)
	}
	db := s.DB

	fields, err := FieldsFromJSON(row.FieldsJSON)
	if err != nil {
		return false
	}
	// 已激活且未过期的票：直接返回"无改动"。
	// 客户端看门狗会以约 17ms 间隔反复发 3/7 验票；若每次都重写
	// oieepfcgjgn（游戏分钟）和 iingicaldbk（到期时间，随 now 变化），
	// 就会每秒推一次 4/21 remove + 4/22 add，把客户端刚构造好的租用
	// 记录连同票对象打掉，客户端判定激活失败又重发 3/7——无限循环，
	// 表现为上船黑屏。因此只有真正需要激活（0→1）或过期重置时才写入。
	if ki := fieldIdxByCS(row.WireClass, "knpoijanngk"); ki >= 0 && ki < len(fields) {
		if u64Of(fields[ki].Num) == 1 {
			active := true
			if ei := fieldIdxByCS(row.WireClass, csUpdated); ei >= 0 && ei < len(fields) {
				active = u64Of(fields[ei].Num) > uint64(proto.WindowsFiletimeNow())
			}
			if active {
				return false
			}
		}
	}
	changed := false
	// 已过期的票：先清掉"激活"标记，让后面的正常激活逻辑重新写入。
	// 否则过期票 knpoijanngk 还是 1，下面的 !=1 判定永远不成立，changed=false，
	// 客户端弹框里继续显示锁定图标（认为票已在用），永远不让用户选中。
	now := uint64(proto.WindowsFiletimeNow())
	if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
		if u64Of(fields[idx].Num) > 0 && u64Of(fields[idx].Num) <= now {
			ctx.Log.Info("3/7 票已过期，重置激活标记", "guid", guidShort(row.GUID))
			if ki := fieldIdxByCS(row.WireClass, "knpoijanngk"); ki >= 0 && ki < len(fields) {
				if u64Of(fields[ki].Num) != 0 {
					fields[ki].Num = ptrU64(0)
					changed = true
				}
			}
			if si := fieldIdxByCS(row.WireClass, csStateEnum); si >= 0 && si < len(fields) {
				cur := u64Of(fields[si].Num)
				if cur&boatTicketInUseFlag != 0 {
					fields[si].Num = ptrU64(cur &^ boatTicketInUseFlag)
					changed = true
				}
			}
		}
	}
	// 正常激活：knpoijanngk 设为 1（未过期的票第一次使用 / 过期票重置后重新激活）。
	if idx := fieldIdxByCS(row.WireClass, "knpoijanngk"); idx >= 0 && idx < len(fields) {
		if u64Of(fields[idx].Num) != 1 {
			fields[idx].Num = ptrU64(1)
			changed = true
		}
	}
	// oieepfcgjgn 是"租用开始时的游戏时间"（客户端世界时钟 24 倍速、从登录起算），
	// 不是时长！客户端公式：剩余 = 票时长 + min(oieepfcgjgn, W) - W，
	// 只有当 oieepfcgjgn 是起始游戏时间时，剩余才会随时间递减。
	gameMin := uint64(24 * time.Since(ctx.Sess.startedAt).Minutes())
	if idx := fieldIdxByCS(row.WireClass, "oieepfcgjgn"); idx >= 0 && idx < len(fields) {
		if u64Of(fields[idx].Num) != gameMin {
			fields[idx].Num = ptrU64(gameMin)
			changed = true
		}
	}
	// 状态位置 "使用中"（bhhaopagglk.pmdfjipkbmn = 0x100000）：客户端据此
	// 在提示里显示 kItemState_TicketInUse，而不是走到维修倒计时分支。
	if idx := fieldIdxByCS(row.WireClass, csStateEnum); idx >= 0 && idx < len(fields) {
		cur := u64Of(fields[idx].Num)
		if cur&boatTicketInUseFlag == 0 {
			fields[idx].Num = ptrU64(cur | boatTicketInUseFlag)
			changed = true
		}
	}
	// 时效对象（wire 类 lcjcffibmbi / dto jgjlgegndbk）必须存在，客户端靠它
	// 显示"已激活"；但类型位必须为 0（无时效），否则会走维修倒计时分支
	// （kItemState_Repairing → "在工作坊里 X 天后修理完"）。
	// 判定见 knmjphiefdm.falmopiaoca：item.时效对象 != null && 类型 == 1 才进维修段。
	if idx := fieldIdxByCS(row.WireClass, "bkmnbppciao"); idx >= 0 && idx < len(fields) {
		if fields[idx].Val == nil || fields[idx].Val.Kind != proto.KindObj {
			fields[idx].Val = wiregen.MustMakeObj("lcjcffibmbi", map[int]*proto.Value{
				0: proto.StrVal(""),
				1: proto.U32(0), // 无时效：避开维修倒计时分支
				// 起始必须是"当前分钟数"（unix 分钟），不能填时长：
				// 客户端算剩余 = 起始 + 时长 - 当前，填 1440 会算成常量。
				2: proto.U32(uint32(time.Now().Unix() / 60)),
				3: proto.U32(uint32(minutes)),
			})
			changed = true
		} else {
			// 复用库里的时效对象时，必须把 f0（U32N 字符串）的 nil 规范化为
			// 空字符串：nil 会编码成 0x7FFFFFFF（"null u32str"约定），客户端
			// 构造租用记录 jfnimfgkepk 时按长度读字符串，0x7FFFFFFF 远大于
			// 剩余数据 → "Параметр count не равен размеру сериализованных
			// данных"（Player.log 实测）→ 记录构造失败 → BoatWatchDog 以
			// 16ms 间隔死循环重发 3/7（表现为选票后反复 activate 上不了船）。
			obj := fields[idx].Val
			if len(obj.Fields) > 0 && obj.Fields[0].Str == nil {
				empty := ""
				obj.Fields[0].Str = &empty
				changed = true
			}
		}
	}
	// 票的到期时间直接对齐到"现在 + 租期"：客户端提示里的"X 后过期"
	// 就是用这个字段算的，若只延长不缩短，短租期测试会显示成票的原始时长。
	if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
		// 游戏时间 24 倍速：现实分钟 = ceil(游戏分钟 / 24)
		realMin := (minutes + 23) / 24
		if realMin < 1 {
			realMin = 1
		}
		expiry := uint64(proto.WindowsFiletimeNow()) + realMin*60*10_000_000
		if u64Of(fields[idx].Num) != expiry {
			fields[idx].Num = ptrU64(expiry)
			changed = true
		}
	}
	if !changed {
		return false
	}
	fj, jerr := FieldsToJSON(fields)
	if jerr != nil {
		return false
	}
	if uerr := db.UpdateItemFields(ctx.Sess.pushCtx(), row.GUID, fj, row.Qty, row.Durability,
		row.Workmanship, row.RigGUID); uerr != nil {
		ctx.Log.Warn("3/7 租用状态落库失败", "guid", guidShort(row.GUID), "err", uerr)
		return false
	}
	row.FieldsJSON = fj
	return true
}

// isZeroGUID 判断解码出的 GUID 是否为空（null guid 表示为 32 个 0）。
func isZeroGUID(g string) bool {
	for i := 0; i < len(g); i++ {
		if g[i] != '0' {
			return false
		}
	}
	return true
}

// activeRentalTicket 找一张"已激活且未过期"的船票并物化返回。
//
// 判定：knpoijanngk 为真，且票的到期时间（iingicaldbk，FILETIME）仍在未来。
// startTicketRent 写入租用状态时会把到期时间对齐到租期结束，因此这一个
// 条件就同时覆盖了"已租用"和"未过期"。
func (s *Server) activeRentalTicket(ctx *Ctx, station uint64) *proto.Value {
	cctx := ctx.Sess.pushCtx()
	guids, err := s.DB.ItemGUIDsByContainers(cctx, ctx.Sess.Userid, []string{"backpack"})
	if err != nil {
		return nil
	}
	now := uint64(proto.WindowsFiletimeNow())
	for _, g := range guids {
		row, rerr := s.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.UID != ctx.Sess.Userid || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		if idx := fieldIdxByCS(row.WireClass, "knpoijanngk"); idx < 0 || idx >= len(fields) {
			continue
		} else if u64Of(fields[idx].Num) != 1 {
			continue
		}
		if idx := fieldIdxByCS(row.WireClass, csUpdated); idx >= 0 && idx < len(fields) {
			if u64Of(fields[idx].Num) <= now {
				continue // 已过期
			}
		}
		if def, derr := s.DB.GetItemDef(cctx, row.NumericID); derr == nil && def != nil && station > 0 {
			if st, ok := defAttrU64(def, boatTicketStationAttr); ok && st != station {
				continue
			}
		}
		tree, terr := s.ItemToTree(cctx, row)
		if terr == nil {
			return tree
		}
	}
	return nil
}

// pushTicketRemaining 把已租用船票的"剩余分钟"写进票的数值字段并推送。
//
// 客户端提示里的到期倒计时直接显示该字段（运行时验证：pioghgjeelp 的返回值
// 恒等于它，且不随时间变化），所以只能由服务端周期性下发新的剩余值，
// 提示才会随时间递减。
func (s *Session) pushTicketRemaining() {
	if s.Srv == nil || s.Srv.DB == nil {
		return
	}
	cctx := s.pushCtx()
	guids, err := s.Srv.DB.ItemGUIDsByContainers(cctx, s.Userid, []string{"backpack"})
	if err != nil {
		return
	}
	nowMin := uint64(time.Now().Unix() / 60)
	for _, g := range guids {
		row, rerr := s.Srv.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		ki := fieldIdxByCS(row.WireClass, "knpoijanngk")
		if ki < 0 || ki >= len(fields) || u64Of(fields[ki].Num) != 1 {
			continue
		}
		ri := fieldIdxByCS(row.WireClass, "bkmnbppciao")
		if ri < 0 || ri >= len(fields) || fields[ri].Val == nil || fields[ri].Val.Kind != proto.KindObj {
			continue
		}
		rent := fields[ri].Val
		if len(rent.Fields) < 4 {
			continue
		}
		start := u64Of(rent.Fields[2].Num)
		dur := u64Of(rent.Fields[3].Num)
		// 票时长是"游戏分钟"（24 倍速），换算成现实分钟判断是否到期。
		// 游戏时间 24 倍速：现实分钟 = ceil(游戏分钟 / 24)
		realDur := (dur + 23) / 24
		if realDur < 1 {
			realDur = 1
		}
		var remain uint64
		if start+realDur > nowMin {
			remain = start + realDur - nowMin
		}
		tree, terr := s.Srv.ItemToTree(cctx, row)
		if terr != nil {
			continue
		}
		if vi := fieldIdxByCS(row.WireClass, "oieepfcgjgn"); vi >= 0 && vi < len(tree.Fields) {
			tree.Fields[vi].Num = ptrU64(remain)
		}
		// 先"移除"再"添加"，强制客户端替换物品对象并重建提示。
		s.Push(4, 21, proto.Args524(proto.GuidArr([]string{g})))
		s.Push(4, 22, proto.Args(proto.S("61"), tree))
		s.log.Info("船票剩余分钟推送", "guid", guidShort(g), "remain", remain, "dur", dur)
	}
}

// expireRentalTickets 清理已过期的租用票：直接从背包移除并推送 4/21。
// 票是"用一次就消耗"的道具，租期结束后不应留在背包里。
func (s *Session) expireRentalTickets() {
	if s.Srv == nil || s.Srv.DB == nil {
		return
	}
	cctx := s.pushCtx()
	guids, err := s.Srv.DB.ItemGUIDsByContainers(cctx, s.Userid, []string{"backpack"})
	if err != nil {
		return
	}
	nowMin := uint64(time.Now().Unix() / 60)
	for _, g := range guids {
		row, rerr := s.Srv.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		ki := fieldIdxByCS(row.WireClass, "knpoijanngk")
		if ki < 0 || ki >= len(fields) || u64Of(fields[ki].Num) != 1 {
			continue
		}
		ri := fieldIdxByCS(row.WireClass, "bkmnbppciao")
		if ri < 0 || ri >= len(fields) || fields[ri].Val == nil || fields[ri].Val.Kind != proto.KindObj {
			continue
		}
		rent := fields[ri].Val
		if len(rent.Fields) < 4 {
			continue
		}
		start := u64Of(rent.Fields[2].Num)
		dur := u64Of(rent.Fields[3].Num)
		// 票时长是游戏分钟（24 倍速），换算成现实分钟。
		// 游戏时间 24 倍速：现实分钟 = ceil(游戏分钟 / 24)
		realDur := (dur + 23) / 24
		if realDur < 1 {
			realDur = 1
		}
		if start+realDur > nowMin {
			continue
		}
		// 到期先"取消使用中"：推 4/22 让 HUD 立刻恢复正常，
		// 票本身保留到玩家下船时再删除（避免船上出现"UI 在用、服务端没有"的矛盾）。
		if ki < len(fields) {
			fields[ki].Num = ptrU64(0)
		}
		fj, jerr := FieldsToJSON(fields)
		if jerr != nil {
			continue
		}
		if uerr := s.Srv.DB.UpdateItemFields(cctx, g, fj, row.Qty, row.Durability, row.Workmanship, row.RigGUID); uerr != nil {
			s.log.Error("船票到期状态写回失败", "guid", guidShort(g), "err", uerr)
			continue
		}
		tree, terr := s.Srv.ItemToTree(cctx, row)
		if terr != nil {
			continue
		}
		if vi := fieldIdxByCS(row.WireClass, "oieepfcgjgn"); vi >= 0 && vi < len(tree.Fields) {
			tree.Fields[vi].Num = ptrU64(0)
		}
		s.Push(4, 21, proto.Args524(proto.GuidArr([]string{g})))
		s.Push(4, 22, proto.Args(proto.S("61"), tree))
		s.log.Info("船票到期，已取消使用中", "guid", guidShort(g), "dur", dur)
	}
}

// cleanupConsumedTickets 删除已到期并被取消使用中的船票（已消耗）。
func cleanupConsumedTickets(ctx *Ctx) {
	if ctx == nil || ctx.Sess == nil || ctx.Sess.Srv == nil || ctx.Sess.Srv.DB == nil {
		return
	}
	s := ctx.Sess
	cctx := s.pushCtx()
	guids, err := s.Srv.DB.ItemGUIDsByContainers(cctx, s.Userid, []string{"backpack"})
	if err != nil {
		return
	}
	nowMin := uint64(time.Now().Unix() / 60)
	for _, g := range guids {
		row, rerr := s.Srv.DB.GetItemByGUID(cctx, g)
		if rerr != nil || row == nil || row.TypeID != boatTicketTypeID {
			continue
		}
		fields, ferr := FieldsFromJSON(row.FieldsJSON)
		if ferr != nil {
			continue
		}
		ki := fieldIdxByCS(row.WireClass, "knpoijanngk")
		if ki < 0 || ki >= len(fields) || u64Of(fields[ki].Num) != 0 {
			continue
		}
		ri := fieldIdxByCS(row.WireClass, "bkmnbppciao")
		if ri < 0 || ri >= len(fields) || fields[ri].Val == nil || fields[ri].Val.Kind != proto.KindObj {
			continue
		}
		rent := fields[ri].Val
		if len(rent.Fields) < 4 {
			continue
		}
		start := u64Of(rent.Fields[2].Num)
		dur := u64Of(rent.Fields[3].Num)
		realDur := (dur + 23) / 24
		if realDur < 1 {
			realDur = 1
		}
		if start+realDur > nowMin {
			continue // 还没到期，保留
		}
		if derr := s.Srv.DB.DeleteItems(cctx, []string{g}); derr != nil {
			continue
		}
		s.Push(4, 21, proto.Args524(proto.GuidArr([]string{g})))
		s.log.Info("已消耗船票已删除", "guid", guidShort(g))
	}
}

// fieldNameByIdx 从 wiregen 拿到 wire class 的第 idx 个字段名；找不到返回 "?"。
func fieldNameByIdx(wireClass string, idx int) string {
	ci, ok := wiregen.ClassMeta[wireClass]
	if !ok || idx >= len(ci.Fields) {
		return "?"
	}
	return ci.Fields[idx].Name
}

// fmtField 把 proto.Field 格式化成字符串，用于 debug dump。
func fmtField(f proto.Field) string {
	switch f.Op {
	case "OPT":
		if f.Num == nil {
			return "null"
		}
		return fmt.Sprintf("opt(%d)", *f.Num)
	case "U32":
		return fmt.Sprintf("u32(%d)", *f.Num)
	case "U64":
		return fmt.Sprintf("u64(%d)", *f.Num)
	case "BYTE":
		return fmt.Sprintf("byte(%d)", *f.Num)
	case "U16":
		return fmt.Sprintf("u16(%d)", *f.Num)
	case "U32N":
		if f.Str == nil {
			return "u32n-null"
		}
		return "u32n(" + *f.Str + ")"
	case "STR":
		if f.Str == nil {
			return "str-null"
		}
		return "str(" + *f.Str + ")"
	case "OPTGUID":
		if f.Hex == "" {
			return "null-g"
		}
		return f.Hex
	case "GUID16":
		if f.Hex == "" {
			return "<no-hex>"
		}
		return f.Hex
	case "VAL":
		if f.Val == nil {
			return "null-val"
		}
		if f.Val.Kind == proto.KindObj {
			var parts []string
			for _, sub := range f.Val.Fields {
				parts = append(parts, fmtField(sub))
			}
			return fmt.Sprintf("{type=%d,class=%s,[%s]}", f.Val.TypeID, f.Val.Class, strings.Join(parts, ","))
		}
		if f.Val.Kind == proto.KindNull {
			return "null-obj"
		}
		return fmt.Sprintf("<val kind=%s>", f.Val.Kind)
	default:
		return fmt.Sprintf("<op=%s>", f.Op)
	}
}
