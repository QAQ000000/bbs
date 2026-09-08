package store

import (
	"context"
	"encoding/json"
	"time"
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
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.username,coalesce(a.balance,0) FROM users u JOIN points_accounts a ON a.user_id=u.id ORDER BY a.balance DESC,u.id LIMIT $1`, limit)
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

func (s *Store) SaveAnalyticsSnapshot(ctx context.Context, name string, periodStart time.Time, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO analytics_snapshots(name,period_start,payload) VALUES($1,$2,$3) ON CONFLICT(name,period_start) DO UPDATE SET generated_at=now(),payload=EXCLUDED.payload`, name, periodStart, raw)
	return err
}

func (s *Store) LatestAnalyticsSnapshot(ctx context.Context, name string) (json.RawMessage, time.Time, error) {
	var raw json.RawMessage
	var generated time.Time
	err := s.pool.QueryRow(ctx, `SELECT payload,generated_at FROM analytics_snapshots WHERE name=$1 ORDER BY period_start DESC LIMIT 1`, name).Scan(&raw, &generated)
	return raw, generated, err
}
