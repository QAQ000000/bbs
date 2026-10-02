package store

import (
	"testing"
	"time"
)

func TestPointsPeriodCalendarBounds(t *testing.T) {
	at := time.Date(2026, 10, 4, 16, 30, 0, 0, time.UTC) // Monday in Shanghai.
	for _, tc := range []struct{ period, start, end string }{
		{"day", "2026-10-05T00:00:00+08:00", "2026-10-06T00:00:00+08:00"},
		{"week", "2026-10-05T00:00:00+08:00", "2026-10-12T00:00:00+08:00"},
		{"month", "2026-10-01T00:00:00+08:00", "2026-11-01T00:00:00+08:00"},
	} {
		start, end := PointsPeriodBounds(tc.period, at)
		if start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end {
			t.Fatal(tc.period, start, end)
		}
	}
}
