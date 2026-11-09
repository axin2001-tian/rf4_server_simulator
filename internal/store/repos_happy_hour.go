package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const HappyHourMillis = int64(60 * 60 * 1000)

// 欢乐时光资格：0=未发放，1=限时，2=无限制。
const (
	HappyHourNone      = int64(0)
	HappyHourLimited   = int64(1)
	HappyHourUnlimited = int64(2)
)

var happyHourZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type HappyHourEntitlement struct {
	Qualified bool  `json:"qualified"`
	Unlimited bool  `json:"unlimited"`
	ExpiresAt int64 `json:"expires_at"`
}

// HappyHourEntitled 按资格类型和到期时间判断当前是否享有欢乐时光。
func HappyHourEntitled(kind, expiresAt, now int64) HappyHourEntitlement {
	if kind == HappyHourUnlimited {
		return HappyHourEntitlement{Qualified: true, Unlimited: true}
	}
	return HappyHourEntitlement{Qualified: kind == HappyHourLimited && expiresAt > now, ExpiresAt: expiresAt}
}

type HappyHourStatus struct {
	Enabled          bool    `json:"enabled"`
	Qualified        bool    `json:"qualified"`
	Unlimited        bool    `json:"unlimited"`
	ExpiresAt        int64   `json:"expires_at"`
	Started          bool    `json:"started"`
	Active           bool    `json:"active"`
	RemainingSeconds int64   `json:"remaining_seconds"`
	XPBonus          float64 `json:"xp_bonus"`
	BiteBonus        float64 `json:"bite_bonus"`
	ProficiencyBonus float64 `json:"proficiency_bonus"`
}

func happyHourDay(now time.Time) string { return now.In(happyHourZone).Format("2006-01-02") }

func happyHourStatus(enabled bool, ent HappyHourEntitlement, started, active bool, used int64) HappyHourStatus {
	remaining := max(int64(0), HappyHourMillis-used)
	r := HappyHourStatus{Enabled: enabled, Qualified: ent.Qualified, Unlimited: ent.Unlimited, ExpiresAt: ent.ExpiresAt,
		Started: started, Active: enabled && ent.Qualified && active && remaining > 0,
		RemainingSeconds: (remaining + 999) / 1000}
	if r.Active {
		r.XPBonus, r.BiteBonus, r.ProficiencyBonus = .5, .1, .25
	}
	return r
}

func happyHourEntitlementTx(ctx context.Context, tx *sql.Tx, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(happy_hour,0),COALESCE(happy_hour_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

func (d *DB) happyHourEntitlement(ctx context.Context, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(happy_hour,0),COALESCE(happy_hour_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

// AdvanceHappyHour 只累计同一在线会话的有效钓鱼时间，离线和进程重启不补扣。
// 未发放资格或资格过期时不计时、不生效。
func (d *DB) AdvanceHappyHour(ctx context.Context, uid int64, owner string, now time.Time, fishing, takeover bool, maxGap time.Duration) (HappyHourStatus, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return HappyHourStatus{}, err
	}
	defer tx.Rollback()
	// 先取得写锁，多个连接不能同时消费同一账户的时长。
	if _, err := tx.ExecContext(ctx, `UPDATE happy_hour_state SET active=active WHERE uid=?`, uid); err != nil {
		return HappyHourStatus{}, err
	}
	enabled := true
	var flag string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM tunables WHERE key='happy_hour_enabled'`).Scan(&flag); err == nil {
		enabled = flag == "1"
	} else if !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	ent, err := happyHourEntitlementTx(ctx, tx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	effective := enabled && ent.Qualified
	var day, previousOwner string
	var used, last int64
	var previousActive bool
	err = tx.QueryRowContext(ctx, `SELECT local_day,used_millis,last_tick,session_key,active FROM happy_hour_state WHERE uid=?`, uid).
		Scan(&day, &used, &last, &previousOwner, &previousActive)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	if exists && previousOwner != owner && (!takeover || previousActive && now.UnixMilli()-last < max(30000, maxGap.Milliseconds())) {
		return happyHourStatus(enabled, ent, true, false, used), nil
	}
	millis, currentDay := now.UnixMilli(), happyHourDay(now)
	if !exists || day != currentDay {
		used = 0
		if exists && previousOwner == owner && previousActive {
			midnight := time.Date(now.In(happyHourZone).Year(), now.In(happyHourZone).Month(), now.In(happyHourZone).Day(), 0, 0, 0, 0, happyHourZone).UnixMilli()
			last = max(last, midnight)
		} else {
			last = millis
		}
	}
	if ent.Qualified && exists && previousOwner == owner && previousActive {
		elapsed := max(int64(0), millis-last)
		if maxGap > 0 {
			elapsed = min(elapsed, maxGap.Milliseconds())
		}
		used = min(HappyHourMillis, used+elapsed)
	}
	active := fishing && effective && used < HappyHourMillis
	if !exists && !active {
		return happyHourStatus(enabled, ent, false, false, 0), nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO happy_hour_state(uid,local_day,used_millis,last_tick,session_key,active)
		VALUES(?,?,?,?,?,?) ON CONFLICT(uid) DO UPDATE SET local_day=excluded.local_day,
		used_millis=excluded.used_millis,last_tick=excluded.last_tick,session_key=excluded.session_key,active=excluded.active`,
		uid, currentDay, used, millis, owner, active); err != nil {
		return HappyHourStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active, used), nil
}

func (d *DB) GetHappyHourStatus(ctx context.Context, uid int64, now time.Time) (HappyHourStatus, error) {
	enabled := true
	if flag, found, err := d.TunableGet(ctx, "happy_hour_enabled"); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	} else if found {
		enabled = flag == "1"
	}
	ent, err := d.happyHourEntitlement(ctx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	var day string
	var used int64
	var last int64
	var active bool
	err = d.SQL.QueryRowContext(ctx, `SELECT local_day,used_millis,active,last_tick FROM happy_hour_state WHERE uid=?`, uid).Scan(&day, &used, &active, &last)
	if errors.Is(err, sql.ErrNoRows) || err == nil && day != happyHourDay(now) {
		return happyHourStatus(enabled, ent, false, false, 0), nil
	}
	if err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active && now.UnixMilli()-last <= 30000, used), nil
}

// SetHappyHourSubscription 保存管理员发放的欢乐时光资格，并重置当天计时。
func (d *DB) SetHappyHourSubscription(ctx context.Context, uid, kind, expiresAt int64, admin string, now int64) error {
	if uid <= 0 || kind < HappyHourNone || kind > HappyHourUnlimited ||
		(kind == HappyHourLimited && (expiresAt <= now || expiresAt > 253402300799)) ||
		(kind != HappyHourLimited && expiresAt != 0) {
		return fmt.Errorf("请选择有效的欢乐时光资格和到期时间")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 客户端把欢乐时光当作高级会员权益（主页显示「需要高级会员」），
	// 所以发放资格时同步把会员状态置成同一期限，否则游戏内永远不解锁。
	// 已有永久会员不降级；取消资格只撤欢乐时光，会员由会员入口单独管理。
	var result sql.Result
	if kind == HappyHourNone {
		result, err = tx.ExecContext(ctx,
			"UPDATE profile SET happy_hour=?,happy_hour_exp=?,updated_ts=? WHERE uid=?", kind, expiresAt, now, uid)
	} else {
		mKind, mExp := int64(1), expiresAt
		if kind == HappyHourUnlimited {
			mKind, mExp = 2, 0
		}
		result, err = tx.ExecContext(ctx, `UPDATE profile SET happy_hour=?,happy_hour_exp=?,
			membership=CASE WHEN membership=2 THEN 2 ELSE ? END,
			membership_exp=CASE WHEN membership=2 THEN membership_exp ELSE ? END,
			updated_ts=? WHERE uid=?`, kind, expiresAt, mKind, mExp, now, uid)
	}
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("本地角色不存在")
	}
	if kind != HappyHourNone {
		// 发放新资格时清空当天已用时长，避免旧余额让玩家以为没生效。
		if _, err := tx.ExecContext(ctx, "DELETE FROM happy_hour_state WHERE uid=?", uid); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_queue(payload,created_ts) VALUES(?,?)", fmt.Sprintf(`{"op":"refresh_player","uid":%d}`, uid), now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO audit_log(admin,action,target,detail,ts) VALUES(?,'happy_hour_update',?,?,?)", admin, fmt.Sprint(uid), fmt.Sprintf("type=%d expiry=%d", kind, expiresAt), now); err != nil {
		return err
	}
	return tx.Commit()
}
