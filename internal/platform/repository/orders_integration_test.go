package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/platform/security/ordertoken"
	"github.com/WithSoull/avito-kitchen/internal/platform/service"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	"github.com/WithSoull/avito-kitchen/internal/testutil/postgrestest"
)

func TestCreateOrderIsAtomicIdempotentAndSnapshotsPrice(t *testing.T) {
	databaseURL := postgrestest.CreateDatabase(t, "platform_orders")
	postgrestest.ApplySQLFiles(t, databaseURL,
		"migrations/platform/000001_init.up.sql", "migrations/platform/000002_separate_cancellation_state.up.sql",
		"migrations/platform/000003_menu_version_as_bigint.up.sql", "migrations/platform/000004_query_indexes.up.sql",
		"migrations/platform/000005_catalog_foundation.up.sql", "migrations/platform/000006_order_item_position.up.sql",
		"migrations/platform/000007_outbox_leases.up.sql", "migrations/platform/000008_order_event_constraints.up.sql",
		"migrations/platform/000009_cancellation_outbox.up.sql",
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
	snapshot := domain.MenuSnapshot{Version: 1, Categories: []domain.MenuCategory{{ExternalID: "pizza", Name: "Pizza", Items: []domain.MenuItem{{ExternalID: "margherita", Name: "Margherita", Price: domain.Money{Amount: 60000, Currency: "RUB"}, IsAvailable: true}}}}}
	if err := partner.ReplaceMenu(context.Background(), venueID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := partner.UpdateAvailability(context.Background(), venueID, domain.AvailabilityUpdate{IsAcceptingOrders: true}); err != nil {
		t.Fatal(err)
	}
	menu, err := catalog.GetMenu(context.Background(), venueID)
	if err != nil {
		t.Fatal(err)
	}
	itemID := menu.Categories[0].Items[0].ID
	request := domain.CreateOrderRequest{CustomerRef: "customer-1", VenueID: venueID, MenuVersion: 1,
		Items:    []domain.OrderItemRequest{{MenuItemID: itemID, Quantity: 2}},
		Delivery: domain.Delivery{Address: domain.Address{City: "Москва", Street: "Тестовая", House: "1"}, Phone: "+79990000000"}}
	orders := service.NewOrders(repo, "test-order-token-secret-at-least-32-bytes", time.Minute)
	first, err := orders.CreateOrder(context.Background(), "idempotency-key-0001", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Order.Status != "pending_confirmation" || first.Order.TotalAmount != 120000 || first.Order.Items[0].UnitPrice != 60000 {
		t.Fatalf("created order = %#v", first.Order)
	}
	if _, err := ordertoken.Hash(first.TrackingToken); err != nil {
		t.Fatalf("tracking token: %v", err)
	}
	second, err := orders.CreateOrder(context.Background(), "idempotency-key-0001", request)
	if err != nil || second.Order.ID != first.Order.ID || second.TrackingToken != first.TrackingToken || !second.Order.CreatedAt.Equal(first.Order.CreatedAt) {
		t.Fatalf("retry=%#v err=%v", second, err)
	}
	changed := request
	changed.Items = []domain.OrderItemRequest{{MenuItemID: itemID, Quantity: 3}}
	if _, err := orders.CreateOrder(context.Background(), "idempotency-key-0001", changed); code(err) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("changed retry error = %v", err)
	}
	if _, err := database.Exec(context.Background(), `
		CREATE FUNCTION reject_history_for_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced rollback'; END $$;
		CREATE TRIGGER reject_history_for_test BEFORE INSERT ON order_status_history FOR EACH ROW EXECUTE FUNCTION reject_history_for_test()`); err != nil {
		t.Fatal(err)
	}
	rollbackRequest := request
	rollbackRequest.CustomerRef = "customer-rollback"
	if _, err := orders.CreateOrder(context.Background(), "idempotency-key-rollback", rollbackRequest); err == nil {
		t.Fatal("forced history failure must fail checkout")
	}
	if _, err := database.Exec(context.Background(), "DROP TRIGGER reject_history_for_test ON order_status_history; DROP FUNCTION reject_history_for_test()"); err != nil {
		t.Fatal(err)
	}
	failedRequest := request
	failedRequest.CustomerRef = "customer-failures"
	if _, err := database.Exec(context.Background(), "UPDATE venues SET is_accepting_orders=false WHERE id=$1", venueID); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.CreateOrder(context.Background(), "idempotency-closed-01", failedRequest); code(err) != "VENUE_CLOSED" {
		t.Fatalf("closed venue error = %v", err)
	}
	if _, err := database.Exec(context.Background(), "UPDATE venues SET is_accepting_orders=true WHERE id=$1", venueID); err != nil {
		t.Fatal(err)
	}
	failedRequest.MenuVersion = 2
	if _, err := orders.CreateOrder(context.Background(), "idempotency-menu-0001", failedRequest); code(err) != "MENU_CHANGED" {
		t.Fatalf("menu version error = %v", err)
	}
	failedRequest.MenuVersion = 1
	if _, err := database.Exec(context.Background(), "UPDATE menu_items SET is_available=false WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.CreateOrder(context.Background(), "idempotency-items-001", failedRequest); code(err) != "ITEMS_UNAVAILABLE" {
		t.Fatalf("unavailable item error = %v", err)
	}
	if _, err := database.Exec(context.Background(), "UPDATE menu_items SET is_available=true WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(context.Background(), "UPDATE venues SET minimum_order_amount=200000 WHERE id=$1", venueID); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.CreateOrder(context.Background(), "idempotency-minimum-01", failedRequest); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("minimum error = %v", err)
	}
	if _, err := database.Exec(context.Background(), "UPDATE venues SET minimum_order_amount=50000 WHERE id=$1", venueID); err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec(context.Background(), "UPDATE menu_items SET price_amount=99999 WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	var ordersCount, itemsCount, historyCount, outboxCount int
	if err := database.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM orders), (SELECT count(*) FROM order_items),
		(SELECT count(*) FROM order_status_history), (SELECT count(*) FROM outbox)`).Scan(&ordersCount, &itemsCount, &historyCount, &outboxCount); err != nil {
		t.Fatal(err)
	}
	if ordersCount != 1 || itemsCount != 1 || historyCount != 1 || outboxCount != 1 {
		t.Fatalf("counts=%d/%d/%d/%d", ordersCount, itemsCount, historyCount, outboxCount)
	}
	var snapshotPrice int64
	if err := database.QueryRow(context.Background(), "SELECT unit_price FROM order_items WHERE order_id=$1", first.Order.ID).Scan(&snapshotPrice); err != nil || snapshotPrice != 60000 {
		t.Fatalf("snapshot price=%d err=%v", snapshotPrice, err)
	}
	tokenHash, err := ordertoken.Hash(first.TrackingToken)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := orders.GetOrder(context.Background(), first.Order.ID, tokenHash)
	if err != nil || loaded.ID != first.Order.ID || len(loaded.Items) != 1 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if _, err := orders.GetOrder(context.Background(), first.Order.ID, "wrong-token-hash"); code(err) != "ORDER_NOT_FOUND" {
		t.Fatalf("wrong token error=%v", err)
	}
	cancelled, err := orders.CancelOrder(context.Background(), first.Order.ID, tokenHash)
	if err != nil || cancelled.Status != "cancelled" || cancelled.CancellationStatus != "confirmed" {
		t.Fatalf("local cancellation=%#v err=%v", cancelled, err)
	}
	repeatedCancellation, err := orders.CancelOrder(context.Background(), first.Order.ID, tokenHash)
	if err != nil || repeatedCancellation.Status != "cancelled" {
		t.Fatalf("repeated cancellation=%#v err=%v", repeatedCancellation, err)
	}
	var confirmationProcessed bool
	if err := database.QueryRow(context.Background(), "SELECT processed_at IS NOT NULL FROM outbox WHERE aggregate_id=$1", first.Order.ID).Scan(&confirmationProcessed); err != nil || !confirmationProcessed {
		t.Fatalf("confirmation processed=%v err=%v", confirmationProcessed, err)
	}

	acceptedRequest := request
	acceptedRequest.CustomerRef = "customer-accepted-cancel"
	accepted, err := orders.CreateOrder(context.Background(), "idempotency-cancel-01", acceptedRequest)
	if err != nil {
		t.Fatal(err)
	}
	acceptedHash, _ := ordertoken.Hash(accepted.TrackingToken)
	if _, err := database.Exec(context.Background(), `UPDATE orders SET status='accepted',external_order_id='venue-order' WHERE id=$1`, accepted.Order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(context.Background(), `UPDATE outbox SET processed_at=now() WHERE aggregate_id=$1 AND event_type='confirm_order'`, accepted.Order.ID); err != nil {
		t.Fatal(err)
	}
	requested, err := orders.CancelOrder(context.Background(), accepted.Order.ID, acceptedHash)
	if err != nil || requested.Status != "accepted" || requested.CancellationStatus != "requested" {
		t.Fatalf("requested cancellation=%#v err=%v", requested, err)
	}
	job, err := repo.ClaimOutbox(context.Background(), time.Second)
	if err != nil || job.EventType != "cancel_order" || job.OrderID != accepted.Order.ID {
		t.Fatalf("cancellation job=%#v err=%v", job, err)
	}
	if err := repo.ResolveCancellation(context.Background(), job, domain.VenueCancellationDecision{Result: "cancelled", CurrentStatus: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	resolved, err := orders.GetOrder(context.Background(), accepted.Order.ID, acceptedHash)
	if err != nil || resolved.Status != "cancelled" || resolved.CancellationStatus != "confirmed" {
		t.Fatalf("resolved cancellation=%#v err=%v", resolved, err)
	}
	cancelEvent := domain.OrderEvent{EventID: "00000000-0000-4000-8000-000000000401", Sequence: 1, Type: "order_cancelled", OccurredAt: time.Now().UTC()}
	if err := repo.RecordOrderEvent(context.Background(), venueID, accepted.Order.ID, cancelEvent); err != nil {
		t.Fatal(err)
	}
	var cancelledHistory int
	if err := database.QueryRow(context.Background(), `SELECT count(*) FROM order_status_history
		WHERE order_id=$1 AND status='cancelled'`, accepted.Order.ID).Scan(&cancelledHistory); err != nil || cancelledHistory != 1 {
		t.Fatalf("cancelled history=%d err=%v", cancelledHistory, err)
	}
}
