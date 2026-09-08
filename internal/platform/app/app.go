package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/config"
	"github.com/WithSoull/avito-kitchen/internal/platform/integration/venueclient"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/platform/service"
	httptransport "github.com/WithSoull/avito-kitchen/internal/platform/transport/http"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
)

const schemaVersion int64 = 9

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	database, err := postgres.Open(ctx, postgres.Config{
		URL: cfg.DatabaseURL, ConnectTimeout: cfg.DBConnectTimeout, QueryTimeout: cfg.DBQueryTimeout,
		MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns, MaxConnLifetime: cfg.DBMaxConnLifetime, MaxConnIdleTime: cfg.DBMaxConnIdleTime,
	})
	if err != nil {
		return err
	}
	defer database.Close()
	readiness := postgres.NewMigrationReadiness(database, schemaVersion)
	metrics := &httpx.Metrics{}

	repo := repository.New(database, database)
	venue, err := venueclient.New(cfg.VenueBaseURL, cfg.PlatformToken, cfg.VenueRequestTimeout)
	if err != nil {
		return err
	}
	worker := service.NewOutboxWorker(repo, venue, service.OutboxConfig{Workers: cfg.OutboxWorkers, PollInterval: cfg.OutboxPollInterval, Lease: cfg.OutboxLease, MaxAttempts: cfg.OutboxMaxAttempts, Metrics: metrics}, logger)
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() {
		worker.Run(workerCtx)
		close(workerDone)
	}()
	handler := httptransport.NewHandler(
		service.NewCatalog(repo),
		service.NewOrders(repo, cfg.OrderTokenSecret, cfg.ConfirmationTimeout),
		service.NewPartner(repo),
		httptransport.Config{PartnerToken: cfg.PartnerToken, PartnerVenueID: cfg.PartnerVenueID, AllowedOrigins: cfg.AllowedOrigins, MaxRequestBytes: cfg.MaxRequestBytes, Readiness: readiness, ReadinessTimeout: cfg.DBConnectTimeout, Metrics: metrics, CheckoutConcurrency: cfg.CheckoutConcurrency, CheckoutRateLimit: cfg.CheckoutRateLimit},
		logger,
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("platform HTTP server started", "address", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		<-workerDone
		return nil
	case err := <-errCh:
		stopWorker()
		<-workerDone
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	}
}
