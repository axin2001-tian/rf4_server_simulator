package gameserver

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"rf4server/internal/store"
)

func (sess *Session) updateHappyHour(now time.Time, closing bool) store.HappyHourStatus {
	if sess == nil || sess.Srv == nil || sess.Srv.DB == nil {
		return store.HappyHourStatus{}
	}
	s := sess.Srv
	sess.happyOpMu.Lock()
	defer sess.happyOpMu.Unlock()
	closing = closing || sess.Closed()
	fishing := false
	if !closing {
		for _, st := range s.fishingStatesFor(sess.Userid) {
			st.mu.Lock()
			active := st.CastActive && st.InWater && !st.Resolved
			gear, pending := st.GearGuid, st.Pending != nil
			st.mu.Unlock()
			if !active || gear == "" {
				continue
			}
			rod, err := s.DB.GetItemByGUID(sess.pushCtx(), gear)
			if err != nil || rod == nil || rod.UID != sess.Userid || rod.Durability <= 0 {
				continue
			}
			def, err := s.itemDefForRow(sess.pushCtx(), rod)
			if err == nil && def != nil && def.Category == gearCatRod && (pending || s.canFishWithBait(sess.pushCtx(), sess.Userid, gear)) {
				fishing = true
				break
			}
		}
	}

	owner := fmt.Sprintf("%d:%d", sess.ID, sess.startedAt.UnixNano())
	status, err := s.DB.AdvanceHappyHour(s.dbCtx(), sess.Userid, owner, now, fishing, !closing,
		max(30*time.Second, 3*s.cfg.KeepaliveInterval()))
	if err != nil {
		s.log.Warn("欢乐时光状态未保存", "err", err)
		return store.HappyHourStatus{}
	}
	sess.happyMu.Lock()
	prevActive := sess.happyStatus.Active
	sess.happyStatus = status
	sess.happyMu.Unlock()

	// 抛竿进入欢乐时光的那一拍：把档案推给客户端一次。客户端靠档案里
	// 第 24 块的状态从 1(Ready) 变成 3(Activated) 来弹「欢乐时光开始了」提示
	// （kNotification_HappyHourStart），不推档案客户端就永远看不到这个变化。
	// 只在「不活跃 -> 活跃」的边上推一次，保活不会反复弹。
	if status.Active && !prevActive {
		sess.happyMu.Lock()
		fire := !sess.happyStartNotified
		sess.happyStartNotified = true
		sess.happyMu.Unlock()
		if fire {
			// 异步推送：BuildProfile307W 内部会经 stateFor 重排咬钩 timer
			// （biteTicket++），同步调用会让正在派发的咬钩票失效、鱼口被推迟。
			go sess.Push2_1()
		}
	} else if !status.Active {
		sess.happyMu.Lock()
		sess.happyStartNotified = false
		sess.happyMu.Unlock()
	}
	return status
}

func (sess *Session) currentHappyHour() store.HappyHourStatus {
	if sess == nil {
		return store.HappyHourStatus{}
	}
	sess.happyMu.RLock()
	defer sess.happyMu.RUnlock()
	return sess.happyStatus
}

// happyHourBiteChance 只提高已有吃饵概率，保留低兴趣上限及禁饵。
func happyHourBiteChance(interest, bonus float64) float64 {
	if interest <= 0 {
		return 0
	}
	chance := interest * (1 + clampFloat(bonus, 0, .1))
	if interest <= .1 {
		return math.Min(.1, chance)
	}
	return math.Min(1, chance)
}

func (c fishRollContext) acceptHappyHourBite(interest float64, activities ...float64) bool {
	roll := rand.Float64
	if c.biteRoll != nil {
		roll = c.biteRoll
	}
	chance := happyHourBiteChance(interest, c.happyBite)
	if len(activities) > 0 {
		chance *= clampFloat(activities[0], 0, 1)
	}
	return roll() < chance
}

func happyHourProficiencyRoll(current, roll float64, active bool) float64 {
	if !active {
		return roll
	}
	base := fishingProficiencyGainChance(current)
	chance := math.Min(.95, base*1.25)
	if chance > 0 {
		return roll * base / chance
	}
	return roll
}
