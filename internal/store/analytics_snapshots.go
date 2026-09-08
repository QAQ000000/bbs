package store

import (
	"context"
	"encoding/json"
	"time"
)

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
