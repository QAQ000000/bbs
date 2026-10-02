package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type LeaderboardEntry struct {
	UserID   int64  `json:"userId"`
	Username string `json:"username"`
	Score    int64  `json:"score"`
}

// RefreshPointsLeaderboard materializes the current points ranking for fast reads.
func (s *Store) RefreshPointsLeaderboard(ctx context.Context, periodStart time.Time, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	// 生成时就排除当前被禁止登录（封禁）的账号；读取时会再校验一次。
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.username,coalesce(a.balance,0) FROM users u JOIN points_accounts a ON a.user_id=u.id
	 WHERE u.blocked_until IS NULL OR u.blocked_until<=now()
	 ORDER BY a.balance DESC,u.id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []LeaderboardEntry{}
	for rows.Next() {
		var v LeaderboardEntry
		if err := rows.Scan(&v.UserID, &v.Username, &v.Score); err != nil {
			return err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.SaveAnalyticsSnapshot(ctx, "points", periodStart, items)
}

func (s *Store) RefreshSiteReport(ctx context.Context, periodStart time.Time) error {
	settings, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	v, err := s.siteReportAt(ctx, settings.ReportTimeZone, nil)
	if err != nil {
		return err
	}
	return s.SaveAnalyticsSnapshot(ctx, "site", periodStart, v)
}

func (s *Store) SaveAnalyticsSnapshot(ctx context.Context, name string, periodStart time.Time, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO analytics_snapshots(name,period_start,payload) VALUES($1,$2,$3) ON CONFLICT(name,period_start) DO UPDATE SET generated_at=now(),payload=EXCLUDED.payload`, name, periodStart, raw)
	return err
}

type AnalyticsSnapshot struct {
	Name        string          `json:"name"`
	PeriodStart time.Time       `json:"periodStart"`
	GeneratedAt time.Time       `json:"generatedAt"`
	Payload     json.RawMessage `json:"payload"`
}

// PruneAnalyticsSnapshots removes at most 500 expired generated snapshots. The
// latest bucket of each built-in kind survives even after a long refresh outage.
// Read the current validated policy for every batch; invalid settings never
// become a default deletion policy. Other snapshot kinds are left untouched.
func (s *Store) PruneAnalyticsSnapshots(ctx context.Context) (int64, error) {
	settings, err := s.Settings(ctx)
	if err != nil || settings.AnalyticsRetentionDays == 0 {
		return 0, err
	}
	result, err := s.pool.Exec(ctx, `WITH latest AS MATERIALIZED (
		SELECT DISTINCT ON(name) name,period_start FROM analytics_snapshots
		WHERE name IN ('points','site','points.day','points.week','points.month') ORDER BY name,period_start DESC
	), expired AS (
		SELECT a.name,a.period_start FROM analytics_snapshots a
		JOIN latest l ON a.name=l.name
		WHERE CASE a.name
		 WHEN 'points.day' THEN a.period_start+interval '1 day'
		 WHEN 'points.week' THEN a.period_start+interval '7 days'
		 WHEN 'points.month' THEN ((a.period_start AT TIME ZONE 'Asia/Shanghai')+interval '1 month') AT TIME ZONE 'Asia/Shanghai'
		 ELSE a.period_start END < now()-$1::int*interval '24 hours'
		AND a.period_start < l.period_start
		ORDER BY a.period_start,a.name LIMIT 500
		FOR UPDATE OF a SKIP LOCKED
	)
	DELETE FROM analytics_snapshots a USING expired e
	WHERE a.name=e.name AND a.period_start=e.period_start`, settings.AnalyticsRetentionDays)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// PublicUserIDs 返回给定用户中当前仍可公开展示的集合。读取旧快照时也会调用，
// 这样封禁生效不需要等待下一次快照刷新。禁言（banned）用户的资料仍属公开，保留。
func (s *Store) PublicUserIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM users WHERE id=ANY($1) AND (blocked_until IS NULL OR blocked_until<=now())`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Store) LatestAnalyticsSnapshot(ctx context.Context, name string) (AnalyticsSnapshot, error) {
	v := AnalyticsSnapshot{Name: name}
	err := s.pool.QueryRow(ctx, `SELECT payload,period_start,generated_at FROM analytics_snapshots WHERE name=$1 ORDER BY period_start DESC LIMIT 1`, name).Scan(&v.Payload, &v.PeriodStart, &v.GeneratedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
