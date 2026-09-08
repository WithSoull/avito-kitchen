package service

import (
	"context"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
)

type ordersRepositoryFake struct {
	command domain.CreateOrderCommand
	err     error
}

func (fake *ordersRepositoryFake) CreateOrder(_ context.Context, command domain.CreateOrderCommand) (domain.Order, error) {
	fake.command = command
	return domain.Order{ID: command.OrderID}, fake.err
}
func (*ordersRepositoryFake) GetOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, repository.ErrOrderNotFound
}
func (*ordersRepositoryFake) CancelOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, repository.ErrOrderNotFound
}

func TestCreateOrderValidationAndPreparation(t *testing.T) {
	fake := &ordersRepositoryFake{}
	orders := NewOrders(fake, "test-order-token-secret-at-least-32-bytes", time.Minute)
	request := validCheckout()
	result, err := orders.CreateOrder(context.Background(), "idempotency-key-0001", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Order.ID == "" || result.TrackingToken == "" || fake.command.RequestHash == "" || fake.command.TrackingTokenHash == "" {
		t.Fatalf("result=%#v command=%#v", result, fake.command)
	}
	second, err := orders.CreateOrder(context.Background(), "idempotency-key-0001", request)
	if err != nil || second.TrackingToken != result.TrackingToken {
		t.Fatalf("deterministic retry token: result=%#v err=%v", second, err)
	}

	invalid := validCheckout()
	invalid.Items = append(invalid.Items, invalid.Items[0])
	if _, err := orders.CreateOrder(context.Background(), "idempotency-key-0002", invalid); errorCode(err) != "VALIDATION_FAILED" {
		t.Fatalf("duplicate item error = %v", err)
	}
	invalid = validCheckout()
	invalid.Delivery.Phone = "89990000000"
	if _, err := orders.CreateOrder(context.Background(), "idempotency-key-0003", invalid); errorCode(err) != "VALIDATION_FAILED" {
		t.Fatalf("phone error = %v", err)
	}

	fake.err = repository.ErrIdempotencyKeyReused
	if _, err := orders.CreateOrder(context.Background(), "idempotency-key-0004", validCheckout()); errorCode(err) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("idempotency error = %v", err)
	}
}

func validCheckout() domain.CreateOrderRequest {
	return domain.CreateOrderRequest{
		CustomerRef: "customer-1", VenueID: "00000000-0000-4000-8000-000000000001", MenuVersion: 1,
		Items:    []domain.OrderItemRequest{{MenuItemID: "00000000-0000-4000-8000-000000000002", Quantity: 1}},
		Delivery: domain.Delivery{Address: domain.Address{City: "Москва", Street: "Тестовая", House: "1"}, Phone: "+79990000000"},
	}
}
