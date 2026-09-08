package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
)

func TestOpenFailsForUnavailableDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := postgres.Open(ctx, postgres.Config{
		URL: "postgres://invalid:invalid@127.0.0.1:1/missing?sslmode=disable", ConnectTimeout: 100 * time.Millisecond,
		QueryTimeout: time.Second, MaxConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute,
	})
	if err == nil {
		t.Fatal("expected unavailable database error")
	}
}
