package repository

import (
	"context"
	"errors"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
)

var ErrVenueNotFound = errors.New("venue not found")
var ErrMenuNotFound = errors.New("menu not found")
var ErrStaleMenuVersion = errors.New("stale menu version")
var ErrMenuVersionConflict = errors.New("menu version conflict")
var ErrVenueClosed = errors.New("venue closed")
var ErrMenuChanged = errors.New("menu changed")
var ErrItemsUnavailable = errors.New("items unavailable")
var ErrMinimumOrder = errors.New("minimum order amount is not reached")
var ErrAmountOverflow = errors.New("order amount overflow")
var ErrIdempotencyKeyReused = errors.New("idempotency key reused")
var ErrNoOutboxJob = errors.New("no outbox job available")
var ErrOutboxLeaseLost = errors.New("outbox lease lost")
var ErrOrderNotFound = errors.New("order not found")
var ErrOrderVenueMismatch = errors.New("order venue mismatch")
var ErrEventConflict = errors.New("order event conflict")
var ErrEventSequenceConflict = errors.New("order event sequence conflict")
var ErrInvalidOrderTransition = errors.New("invalid order transition")
var ErrOrderCancellationNotAllowed = errors.New("order cancellation not allowed")

type Catalog interface {
	ListVenues(context.Context, domain.VenueListQuery) (domain.VenuePage, error)
	GetVenue(context.Context, string) (domain.Venue, error)
	GetMenu(context.Context, string) (domain.Menu, error)
}

type Orders interface {
	CreateOrder(context.Context, domain.CreateOrderCommand) (domain.Order, error)
	GetOrder(context.Context, string, string) (domain.Order, error)
	CancelOrder(context.Context, string, string) (domain.Order, error)
}

type Partner interface {
	ReplaceMenu(context.Context, string, domain.MenuSnapshot, string) error
	UpdateAvailability(context.Context, string, domain.AvailabilityUpdate) error
	RecordOrderEvent(context.Context, string, string, domain.OrderEvent) error
}

type Outbox interface {
	ClaimOutbox(context.Context, time.Duration) (domain.OutboxJob, error)
	ResolveOutbox(context.Context, domain.OutboxJob, domain.VenueOrderDecision) error
	ResolveCancellation(context.Context, domain.OutboxJob, domain.VenueCancellationDecision) error
	RetryOutbox(context.Context, domain.OutboxJob, time.Time, bool) error
}

type Repository struct {
	db         postgres.DBTX
	transactor postgres.Transactor
}

func New(db postgres.DBTX, transactor postgres.Transactor) *Repository {
	return &Repository{db: db, transactor: transactor}
}
