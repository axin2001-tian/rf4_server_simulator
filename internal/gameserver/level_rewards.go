package gameserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// levelReward 升到指定等级的奖励。
type levelReward struct {
	Level  int64               `json:"level"`
	Silver int64               `json:"silver"`
	Items  []premiumBundleItem `json:"items"`
}

// levelRewardsKVKey 升级奖励配置的 kv 键；改这个 JSON 即可调整，不必重启进程。
const levelRewardsKVKey = "level_rewards"

// defaultLevelRewards 内置默认升级奖励阶梯（可按服主喜好改 kv 覆盖）。
// 每级都给一点饵料/银币，5/10/15/20/25/30 级给装备。
var defaultLevelRewards = []levelReward{
	{Level: 2, Silver: 300, Items: []premiumBundleItem{{ItemID: "worm_01", Qty: 20}, {ItemID: "bread", Qty: 10}}},
	{Level: 3, Silver: 500, Items: []premiumBundleItem{{ItemID: "worm_01", Qty: 30}, {ItemID: "bloodworm", Qty: 20}}},
	{Level: 4, Silver: 700, Items: []premiumBundleItem{{ItemID: "bread", Qty: 20}, {ItemID: "spinnerbaits_1244", Qty: 1}}},
	{Level: 5, Silver: 1000, Items: []premiumBundleItem{{ItemID: "spin_5820", Qty: 1}, {ItemID: "worm_01", Qty: 40}}},
	{Level: 6, Silver: 1200, Items: []premiumBundleItem{{ItemID: "bloodworm", Qty: 40}}},
	{Level: 7, Silver: 1400, Items: []premiumBundleItem{{ItemID: "bread", Qty: 30}}},
	{Level: 8, Silver: 1600, Items: []premiumBundleItem{{ItemID: "orig_20008_16", Qty: 10}}},
	{Level: 9, Silver: 1800, Items: []premiumBundleItem{{ItemID: "spinnerbaits_1103", Qty: 2}}},
	{Level: 10, Silver: 3000, Items: []premiumBundleItem{{ItemID: "tele_10012_tl16", Qty: 1}, {ItemID: "worm_01", Qty: 50}}},
	{Level: 12, Silver: 3500, Items: []premiumBundleItem{{ItemID: "bread", Qty: 40}}},
	{Level: 15, Silver: 5000, Items: []premiumBundleItem{{ItemID: "feeder_10091_FM130XH", Qty: 1}}},
	{Level: 18, Silver: 6000, Items: []premiumBundleItem{{ItemID: "orig_20008_16", Qty: 20}}},
	{Level: 20, Silver: 8000, Items: []premiumBundleItem{{ItemID: "spin_10001_S60L", Qty: 1}}},
	{Level: 25, Silver: 12000, Items: []premiumBundleItem{{ItemID: "spin_5140", Qty: 1}}},
	{Level: 30, Silver: 20000, Items: []premiumBundleItem{{ItemID: "pilkr_10208_fu88_sbtn", Qty: 1}}},
}

// levelRewards 读当前生效的升级奖励表：优先 kv.level_rewards，缺失或解析失败用内置默认。
func (s *Server) levelRewards(ctx context.Context) []levelReward {
	if s.DB != nil {
		var raw string
		if err := s.DB.SQL.QueryRowContext(ctx,
			`SELECT v FROM kv WHERE k=?`, levelRewardsKVKey).Scan(&raw); err == nil && raw != "" {
			var defs []levelReward
			if json.Unmarshal([]byte(raw), &defs) == nil && len(defs) > 0 {
				return defs
			}
		}
	}
	return defaultLevelRewards
}

// grantLevelUps 在升级后发放对应等级的奖励：银币 + 物品（逐件分配槽位并推送）。
// 一次跨多级时按等级逐个结算。
func (s *Server) grantLevelUps(ctx context.Context, c *Ctx, uid, fromLevel, toLevel int64) (int, int64) {
	if toLevel <= fromLevel {
		return 0, 0
	}
	rewards := s.levelRewards(ctx)
	byLevel := make(map[int64]levelReward, len(rewards))
	for _, r := range rewards {
		byLevel[r.Level] = r
	}
	var slot int64
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(slot),-1)+1 FROM item WHERE uid=? AND container='backpack'`,
		uid).Scan(&slot); err != nil {
		c.Log.Warn("升级奖励取空槽失败", "err", err)
		return 0, 0
	}
	itemsGranted := 0
	var silverTotal int64
	for lvl := fromLevel + 1; lvl <= toLevel; lvl++ {
		r, ok := byLevel[lvl]
		if !ok {
			continue
		}
		if r.Silver > 0 {
			if _, err := s.DB.SQL.ExecContext(ctx,
				`UPDATE profile SET silver=silver+?, updated_ts=? WHERE uid=?`,
				r.Silver, float64(time.Now().Unix()), uid); err != nil {
				c.Log.Warn("升级奖励银币发放失败", "level", lvl, "err", err)
			} else {
				silverTotal += r.Silver
			}
		}
		for _, it := range r.Items {
			qty := it.Qty
			if qty <= 0 {
				qty = 1
			}
			row, gerr := s.CreateItem(ctx, uid, it.ItemID, "backpack", qty)
			if gerr != nil || row == nil {
				c.Log.Warn("升级奖励物品发放失败", "level", lvl, "item", it.ItemID, "err", gerr)
				continue
			}
			if _, serr := s.DB.SQL.ExecContext(ctx,
				`UPDATE item SET slot=? WHERE guid=?`, slot, row.GUID); serr != nil {
				c.Log.Warn("升级奖励槽位设置失败", "level", lvl, "item", it.ItemID, "err", serr)
			}
			slot++
			itemsGranted++
			if c.Sess != nil {
				s.pushItemUpdate(c.Sess.Userid, row)
			}
		}
		c.Log.Info("升级奖励已发放", "level", lvl, "silver", r.Silver, "items", len(r.Items))
	}
	if itemsGranted > 0 || silverTotal > 0 {
		c.Log.Info("升级奖励汇总", "from", fromLevel, "to", toLevel,
			"items", itemsGranted, "silver", silverTotal)
	}
	return itemsGranted, silverTotal
}

// makeChatEvent 构造一条 401 聊天事件对象（f0=事件类型，f1=文本，f2=玩家 uid，f3=时间）。
func makeChatEvent(kind uint32, text string, uid uint64) *proto.Value {
	return wiregen.MustMakeObj("lfbdbaffeai", map[int]*proto.Value{
		0: proto.U32(kind),
		1: proto.StrVal(text),
		2: proto.U32(uint32(uid)),
		3: proto.U64(uint64(proto.WindowsFiletimeNow())),
		4: proto.Null(),
	})
}

// pushLevelUp 给客户端推一条升级播报（聊天事件类型 11 = showLevelUp）。
func (s *Server) pushLevelUp(sess *Session, level int64, name string) {
	if sess == nil {
		return
	}
	if name == "" {
		name = "player"
	}
	msgObj := makeChatEvent(11, fmt.Sprintf("%s 升到了 %d 级", name, level), uint64(sess.Userid))
	ch := sessChannel(sess)
	sess.Push(24, 27, proto.Args524(msgObj, proto.StrVal(ch.Name), proto.U32(1)))
}
