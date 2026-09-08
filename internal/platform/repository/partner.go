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

func (r *Repository) ReplaceMenu(ctx context.Context, venueID string, snapshot domain.MenuSnapshot, snapshotHash string) error {
	return r.transactor.WithinTransaction(ctx, func(ctx context.Context, tx postgres.DBTX) error {
		var currentVersion *int64
		var currentHash string
		err := tx.QueryRow(ctx, "SELECT menu_version, COALESCE(menu_snapshot_hash, '') FROM venues WHERE id = $1 FOR UPDATE", venueID).Scan(&currentVersion, &currentHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVenueNotFound
		}
		if err != nil {
			return fmt.Errorf("lock venue menu: %w", err)
		}
		if currentVersion != nil {
			if snapshot.Version < *currentVersion {
				return ErrStaleMenuVersion
			}
			if snapshot.Version == *currentVersion {
				if snapshotHash == currentHash {
					return nil
				}
				return ErrMenuVersionConflict
			}
		}

		if _, err := tx.Exec(ctx, "UPDATE menu_items SET is_active = false, is_available = false, updated_at = now() WHERE venue_id = $1", venueID); err != nil {
			return fmt.Errorf("deactivate menu items: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE menu_categories SET is_active = false, updated_at = now() WHERE venue_id = $1", venueID); err != nil {
			return fmt.Errorf("deactivate menu categories: %w", err)
		}

		for categoryPosition, category := range snapshot.Categories {
			categoryID, err := shareduuid.New()
			if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `
				INSERT INTO menu_categories (id, venue_id, external_id, name, position, is_active)
				VALUES ($1, $2, $3, $4, $5, true)
				ON CONFLICT (venue_id, external_id) DO UPDATE SET
					name = EXCLUDED.name, position = EXCLUDED.position, is_active = true, updated_at = now()
				RETURNING id`, categoryID, venueID, category.ExternalID, category.Name, categoryPosition).Scan(&categoryID)
			if err != nil {
				return fmt.Errorf("upsert menu category: %w", err)
			}

			for itemPosition, item := range category.Items {
				itemID, err := shareduuid.New()
				if err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `
					INSERT INTO menu_items (id, venue_id, category_id, external_id, name, description, price_amount, currency, is_available, is_active, position)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true, $10)
					ON CONFLICT (venue_id, external_id) DO UPDATE SET
						category_id = EXCLUDED.category_id, name = EXCLUDED.name, description = EXCLUDED.description,
						price_amount = EXCLUDED.price_amount, currency = EXCLUDED.currency,
						is_available = EXCLUDED.is_available, is_active = true, position = EXCLUDED.position, updated_at = now()`,
					itemID, venueID, categoryID, item.ExternalID, item.Name, item.Description, item.Price.Amount, item.Price.Currency, item.IsAvailable, itemPosition)
				if err != nil {
					return fmt.Errorf("upsert menu item: %w", err)
				}
			}
		}

		result, err := tx.Exec(ctx, "UPDATE venues SET menu_version = $2, menu_snapshot_hash = $3, menu_updated_at = now(), updated_at = now() WHERE id = $1", venueID, snapshot.Version, snapshotHash)
		if err != nil {
			return fmt.Errorf("update venue menu version: %w", err)
		}
		if result.RowsAffected() != 1 {
			return ErrVenueNotFound
		}
		return nil
	})
}

func (r *Repository) UpdateAvailability(ctx context.Context, venueID string, update domain.AvailabilityUpdate) error {
	result, err := r.db.Exec(ctx, "UPDATE venues SET is_accepting_orders = $2, availability_reason = $3, updated_at = now() WHERE id = $1", venueID, update.IsAcceptingOrders, update.Reason)
	if err != nil {
		return fmt.Errorf("update venue availability: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrVenueNotFound
	}
	return nil
}
