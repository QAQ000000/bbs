package db

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPerformanceTraceRedactsSensitiveData(t *testing.T) {
	var log bytes.Buffer
	tracer := &performanceTracer{logger: slog.New(slog.NewJSONHandler(&log, nil))}
	ctx := context.WithValue(context.Background(), queryTraceKey{}, queryTiming{time.Now().Add(-time.Second), "SELECT 'private-token'"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "55P03", Message: "secret-password", Detail: "private-email"}})
	out := log.String()
	for _, secret := range []string{"private-token", "secret-password", "private-email", "SELECT"} {
		if strings.Contains(out, secret) {
			t.Fatal("sensitive data logged", secret)
		}
	}
	if !strings.Contains(out, "queryHash") || !strings.Contains(out, "55P03") {
		t.Fatal("missing diagnostic fields", out)
	}
}

func TestPerformancePoolSizeValidation(t *testing.T) {
	for _, bounds := range [][2]int{{0, 0}, {20, 21}, {1001, 0}, {20, -1}} {
		if pool, err := OpenWithPoolSize(context.Background(), "", bounds[0], bounds[1]); err == nil || pool != nil {
			t.Fatal("accepted invalid pool size", bounds)
		}
	}
}
