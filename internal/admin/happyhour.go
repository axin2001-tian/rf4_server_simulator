package admin

import (
	"net/http"
	"time"

	"rf4server/internal/store"
)

// handlePlayerHappyHour 发放欢乐时光资格：days>0 限时天数，0 无限制，-1 取消资格。
func (s *Server) handlePlayerHappyHour(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID  int64 `json:"uid"`
		Days int64 `json:"days"`
	}
	if err := readJSON(r, &req); err != nil || req.UID <= 0 || req.Days < -1 || req.Days > 3650 {
		httpError(w, 400, "参数错误")
		return
	}
	now := time.Now()
	kind, expires := store.HappyHourNone, int64(0)
	switch {
	case req.Days > 0:
		kind = store.HappyHourLimited
		expires = now.AddDate(0, 0, int(req.Days)).Unix()
	case req.Days == 0:
		kind = store.HappyHourUnlimited
	}
	if err := s.db.SetHappyHourSubscription(r.Context(), req.UID, kind, expires, adminOf(r), now.Unix()); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	status, _ := s.db.GetHappyHourStatus(r.Context(), req.UID, now)
	writeJSON(w, map[string]any{"ok": true, "happy_hour": status})
}
