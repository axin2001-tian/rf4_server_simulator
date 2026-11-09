package store

// 新年福袋（礼物袋，物品类别 73）：客户端资源里只有 15 张礼包图标与物品定义，
// 没有商店货架也没有进货表；官方活动里袋子由任务/钓获随机发放，开袋内容由
// 官方服务端决定。本文件只提供“有哪些福袋、玩家手上有多少、今天掉了多少”
// 三类只读查询，掉落与开关逻辑放在 gameserver。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// 福袋活动运行参数键。
const (
	GiftBagDropEnabledKey = "gift_bag_drop_enabled" // 1=开启福袋咬钩
	GiftBagDropChanceKey  = "gift_bag_drop_chance"  // 每次咬钩福袋概率 0~1
	GiftBagDropYearsKey   = "gift_bag_drop_years"   // 年份位掩码：1=2021 2=2022 4=2023 8=2024 16=2024EX
	GiftBagDropDailyKey   = "gift_bag_drop_daily"   // 每个玩家每天最多掉几个，0=不限
	GiftBagBiteSpeciesKey = "gift_bag_bite_species" // 福袋咬钩的载体鱼种（空=沿用本杆判出的鱼种）
)

// 年份位掩码。
const (
	GiftBagYear2021   = 1
	GiftBagYear2022   = 2
	GiftBagYear2023   = 4
	GiftBagYear2024   = 8
	GiftBagYear2024EX = 16
	GiftBagAllYears   = GiftBagYear2021 | GiftBagYear2022 | GiftBagYear2023 | GiftBagYear2024 | GiftBagYear2024EX
)

// GiftBagEntry 描述一个福袋物品定义。
type GiftBagEntry struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Year     string `json:"year"`     // 2021 / 2022 / 2023 / 2024 / 2024EX
	Mask     int64  `json:"mask"`     // 年份位
	No       int64  `json:"no"`       // 该年份内的袋号
	Progress int64  `json:"progress"` // item_def 里的进度值（10 递增到 100）
}

// GiftBagOwned 玩家持有的福袋（按物品聚合）。
type GiftBagOwned struct {
	ItemID string `json:"item_id"`
	Year   string `json:"year"`
	No     int64  `json:"no"`
	Count  int64  `json:"count"`
}

// giftBagYear 从 item_id 解析年份与袋号：
// gift_bag_1..10 → 2021；gift_bag_ny22_* → 2022；ny23 → 2023；ny24 → 2024；
// ny24_ex_* → 2024EX（袋号 9~15）。
func giftBagYear(itemID string) (string, int64, int64) {
	rest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(itemID)), "gift_bag_")
	pick := func(prefix, year string, mask int64) (string, int64, int64) {
		no, _ := strconv.ParseInt(strings.TrimPrefix(rest, prefix), 10, 64)
		return year, mask, no
	}
	switch {
	case strings.HasPrefix(rest, "ny24_ex_"):
		// EX 系列接着 2024 的 8 号往后排，界面统一显示为“第 9~15 号”。
		year, mask, no := pick("ny24_ex_", "2024EX", GiftBagYear2024EX)
		return year, mask, no + 8
	case strings.HasPrefix(rest, "ny24_"):
		return pick("ny24_", "2024", GiftBagYear2024)
	case strings.HasPrefix(rest, "ny23_"):
		return pick("ny23_", "2023", GiftBagYear2023)
	case strings.HasPrefix(rest, "ny22_"):
		return pick("ny22_", "2022", GiftBagYear2022)
	default:
		no, _ := strconv.ParseInt(rest, 10, 64)
		return "2021", GiftBagYear2021, no
	}
}

// GiftBagCatalog 返回全部福袋定义，按年份与袋号排序。
func (d *DB) GiftBagCatalog(ctx context.Context) ([]GiftBagEntry, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT d.item_id,
		       COALESCE(NULLIF(TRIM(COALESCE(c.name_zh,'')),''), COALESCE(c.name,'')) AS name,
		       COALESCE(json_extract(d.attrs_json,'$.obifdihmcfn'),0)
		FROM item_def d
		LEFT JOIN item_catalog c ON c.item_id = d.item_id
		WHERE COALESCE(d.category,0) = 73 AND d.item_id LIKE 'gift_bag%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GiftBagEntry
	for rows.Next() {
		var e GiftBagEntry
		if err := rows.Scan(&e.ItemID, &e.Name, &e.Progress); err != nil {
			return nil, err
		}
		e.Year, e.Mask, e.No = giftBagYear(e.ItemID)
		if e.Name == "" {
			e.Name = "礼物袋"
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortGiftBags(out)
	return out, nil
}

func sortGiftBags(list []GiftBagEntry) {
	order := map[string]int{"2021": 1, "2022": 2, "2023": 3, "2024": 4, "2024EX": 5}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := list[i], list[j]
			if order[a.Year] > order[b.Year] || (order[a.Year] == order[b.Year] && a.No > b.No) {
				list[i], list[j] = b, a
			}
		}
	}
}

// PlayerGiftBags 统计玩家手上的福袋数量（按物品聚合）。
func (d *DB) PlayerGiftBags(ctx context.Context, uid int64) ([]GiftBagOwned, error) {
	rows, err := d.SQL.QueryContext(ctx, `
		SELECT item_id, COUNT(*) FROM item
		WHERE uid = ? AND item_id LIKE 'gift_bag%'
		GROUP BY item_id ORDER BY item_id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GiftBagOwned
	for rows.Next() {
		var o GiftBagOwned
		if err := rows.Scan(&o.ItemID, &o.Count); err != nil {
			return nil, err
		}
		o.Year, _, o.No = giftBagYear(o.ItemID)
		out = append(out, o)
	}
	return out, rows.Err()
}

// GiftBagPoolKey 开袋奖池存放在共享 kv 表，值为 JSON 数组。
const GiftBagPoolKey = "gift_bag_reward_pool"

// GiftBagReward 福袋开袋奖池条目。
// Mask 为年份位，0 表示所有年份通用；Qty 是发放单位数（可堆叠物品按定义单位换算）。
type GiftBagReward struct {
	Mask   int64   `json:"mask"`
	ItemID string  `json:"item_id"`
	Qty    int64   `json:"qty"`
	Weight float64 `json:"weight"`
}

// DefaultGiftBagRewards 未配置奖池时的默认内容：随机一份常见天然饵。
func DefaultGiftBagRewards() []GiftBagReward {
	return []GiftBagReward{
		{Mask: 0, ItemID: "bloodworm", Qty: 1, Weight: 20},
		{Mask: 0, ItemID: "muckworm", Qty: 1, Weight: 20},
		{Mask: 0, ItemID: "black_leech", Qty: 1, Weight: 20},
		{Mask: 0, ItemID: "baitfrog", Qty: 1, Weight: 10},
		{Mask: 0, ItemID: "worm_01", Qty: 1, Weight: 20},
		{Mask: 0, ItemID: "crushed_boils", Qty: 1, Weight: 10},
	}
}

// GiftBagRewards 读取奖池配置；未配置或为空时返回默认奖池。
func (d *DB) GiftBagRewards(ctx context.Context) ([]GiftBagReward, error) {
	raw, ok, err := d.KVGet(ctx, GiftBagPoolKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultGiftBagRewards(), nil
		}
		return nil, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return DefaultGiftBagRewards(), nil
	}
	var list []GiftBagReward
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return DefaultGiftBagRewards(), nil
	}
	return list, nil
}

// SetGiftBagRewards 覆盖写入奖池配置。
func (d *DB) SetGiftBagRewards(ctx context.Context, list []GiftBagReward) error {
	buf, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return d.KVSet(ctx, GiftBagPoolKey, string(buf))
}

// GiftBagYearOf 导出年份解析，供 gameserver 开袋时按年份匹配奖池。
func GiftBagYearOf(itemID string) (string, int64, int64) {
	return giftBagYear(itemID)
}

// CountGiftBagsSince 统计玩家在 since（Unix 秒）之后获得的福袋数量，用于每日上限。
func (d *DB) CountGiftBagsSince(ctx context.Context, uid int64, since float64) (int64, error) {
	var n int64
	err := d.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM item WHERE uid = ? AND item_id LIKE 'gift_bag%' AND created_ts >= ?`,
		uid, since).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
