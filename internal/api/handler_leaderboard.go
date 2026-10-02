package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"dzforum/internal/store"
)

// publicPointsLeaderboard 是公开的「积分余额榜」：只读 Worker 生成的快照，
// 不在请求内同步重算全站排名；读取时再次按当前公开状态过滤。
func (s *Server) publicPointsLeaderboard(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "balance"
	}
	if period != "balance" && period != "day" && period != "week" && period != "month" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "未知榜单周期")
		return
	}
	now := time.Now()
	at := now
	if date := r.URL.Query().Get("date"); date != "" {
		zone, _ := time.LoadLocation("Asia/Shanghai")
		var err error
		at, err = time.ParseInLocation("2006-01-02", date, zone)
		if err != nil || at.After(now) || period == "balance" {
			s.fail(w, r, 422, "VALIDATION_FAILED", "需要有效的非未来周期日期；余额榜不支持历史日期")
			return
		}
	}
	meta := map[string]any{
		"period": period, "timeZone": "Asia/Shanghai", "limit": 100,
		"staleAfterSeconds":      int64(analyticsStaleAfter / time.Second),
		"refreshIntervalSeconds": int64(analyticsRefreshInterval / time.Second),
	}
	var snap store.AnalyticsSnapshot
	var err error
	if period == "balance" {
		snap, err = s.st.LatestAnalyticsSnapshot(r.Context(), "points")
	} else {
		start, end := store.PointsPeriodBounds(period, at)
		meta["periodStart"], meta["periodEnd"] = start, end
		snap, err = s.st.PeriodLeaderboardSnapshot(r.Context(), period, start)
	}
	if errors.Is(err, store.ErrNotFound) {
		// 快照尚未生成：明确给出状态，不用空榜冒充真实结果。
		meta["status"] = "unavailable"
		meta["generatedAt"] = nil
		meta["stale"] = false
		meta["ageSeconds"] = int64(0)
		meta["entries"] = []map[string]any{}
		s.respond(w, 200, meta)
		return
	}
	if err != nil {
		s.fail(w, r, 503, "LEADERBOARD_UNAVAILABLE", "排行榜快照暂不可用，请稍后重试")
		return
	}
	var raw []store.LeaderboardEntry
	if period == "balance" {
		err = json.Unmarshal(snap.Payload, &raw)
	} else {
		var board store.PeriodLeaderboard
		err = json.Unmarshal(snap.Payload, &board)
		if err == nil && (board.Period != period || !board.PeriodStart.Equal(snap.PeriodStart) || board.AsOf.IsZero() || board.Entries == nil || !board.PeriodEnd.After(board.PeriodStart)) {
			err = errors.New("invalid period snapshot")
		}
		raw = board.Entries
		meta["asOf"] = board.AsOf
		meta["complete"] = !board.AsOf.Before(board.PeriodEnd)
	}
	if err != nil {
		s.fail(w, r, 503, "LEADERBOARD_UNAVAILABLE", "排行榜快照损坏，请稍后重试")
		return
	}
	ids := make([]int64, 0, len(raw))
	for _, e := range raw {
		ids = append(ids, e.UserID)
	}
	allowed, err := s.st.PublicUserIDs(r.Context(), ids)
	if err != nil {
		s.readError(w, r, err)
		return
	}
	entries := make([]map[string]any, 0, len(raw))
	rank := 0
	for _, e := range raw {
		if !allowed[e.UserID] {
			continue
		}
		rank++
		entries = append(entries, map[string]any{
			"rank":      rank,
			"userId":    idString(e.UserID),
			"username":  e.Username,
			"avatarUrl": "/avatar/" + idString(e.UserID),
			"points":    e.Score,
		})
	}
	age := max(time.Since(snap.GeneratedAt), 0)
	complete, _ := meta["complete"].(bool)
	stale := age >= analyticsStaleAfter && !complete
	status := "ready"
	if stale {
		status = "stale"
	}
	meta["status"] = status
	meta["generatedAt"] = snap.GeneratedAt
	meta["stale"] = stale
	meta["ageSeconds"] = int64(age / time.Second)
	meta["entries"] = entries
	s.respond(w, 200, meta)
}
