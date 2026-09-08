package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
)

type CallbackSender interface {
	SendOrderEvent(context.Context, domain.CallbackJob) error
}

type CallbackConfig struct {
	Workers      int
	PollInterval time.Duration
	Lease        time.Duration
	Metrics      *httpx.Metrics
}

type CallbackWorker struct {
	repository repository.Callbacks
	platform   CallbackSender
	config     CallbackConfig
	logger     *slog.Logger
}

func NewCallbackWorker(repo repository.Callbacks, platform CallbackSender, config CallbackConfig, logger *slog.Logger) *CallbackWorker {
	return &CallbackWorker{repository: repo, platform: platform, config: config, logger: logger}
}

func (worker *CallbackWorker) Run(ctx context.Context) {
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

func (worker *CallbackWorker) runOne(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if worker.processOne(context.WithoutCancel(ctx)) {
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

func (worker *CallbackWorker) processOne(ctx context.Context) bool {
	job, err := worker.repository.ClaimCallback(ctx, worker.config.Lease)
	if errors.Is(err, repository.ErrNoCallbackJob) {
		return false
	}
	if err != nil {
		worker.logger.Error("claim callback", "error", err)
		return false
	}
	if err := worker.platform.SendOrderEvent(ctx, job); err != nil {
		next := time.Now().Add(callbackBackoff(job.Attempts, rand.Float64()))
		worker.logger.Warn("callback delivery retry", "order_id", job.PlatformOrderID, "attempt", job.Attempts)
		if retryErr := worker.repository.RetryCallback(ctx, job, next); retryErr != nil && !errors.Is(retryErr, repository.ErrCallbackLeaseLost) {
			worker.logger.Error("retry callback", "order_id", job.PlatformOrderID, "error", retryErr)
		} else if retryErr == nil && worker.config.Metrics != nil {
			worker.config.Metrics.JobRetried()
		}
		return true
	}
	if err := worker.repository.CompleteCallback(ctx, job); err != nil && !errors.Is(err, repository.ErrCallbackLeaseLost) {
		worker.logger.Error("complete callback", "order_id", job.PlatformOrderID, "error", err)
	} else if err == nil {
		worker.logger.Info("callback delivered", "order_id", job.PlatformOrderID, "attempt", job.Attempts)
		if worker.config.Metrics != nil {
			worker.config.Metrics.JobProcessed()
		}
	}
	return true
}

func callbackBackoff(attempt int, jitter float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := min(attempt-1, 6)
	base := time.Second * time.Duration(1<<shift)
	return base + time.Duration(float64(base)*0.25*jitter)
}
