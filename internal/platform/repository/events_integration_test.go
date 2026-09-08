package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestRecordOrderEventIsOrderedIdempotentAndAtomic(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "platform_events")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/platform/000001_init.up.sql", "migrations/platform/000002_separate_cancellation_state.up.sql",
		"migrations/platform/000003_menu_version_as_bigint.up.sql", "migrations/platform/000004_query_indexes.up.sql",
		"migrations/platform/000005_catalog_foundation.up.sql", "migrations/platform/000006_order_item_position.up.sql",
		"migrations/platform/000007_outbox_leases.up.sql", "migrations/platform/000008_order_event_constraints.up.sql",
	)
	database, err := postgres.Open(context.Background(), postgres.Config{URL: databaseURL, ConnectTimeout: time.Second, QueryTimeout: time.Second, MaxConns: 4, MinConns: 1, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const orderID = "00000000-0000-4000-8000-000000000201"
	const venueID = "00000000-0000-4000-8000-000000000001"
	if _, err := database.Exec(context.Background(), `INSERT INTO orders
		(id,customer_ref,venue_id,status,menu_version,items_amount,delivery_amount,total_amount,currency,delivery,
		 idempotency_key,request_hash,tracking_token_hash,confirmation_deadline)
		VALUES ($1,'events-test',$2,'accepted',1,100,0,100,'RUB','{}','events-key','hash','token-hash',now()+interval '1 minute')`, orderID, venueID); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database, database)
	now := time.Now().UTC().Truncate(time.Microsecond)
	gap := domain.OrderEvent{EventID: "00000000-0000-4000-8000-000000000212", Sequence: 2, Type: "order_ready", OccurredAt: now}
	if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, gap); !errors.Is(err, repository.ErrEventSequenceConflict) {
		t.Fatalf("gap error=%v", err)
	}
	if err := repo.RecordOrderEvent(context.Background(), "00000000-0000-4000-8000-000000000099", orderID, gap); !errors.Is(err, repository.ErrOrderVenueMismatch) {
		t.Fatalf("foreign venue error=%v", err)
	}

	events := []domain.OrderEvent{
		{EventID: "00000000-0000-4000-8000-000000000211", Sequence: 1, Type: "order_preparing", OccurredAt: now},
		{EventID: "00000000-0000-4000-8000-000000000212", Sequence: 2, Type: "order_ready", OccurredAt: now.Add(time.Second)},
		{EventID: "00000000-0000-4000-8000-000000000213", Sequence: 3, Type: "order_completed", OccurredAt: now.Add(2 * time.Second)},
	}
	if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, events[0]); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, events[0]); err != nil {
		t.Fatalf("duplicate event: %v", err)
	}
	conflict := events[0]
	conflict.Type = "order_ready"
	if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, conflict); !errors.Is(err, repository.ErrEventConflict) {
		t.Fatalf("event id conflict=%v", err)
	}
	wrongTransition := events[1]
	wrongTransition.Type = "order_completed"
	if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, wrongTransition); !errors.Is(err, repository.ErrInvalidOrderTransition) {
		t.Fatalf("transition error=%v", err)
	}
	for _, event := range events[1:] {
		if err := repo.RecordOrderEvent(context.Background(), venueID, orderID, event); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var sequence int64
	if err := database.QueryRow(context.Background(), "SELECT status,venue_event_sequence FROM orders WHERE id=$1", orderID).Scan(&status, &sequence); err != nil || status != "completed" || sequence != 3 {
		t.Fatalf("status=%s sequence=%d err=%v", status, sequence, err)
	}
	var eventCount, historyCount int
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM partner_order_events WHERE order_id=$1", orderID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM order_status_history WHERE order_id=$1", orderID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 3 || historyCount != 3 {
		t.Fatalf("events=%d history=%d", eventCount, historyCount)
	}
}
