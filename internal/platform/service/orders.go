package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/security/ordertoken"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func (service *OrdersService) CreateOrder(ctx context.Context, idempotencyKey string, request domain.CreateOrderRequest) (domain.CreateOrderResult, error) {
	request.CustomerRef = strings.TrimSpace(request.CustomerRef)
	request.Delivery.Address.City = strings.TrimSpace(request.Delivery.Address.City)
	request.Delivery.Address.Street = strings.TrimSpace(request.Delivery.Address.Street)
	request.Delivery.Address.House = strings.TrimSpace(request.Delivery.Address.House)
	request.Delivery.Phone = strings.TrimSpace(request.Delivery.Phone)
	if err := validateCheckout(idempotencyKey, request); err != nil {
		return domain.CreateOrderResult{}, err
	}
	requestHash, err := checkoutHash(request)
	if err != nil {
		return domain.CreateOrderResult{}, apperror.Wrap(apperror.KindInternal, "INTERNAL_ERROR", "internal server error", err)
	}
	orderID, err := shareduuid.New()
	if err != nil {
		return domain.CreateOrderResult{}, apperror.Wrap(apperror.KindInternal, "INTERNAL_ERROR", "internal server error", err)
	}
	token, tokenHash, err := ordertoken.Derive(service.tokenSecret, request.CustomerRef, idempotencyKey)
	if err != nil || service.confirmationTimeout <= 0 {
		return domain.CreateOrderResult{}, apperror.New(apperror.KindInternal, "INTERNAL_ERROR", "internal server error")
	}
	order, err := service.repository.CreateOrder(ctx, domain.CreateOrderCommand{
		OrderID: orderID, IdempotencyKey: idempotencyKey, RequestHash: requestHash,
		TrackingTokenHash: tokenHash, ConfirmationDeadline: time.Now().UTC().Add(service.confirmationTimeout), Request: request,
	})
	if err != nil {
		return domain.CreateOrderResult{}, mapRepositoryError(err)
	}
	return domain.CreateOrderResult{Order: order, TrackingToken: token}, nil
}

func (service *OrdersService) GetOrder(ctx context.Context, orderID, tokenHash string) (domain.Order, error) {
	order, err := service.repository.GetOrder(ctx, orderID, tokenHash)
	return order, mapRepositoryError(err)
}

func (service *OrdersService) CancelOrder(ctx context.Context, orderID, tokenHash string) (domain.Order, error) {
	order, err := service.repository.CancelOrder(ctx, orderID, tokenHash)
	return order, mapRepositoryError(err)
}

func validateCheckout(key string, request domain.CreateOrderRequest) error {
	if len(key) < 16 || len(key) > 128 || request.CustomerRef == "" || len(request.CustomerRef) > 128 ||
		!shareduuid.IsValid(request.VenueID) || request.MenuVersion < 1 || len(request.Items) < 1 || len(request.Items) > 100 ||
		request.Delivery.Address.City == "" || request.Delivery.Address.Street == "" || request.Delivery.Address.House == "" ||
		!phonePattern.MatchString(request.Delivery.Phone) || len(request.Delivery.Comment) > 500 {
		return invalidRequest("checkout fields are invalid")
	}
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if !shareduuid.IsValid(item.MenuItemID) || item.Quantity < 1 || item.Quantity > 100 {
			return invalidRequest("checkout item is invalid")
		}
		if _, exists := seen[item.MenuItemID]; exists {
			return invalidRequest("checkout items must be unique")
		}
		seen[item.MenuItemID] = struct{}{}
	}
	return nil
}

func checkoutHash(request domain.CreateOrderRequest) (string, error) {
	canonical := request
	canonical.Items = append([]domain.OrderItemRequest(nil), request.Items...)
	sort.Slice(canonical.Items, func(i, j int) bool { return canonical.Items[i].MenuItemID < canonical.Items[j].MenuItemID })
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}
