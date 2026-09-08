package repository

import (
	"context"
	"errors"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

var ErrMenuNotFound = errors.New("venue menu not found")
var ErrOrderNotFound = errors.New("venue order not found")
var ErrInvalidOrderTransition = errors.New("invalid venue order transition")
var ErrNoCallbackJob = errors.New("no callback job available")
var ErrCallbackLeaseLost = errors.New("callback lease lost")

type Menu interface {
	CurrentMenu(context.Context) (domain.MenuSnapshot, error)
}

type Orders interface {
	DecideOrder(context.Context, string, domain.OrderCommand) (domain.OrderDecision, error)
	CancelOrder(context.Context, string) (domain.CancellationDecision, error)
	ListOrders(context.Context) ([]domain.ManagedOrder, error)
	GetOrder(context.Context, string) (domain.ManagedOrder, error)
	StartPreparing(context.Context, string) (domain.ManagedOrder, error)
	MarkReady(context.Context, string) (domain.ManagedOrder, error)
	Complete(context.Context, string) (domain.ManagedOrder, error)
}

type Callbacks interface {
	ClaimCallback(context.Context, time.Duration) (domain.CallbackJob, error)
	CompleteCallback(context.Context, domain.CallbackJob) error
	RetryCallback(context.Context, domain.CallbackJob, time.Time) error
}

type Repository struct {
	db         postgres.DBTX
	transactor postgres.Transactor
}

func New(db postgres.DBTX, transactor postgres.Transactor) *Repository {
	return &Repository{db: db, transactor: transactor}
}
