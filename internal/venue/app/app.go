package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/venue/config"
	"github.com/WithSoull/avito-kitchen/internal/venue/integration/platformclient"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
	"github.com/WithSoull/avito-kitchen/internal/venue/service"
	httptransport "github.com/WithSoull/avito-kitchen/internal/venue/transport/http"
)

const schemaVersion int64 = 7

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
	platform, err := platformclient.New(cfg.PlatformBaseURL, cfg.PartnerToken, cfg.PlatformRequestTimeout)
	if err != nil {
		return err
	}
	publishCtx, cancelPublish := context.WithTimeout(ctx, cfg.MenuPublishTimeout)
	err = service.NewMenuPublisher(repo, platform).Publish(publishCtx)
	cancelPublish()
	if err != nil {
		return err
	}
	worker := service.NewCallbackWorker(repo, platform, service.CallbackConfig{
		Workers: cfg.CallbackWorkers, PollInterval: cfg.CallbackPollInterval, Lease: cfg.CallbackLease, Metrics: metrics,
	}, logger)
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() {
		worker.Run(workerCtx)
		close(workerDone)
	}()
	handler := httptransport.NewHandler(
		service.NewOrders(repo),
		httptransport.Config{PlatformToken: cfg.PlatformToken, StaffToken: cfg.VenueStaffToken, AllowedOrigins: cfg.AllowedOrigins, MaxRequestBytes: cfg.MaxRequestBytes, Readiness: readiness, ReadinessTimeout: cfg.DBConnectTimeout, TestDecisionDelay: cfg.TestDecisionDelay, TestDecisionStatus: cfg.TestDecisionStatus, Metrics: metrics},
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
		logger.Info("venue HTTP server started", "address", cfg.HTTPAddr)
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
