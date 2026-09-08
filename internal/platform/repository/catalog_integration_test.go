package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/platform/service"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestCatalogMenuSyncVersioningAndSoftDelete(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "platform_catalog")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/platform/000001_init.up.sql", "migrations/platform/000002_separate_cancellation_state.up.sql",
		"migrations/platform/000003_menu_version_as_bigint.up.sql", "migrations/platform/000004_query_indexes.up.sql",
		"migrations/platform/000005_catalog_foundation.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second, MaxConns: 4, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := repository.New(database, database)
	partner := service.NewPartner(repo)
	catalog := service.NewCatalog(repo)
	venueID := "00000000-0000-4000-8000-000000000001"

	snapshot := domain.MenuSnapshot{Version: 1, Categories: []domain.MenuCategory{{ExternalID: "pizza", Name: "Pizza", Items: []domain.MenuItem{
		{ExternalID: "margherita", Name: "Margherita", Price: domain.Money{Amount: 59000, Currency: "RUB"}, IsAvailable: true},
		{ExternalID: "pepperoni", Name: "Pepperoni", Price: domain.Money{Amount: 69000, Currency: "RUB"}, IsAvailable: true},
	}}}}
	if err := partner.ReplaceMenu(context.Background(), venueID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := partner.ReplaceMenu(context.Background(), venueID, snapshot); err != nil {
		t.Fatalf("idempotent repeat: %v", err)
	}
	menu, err := catalog.GetMenu(context.Background(), venueID)
	if err != nil {
		t.Fatal(err)
	}
	if menu.Version != 1 || len(menu.Categories) != 1 || len(menu.Categories[0].Items) != 2 {
		t.Fatalf("menu=%#v", menu)
	}

	changedSameVersion := snapshot
	changedSameVersion.Categories[0].Items[0].Name = "Changed"
	if err := partner.ReplaceMenu(context.Background(), venueID, changedSameVersion); code(err) != "MENU_VERSION_CONFLICT" {
		t.Fatalf("same version error=%v", err)
	}
	snapshot.Version = 2
	snapshot.Categories[0].Items = snapshot.Categories[0].Items[:1]
	if err := partner.ReplaceMenu(context.Background(), venueID, snapshot); err != nil {
		t.Fatal(err)
	}
	stale := snapshot
	stale.Version = 1
	if err := partner.ReplaceMenu(context.Background(), venueID, stale); code(err) != "STALE_MENU_VERSION" {
		t.Fatalf("stale error=%v", err)
	}
	menu, err = catalog.GetMenu(context.Background(), venueID)
	if err != nil {
		t.Fatal(err)
	}
	if len(menu.Categories[0].Items) != 1 {
		t.Fatalf("active items=%d", len(menu.Categories[0].Items))
	}
	var inactive int
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM menu_items WHERE venue_id=$1 AND NOT is_active", venueID).Scan(&inactive); err != nil {
		t.Fatal(err)
	}
	if inactive != 1 {
		t.Fatalf("inactive items=%d", inactive)
	}
	if err := partner.UpdateAvailability(context.Background(), venueID, domain.AvailabilityUpdate{IsAcceptingOrders: true}); err != nil {
		t.Fatal(err)
	}
	venue, err := catalog.GetVenue(context.Background(), venueID)
	if err != nil || !venue.IsAcceptingOrders {
		t.Fatalf("venue=%#v err=%v", venue, err)
	}
}

func code(err error) string {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return ""
	}
	return appErr.Code
}
