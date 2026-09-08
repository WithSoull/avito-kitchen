package platformclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/integration/platformclient"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestClientPublishesMappedMenuAndAvailability(t *testing.T) {
	requests := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Authorization") != "Bearer partner-secret" || r.Header.Get("X-Request-ID") == "" {
			t.Errorf("missing auth or request ID")
		}
		switch r.URL.Path {
		case "/partner/v1/menu":
			var payload struct {
				Version    int64 `json:"version"`
				Categories []struct {
					Items []struct {
						ExternalID string `json:"external_id"`
					} `json:"items"`
				} `json:"categories"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.Version != 7 || payload.Categories[0].Items[0].ExternalID != "pizza" {
				t.Errorf("payload=%#v", payload)
			}
		case "/partner/v1/availability":
			var payload struct {
				IsAcceptingOrders bool `json:"is_accepting_orders"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || !payload.IsAcceptingOrders {
				t.Errorf("payload=%#v err=%v", payload, err)
			}
		case "/partner/v1/orders/00000000-0000-4000-8000-000000000201/events":
			var payload domain.OrderEvent
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Sequence != 1 || payload.Type != "order_preparing" {
				t.Errorf("payload=%#v err=%v", payload, err)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	client, err := platformclient.New("http://platform.test", "partner-secret", time.Second, platformclient.WithRoundTripper(transport))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.MenuSnapshot{Version: 7, Categories: []domain.MenuCategory{{ExternalID: "main", Name: "Main", Items: []domain.MenuItem{{ExternalID: "pizza", Name: "Pizza", Price: domain.Money{Amount: 100, Currency: "RUB"}, IsAvailable: true}}}}}
	if err := client.ReplaceMenu(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateAvailability(context.Background(), domain.Availability{IsAcceptingOrders: true}); err != nil {
		t.Fatal(err)
	}
	event := domain.OrderEvent{EventID: "00000000-0000-4000-8000-000000000211", Sequence: 1, Type: "order_preparing", OccurredAt: time.Now()}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendOrderEvent(context.Background(), domain.CallbackJob{PlatformOrderID: "00000000-0000-4000-8000-000000000201", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestClientRejectsUnexpectedStatus(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusConflict, Body: io.NopCloser(strings.NewReader("private upstream detail")), Header: make(http.Header), Request: r}, nil
	})
	client, err := platformclient.New("http://platform.test", "partner-secret", time.Second, platformclient.WithRoundTripper(transport))
	if err != nil {
		t.Fatal(err)
	}
	err = client.UpdateAvailability(context.Background(), domain.Availability{})
	if err == nil || err.Error() != "platform returned HTTP 409" {
		t.Fatalf("error=%v", err)
	}
}
