package gameserver

import (
	_ "embed"
	"encoding/json"
	"sync"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 21/5 与 22/5 返回不同的 args 容器；元素类型必须与协议契约一致。

//go:embed assets/shop_boats.json
var shopBoatsJSON []byte

type shopBoatRow struct {
	Name    string `json:"name"`
	F1      uint32 `json:"f1"`
	Tickets uint64 `json:"tickets"`
	F3      uint32 `json:"f3"`
	Price   uint64 `json:"price"`
}

var (
	shopBoatsOnce sync.Once
	shopBoats     []shopBoatRow
)

func loadShopBoats() []shopBoatRow {
	shopBoatsOnce.Do(func() {
		var payload struct {
			Rows []shopBoatRow `json:"rows"`
		}
		if err := json.Unmarshal(shopBoatsJSON, &payload); err == nil {
			shopBoats = payload.Rows
		}
	})
	return shopBoats
}

func registerMissingEconHandlers(d *Dispatch) {
	d.Register(21, 5, hWorkshopRedeem)
	d.Register(22, 5, hStoreBoats)
	d.RegisterResponseContract(21, 5, ResponseContractRule{
		Kind: proto.KindArgs, ArgCode: "145",
	})
	d.RegisterResponseContract(22, 5, ResponseContractRule{
		Kind: proto.KindArgs, ArgCode: "149", ArgItemTypeIDs: []uint32{560},
	})
}

// hWorkshopRedeem 处理 21/5 工坊兑换；当前无可兑换内容，返回空 args(145)。
func hWorkshopRedeem(ctx *Ctx) ([]byte, bool) {
	body, err := proto.EncodeValue(proto.Args(proto.S("145")), ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// hStoreBoats 处理 22/5 船只商店；返回 args(149) 中的船只条目，f1/f3 保持数据表位型。
// 注意：已拥有的房屋也要返回——实测（2026-11-05）该列表是客户端渲染房屋
// 卡片（购买/检查/进入）的唯一数据源，过滤已拥有条目会让客户端在点击
// 已购房屋时因找不到数据而停留在“正在下载”。
func hStoreBoats(ctx *Ctx) ([]byte, bool) {
	out := proto.Args(proto.S("149"))
	for _, row := range loadShopBoats() {
		out.Items = append(out.Items, wiregen.MustMakeObj("efdgfpfbjai", map[int]*proto.Value{
			0: proto.StrVal(row.Name),
			1: proto.U32(row.F1),
			2: proto.U64(row.Tickets),
			3: proto.U32(row.F3),
			4: proto.U64(row.Price),
		}))
	}
	body, err := proto.EncodeValue(out, ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

func init() { RegisterExtra(registerMissingEconHandlers) }
