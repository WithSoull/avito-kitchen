package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
)

type Orders interface {
	DecideOrder(context.Context, string, domain.OrderCommand) (domain.OrderDecision, error)
	CancelOrder(context.Context, string) (domain.CancellationDecision, error)
	ListOrders(context.Context) ([]domain.ManagedOrder, error)
	GetOrder(context.Context, string) (domain.ManagedOrder, error)
	StartPreparing(context.Context, string) (domain.ManagedOrder, error)
	MarkReady(context.Context, string) (domain.ManagedOrder, error)
	Complete(context.Context, string) (domain.ManagedOrder, error)
}

type OrdersService struct{ repository repository.Orders }

func NewOrders(repo repository.Orders) *OrdersService { return &OrdersService{repository: repo} }

func (service *OrdersService) DecideOrder(ctx context.Context, idempotencyKey string, command domain.OrderCommand) (domain.OrderDecision, error) {
	keyLength := utf8.RuneCountInString(idempotencyKey)
	if keyLength < 16 || keyLength > 128 || !shareduuid.IsValid(command.PlatformOrderID) || command.MenuVersion < 1 || len(command.Items) < 1 || len(command.Items) > 100 {
		return domain.OrderDecision{}, validationError()
	}
	seen := make(map[string]struct{}, len(command.Items))
	for _, item := range command.Items {
		if !validRequiredText(item.ExternalItemID, 128) || !validRequiredText(item.Name, 200) || item.Quantity < 1 || item.Quantity > 100 || item.ExpectedUnitPrice < 0 ||
			(item.ExpectedUnitPrice > 0 && int64(item.Quantity) > math.MaxInt64/item.ExpectedUnitPrice) {
			return domain.OrderDecision{}, validationError()
		}
		if _, exists := seen[item.ExternalItemID]; exists {
			return domain.OrderDecision{}, validationError()
		}
		seen[item.ExternalItemID] = struct{}{}
	}
	decision, err := service.repository.DecideOrder(ctx, idempotencyKey, command)
	return decision, mapError(err)
}

func validRequiredText(value string, maxRunes int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maxRunes
}

func (service *OrdersService) ListOrders(ctx context.Context) ([]domain.ManagedOrder, error) {
	orders, err := service.repository.ListOrders(ctx)
	return orders, mapError(err)
}

func (service *OrdersService) GetOrder(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	order, err := service.repository.GetOrder(ctx, orderID)
	return order, mapError(err)
}

func (service *OrdersService) CancelOrder(ctx context.Context, platformOrderID string) (domain.CancellationDecision, error) {
	decision, err := service.repository.CancelOrder(ctx, platformOrderID)
	return decision, mapError(err)
}
func (service *OrdersService) StartPreparing(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	order, err := service.repository.StartPreparing(ctx, orderID)
	return order, mapError(err)
}
func (service *OrdersService) MarkReady(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	order, err := service.repository.MarkReady(ctx, orderID)
	return order, mapError(err)
}
func (service *OrdersService) Complete(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	order, err := service.repository.Complete(ctx, orderID)
	return order, mapError(err)
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrOrderNotFound) {
		return apperror.New(apperror.KindNotFound, "ORDER_NOT_FOUND", "order was not found")
	}
	if errors.Is(err, repository.ErrInvalidOrderTransition) {
		return apperror.New(apperror.KindConflict, "INVALID_ORDER_TRANSITION", "order cannot make the requested transition")
	}
	return apperror.Wrap(apperror.KindInternal, "INTERNAL_ERROR", "internal server error", err)
}

func validationError() error {
	return apperror.New(apperror.KindValidation, "VALIDATION_FAILED", "order command is invalid")
}
