package gameserver

// 新年福袋：作为独立钓获目标，咬钩瞬间按后台参数真实抽签，命中的那尾鱼
// 携带福袋（RolledFish.GiftBagID）。结算时只按携带标记确定性发放：
// 以鱼 GUID 派生袋子实例 GUID，重放同一尾鱼不会重复发放；
// 801 奖励列表与背包推送都按这个 GUID 取实例。

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"rf4server/internal/store"
)

// giftBagStableGUID 用鱼 GUID 派生福袋实例的稳定 GUID：同一尾鱼重放结算
// 拿到同一个袋子标识。
func giftBagStableGUID(uid int64, fishGUID string) string {
	seed := fmt.Sprintf("rf4-gift-bag:%d:%s", uid, strings.ToLower(strings.TrimSpace(fishGUID)))
	hash := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%x", hash[16:32])
}

// startOfLocalDay 返回本地时区今天 0 点的 Unix 秒，用于每日掉落上限。
func startOfLocalDay(now time.Time) float64 {
	y, m, d := now.Date()
	return float64(time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix())
}

// rollGiftBagBite 在咬钩瞬间做一次真实抽签，返回这尾鱼携带的福袋 itemID；
// 返回空串表示不带福袋。开关、概率、每日上限、年份白名单都在这里判定，
// 结算不再重新抽签。
func (s *Server) rollGiftBagBite(ctx context.Context, uid int64) string {
	if s.tunableF(store.GiftBagDropEnabledKey, 1) <= 0 {
		return ""
	}
	chance := cardProbability(s.tunableF(store.GiftBagDropChanceKey, .02), .02)
	if chance <= 0 || rand.Float64() >= chance {
		return ""
	}
	if daily := int64(s.tunableF(store.GiftBagDropDailyKey, 10)); daily > 0 {
		n, err := s.DB.CountGiftBagsSince(ctx, uid, startOfLocalDay(time.Now()))
		if err != nil {
			s.log.Warn("福袋每日计数读取失败，本杆不发袋", "uid", uid, "err", err)
			return ""
		}
		if n >= daily {
			return ""
		}
	}
	mask := int64(s.tunableF(store.GiftBagDropYearsKey, float64(store.GiftBagAllYears)))
	catalog, err := s.DB.GiftBagCatalog(ctx)
	if err != nil {
		s.log.Warn("福袋目录读取失败，本杆不发袋", "uid", uid, "err", err)
		return ""
	}
	candidates := make([]store.GiftBagEntry, 0, len(catalog))
	for _, bag := range catalog {
		if mask&bag.Mask != 0 {
			candidates = append(candidates, bag)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[rand.Intn(len(candidates))].ItemID
}

// prepareCatchGiftBag 按咬钩时决定的 GiftBagID 确定性构造福袋物品（未落库）。
// 未携带福袋的鱼返回 nil；发放与否不再重新抽签，保证"咬到必得"。
func (s *Server) prepareCatchGiftBag(ctx context.Context, uid int64, f *RolledFish) (*store.Item, error) {
	if f == nil || f.GiftBagID == "" {
		return nil, nil
	}
	return s.prepareNewItem(ctx, uid, f.GiftBagID, "backpack", 1, giftBagStableGUID(uid, f.FishGUID))
}
