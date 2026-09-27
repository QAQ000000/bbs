package store

import (
	"context"
	"sync"
	"time"
	_ "time/tzdata" // Keep timezone validation available in binary-only deployments.
)

var reportTimeZones sync.Map

func validReportTimeZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	if name == "UTC" {
		return true
	}
	if _, ok := reportTimeZones.Load(name); ok {
		return true
	}
	if _, err := time.LoadLocation(name); err != nil {
		return false
	}
	reportTimeZones.Store(name, struct{}{})
	return true
}

// Existing capitalized statistics fields are retained in the snapshot payload.
// The local reporting day is separate from the UTC storage bucket.
type SiteReport struct {
	SiteStats
	TimeZone     string    `json:"timeZone"`
	ReportDate   string    `json:"reportDate"`
	DayStart     time.Time `json:"dayStart"`
	NextDayStart time.Time `json:"nextDayStart"`
}

// nil at uses the database clock; explicit instants support deterministic day
// boundary checks. Construct adjacent calendar midnights instead of adding 24
// hours, since a local reporting day can contain 23 or 25 hours around DST.
func (s *Store) siteReportAt(ctx context.Context, zone string, at *time.Time) (SiteReport, error) {
	v := SiteReport{TimeZone: zone}
	err := s.pool.QueryRow(ctx, `WITH local_day AS (
		SELECT (coalesce($1::timestamptz,now()) AT TIME ZONE $2)::date AS d
	), bounds AS (
		SELECT d::timestamp AT TIME ZONE $2 AS today,
		(d-1)::timestamp AT TIME ZONE $2 AS yesterday,
		(d+1)::timestamp AT TIME ZONE $2 AS tomorrow,to_char(d,'YYYY-MM-DD') AS report_date FROM local_day
	), counts AS (
		SELECT count(*) FILTER (WHERE p.created_at>=b.today AND p.created_at<b.tomorrow) AS today,
		count(*) FILTER (WHERE p.created_at>=b.yesterday AND p.created_at<b.today) AS yesterday,
		count(*) AS total
		FROM posts p JOIN threads t ON t.id=p.thread_id CROSS JOIN bounds b
		WHERE NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending`+forumFilter(ctx, "t.forum_id")+`
	) SELECT c.today,c.yesterday,c.total,
	(SELECT count(*) FROM threads WHERE NOT deleted AND NOT pending`+forumFilter(ctx, "forum_id")+`),
	(SELECT count(*) FROM users),b.report_date,b.today,b.tomorrow FROM counts c CROSS JOIN bounds b`,
		at, zone).Scan(&v.TodayPosts, &v.Yesterday, &v.TotalPosts, &v.TotalThreads, &v.Members, &v.ReportDate, &v.DayStart, &v.NextDayStart)
	return v, err
}
