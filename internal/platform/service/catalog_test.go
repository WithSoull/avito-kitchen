package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
)

type catalogRepositoryFake struct {
	query domain.VenueListQuery
	page  domain.VenuePage
}

func (fake *catalogRepositoryFake) ListVenues(_ context.Context, query domain.VenueListQuery) (domain.VenuePage, error) {
	fake.query = query
	return fake.page, nil
}
func (*catalogRepositoryFake) GetVenue(context.Context, string) (domain.Venue, error) {
	return domain.Venue{}, repository.ErrVenueNotFound
}
func (*catalogRepositoryFake) GetMenu(context.Context, string) (domain.Menu, error) {
	return domain.Menu{}, repository.ErrMenuNotFound
}

func TestCatalogPaginationAndErrors(t *testing.T) {
	fake := &catalogRepositoryFake{page: domain.VenuePage{NextCursor: "00000000-0000-4000-8000-000000000002"}}
	catalog := NewCatalog(fake)
	page, err := catalog.ListVenues(context.Background(), " pizza ", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if fake.query.Search != "pizza" || fake.query.Limit != 10 || page.NextCursor == "" {
		t.Fatalf("query=%#v page=%#v", fake.query, page)
	}
	if _, err := catalog.ListVenues(context.Background(), "", "not-a-cursor", 10); errorCode(err) != "VALIDATION_FAILED" {
		t.Fatalf("cursor error = %v", err)
	}
	if _, err := catalog.GetVenue(context.Background(), "id"); errorCode(err) != "VENUE_NOT_FOUND" {
		t.Fatalf("venue error = %v", err)
	}
	if _, err := catalog.GetMenu(context.Background(), "id"); errorCode(err) != "MENU_NOT_FOUND" {
		t.Fatalf("menu error = %v", err)
	}
}

type partnerRepositoryFake struct {
	calls  int
	hashes []string
	err    error
}

func (fake *partnerRepositoryFake) ReplaceMenu(_ context.Context, _ string, _ domain.MenuSnapshot, hash string) error {
	fake.calls++
	fake.hashes = append(fake.hashes, hash)
	return fake.err
}
func (*partnerRepositoryFake) UpdateAvailability(context.Context, string, domain.AvailabilityUpdate) error {
	return nil
}
func (*partnerRepositoryFake) RecordOrderEvent(context.Context, string, string, domain.OrderEvent) error {
	return nil
}

func TestPartnerSnapshotValidationHashAndErrorMapping(t *testing.T) {
	fake := &partnerRepositoryFake{}
	partner := NewPartner(fake)
	snapshot := domain.MenuSnapshot{Version: 1, Categories: []domain.MenuCategory{{ExternalID: "pizza", Name: "Pizza", Items: []domain.MenuItem{{ExternalID: "margarita", Name: "Margarita", Price: domain.Money{Amount: 100, Currency: "RUB"}}}}}}
	if err := partner.ReplaceMenu(context.Background(), "venue", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := partner.ReplaceMenu(context.Background(), "venue", snapshot); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 || fake.hashes[0] != fake.hashes[1] {
		t.Fatalf("calls=%d hashes=%v", fake.calls, fake.hashes)
	}

	invalid := snapshot
	invalid.Categories = append(invalid.Categories, invalid.Categories[0])
	if err := partner.ReplaceMenu(context.Background(), "venue", invalid); errorCode(err) != "VALIDATION_FAILED" || fake.calls != 2 {
		t.Fatalf("validation error=%v calls=%d", err, fake.calls)
	}
	invalid = snapshot
	invalid.Categories[0].Items[0].Description = strings.Repeat("я", 2001)
	if err := partner.ReplaceMenu(context.Background(), "venue", invalid); errorCode(err) != "VALIDATION_FAILED" || fake.calls != 2 {
		t.Fatalf("description validation error=%v calls=%d", err, fake.calls)
	}
	snapshot.Categories[0].Items[0].Description = ""
	invalid = snapshot
	invalid.Categories[0].Items[0].Price.Currency = "₽"
	if err := partner.ReplaceMenu(context.Background(), "venue", invalid); errorCode(err) != "VALIDATION_FAILED" || fake.calls != 2 {
		t.Fatalf("currency validation error=%v calls=%d", err, fake.calls)
	}
	snapshot.Categories[0].Items[0].Price.Currency = "RUB"
	fake.err = repository.ErrStaleMenuVersion
	if err := partner.ReplaceMenu(context.Background(), "venue", snapshot); errorCode(err) != "STALE_MENU_VERSION" {
		t.Fatalf("stale error=%v", err)
	}
	fake.err = repository.ErrMenuVersionConflict
	if err := partner.ReplaceMenu(context.Background(), "venue", snapshot); errorCode(err) != "MENU_VERSION_CONFLICT" {
		t.Fatalf("conflict error=%v", err)
	}
}

func errorCode(err error) string {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return ""
	}
	return appErr.Code
}
