package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

type callbackRepositoryFake struct {
	job       domain.CallbackJob
	claims    int
	completed int
	retried   int
}

func (fake *callbackRepositoryFake) ClaimCallback(context.Context, time.Duration) (domain.CallbackJob, error) {
	fake.claims++
	return fake.job, nil
}
func (fake *callbackRepositoryFake) CompleteCallback(context.Context, domain.CallbackJob) error {
	fake.completed++
	return nil
}
func (fake *callbackRepositoryFake) RetryCallback(context.Context, domain.CallbackJob, time.Time) error {
	fake.retried++
	return nil
}

type callbackSenderFake struct {
	err   error
	calls int
}

func (fake *callbackSenderFake) SendOrderEvent(context.Context, domain.CallbackJob) error {
	fake.calls++
	return fake.err
}

func TestCallbackWorkerRetriesThenCompletes(t *testing.T) {
	repo := &callbackRepositoryFake{job: domain.CallbackJob{Attempts: 1}}
	sender := &callbackSenderFake{err: errors.New("temporary")}
	worker := NewCallbackWorker(repo, sender, CallbackConfig{Lease: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.processOne(context.Background())
	if repo.retried != 1 || repo.completed != 0 {
		t.Fatalf("retried=%d completed=%d", repo.retried, repo.completed)
	}
	sender.err = nil
	worker.processOne(context.Background())
	if repo.completed != 1 {
		t.Fatalf("completed=%d", repo.completed)
	}
}

func TestCallbackWorkerStopsBeforeClaimAfterCancellation(t *testing.T) {
	repo := &callbackRepositoryFake{}
	worker := NewCallbackWorker(repo, &callbackSenderFake{}, CallbackConfig{Workers: 1, PollInterval: time.Millisecond, Lease: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(ctx)
	if repo.claims != 0 {
		t.Fatalf("claims=%d", repo.claims)
	}
}

func TestCallbackBackoffIsBounded(t *testing.T) {
	if got := callbackBackoff(1, 0); got != time.Second {
		t.Fatalf("first=%v", got)
	}
	if got := callbackBackoff(100, 1); got != 80*time.Second {
		t.Fatalf("capped=%v", got)
	}
}
