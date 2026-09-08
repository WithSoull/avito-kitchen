package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

func (r *Repository) ClaimCallback(ctx context.Context, lease time.Duration) (domain.CallbackJob, error) {
	lockToken, err := shareduuid.New()
	if err != nil {
		return domain.CallbackJob{}, err
	}
	var job domain.CallbackJob
	err = r.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT current.id
			FROM venue_outbox current
			WHERE current.processed_at IS NULL
			  AND current.available_at <= now()
			  AND (current.locked_until IS NULL OR current.locked_until < now())
			  AND NOT EXISTS (
				SELECT 1 FROM venue_outbox earlier
				WHERE earlier.venue_order_id=current.venue_order_id
				  AND earlier.sequence < current.sequence
				  AND earlier.processed_at IS NULL
			  )
			ORDER BY current.available_at,current.created_at
			FOR UPDATE OF current SKIP LOCKED LIMIT 1
		)
		UPDATE venue_outbox current
		SET locked_until=now()+($1 * interval '1 millisecond'),lock_token=$2,attempts=attempts+1
		FROM candidate,venue_orders orders
		WHERE current.id=candidate.id AND orders.id=current.venue_order_id
		RETURNING current.id,orders.platform_order_id,current.lock_token,current.payload,current.attempts`, lease.Milliseconds(), lockToken).
		Scan(&job.ID, &job.PlatformOrderID, &job.LockToken, &job.Payload, &job.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CallbackJob{}, ErrNoCallbackJob
	}
	if err != nil {
		return domain.CallbackJob{}, fmt.Errorf("claim callback: %w", err)
	}
	return job, nil
}

func (r *Repository) CompleteCallback(ctx context.Context, job domain.CallbackJob) error {
	result, err := r.db.Exec(ctx, `UPDATE venue_outbox
		SET processed_at=now(),locked_until=NULL,lock_token=NULL,last_error=NULL
		WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken)
	if err != nil {
		return fmt.Errorf("complete callback: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrCallbackLeaseLost
	}
	return nil
}

func (r *Repository) RetryCallback(ctx context.Context, job domain.CallbackJob, availableAt time.Time) error {
	result, err := r.db.Exec(ctx, `UPDATE venue_outbox
		SET available_at=$3,locked_until=NULL,lock_token=NULL,last_error='platform callback failed'
		WHERE id=$1 AND lock_token=$2 AND processed_at IS NULL`, job.ID, job.LockToken, availableAt)
	if err != nil {
		return fmt.Errorf("retry callback: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrCallbackLeaseLost
	}
	return nil
}
