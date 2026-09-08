package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

func (r *Repository) CreateOrder(ctx context.Context, command domain.CreateOrderCommand) (domain.Order, error) {
	var created domain.Order
	err := r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, hashtextextended($2, 0)))", command.Request.CustomerRef, command.IdempotencyKey); err != nil {
			return fmt.Errorf("lock idempotency scope: %w", err)
		}
		existing, hash, err := findIdempotentOrder(ctx, tx, command.Request.CustomerRef, command.IdempotencyKey)
		if err == nil {
			if hash != command.RequestHash {
				return ErrIdempotencyKeyReused
			}
			created = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		var accepting bool
		var currentVersion *int64
		var currency string
		var minimum int64
		err = tx.QueryRow(ctx, `SELECT is_accepting_orders, menu_version, currency, minimum_order_amount FROM venues WHERE id=$1 FOR SHARE`, command.Request.VenueID).
			Scan(&accepting, &currentVersion, &currency, &minimum)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVenueNotFound
		}
		if err != nil {
			return fmt.Errorf("lock checkout venue: %w", err)
		}
		if !accepting {
			return ErrVenueClosed
		}
		if currentVersion == nil || *currentVersion != command.Request.MenuVersion {
			return ErrMenuChanged
		}

		requestedIDs := make([]string, len(command.Request.Items))
		quantities := make(map[string]int, len(command.Request.Items))
		for index, item := range command.Request.Items {
			requestedIDs[index] = item.MenuItemID
			quantities[item.MenuItemID] = item.Quantity
		}
		rows, err := tx.Query(ctx, `
			SELECT id, external_id, name, price_amount, currency
			FROM menu_items
			WHERE venue_id=$1 AND id=ANY($2::uuid[]) AND is_active AND is_available
			FOR SHARE`, command.Request.VenueID, requestedIDs)
		if err != nil {
			return fmt.Errorf("load checkout items: %w", err)
		}
		menuItems := make(map[string]domain.OrderItem, len(requestedIDs))
		for rows.Next() {
			var item domain.OrderItem
			var itemCurrency string
			if err := rows.Scan(&item.MenuItemID, &item.ExternalItemID, &item.Name, &item.UnitPrice, &itemCurrency); err != nil {
				rows.Close()
				return fmt.Errorf("scan checkout item: %w", err)
			}
			if itemCurrency != currency {
				rows.Close()
				return ErrMenuChanged
			}
			menuItems[item.MenuItemID] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate checkout items: %w", err)
		}
		rows.Close()
		if len(menuItems) != len(requestedIDs) {
			return ErrItemsUnavailable
		}
		items := make([]domain.OrderItem, 0, len(requestedIDs))
		var itemsAmount int64
		for _, requested := range command.Request.Items {
			item := menuItems[requested.MenuItemID]
			item.Quantity = quantities[item.MenuItemID]
			if item.UnitPrice > 0 && int64(item.Quantity) > math.MaxInt64/item.UnitPrice {
				return ErrAmountOverflow
			}
			item.TotalPrice = item.UnitPrice * int64(item.Quantity)
			if itemsAmount > math.MaxInt64-item.TotalPrice {
				return ErrAmountOverflow
			}
			itemsAmount += item.TotalPrice
			items = append(items, item)
		}
		if itemsAmount < minimum {
			return ErrMinimumOrder
		}

		deliveryJSON, err := json.Marshal(command.Request.Delivery)
		if err != nil {
			return fmt.Errorf("encode delivery: %w", err)
		}
		created = domain.Order{ID: command.OrderID, CustomerRef: command.Request.CustomerRef, VenueID: command.Request.VenueID,
			Status: "pending_confirmation", CancellationStatus: "none", Items: items, ItemsAmount: itemsAmount,
			DeliveryAmount: 0, TotalAmount: itemsAmount, Currency: currency}
		err = tx.QueryRow(ctx, `
			INSERT INTO orders (id, customer_ref, venue_id, status, menu_version, items_amount,
				delivery_amount, total_amount, currency, delivery, idempotency_key, request_hash,
				tracking_token_hash, confirmation_deadline, cancellation_status)
			VALUES ($1,$2,$3,'pending_confirmation',$4,$5,0,$5,$6,$7,$8,$9,$10,$11,'none')
			RETURNING created_at, updated_at`,
			command.OrderID, command.Request.CustomerRef, command.Request.VenueID, command.Request.MenuVersion,
			itemsAmount, currency, deliveryJSON, command.IdempotencyKey, command.RequestHash,
			command.TrackingTokenHash, command.ConfirmationDeadline).Scan(&created.CreatedAt, &created.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		for position, item := range items {
			itemID, err := shareduuid.New()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_items
				(id,order_id,menu_item_id,external_item_id,name,quantity,unit_price,total_price,currency,position)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, itemID, command.OrderID, item.MenuItemID,
				item.ExternalItemID, item.Name, item.Quantity, item.UnitPrice, item.TotalPrice, currency, position); err != nil {
				return fmt.Errorf("insert order item: %w", err)
			}
		}
		historyID, err := shareduuid.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_status_history (id,order_id,status,source,occurred_at) VALUES ($1,$2,'pending_confirmation','customer',$3)`, historyID, command.OrderID, created.CreatedAt); err != nil {
			return fmt.Errorf("insert order history: %w", err)
		}
		outboxID, err := shareduuid.New()
		if err != nil {
			return err
		}
		commandItems := make([]venueOrderItem, len(items))
		for index, item := range items {
			commandItems[index] = venueOrderItem{ExternalItemID: item.ExternalItemID, Name: item.Name, Quantity: item.Quantity, ExpectedUnitPrice: item.UnitPrice}
		}
		payload, err := json.Marshal(venueOrderCommand{PlatformOrderID: command.OrderID, MenuVersion: command.Request.MenuVersion, Items: commandItems})
		if err != nil {
			return fmt.Errorf("encode order command: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox (id,aggregate_type,aggregate_id,event_type,payload) VALUES ($1,'order',$2,'confirm_order',$3)`, outboxID, command.OrderID, payload); err != nil {
			return fmt.Errorf("insert order outbox: %w", err)
		}
		return nil
	})
	return created, err
}

func (r *Repository) GetOrder(ctx context.Context, orderID, tokenHash string) (domain.Order, error) {
	return getOrderByAccess(ctx, r.db, orderID, tokenHash)
}

func (r *Repository) CancelOrder(ctx context.Context, orderID, tokenHash string) (domain.Order, error) {
	var order domain.Order
	err := r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var status, cancellationStatus string
		err := tx.QueryRow(ctx, `SELECT status,cancellation_status FROM orders
			WHERE id=$1 AND tracking_token_hash=$2 FOR UPDATE`, orderID, tokenHash).Scan(&status, &cancellationStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		if err != nil {
			return fmt.Errorf("lock order cancellation: %w", err)
		}
		switch {
		case status == "cancelled" && cancellationStatus == "confirmed":
			// Exact repeat after a completed cancellation.
		case cancellationStatus == "requested" || cancellationStatus == "rejected":
			// Existing request/result is returned without another outbox command.
		case status == "pending_confirmation":
			var attempts int
			var unlocked bool
			err := tx.QueryRow(ctx, `SELECT attempts,lock_token IS NULL FROM outbox
				WHERE aggregate_id=$1 AND event_type='confirm_order' AND processed_at IS NULL FOR UPDATE`, orderID).
				Scan(&attempts, &unlocked)
			if errors.Is(err, pgx.ErrNoRows) || attempts != 0 || !unlocked {
				return ErrOrderCancellationNotAllowed
			}
			if err != nil {
				return fmt.Errorf("lock confirmation cancellation: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE outbox SET processed_at=now(),last_error='cancelled before dispatch'
				WHERE aggregate_id=$1 AND event_type='confirm_order' AND processed_at IS NULL`, orderID); err != nil {
				return fmt.Errorf("cancel confirmation job: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE orders SET status='cancelled',cancellation_status='confirmed',updated_at=now() WHERE id=$1`, orderID); err != nil {
				return fmt.Errorf("cancel pending order: %w", err)
			}
			if err := insertOrderHistory(ctx, tx, orderID, "cancelled", "customer", "cancelled before venue dispatch"); err != nil {
				return err
			}
		case status == "accepted" && (cancellationStatus == "none" || cancellationStatus == "failed"):
			outboxID, err := shareduuid.New()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE orders SET cancellation_status='requested',updated_at=now() WHERE id=$1`, orderID); err != nil {
				return fmt.Errorf("request order cancellation: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO outbox
				(id,aggregate_type,aggregate_id,event_type,payload) VALUES ($1,'order',$2,'cancel_order','{}')`, outboxID, orderID); err != nil {
				return fmt.Errorf("insert cancellation outbox: %w", err)
			}
		default:
			return ErrOrderCancellationNotAllowed
		}
		order, err = getOrderByAccess(ctx, tx, orderID, tokenHash)
		return err
	})
	return order, err
}

func getOrderByAccess(ctx context.Context, db postgres.DBTX, orderID, tokenHash string) (domain.Order, error) {
	var order domain.Order
	err := db.QueryRow(ctx, `SELECT id,customer_ref,venue_id,COALESCE(external_order_id,''),status,cancellation_status,
		COALESCE(rejection_reason,''),items_amount,delivery_amount,total_amount,currency,created_at,updated_at
		FROM orders WHERE id=$1 AND tracking_token_hash=$2`, orderID, tokenHash).Scan(
		&order.ID, &order.CustomerRef, &order.VenueID, &order.ExternalOrderID, &order.Status, &order.CancellationStatus,
		&order.RejectionReason, &order.ItemsAmount, &order.DeliveryAmount, &order.TotalAmount, &order.Currency,
		&order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, ErrOrderNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("get order by access token: %w", err)
	}
	rows, err := db.Query(ctx, `SELECT menu_item_id,external_item_id,name,quantity,unit_price,total_price
		FROM order_items WHERE order_id=$1 ORDER BY position`, orderID)
	if err != nil {
		return domain.Order{}, fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.MenuItemID, &item.ExternalItemID, &item.Name, &item.Quantity, &item.UnitPrice, &item.TotalPrice); err != nil {
			return domain.Order{}, fmt.Errorf("scan order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}
	return order, rows.Err()
}

func insertOrderHistory(ctx context.Context, tx postgres.DBTX, orderID, status, source, reason string) error {
	historyID, err := shareduuid.New()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO order_status_history
		(id,order_id,status,source,reason,occurred_at) VALUES ($1,$2,$3,$4,NULLIF($5,''),now())`,
		historyID, orderID, status, source, reason); err != nil {
		return fmt.Errorf("insert order history: %w", err)
	}
	return nil
}

type venueOrderCommand struct {
	PlatformOrderID string           `json:"platform_order_id"`
	MenuVersion     int64            `json:"menu_version"`
	Items           []venueOrderItem `json:"items"`
}

type venueOrderItem struct {
	ExternalItemID    string `json:"external_item_id"`
	Name              string `json:"name"`
	Quantity          int    `json:"quantity"`
	ExpectedUnitPrice int64  `json:"expected_unit_price"`
}

func findIdempotentOrder(ctx context.Context, db postgres.DBTX, customerRef, key string) (domain.Order, string, error) {
	var order domain.Order
	var requestHash string
	err := db.QueryRow(ctx, `SELECT id,customer_ref,venue_id,COALESCE(external_order_id,''),status,cancellation_status,
		COALESCE(rejection_reason,''),items_amount,delivery_amount,total_amount,currency,created_at,updated_at,request_hash
		FROM orders WHERE customer_ref=$1 AND idempotency_key=$2`, customerRef, key).Scan(
		&order.ID, &order.CustomerRef, &order.VenueID, &order.ExternalOrderID, &order.Status, &order.CancellationStatus,
		&order.RejectionReason, &order.ItemsAmount, &order.DeliveryAmount, &order.TotalAmount, &order.Currency,
		&order.CreatedAt, &order.UpdatedAt, &requestHash)
	if err != nil {
		return domain.Order{}, "", err
	}
	rows, err := db.Query(ctx, `SELECT menu_item_id,external_item_id,name,quantity,unit_price,total_price FROM order_items WHERE order_id=$1 ORDER BY position`, order.ID)
	if err != nil {
		return domain.Order{}, "", fmt.Errorf("load existing order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.MenuItemID, &item.ExternalItemID, &item.Name, &item.Quantity, &item.UnitPrice, &item.TotalPrice); err != nil {
			return domain.Order{}, "", fmt.Errorf("scan existing order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}
	return order, requestHash, rows.Err()
}
