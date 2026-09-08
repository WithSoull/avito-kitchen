package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
)

func TestCatalogResponsesMatchPublicContract(t *testing.T) {
	t.Parallel()

	const venueID = "00000000-0000-4000-8000-000000000001"
	updatedAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	venue := domain.Venue{
		ID: venueID, Name: "Тестовая кухня", Description: "Пицца",
		Address: "Москва", Currency: "RUB", MinimumOrderAmount: 50000,
		IsAcceptingOrders: true,
	}
	catalog := catalogFake{
		page:  domain.VenuePage{Items: []domain.Venue{venue}, NextCursor: "opaque-cursor"},
		venue: venue,
		menu: domain.Menu{VenueID: venueID, Version: 3, UpdatedAt: updatedAt, Categories: []domain.MenuCategory{{
			ExternalID: "pizza", Name: "Пицца", Items: []domain.MenuItem{{
				ID: "00000000-0000-4000-8000-000000000002", ExternalID: "margherita",
				Name: "Маргарита", Description: "Томаты и сыр",
				Price: domain.Money{Amount: 59000, Currency: "RUB"}, IsAvailable: true,
			}},
		}}},
	}
	handler := NewHandler(catalog, ordersFake{}, &partnerFake{}, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()

	tests := []struct {
		name     string
		path     string
		wantKeys []string
		check    func(*testing.T, map[string]any)
	}{
		{
			name: "venue page", path: "/api/v1/venues?limit=1",
			wantKeys: []string{"items", "next_cursor"},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				items, ok := body["items"].([]any)
				if !ok || len(items) != 1 {
					t.Fatalf("items = %#v", body["items"])
				}
				summary := items[0].(map[string]any)
				if _, leaksAddress := summary["address"]; leaksAddress {
					t.Fatal("venue summary must not contain address")
				}
			},
		},
		{
			name: "venue", path: "/api/v1/venues/" + venueID,
			wantKeys: []string{"id", "name", "description", "address", "currency", "minimum_order_amount", "is_accepting_orders"},
		},
		{
			name: "menu snapshot", path: "/api/v1/venues/" + venueID + "/menu",
			wantKeys: []string{"venue_id", "version", "updated_at", "categories"},
			check: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["version"] != float64(3) || body["updated_at"] != updatedAt.Format(time.RFC3339) {
					t.Fatalf("menu metadata = %#v", body)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			assertExactKeys(t, body, test.wantKeys)
			if test.check != nil {
				test.check(t, body)
			}
		})
	}
}

func assertExactKeys(t *testing.T, value map[string]any, keys []string) {
	t.Helper()
	if len(value) != len(keys) {
		t.Fatalf("keys = %#v, want %#v", value, keys)
	}
	for _, key := range keys {
		if _, exists := value[key]; !exists {
			t.Errorf("missing key %q in %#v", key, value)
		}
	}
}

type checkoutOrdersFake struct{ result domain.CreateOrderResult }

func (fake checkoutOrdersFake) CreateOrder(context.Context, string, domain.CreateOrderRequest) (domain.CreateOrderResult, error) {
	return fake.result, nil
}
func (checkoutOrdersFake) GetOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}
func (checkoutOrdersFake) CancelOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

func TestCreateOrderReturnsAcceptedLocationAndToken(t *testing.T) {
	t.Parallel()
	const orderID = "00000000-0000-4000-8000-000000000009"
	result := domain.CreateOrderResult{Order: domain.Order{ID: orderID, CustomerRef: "customer-1",
		VenueID: "00000000-0000-4000-8000-000000000001", Status: "pending_confirmation",
		CancellationStatus: "none", Items: []domain.OrderItem{}, Currency: "RUB", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		TrackingToken: "tracking-token-at-least-thirty-two-characters"}
	handler := NewHandler(catalogFake{}, checkoutOrdersFake{result: result}, &partnerFake{}, Config{MaxRequestBytes: 4096}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()
	body := `{"customer_ref":"customer-1","venue_id":"00000000-0000-4000-8000-000000000001","menu_version":1,"items":[{"menu_item_id":"00000000-0000-4000-8000-000000000002","quantity":1}],"delivery":{"address":{"city":"Москва","street":"Тестовая","house":"1"},"phone":"+79990000000"}}`
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idempotency-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || response.Header().Get("Location") != "/api/v1/orders/"+orderID {
		t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, payload, []string{"order", "tracking_token"})
}
