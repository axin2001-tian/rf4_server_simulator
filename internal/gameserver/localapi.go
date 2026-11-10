// localapi.go —— 面板本地接口（只监听 127.0.0.1）。
//
// 游戏客户端只会渲染"该店本地商品目录"里的条目，服务端新增的酒类不会出现在商店界面里，
// 所以由传送面板直接调用这里的接口下单：服务端走与商店购买完全相同的扣款+入库事务，
// 再推送档案，客户端背包即可看到新买的酒。
package gameserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"rf4server/internal/store"
	"strconv"
	"time"
)

// drinkShelf 是面板酒类页返回的一条货架记录。
type drinkShelf struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Silver int64   `json:"silver"`
	Gold   int64   `json:"gold"`
	Stock  int64   `json:"stock"`
	Rate   float64 `json:"rate"` // 经验加成峰值（0.12 = +12%）
}

// StartLocalAPI 启动面板用的本地 HTTP 接口，返回停止函数。
func (s *Server) StartLocalAPI(addr string) (func(), error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/shop/drinks", s.apiShopDrinks)
	mux.HandleFunc("/api/shop/buy", s.apiShopBuy)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(ln) }()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// apiShopDrinks 返回该店货架上的全部酒类（按 drink_rules 里的酒类白名单过滤）。
func (s *Server) apiShopDrinks(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store")
	if storeID == "" {
		storeID = "level_004_foodshop"
	}
	if s.DB == nil {
		writeJSON(w, map[string]any{"ok": false, "err": "数据库未就绪"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.DB.ShopItems(ctx, storeID)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "err": err.Error()})
		return
	}
	out := make([]drinkShelf, 0, len(rows))
	for _, p := range rows {
		rule, ok := drinkRuleForItem(p.ItemID)
		if !ok {
			continue
		}
		out = append(out, drinkShelf{
			ID: p.ItemID, Name: s.itemDisplayName(ctx, p.ItemID),
			Silver: p.Silver, Gold: p.Gold, Stock: p.Stock, Rate: rule.Peak,
		})
	}
	writeJSON(w, map[string]any{"ok": true, "store": storeID, "items": out})
}

// itemDisplayName 优先用中文名，其次英文名，最后回落到 itemID。
func (s *Server) itemDisplayName(ctx context.Context, itemID string) string {
	if s.DB == nil || s.DB.SQL == nil {
		return itemID
	}
	var zh, en string
	err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(name_zh,''), COALESCE(name,'') FROM item_catalog WHERE item_id=?`, itemID).Scan(&zh, &en)
	if err != nil {
		return itemID
	}
	if zh != "" {
		return zh
	}
	if en != "" {
		return en
	}
	return itemID
}

// apiShopBuy 面板下单：与 22/2、22/3 商店购买走同一套事务，成功后推送档案。
func (s *Server) apiShopBuy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	storeID := q.Get("store")
	if storeID == "" {
		storeID = "level_004_foodshop"
	}
	itemID := q.Get("item")
	uid, _ := strconv.ParseInt(q.Get("uid"), 10, 64)
	if uid <= 0 {
		uid = 1000001
	}
	goldCol := q.Get("gold") == "1" || q.Get("gold") == "true"
	if s.DB == nil || itemID == "" {
		writeJSON(w, map[string]any{"ok": false, "err": "参数无效"})
		return
	}
	if _, ok := drinkRuleForItem(itemID); !ok {
		writeJSON(w, map[string]any{"ok": false, "err": "该商品不是酒类"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	price, err := s.buyDrink(ctx, uid, storeID, itemID, goldCol)
	if err != nil {
		msg := "购买未完成，请稍后重试"
		if errors.Is(err, errInsufficientFunds) {
			msg = "余额不足，未购买商品"
		}
		s.log.Warn("面板买酒失败", "uid", uid, "store", storeID, "item", itemID, "err", err)
		writeJSON(w, map[string]any{"ok": false, "err": msg})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "price": price, "item": itemID})
}

// buyDrink 扣款 + 入库 + 推送档案；事务与商店购买保持一致。
func (s *Server) buyDrink(ctx context.Context, uid int64, storeID, itemID string, goldCol bool) (int64, error) {
	price, err := s.storePrice(ctx, storeID, itemID, 0, goldCol)
	if err != nil {
		return 0, err
	}
	prepared, _, err := s.prepareStoreGoods(ctx, uid, itemID)
	if err != nil {
		return 0, err
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	silverDelta, goldDelta := -int64(price), int64(0)
	currency := "silver"
	if goldCol {
		currency = "gold"
		silverDelta, goldDelta = 0, -int64(price)
	}
	note := fmt.Sprintf("store=%s currency=%s via=panel", storeID, currency)
	if err := applyWalletTx(ctx, tx, uid, silverDelta, goldDelta, "buy", itemID, "", note); err != nil {
		return 0, err
	}
	granted := make([]*store.Item, 0, len(prepared))
	for _, it := range prepared {
		row, _, gerr := grantPreparedItemTx(ctx, tx, it, true)
		if gerr != nil {
			return 0, gerr
		}
		if row != nil {
			granted = append(granted, row)
		}
	}
	if len(granted) > 0 {
		// 与 22/2 一样留一条购买凭据，客户端商店里的「退货」才有据可依
		if err := s.saveStorePurchaseTx(ctx, tx, uid, storeID, itemID, price, goldCol, granted); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if sess := s.liveSessionByUID(uid); sess != nil {
		// 面板下单走的是本地 HTTP，没有 22/2 那条购买回执，客户端不会自己往背包里加东西：
		// 只推 2/1 只会刷新银币/等级，背包还是空的（扣了钱看不到货）。
		// 这里按「背包物品变更」通道把新物品再推一遍，客户端才会真的入包。
		guids := make([]string, 0, len(granted))
		for _, row := range granted {
			if row != nil && row.GUID != "" {
				guids = append(guids, row.GUID)
			}
		}
		if len(guids) > 0 {
			sess.Push4_22ItemGUIDs(guids)
		}
		sess.Push2_1()
	}
	return int64(price), nil
}
