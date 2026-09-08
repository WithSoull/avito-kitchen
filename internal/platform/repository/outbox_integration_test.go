package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/platform/service"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestOutboxClaimLeaseRecoveryAndLateResponse(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "platform_outbox")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/platform/000001_init.up.sql", "migrations/platform/000002_separate_cancellation_state.up.sql",
		"migrations/platform/000003_menu_version_as_bigint.up.sql", "migrations/platform/000004_query_indexes.up.sql",
		"migrations/platform/000005_catalog_foundation.up.sql", "migrations/platform/000006_order_item_position.up.sql",
		"migrations/platform/000007_outbox_leases.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second, MaxConns: 6, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := repository.New(database, database)
	partner := service.NewPartner(repo)
	venueID := "00000000-0000-4000-8000-000000000001"
	if err := partner.ReplaceMenu(context.Background(), venueID, domain.MenuSnapshot{Version: 1, Categories: []domain.MenuCategory{{ExternalID: "main", Name: "Main", Items: []domain.MenuItem{{ExternalID: "item", Name: "Item", Price: domain.Money{Amount: 60000, Currency: "RUB"}, IsAvailable: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := partner.UpdateAvailability(context.Background(), venueID, domain.AvailabilityUpdate{IsAcceptingOrders: true}); err != nil {
		t.Fatal(err)
	}
	menu, err := service.NewCatalog(repo).GetMenu(context.Background(), venueID)
	if err != nil {
		t.Fatal(err)
	}
	request := domain.CreateOrderRequest{CustomerRef: "outbox-customer", VenueID: venueID, MenuVersion: 1,
		Items:    []domain.OrderItemRequest{{MenuItemID: menu.Categories[0].Items[0].ID, Quantity: 1}},
		Delivery: domain.Delivery{Address: domain.Address{City: "Москва", Street: "Тестовая", House: "1"}, Phone: "+79990000000"}}
	created, err := service.NewOrders(repo, "test-order-token-secret-at-least-32-bytes", time.Minute).CreateOrder(context.Background(), "outbox-idempotency-01", request)
	if err != nil {
		t.Fatal(err)
	}

	jobs := make([]domain.OutboxJob, 2)
	errorsFound := make([]error, 2)
	var group sync.WaitGroup
	for index := range jobs {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			jobs[index], errorsFound[index] = repo.ClaimOutbox(context.Background(), time.Minute)
		}(index)
	}
	group.Wait()
	claimed := -1
	for index, claimErr := range errorsFound {
		if claimErr == nil {
			claimed = index
		} else if !errors.Is(claimErr, repository.ErrNoOutboxJob) {
			t.Fatal(claimErr)
		}
	}
	if claimed < 0 || errorsFound[1-claimed] == nil {
		t.Fatalf("claims=%#v errors=%#v", jobs, errorsFound)
	}
	oldJob := jobs[claimed]
	if _, err := database.Exec(context.Background(), "UPDATE outbox SET locked_until=now()-interval '1 second' WHERE id=$1", oldJob.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.ClaimOutbox(context.Background(), time.Minute)
	if err != nil || recovered.LockToken == oldJob.LockToken {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	decision := domain.VenueOrderDecision{Result: "accepted", ExternalOrderID: "00000000-0000-4000-8000-000000000099", AcceptedItemsAmount: 60000, Currency: "RUB"}
	if err := repo.ResolveOutbox(context.Background(), recovered, decision); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResolveOutbox(context.Background(), oldJob, decision); !errors.Is(err, repository.ErrOutboxLeaseLost) {
		t.Fatalf("late response error=%v", err)
	}
	var status string
	var historyCount int
	if err := database.QueryRow(context.Background(), "SELECT status,(SELECT count(*) FROM order_status_history WHERE order_id=$1) FROM orders WHERE id=$1", created.Order.ID).Scan(&status, &historyCount); err != nil {
		t.Fatal(err)
	}
	if status != "accepted" || historyCount != 2 {
		t.Fatalf("status=%s history=%d", status, historyCount)
	}
}
