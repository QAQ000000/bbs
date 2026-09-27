package store

import (
	"context"
	"testing"
	"time"
)

func TestReportTimeZoneCalendarBounds(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, uid, "author", "report dates", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html)
		SELECT $1,$2,g,'boundary','' FROM generate_series(2,6) g`, th.ID, uid); err != nil {
		t.Fatal(err)
	}
	scoped := WithVisibleForums(ctx, []int64{fid})
	parse := func(value string) time.Time {
		v, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct{ zone, at, date, start, end, yesterday string }{
		{"UTC", "2026-01-01T12:00:00Z", "2026-01-01", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "2025-12-31T00:00:00Z"},
		{"Asia/Shanghai", "2026-01-01T18:00:00Z", "2026-01-02", "2026-01-01T16:00:00Z", "2026-01-02T16:00:00Z", "2025-12-31T16:00:00Z"},
		{"America/New_York", "2026-03-08T12:00:00Z", "2026-03-08", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", "2026-03-07T05:00:00Z"},
		{"America/New_York", "2026-11-01T12:00:00Z", "2026-11-01", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z", "2026-10-31T04:00:00Z"},
	} {
		t.Run(tc.zone+"/"+tc.date, func(t *testing.T) {
			at, start, end, yesterday := parse(tc.at), parse(tc.start), parse(tc.end), parse(tc.yesterday)
			stamps := []time.Time{yesterday.Add(-time.Second), yesterday, start.Add(-time.Second), start, end.Add(-time.Second), end}
			if _, err := testPool.Exec(ctx, `UPDATE posts p SET created_at=v.at FROM unnest($2::timestamptz[]) WITH ORDINALITY v(at,floor) WHERE p.thread_id=$1 AND p.floor=v.floor`, th.ID, stamps); err != nil {
				t.Fatal(err)
			}
			v, err := testStore.siteReportAt(scoped, tc.zone, &at)
			if err != nil || v.TimeZone != tc.zone || v.ReportDate != tc.date || !v.DayStart.Equal(start) || !v.NextDayStart.Equal(end) || v.TodayPosts != 2 || v.Yesterday != 2 || v.TotalPosts != 6 || v.TotalThreads != 1 {
				t.Fatal(v, err)
			}
			// Empty visibility must still return calendar metadata and zero counts.
			empty, err := testStore.siteReportAt(WithVisibleForums(ctx, nil), tc.zone, &at)
			if err != nil || empty.TotalPosts != 0 || empty.TotalThreads != 0 || empty.ReportDate != tc.date {
				t.Fatal(empty, err)
			}
		})
	}
}
