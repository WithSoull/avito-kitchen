package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
	"github.com/WithSoull/avito-kitchen/internal/venue/app"
	"github.com/WithSoull/avito-kitchen/internal/venue/config"
)

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "venue_shutdown")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/venue/000001_init.up.sql",
		"migrations/venue/000002_menu_version_as_bigint.up.sql",
		"migrations/venue/000003_query_indexes.up.sql",
		"migrations/venue/000004_seed_menu.up.sql",
		"migrations/venue/000005_order_acceptance.up.sql",
		"migrations/venue/000006_callback_leases.up.sql",
		"migrations/venue/000007_cancellation_decision.up.sql",
	)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer platform.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err := app.Run(ctx, config.Config{
		HTTPAddr: "127.0.0.1:0", DatabaseURL: databaseURL, PlatformBaseURL: platform.URL,
		PlatformToken: "test-platform", VenueStaffToken: "test-staff",
		PartnerToken: "test-partner", PlatformRequestTimeout: time.Second, MenuPublishTimeout: time.Second,
		CallbackWorkers: 1, CallbackPollInterval: 10 * time.Millisecond, CallbackLease: 2 * time.Second,
		DBConnectTimeout: time.Second, DBQueryTimeout: time.Second, DBMaxConns: 2, DBMinConns: 1,
		DBMaxConnLifetime: time.Minute, DBMaxConnIdleTime: time.Minute,
		MaxRequestBytes: 1024, MaxHeaderBytes: 1024, ShutdownTimeout: time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
}
