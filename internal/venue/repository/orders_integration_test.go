package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/repository"
	"github.com/WithSoull/avito-kitchen/internal/venue/service"
)

func TestOrderDecisionIsConcurrentSafeAndIdempotent(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "venue_orders")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/venue/000001_init.up.sql", "migrations/venue/000002_menu_version_as_bigint.up.sql",
		"migrations/venue/000003_query_indexes.up.sql", "migrations/venue/000004_seed_menu.up.sql",
		"migrations/venue/000005_order_acceptance.up.sql",
		"migrations/venue/000006_callback_leases.up.sql",
		"migrations/venue/000007_cancellation_decision.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second, MaxConns: 6, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(context.Background(), "UPDATE venue_menu_items SET stock_quantity=1 WHERE external_id='margherita'"); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database, database)
	orders := service.NewOrders(repo)
	commands := []domain.OrderCommand{
		orderCommand("00000000-0000-4000-8000-000000000101", 59000),
		orderCommand("00000000-0000-4000-8000-000000000102", 59000),
	}
	decisions := make([]domain.OrderDecision, 2)
	decisionErrors := make([]error, 2)
	var group sync.WaitGroup
	for index := range commands {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			decisions[index], decisionErrors[index] = orders.DecideOrder(context.Background(), "idempotency-key-0001", commands[index])
		}(index)
	}
	group.Wait()
	accepted, rejected := 0, 0
	acceptedIndex := 0
	for index, decision := range decisions {
		if decisionErrors[index] != nil {
			t.Fatal(decisionErrors[index])
		}
		if decision.Result == "accepted" {
			accepted++
			acceptedIndex = index
		} else if decision.Reason == "items_unavailable" {
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("decisions = %#v", decisions)
	}
	repeated, err := orders.DecideOrder(context.Background(), "another-idempotency-key", commands[acceptedIndex])
	if err != nil || repeated.Result != decisions[acceptedIndex].Result || repeated.ExternalOrderID != decisions[acceptedIndex].ExternalOrderID {
		t.Fatalf("repeat=%#v original=%#v err=%v", repeated, decisions[acceptedIndex], err)
	}
	var stock int
	if err := database.QueryRow(context.Background(), "SELECT stock_quantity FROM venue_menu_items WHERE external_id='margherita'").Scan(&stock); err != nil || stock != 0 {
		t.Fatalf("stock=%d err=%v", stock, err)
	}

	priceCommand := orderCommand("00000000-0000-4000-8000-000000000103", 1)
	priceDecision, err := orders.DecideOrder(context.Background(), "idempotency-price-01", priceCommand)
	if err != nil || priceDecision.Reason != "menu_changed" {
		t.Fatalf("price decision=%#v err=%v", priceDecision, err)
	}
	if _, err := database.Exec(context.Background(), "UPDATE venue_menu_items SET price_amount=1 WHERE external_id='margherita'"); err != nil {
		t.Fatal(err)
	}
	repeated, err = orders.DecideOrder(context.Background(), "idempotency-price-02", priceCommand)
	if err != nil || repeated.Reason != "menu_changed" {
		t.Fatalf("stable rejection=%#v err=%v", repeated, err)
	}

	managed, err := orders.ListOrders(context.Background())
	if err != nil || len(managed) != 3 {
		t.Fatalf("managed=%#v err=%v", managed, err)
	}
	acceptedOrder, err := orders.GetOrder(context.Background(), decisions[acceptedIndex].ExternalOrderID)
	if err != nil || acceptedOrder.Status != "accepted" || len(acceptedOrder.Items) != 1 {
		t.Fatalf("accepted order=%#v err=%v", acceptedOrder, err)
	}

	preparing, err := orders.StartPreparing(context.Background(), acceptedOrder.ID)
	if err != nil || preparing.Status != "preparing" {
		t.Fatalf("preparing=%#v err=%v", preparing, err)
	}
	preparingAgain, err := orders.StartPreparing(context.Background(), acceptedOrder.ID)
	if err != nil || preparingAgain.Status != "preparing" {
		t.Fatalf("idempotent preparing=%#v err=%v", preparingAgain, err)
	}
	ready, err := orders.MarkReady(context.Background(), acceptedOrder.ID)
	if err != nil || ready.Status != "ready" {
		t.Fatalf("ready=%#v err=%v", ready, err)
	}
	completed, err := orders.Complete(context.Background(), acceptedOrder.ID)
	if err != nil || completed.Status != "completed" {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	if _, err := orders.StartPreparing(context.Background(), acceptedOrder.ID); err == nil {
		t.Fatal("completed order must reject a backward transition")
	}
	var outboxCount int
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM venue_outbox WHERE venue_order_id=$1", acceptedOrder.ID).Scan(&outboxCount); err != nil || outboxCount != 3 {
		t.Fatalf("outbox count=%d err=%v", outboxCount, err)
	}

	first, err := repo.ClaimCallback(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var firstEvent domain.OrderEvent
	if err := json.Unmarshal(first.Payload, &firstEvent); err != nil || firstEvent.Sequence != 1 || firstEvent.Type != "order_preparing" {
		t.Fatalf("first event=%#v err=%v", firstEvent, err)
	}
	if _, err := repo.ClaimCallback(context.Background(), time.Second); !errors.Is(err, repository.ErrNoCallbackJob) {
		t.Fatalf("later sequence claimed before first completed: %v", err)
	}
	if err := repo.RetryCallback(context.Background(), first, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := repo.ClaimCallback(context.Background(), time.Second)
	if err != nil || retried.ID != first.ID || retried.Attempts != 2 {
		t.Fatalf("retried=%#v err=%v", retried, err)
	}
	if err := repo.CompleteCallback(context.Background(), retried); err != nil {
		t.Fatal(err)
	}
	for expectedSequence := int64(2); expectedSequence <= 3; expectedSequence++ {
		job, err := repo.ClaimCallback(context.Background(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		var event domain.OrderEvent
		if err := json.Unmarshal(job.Payload, &event); err != nil || event.Sequence != expectedSequence {
			t.Fatalf("event=%#v err=%v", event, err)
		}
		if err := repo.CompleteCallback(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
}

func orderCommand(id string, price int64) domain.OrderCommand {
	return domain.OrderCommand{PlatformOrderID: id, MenuVersion: 1, Items: []domain.OrderItem{{
		ExternalItemID: "margherita", Name: "Маргарита", Quantity: 1, ExpectedUnitPrice: price,
	}}}
}
