package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/integration/venueclient"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
)

type outboxRepositoryFake struct {
	job      domain.OutboxJob
	claims   int
	resolved int
	retried  int
	terminal bool
}

func (fake *outboxRepositoryFake) ClaimOutbox(context.Context, time.Duration) (domain.OutboxJob, error) {
	fake.claims++
	return fake.job, nil
}
func (fake *outboxRepositoryFake) ResolveOutbox(context.Context, domain.OutboxJob, domain.VenueOrderDecision) error {
	fake.resolved++
	return nil
}
func (fake *outboxRepositoryFake) ResolveCancellation(context.Context, domain.OutboxJob, domain.VenueCancellationDecision) error {
	fake.resolved++
	return nil
}
func (fake *outboxRepositoryFake) RetryOutbox(_ context.Context, _ domain.OutboxJob, _ time.Time, terminal bool) error {
	fake.retried++
	fake.terminal = terminal
	return nil
}

type venueDeciderFake struct {
	err   error
	calls int
}

func (fake *venueDeciderFake) Decide(context.Context, domain.OutboxJob) (domain.VenueOrderDecision, error) {
	fake.calls++
	return domain.VenueOrderDecision{Result: "accepted"}, fake.err
}
func (fake *venueDeciderFake) Cancel(context.Context, domain.OutboxJob) (domain.VenueCancellationDecision, error) {
	fake.calls++
	return domain.VenueCancellationDecision{Result: "cancelled", CurrentStatus: "cancelled"}, fake.err
}

func TestOutboxRetryAndTerminalPolicy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := &outboxRepositoryFake{job: domain.OutboxJob{Attempts: 1, ConfirmationDeadline: time.Now().Add(time.Minute)}}
	venue := &venueDeciderFake{err: &venueclient.Error{Retryable: true, Cause: errors.New("temporary")}}
	worker := NewOutboxWorker(repo, venue, OutboxConfig{Lease: time.Second, MaxAttempts: 3}, logger)
	worker.processOne(context.Background())
	if repo.retried != 1 || repo.terminal {
		t.Fatalf("retry=%d terminal=%v", repo.retried, repo.terminal)
	}
	venue.err = nil
	worker.processOne(context.Background())
	if repo.resolved != 1 {
		t.Fatalf("resolved=%d", repo.resolved)
	}

	repo.job.Attempts = 3
	venue.err = &venueclient.Error{Retryable: true, Cause: errors.New("still unavailable")}
	worker.processOne(context.Background())
	if !repo.terminal {
		t.Fatal("max attempts must terminate confirmation")
	}
}

func TestOutboxExpiresWithoutCallingVenue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := &outboxRepositoryFake{job: domain.OutboxJob{Attempts: 1, ConfirmationDeadline: time.Now().Add(-time.Second)}}
	venue := &venueDeciderFake{}
	worker := NewOutboxWorker(repo, venue, OutboxConfig{Lease: time.Second, MaxAttempts: 3}, logger)

	worker.processOne(context.Background())

	if venue.calls != 0 || repo.retried != 1 || !repo.terminal {
		t.Fatalf("venue_calls=%d retry=%d terminal=%v", venue.calls, repo.retried, repo.terminal)
	}
}

func TestOutboxDispatchesCancellationWithoutConfirmationDeadline(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := &outboxRepositoryFake{job: domain.OutboxJob{EventType: "cancel_order", Attempts: 1}}
	venue := &venueDeciderFake{}
	worker := NewOutboxWorker(repo, venue, OutboxConfig{Lease: time.Second, MaxAttempts: 3}, logger)

	worker.processOne(context.Background())

	if venue.calls != 1 || repo.resolved != 1 || repo.retried != 0 {
		t.Fatalf("venue_calls=%d resolved=%d retried=%d", venue.calls, repo.resolved, repo.retried)
	}
}

func TestOutboxWorkerStopsBeforeClaimAfterCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := &outboxRepositoryFake{}
	worker := NewOutboxWorker(repo, &venueDeciderFake{}, OutboxConfig{
		Workers:      1,
		PollInterval: time.Millisecond,
		Lease:        time.Second,
		MaxAttempts:  3,
	}, logger)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	worker.Run(ctx)

	if repo.claims != 0 {
		t.Fatalf("claims after cancellation=%d", repo.claims)
	}
}

func TestRetryBackoffIsBounded(t *testing.T) {
	if got := retryBackoff(1, 0); got != time.Second {
		t.Fatalf("first backoff=%v", got)
	}
	if got := retryBackoff(100, 1); got != 40*time.Second {
		t.Fatalf("capped backoff=%v", got)
	}
}

var _ repository.Outbox = (*outboxRepositoryFake)(nil)
