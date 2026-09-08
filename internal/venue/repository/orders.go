package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

func (r *Repository) DecideOrder(ctx context.Context, _ string, command domain.OrderCommand) (domain.OrderDecision, error) {
	var decision domain.OrderDecision
	err := r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", command.PlatformOrderID); err != nil {
			return fmt.Errorf("lock platform order: %w", err)
		}
		existing, err := findDecision(ctx, tx, command.PlatformOrderID)
		if err == nil {
			decision = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		var accepting bool
		if err := tx.QueryRow(ctx, "SELECT is_accepting_orders FROM venue_settings WHERE singleton=true FOR SHARE").Scan(&accepting); err != nil {
			return fmt.Errorf("load venue availability: %w", err)
		}
		var currentVersion int64
		if err := tx.QueryRow(ctx, "SELECT max(version) FROM venue_menu_versions").Scan(&currentVersion); err != nil {
			return fmt.Errorf("load venue menu version: %w", err)
		}
		if !accepting {
			decision = domain.OrderDecision{Result: "rejected", Reason: "venue_closed", CurrentMenuVersion: currentVersion}
			return insertRejected(ctx, tx, command, decision)
		}
		if command.MenuVersion != currentVersion {
			decision = domain.OrderDecision{Result: "rejected", Reason: "menu_changed", CurrentMenuVersion: currentVersion}
			return insertRejected(ctx, tx, command, decision)
		}

		externalIDs := make([]string, len(command.Items))
		for index, item := range command.Items {
			externalIDs[index] = item.ExternalItemID
		}
		rows, err := tx.Query(ctx, `SELECT external_id,name,price_amount,currency,stock_quantity,is_available
			FROM venue_menu_items WHERE external_id=ANY($1::text[]) ORDER BY external_id FOR UPDATE`, externalIDs)
		if err != nil {
			return fmt.Errorf("lock venue menu items: %w", err)
		}
		type menuItem struct {
			name, currency string
			price          int64
			stock          *int
			available      bool
		}
		menu := make(map[string]menuItem, len(externalIDs))
		for rows.Next() {
			var id string
			var item menuItem
			if err := rows.Scan(&id, &item.name, &item.price, &item.currency, &item.stock, &item.available); err != nil {
				rows.Close()
				return fmt.Errorf("scan venue menu item: %w", err)
			}
			menu[id] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate venue menu items: %w", err)
		}
		rows.Close()

		unavailable := make([]string, 0)
		managedItems := make([]domain.ManagedOrderItem, len(command.Items))
		var total int64
		currency := ""
		for index, requested := range command.Items {
			item, exists := menu[requested.ExternalItemID]
			if exists && (item.price != requested.ExpectedUnitPrice || (currency != "" && currency != item.currency)) {
				decision = domain.OrderDecision{Result: "rejected", Reason: "menu_changed", CurrentMenuVersion: currentVersion}
				return insertRejected(ctx, tx, command, decision)
			}
			if !exists || !item.available || (item.stock != nil && *item.stock < requested.Quantity) {
				unavailable = append(unavailable, requested.ExternalItemID)
				continue
			}
			currency = item.currency
			if item.price > 0 && int64(requested.Quantity) > math.MaxInt64/item.price {
				return fmt.Errorf("venue order total overflow")
			}
			lineTotal := item.price * int64(requested.Quantity)
			if total > math.MaxInt64-lineTotal {
				return fmt.Errorf("venue order total overflow")
			}
			total += lineTotal
			managedItems[index] = domain.ManagedOrderItem{ExternalItemID: requested.ExternalItemID, Name: item.name, Quantity: requested.Quantity, UnitPrice: item.price, TotalPrice: lineTotal}
		}
		if len(unavailable) > 0 {
			decision = domain.OrderDecision{Result: "rejected", Reason: "items_unavailable", CurrentMenuVersion: currentVersion, UnavailableItemIDs: unavailable}
			return insertRejected(ctx, tx, command, decision)
		}
		for _, item := range command.Items {
			if _, err := tx.Exec(ctx, `UPDATE venue_menu_items SET stock_quantity=stock_quantity-$2,updated_at=now()
				WHERE external_id=$1 AND stock_quantity IS NOT NULL`, item.ExternalItemID, item.Quantity); err != nil {
				return fmt.Errorf("reserve venue stock: %w", err)
			}
		}
		orderID, err := shareduuid.New()
		if err != nil {
			return err
		}
		decision = domain.OrderDecision{Result: "accepted", ExternalOrderID: orderID, AcceptedItemsAmount: total, Currency: currency}
		details, _ := json.Marshal(decision)
		if _, err := tx.Exec(ctx, `INSERT INTO venue_orders
			(id,platform_order_id,menu_version,decision,status,items_amount,currency,decision_details)
			VALUES ($1,$2,$3,'accepted','accepted',$4,$5,$6)`, orderID, command.PlatformOrderID, command.MenuVersion, total, currency, details); err != nil {
			return fmt.Errorf("insert accepted venue order: %w", err)
		}
		for position, item := range managedItems {
			itemID, err := shareduuid.New()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO venue_order_items
				(id,venue_order_id,external_item_id,name,quantity,unit_price,total_price,currency,position)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, itemID, orderID, item.ExternalItemID, item.Name,
				item.Quantity, item.UnitPrice, item.TotalPrice, currency, position); err != nil {
				return fmt.Errorf("insert venue order item: %w", err)
			}
		}
		return nil
	})
	return decision, err
}

func insertRejected(ctx context.Context, tx postgres.DBTX, command domain.OrderCommand, decision domain.OrderDecision) error {
	orderID, err := shareduuid.New()
	if err != nil {
		return err
	}
	details, _ := json.Marshal(decision)
	_, err = tx.Exec(ctx, `INSERT INTO venue_orders
		(id,platform_order_id,menu_version,decision,decision_reason,status,decision_details)
		VALUES ($1,$2,$3,'rejected',$4,'rejected',$5)`, orderID, command.PlatformOrderID, command.MenuVersion, decision.Reason, details)
	if err != nil {
		return fmt.Errorf("insert rejected venue order: %w", err)
	}
	return nil
}

func findDecision(ctx context.Context, db postgres.DBTX, platformOrderID string) (domain.OrderDecision, error) {
	var raw []byte
	if err := db.QueryRow(ctx, "SELECT decision_details FROM venue_orders WHERE platform_order_id=$1", platformOrderID).Scan(&raw); err != nil {
		return domain.OrderDecision{}, err
	}
	var decision domain.OrderDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return domain.OrderDecision{}, fmt.Errorf("decode venue order decision: %w", err)
	}
	return decision, nil
}

func (r *Repository) ListOrders(ctx context.Context) ([]domain.ManagedOrder, error) {
	rows, err := r.db.Query(ctx, `SELECT id,platform_order_id,status,COALESCE(items_amount,0),COALESCE(currency,'RUB'),created_at,updated_at
		FROM venue_orders ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("list venue orders: %w", err)
	}
	defer rows.Close()
	orders := []domain.ManagedOrder{}
	for rows.Next() {
		var order domain.ManagedOrder
		if err := rows.Scan(&order.ID, &order.PlatformOrderID, &order.Status, &order.ItemsAmount, &order.Currency, &order.CreatedAt, &order.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan venue order: %w", err)
		}
		items, err := loadManagedItems(ctx, r.db, order.ID)
		if err != nil {
			return nil, err
		}
		order.Items = items
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (r *Repository) GetOrder(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	var order domain.ManagedOrder
	err := r.db.QueryRow(ctx, `SELECT id,platform_order_id,status,COALESCE(items_amount,0),COALESCE(currency,'RUB'),created_at,updated_at
		FROM venue_orders WHERE id=$1`, orderID).Scan(&order.ID, &order.PlatformOrderID, &order.Status, &order.ItemsAmount, &order.Currency, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ManagedOrder{}, ErrOrderNotFound
	}
	if err != nil {
		return domain.ManagedOrder{}, fmt.Errorf("get venue order: %w", err)
	}
	order.Items, err = loadManagedItems(ctx, r.db, order.ID)
	return order, err
}

func (r *Repository) StartPreparing(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	return r.transitionOrder(ctx, orderID, "accepted", "preparing", "order_preparing")
}

func (r *Repository) MarkReady(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	return r.transitionOrder(ctx, orderID, "preparing", "ready", "order_ready")
}

func (r *Repository) Complete(ctx context.Context, orderID string) (domain.ManagedOrder, error) {
	return r.transitionOrder(ctx, orderID, "ready", "completed", "order_completed")
}

func (r *Repository) CancelOrder(ctx context.Context, platformOrderID string) (domain.CancellationDecision, error) {
	var decision domain.CancellationDecision
	err := r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var orderID, status string
		var sequence int64
		var saved []byte
		err := tx.QueryRow(ctx, `SELECT id,status,next_event_sequence,cancellation_decision
			FROM venue_orders WHERE platform_order_id=$1 FOR UPDATE`, platformOrderID).
			Scan(&orderID, &status, &sequence, &saved)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		if err != nil {
			return fmt.Errorf("lock venue cancellation: %w", err)
		}
		if len(saved) > 0 {
			if err := json.Unmarshal(saved, &decision); err != nil {
				return fmt.Errorf("decode cancellation decision: %w", err)
			}
			return nil
		}
		if status != "accepted" {
			decision = domain.CancellationDecision{Result: "rejected", CurrentStatus: status, Reason: "cancellation_not_allowed"}
			payload, err := json.Marshal(decision)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, "UPDATE venue_orders SET cancellation_decision=$2,updated_at=now() WHERE id=$1", orderID, payload)
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE venue_menu_items item
			SET stock_quantity=item.stock_quantity+ordered.quantity,updated_at=now()
			FROM venue_order_items ordered
			WHERE ordered.venue_order_id=$1 AND ordered.external_item_id=item.external_id
			  AND item.stock_quantity IS NOT NULL`, orderID); err != nil {
			return fmt.Errorf("release venue stock: %w", err)
		}
		decision = domain.CancellationDecision{Result: "cancelled", CurrentStatus: "cancelled"}
		decisionJSON, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		eventID, err := shareduuid.New()
		if err != nil {
			return err
		}
		outboxID, err := shareduuid.New()
		if err != nil {
			return err
		}
		event := domain.OrderEvent{EventID: eventID, Sequence: sequence, Type: "order_cancelled", OccurredAt: time.Now().UTC()}
		eventJSON, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE venue_orders SET status='cancelled',next_event_sequence=next_event_sequence+1,
			cancellation_decision=$2,updated_at=now() WHERE id=$1`, orderID, decisionJSON); err != nil {
			return fmt.Errorf("cancel venue order: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO venue_outbox
			(id,venue_order_id,event_id,sequence,event_type,payload)
			VALUES ($1,$2,$3,$4,'order_cancelled',$5)`, outboxID, orderID, eventID, sequence, eventJSON); err != nil {
			return fmt.Errorf("insert cancellation callback: %w", err)
		}
		return nil
	})
	return decision, err
}

func (r *Repository) transitionOrder(ctx context.Context, orderID, expected, target, eventType string) (domain.ManagedOrder, error) {
	var order domain.ManagedOrder
	err := r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var sequence int64
		err := tx.QueryRow(ctx, `SELECT id,platform_order_id,status,COALESCE(items_amount,0),COALESCE(currency,'RUB'),
			created_at,updated_at,next_event_sequence FROM venue_orders WHERE id=$1 FOR UPDATE`, orderID).Scan(
			&order.ID, &order.PlatformOrderID, &order.Status, &order.ItemsAmount, &order.Currency,
			&order.CreatedAt, &order.UpdatedAt, &sequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		if err != nil {
			return fmt.Errorf("lock venue order transition: %w", err)
		}
		if order.Status == target {
			order.Items, err = loadManagedItems(ctx, tx, order.ID)
			return err
		}
		if order.Status != expected {
			return ErrInvalidOrderTransition
		}
		eventID, err := shareduuid.New()
		if err != nil {
			return err
		}
		outboxID, err := shareduuid.New()
		if err != nil {
			return err
		}
		event := domain.OrderEvent{EventID: eventID, Sequence: sequence, Type: eventType, OccurredAt: time.Now().UTC()}
		payload, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode venue order event: %w", err)
		}
		err = tx.QueryRow(ctx, `UPDATE venue_orders SET status=$2,next_event_sequence=next_event_sequence+1,updated_at=now()
			WHERE id=$1 RETURNING status,updated_at`, orderID, target).Scan(&order.Status, &order.UpdatedAt)
		if err != nil {
			return fmt.Errorf("update venue order status: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO venue_outbox
			(id,venue_order_id,event_id,sequence,event_type,payload)
			VALUES ($1,$2,$3,$4,$5,$6)`, outboxID, orderID, eventID, sequence, eventType, payload); err != nil {
			return fmt.Errorf("insert venue callback event: %w", err)
		}
		order.Items, err = loadManagedItems(ctx, tx, order.ID)
		return err
	})
	return order, err
}

func loadManagedItems(ctx context.Context, db postgres.DBTX, orderID string) ([]domain.ManagedOrderItem, error) {
	rows, err := db.Query(ctx, `SELECT external_item_id,name,quantity,unit_price,total_price FROM venue_order_items WHERE venue_order_id=$1 ORDER BY position`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load venue order items: %w", err)
	}
	defer rows.Close()
	items := []domain.ManagedOrderItem{}
	for rows.Next() {
		var item domain.ManagedOrderItem
		if err := rows.Scan(&item.ExternalItemID, &item.Name, &item.Quantity, &item.UnitPrice, &item.TotalPrice); err != nil {
			return nil, fmt.Errorf("scan venue order item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
