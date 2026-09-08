package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/app"
	"github.com/WithSoull/avito-kitchen/internal/platform/config"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "platform_shutdown")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err := app.Run(ctx, config.Config{
		HTTPAddr: "127.0.0.1:0", DatabaseURL: databaseURL, PartnerToken: "test-partner", PartnerVenueID: "00000000-0000-4000-8000-000000000001",
		VenueBaseURL: "http://127.0.0.1:1", PlatformToken: "test-platform", VenueRequestTimeout: time.Second,
		OutboxWorkers: 1, OutboxPollInterval: 10 * time.Millisecond, OutboxLease: 2 * time.Second, OutboxMaxAttempts: 2,
		DBConnectTimeout: time.Second, DBQueryTimeout: time.Second, DBMaxConns: 2, DBMinConns: 1, DBMaxConnLifetime: time.Minute, DBMaxConnIdleTime: time.Minute,
		MaxRequestBytes: 1024, MaxHeaderBytes: 1024, ShutdownTimeout: time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
}
