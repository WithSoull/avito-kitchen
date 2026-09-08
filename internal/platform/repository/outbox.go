package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/postgres"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

func (r *Repository) ClaimOutbox(ctx context.Context, lease time.Duration) (domain.OutboxJob, error) {
	lockToken, err := shareduuid.New()
	if err != nil {
		return domain.OutboxJob{}, err
	}
	var job domain.OutboxJob
	err = r.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT o.id FROM outbox o
			JOIN orders ord ON ord.id=o.aggregate_id
			WHERE o.processed_at IS NULL AND o.available_at <= now()
			  AND (o.locked_until IS NULL OR o.locked_until < now())
			  AND ((o.event_type='confirm_order' AND ord.status='pending_confirmation')
			    OR (o.event_type='cancel_order' AND ord.cancellation_status='requested'))
			ORDER BY o.available_at,o.created_at
			FOR UPDATE OF o SKIP LOCKED LIMIT 1
		)
		UPDATE outbox o SET locked_until=now()+($1 * interval '1 millisecond'),lock_token=$2,attempts=attempts+1
		FROM candidate c, orders ord
		WHERE o.id=c.id AND ord.id=o.aggregate_id
		RETURNING o.id,o.aggregate_id,o.event_type,o.lock_token,o.payload,o.attempts,ord.confirmation_deadline,ord.items_amount,ord.currency`, lease.Milliseconds(), lockToken).
		Scan(&job.ID, &job.OrderID, &job.EventType, &job.LockToken, &job.Payload, &job.Attempts, &job.ConfirmationDeadline, &job.ExpectedItemsAmount, &job.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OutboxJob{}, ErrNoOutboxJob
	}
	if err != nil {
		return domain.OutboxJob{}, fmt.Errorf("claim outbox job: %w", err)
	}
	return job, nil
}

func (r *Repository) ResolveOutbox(ctx context.Context, job domain.OutboxJob, decision domain.VenueOrderDecision) error {
	return r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM orders WHERE id=$1 FOR UPDATE", job.OrderID); err != nil {
			return fmt.Errorf("lock order decision: %w", err)
		}
		result, err := tx.Exec(ctx, `UPDATE outbox SET processed_at=now(),locked_until=NULL,lock_token=NULL
			WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken)
		if err != nil {
			return fmt.Errorf("complete outbox job: %w", err)
		}
		if result.RowsAffected() != 1 {
			return ErrOutboxLeaseLost
		}
		status := "rejected"
		reason := decision.Reason
		externalOrderID := any(nil)
		if decision.Result == "accepted" {
			status = "accepted"
			reason = ""
			externalOrderID = decision.ExternalOrderID
		}
		result, err = tx.Exec(ctx, `UPDATE orders SET status=$2,external_order_id=$3,rejection_reason=NULLIF($4,''),updated_at=now()
			WHERE id=$1 AND status='pending_confirmation'`, job.OrderID, status, externalOrderID, reason)
		if err != nil {
			return fmt.Errorf("apply venue decision: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		historyID, err := shareduuid.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_status_history (id,order_id,status,source,reason,occurred_at)
			VALUES ($1,$2,$3,'venue',NULLIF($4,''),now())`, historyID, job.OrderID, status, reason); err != nil {
			return fmt.Errorf("record venue decision: %w", err)
		}
		return nil
	})
}

func (r *Repository) ResolveCancellation(ctx context.Context, job domain.OutboxJob, decision domain.VenueCancellationDecision) error {
	return r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var cancellationStatus string
		if err := tx.QueryRow(ctx, "SELECT cancellation_status FROM orders WHERE id=$1 FOR UPDATE", job.OrderID).
			Scan(&cancellationStatus); err != nil {
			return fmt.Errorf("lock cancellation result: %w", err)
		}
		result, err := tx.Exec(ctx, `UPDATE outbox SET processed_at=now(),locked_until=NULL,lock_token=NULL
			WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken)
		if err != nil {
			return fmt.Errorf("complete cancellation job: %w", err)
		}
		if result.RowsAffected() != 1 {
			return ErrOutboxLeaseLost
		}
		if cancellationStatus != "requested" {
			return nil
		}
		if decision.Result == "cancelled" {
			if _, err := tx.Exec(ctx, `UPDATE orders SET status='cancelled',cancellation_status='confirmed',updated_at=now() WHERE id=$1`, job.OrderID); err != nil {
				return fmt.Errorf("apply successful cancellation: %w", err)
			}
			return insertOrderHistory(ctx, tx, job.OrderID, "cancelled", "venue", "cancellation confirmed")
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET cancellation_status='rejected',updated_at=now() WHERE id=$1`, job.OrderID); err != nil {
			return fmt.Errorf("apply rejected cancellation: %w", err)
		}
		return nil
	})
}

func (r *Repository) RetryOutbox(ctx context.Context, job domain.OutboxJob, availableAt time.Time, terminal bool) error {
	return r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM orders WHERE id=$1 FOR UPDATE", job.OrderID); err != nil {
			return fmt.Errorf("lock order retry: %w", err)
		}
		if !terminal {
			result, err := tx.Exec(ctx, `UPDATE outbox SET available_at=$3,locked_until=NULL,lock_token=NULL
				WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken, availableAt)
			if err != nil {
				return fmt.Errorf("reschedule outbox job: %w", err)
			}
			if result.RowsAffected() != 1 {
				return ErrOutboxLeaseLost
			}
			return nil
		}
		lastError := "confirmation expired"
		if job.EventType == "cancel_order" {
			lastError = "cancellation delivery failed"
		}
		result, err := tx.Exec(ctx, `UPDATE outbox SET processed_at=now(),last_error=$3,locked_until=NULL,lock_token=NULL
			WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken, lastError)
		if err != nil {
			return fmt.Errorf("expire outbox job: %w", err)
		}
		if result.RowsAffected() != 1 {
			return ErrOutboxLeaseLost
		}
		if job.EventType == "cancel_order" {
			_, err := tx.Exec(ctx, `UPDATE orders SET cancellation_status='failed',updated_at=now()
				WHERE id=$1 AND cancellation_status='requested'`, job.OrderID)
			return err
		}
		result, err = tx.Exec(ctx, `UPDATE orders SET status='confirmation_expired',updated_at=now()
			WHERE id=$1 AND status='pending_confirmation'`, job.OrderID)
		if err != nil {
			return fmt.Errorf("expire confirmation: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		historyID, err := shareduuid.New()
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO order_status_history (id,order_id,status,source,reason,occurred_at)
			VALUES ($1,$2,'confirmation_expired','platform','confirmation deadline exceeded',now())`, historyID, job.OrderID)
		return err
	})
}
