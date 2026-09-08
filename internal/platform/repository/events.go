package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

func (r *Repository) RecordOrderEvent(ctx context.Context, venueID, orderID string, event domain.OrderEvent) error {
	return r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var ownerID, status string
		var currentSequence int64
		err := tx.QueryRow(ctx, `SELECT venue_id,status,venue_event_sequence FROM orders WHERE id=$1 FOR UPDATE`, orderID).
			Scan(&ownerID, &status, &currentSequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		if err != nil {
			return fmt.Errorf("lock platform order event: %w", err)
		}
		if ownerID != venueID {
			return ErrOrderVenueMismatch
		}

		var existingOrderID, existingType string
		var existingSequence int64
		err = tx.QueryRow(ctx, `SELECT order_id,sequence,event_type FROM partner_order_events WHERE event_id=$1`, event.EventID).
			Scan(&existingOrderID, &existingSequence, &existingType)
		if err == nil {
			if existingOrderID == orderID && existingSequence == event.Sequence && existingType == event.Type {
				return nil
			}
			return ErrEventConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find existing order event: %w", err)
		}
		if event.Sequence != currentSequence+1 {
			return ErrEventSequenceConflict
		}

		target, ok := eventTransition(status, event.Type)
		if !ok {
			return ErrInvalidOrderTransition
		}
		if _, err := tx.Exec(ctx, `INSERT INTO partner_order_events
			(event_id,order_id,sequence,event_type,occurred_at) VALUES ($1,$2,$3,$4,$5)`,
			event.EventID, orderID, event.Sequence, event.Type, event.OccurredAt); err != nil {
			return fmt.Errorf("insert partner order event: %w", err)
		}
		cancellationStatus := any(nil)
		if event.Type == "order_cancelled" {
			cancellationStatus = "confirmed"
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET status=$2,venue_event_sequence=$3,
			cancellation_status=COALESCE($4::text,cancellation_status),updated_at=now() WHERE id=$1`,
			orderID, target, event.Sequence, cancellationStatus); err != nil {
			return fmt.Errorf("apply partner order event: %w", err)
		}
		if event.Type == "order_cancelled" {
			if _, err := tx.Exec(ctx, `UPDATE outbox SET processed_at=COALESCE(processed_at,now()),
				locked_until=NULL,lock_token=NULL WHERE aggregate_id=$1 AND event_type='cancel_order'`, orderID); err != nil {
				return fmt.Errorf("complete cancellation outbox from callback: %w", err)
			}
		}
		if status == target {
			return nil
		}
		historyID, err := shareduuid.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_status_history
			(id,order_id,status,source,reason,occurred_at) VALUES ($1,$2,$3,'venue',NULLIF($4,''),$5)`,
			historyID, orderID, target, event.Reason, event.OccurredAt); err != nil {
			return fmt.Errorf("insert partner event history: %w", err)
		}
		return nil
	})
}

func eventTransition(status, eventType string) (string, bool) {
	switch {
	case status == "accepted" && eventType == "order_preparing":
		return "preparing", true
	case status == "preparing" && eventType == "order_ready":
		return "ready", true
	case status == "ready" && eventType == "order_completed":
		return "completed", true
	case (status == "accepted" || status == "preparing") && eventType == "order_cancelled":
		return "cancelled", true
	case status == "cancelled" && eventType == "order_cancelled":
		return "cancelled", true
	default:
		return "", false
	}
}
