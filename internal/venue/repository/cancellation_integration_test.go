package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
)

func TestVenueCancellationIsIdempotentAndSerializedWithPreparing(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "venue_cancellation")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/venue/000001_init.up.sql", "migrations/venue/000002_menu_version_as_bigint.up.sql",
		"migrations/venue/000003_query_indexes.up.sql", "migrations/venue/000004_seed_menu.up.sql",
		"migrations/venue/000005_order_acceptance.up.sql", "migrations/venue/000006_callback_leases.up.sql",
		"migrations/venue/000007_cancellation_decision.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second, MaxConns: 6, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(context.Background(), "UPDATE venue_menu_items SET stock_quantity=2 WHERE external_id='margherita'"); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database, database)
	firstCommand := orderCommand("00000000-0000-4000-8000-000000000301", 59000)
	secondCommand := orderCommand("00000000-0000-4000-8000-000000000302", 59000)
	first, err := repo.DecideOrder(context.Background(), "first-cancellation", firstCommand)
	if err != nil || first.Result != "accepted" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := repo.DecideOrder(context.Background(), "race-cancellation", secondCommand)
	if err != nil || second.Result != "accepted" {
		t.Fatalf("second=%#v err=%v", second, err)
	}

	decision, err := repo.CancelOrder(context.Background(), firstCommand.PlatformOrderID)
	if err != nil || decision.Result != "cancelled" {
		t.Fatalf("cancel=%#v err=%v", decision, err)
	}
	repeated, err := repo.CancelOrder(context.Background(), firstCommand.PlatformOrderID)
	if err != nil || repeated != decision {
		t.Fatalf("repeat=%#v decision=%#v err=%v", repeated, decision, err)
	}
	var stock int
	if err := database.QueryRow(context.Background(), "SELECT stock_quantity FROM venue_menu_items WHERE external_id='margherita'").Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("stock after idempotent cancel=%d err=%v", stock, err)
	}

	var group sync.WaitGroup
	group.Add(2)
	var raceDecision domain.CancellationDecision
	var cancelErr, preparingErr error
	go func() {
		defer group.Done()
		raceDecision, cancelErr = repo.CancelOrder(context.Background(), secondCommand.PlatformOrderID)
	}()
	go func() {
		defer group.Done()
		_, preparingErr = repo.StartPreparing(context.Background(), second.ExternalOrderID)
	}()
	group.Wait()
	if cancelErr != nil {
		t.Fatal(cancelErr)
	}
	switch raceDecision.Result {
	case "cancelled":
		if preparingErr == nil {
			t.Fatal("preparing must lose when cancellation wins")
		}
	case "rejected":
		if preparingErr != nil || raceDecision.CurrentStatus != "preparing" {
			t.Fatalf("race decision=%#v preparing error=%v", raceDecision, preparingErr)
		}
	default:
		t.Fatalf("unexpected race decision=%#v", raceDecision)
	}

	if _, err := database.Exec(context.Background(), "UPDATE venue_menu_items SET stock_quantity=1 WHERE external_id='margherita'"); err != nil {
		t.Fatal(err)
	}
	lateCommand := orderCommand("00000000-0000-4000-8000-000000000303", 59000)
	late, err := repo.DecideOrder(context.Background(), "late-cancellation", lateCommand)
	if err != nil || late.Result != "accepted" {
		t.Fatalf("late=%#v err=%v", late, err)
	}
	if _, err := repo.StartPreparing(context.Background(), late.ExternalOrderID); err != nil {
		t.Fatal(err)
	}
	lateDecision, err := repo.CancelOrder(context.Background(), lateCommand.PlatformOrderID)
	if err != nil || lateDecision.Result != "rejected" || lateDecision.CurrentStatus != "preparing" {
		t.Fatalf("late cancellation=%#v err=%v", lateDecision, err)
	}
	if _, err := repo.MarkReady(context.Background(), late.ExternalOrderID); err != nil {
		t.Fatal(err)
	}
	stable, err := repo.CancelOrder(context.Background(), lateCommand.PlatformOrderID)
	if err != nil || stable != lateDecision {
		t.Fatalf("stable late decision=%#v original=%#v err=%v", stable, lateDecision, err)
	}
}
