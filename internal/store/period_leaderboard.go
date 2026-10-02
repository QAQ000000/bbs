package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type PeriodLeaderboard struct {
	Period      string             `json:"period"`
	PeriodStart time.Time          `json:"periodStart"`
	PeriodEnd   time.Time          `json:"periodEnd"`
	AsOf        time.Time          `json:"asOf"`
	Entries     []LeaderboardEntry `json:"entries"`
}

// Calendar periods share the timezone used by the points ledger's daily limits.
func PointsPeriodBounds(period string, at time.Time) (time.Time, time.Time) {
	zone, _ := time.LoadLocation("Asia/Shanghai")
	at = at.In(zone)
	start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, zone)
	switch period {
	case "week":
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		return start, start.AddDate(0, 0, 7)
	case "month":
		start = time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, zone)
		return start, start.AddDate(0, 1, 0)
	default:
		return start, start.AddDate(0, 0, 1)
	}
}

func (s *Store) RefreshPeriodLeaderboard(ctx context.Context, period string, at time.Time) error {
	start, end := PointsPeriodBounds(period, at)
	previous, _ := PointsPeriodBounds(period, start.Add(-time.Second))
	if err := s.refreshPeriodLeaderboard(ctx, period, previous, start, at); err != nil {
		return err
	}
	return s.refreshPeriodLeaderboard(ctx, period, start, end, at)
}

func (s *Store) refreshPeriodLeaderboard(ctx context.Context, period string, start, end, at time.Time) error {
	asOf := at
	if end.Before(asOf) {
		asOf = end
	}
	v := PeriodLeaderboard{Period: period, PeriodStart: start, PeriodEnd: end, AsOf: asOf, Entries: []LeaderboardEntry{}}
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.username,sum(l.delta)
	 FROM points_ledger l JOIN users u ON u.id=l.user_id
	 WHERE l.kind<>'baseline' AND l.event_at>=$1 AND l.event_at<$2 AND l.event_at<=$3
	 AND (u.blocked_until IS NULL OR u.blocked_until<=now())
	 GROUP BY u.id,u.username ORDER BY 3 DESC,u.id LIMIT 100`, start, end, at)
	if err != nil {
		return err
	}
	for rows.Next() {
		var entry LeaderboardEntry
		if err = rows.Scan(&entry.UserID, &entry.Username, &entry.Score); err != nil {
			rows.Close()
			return err
		}
		v.Entries = append(v.Entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return s.SaveAnalyticsSnapshot(ctx, "points."+period, start, v)
}

func (s *Store) PeriodLeaderboardSnapshot(ctx context.Context, period string, start time.Time) (AnalyticsSnapshot, error) {
	v := AnalyticsSnapshot{Name: "points." + period, PeriodStart: start}
	err := s.pool.QueryRow(ctx, `SELECT payload,generated_at FROM analytics_snapshots WHERE name=$1 AND period_start=$2`, v.Name, start).Scan(&v.Payload, &v.GeneratedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
