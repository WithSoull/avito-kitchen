package httptransport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
)

type catalogFake struct {
	page  domain.VenuePage
	venue domain.Venue
	menu  domain.Menu
}

func (fake catalogFake) ListVenues(context.Context, string, string, int) (domain.VenuePage, error) {
	return fake.page, nil
}
func (fake catalogFake) GetVenue(context.Context, string) (domain.Venue, error) {
	return fake.venue, nil
}
func (fake catalogFake) GetMenu(context.Context, string) (domain.Menu, error) { return fake.menu, nil }

type ordersFake struct{}

func (ordersFake) CreateOrder(context.Context, string, domain.CreateOrderRequest) (domain.CreateOrderResult, error) {
	return domain.CreateOrderResult{}, nil
}
func (ordersFake) GetOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}
func (ordersFake) CancelOrder(context.Context, string, string) (domain.Order, error) {
	return domain.Order{}, nil
}

type partnerFake struct {
	calls   int
	venueID string
}

func (fake *partnerFake) ReplaceMenu(_ context.Context, venueID string, _ domain.MenuSnapshot) error {
	fake.calls++
	fake.venueID = venueID
	return apperror.New(apperror.KindForbidden, "FORBIDDEN", "resource belongs to another venue")
}
func (*partnerFake) UpdateAvailability(context.Context, string, domain.AvailabilityUpdate) error {
	return nil
}
func (*partnerFake) RecordOrderEvent(context.Context, string, string, domain.OrderEvent) error {
	return nil
}

func TestPartnerAuthenticationAndPrincipal(t *testing.T) {
	partner := &partnerFake{}
	handler := NewHandler(catalogFake{}, ordersFake{}, partner, Config{PartnerToken: "valid-token", PartnerVenueID: "00000000-0000-4000-8000-000000000001", MaxRequestBytes: 1024}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()
	body := `{"version":1,"categories":[{"external_id":"food","name":"Food","items":[]}]}`

	unauthorized := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/partner/v1/menu", strings.NewReader(body))
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorized.Header.Set("Authorization", "Bearer wrong-token")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || partner.calls != 0 {
		t.Fatalf("status=%d calls=%d", unauthorizedResponse.Code, partner.calls)
	}

	forbidden := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/partner/v1/menu", strings.NewReader(body))
	forbidden.Header.Set("Content-Type", "application/json")
	forbidden.Header.Set("Authorization", "Bearer valid-token")
	forbiddenResponse := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenResponse, forbidden)
	if forbiddenResponse.Code != http.StatusForbidden || partner.calls != 1 {
		t.Fatalf("status=%d calls=%d", forbiddenResponse.Code, partner.calls)
	}
	if partner.venueID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("venue ID = %s", partner.venueID)
	}
}
