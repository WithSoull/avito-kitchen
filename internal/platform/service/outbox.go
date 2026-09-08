package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/integration/venueclient"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
)

type VenueGateway interface {
	Decide(context.Context, domain.OutboxJob) (domain.VenueOrderDecision, error)
	Cancel(context.Context, domain.OutboxJob) (domain.VenueCancellationDecision, error)
}

type OutboxConfig struct {
	Workers      int
	PollInterval time.Duration
	Lease        time.Duration
	MaxAttempts  int
	Metrics      *httpx.Metrics
}

type OutboxWorker struct {
	repository repository.Outbox
	venue      VenueGateway
	config     OutboxConfig
	logger     *slog.Logger
}

func NewOutboxWorker(repo repository.Outbox, venue VenueGateway, config OutboxConfig, logger *slog.Logger) *OutboxWorker {
	return &OutboxWorker{repository: repo, venue: venue, config: config, logger: logger}
}

func (worker *OutboxWorker) Run(ctx context.Context) {
	var group sync.WaitGroup
	for range worker.config.Workers {
		group.Add(1)
		go func() {
			defer group.Done()
			worker.runOne(ctx)
		}()
	}
	group.Wait()
}

func (worker *OutboxWorker) runOne(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		processed := worker.processOne(context.WithoutCancel(ctx))
		if processed {
			continue
		}
		timer := time.NewTimer(worker.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (worker *OutboxWorker) processOne(ctx context.Context) bool {
	job, err := worker.repository.ClaimOutbox(ctx, worker.config.Lease)
	if errors.Is(err, repository.ErrNoOutboxJob) {
		return false
	}
	if err != nil {
		worker.logger.Error("claim outbox job", "error", err)
		return false
	}
	if job.EventType != "cancel_order" && !time.Now().Before(job.ConfirmationDeadline) {
		worker.finishRetry(ctx, job, true)
		return true
	}
	var deliveryErr error
	if job.EventType == "cancel_order" {
		decision, err := worker.venue.Cancel(ctx, job)
		deliveryErr = err
		if err == nil {
			if err := worker.repository.ResolveCancellation(ctx, job, decision); err != nil && !errors.Is(err, repository.ErrOutboxLeaseLost) {
				worker.logger.Error("resolve cancellation", "order_id", job.OrderID, "error", err)
			} else if err == nil {
				worker.logger.Info("cancellation delivered", "order_id", job.OrderID, "attempt", job.Attempts, "result", decision.Result)
				worker.recordProcessed()
			}
			return true
		}
	} else {
		decision, err := worker.venue.Decide(ctx, job)
		deliveryErr = err
		if err == nil {
			if err := worker.repository.ResolveOutbox(ctx, job, decision); err != nil && !errors.Is(err, repository.ErrOutboxLeaseLost) {
				worker.logger.Error("resolve outbox job", "order_id", job.OrderID, "error", err)
			} else if err == nil {
				worker.logger.Info("confirmation delivered", "order_id", job.OrderID, "attempt", job.Attempts, "result", decision.Result)
				worker.recordProcessed()
			}
			return true
		}
	}
	terminal := !venueclient.IsRetryable(deliveryErr) || job.Attempts >= worker.config.MaxAttempts
	next := time.Now().Add(retryBackoff(job.Attempts, rand.Float64()))
	if job.EventType != "cancel_order" && !next.Before(job.ConfirmationDeadline) {
		terminal = true
	}
	worker.finishRetryAt(ctx, job, next, terminal)
	return true
}

func (worker *OutboxWorker) finishRetry(ctx context.Context, job domain.OutboxJob, terminal bool) {
	worker.finishRetryAt(ctx, job, time.Now(), terminal)
}

func (worker *OutboxWorker) finishRetryAt(ctx context.Context, job domain.OutboxJob, next time.Time, terminal bool) {
	worker.logger.Warn("outbox delivery retry", "order_id", job.OrderID, "event_type", job.EventType, "attempt", job.Attempts, "terminal", terminal)
	if err := worker.repository.RetryOutbox(ctx, job, next, terminal); err != nil && !errors.Is(err, repository.ErrOutboxLeaseLost) {
		worker.logger.Error("retry outbox job", "order_id", job.OrderID, "terminal", terminal, "error", err)
	} else if err == nil && worker.config.Metrics != nil {
		worker.config.Metrics.JobRetried()
	}
}

func (worker *OutboxWorker) recordProcessed() {
	if worker.config.Metrics != nil {
		worker.config.Metrics.JobProcessed()
	}
}

func retryBackoff(attempt int, jitter float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := min(attempt-1, 5)
	base := time.Second * time.Duration(1<<shift)
	return base + time.Duration(float64(base)*0.25*jitter)
}
