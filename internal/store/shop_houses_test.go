package store

import (
	"context"
	"testing"
)

// 房产商店（housestore_*）走内嵌实抓货架：客户端打开买房界面拉 22/1 时，
// 货架必须包含对应地点的房子商品，否则客户端无法发起购买。
// 价格与 22/5 面板（gameserver shop_boats.json）一致：廓里湖 28000 / 挪威海 44000。
func TestHouseStoreShelfSellsLocationHouse(t *testing.T) {
	db := openShopCatalogTestDB(t)

	res, err := db.effectiveShopShelf(context.Background(), "housestore_012")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != shopShelfSourceHouse {
		t.Fatalf("housestore_012 来源=%s want=%s", res.Source, shopShelfSourceHouse)
	}
	if len(res.Effective) != 1 {
		t.Fatalf("housestore_012 货架=%d 行 want=1", len(res.Effective))
	}
	row := res.Effective[0]
	if row.ItemID != "level_012_north_sea_house" || row.Silver != 4400000 || row.Gold != 0 {
		t.Fatalf("housestore_012 货架行 %+v", row)
	}

	price, err := db.ShopItemGet(context.Background(), "housestore_003", "level_003_viiveri_lake_house")
	if err != nil {
		t.Fatalf("廓里湖房子不在货架: %v", err)
	}
	if price.Silver != 2800000 || price.Gold != 0 {
		t.Fatalf("廓里湖房子价格 %+v want silver=2800000 gold=0", price)
	}
}

// 房产商店不回退到 kind 7 模板，也不参与补齐：货架不应混入农场商品
// 或按类目估价的其他房子。
func TestHouseStoreShelfKeepsOnlyHouse(t *testing.T) {
	db := openShopCatalogTestDB(t)

	rows, err := db.ShopItems(context.Background(), "housestore_003")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ItemID != "level_003_viiveri_lake_house" {
			t.Fatalf("housestore_003 混入非房子商品 %s", row.ItemID)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("housestore_003 货架=%d 行 want=1", len(rows))
	}
}
