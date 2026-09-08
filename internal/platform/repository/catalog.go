package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
)

func (r *Repository) ListVenues(ctx context.Context, query domain.VenueListQuery) (domain.VenuePage, error) {
	var afterID any
	if query.AfterID != "" {
		afterID = query.AfterID
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, name, description, address, currency, minimum_order_amount, is_accepting_orders
		FROM venues
		WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%')
		  AND ($2::uuid IS NULL OR id > $2::uuid)
		ORDER BY id
		LIMIT $3`, query.Search, afterID, query.Limit+1)
	if err != nil {
		return domain.VenuePage{}, fmt.Errorf("list venues: %w", err)
	}
	defer rows.Close()

	venues := make([]domain.Venue, 0, query.Limit+1)
	for rows.Next() {
		var venue domain.Venue
		if err := rows.Scan(&venue.ID, &venue.Name, &venue.Description, &venue.Address, &venue.Currency, &venue.MinimumOrderAmount, &venue.IsAcceptingOrders); err != nil {
			return domain.VenuePage{}, fmt.Errorf("scan venue: %w", err)
		}
		venues = append(venues, venue)
	}
	if err := rows.Err(); err != nil {
		return domain.VenuePage{}, fmt.Errorf("iterate venues: %w", err)
	}
	page := domain.VenuePage{Items: venues}
	if len(venues) > query.Limit {
		page.NextCursor = venues[query.Limit-1].ID
		page.Items = venues[:query.Limit]
	}
	return page, nil
}

func (r *Repository) GetVenue(ctx context.Context, venueID string) (domain.Venue, error) {
	var venue domain.Venue
	err := r.db.QueryRow(ctx, `
		SELECT id, name, description, address, currency, minimum_order_amount, is_accepting_orders
		FROM venues WHERE id = $1`, venueID).Scan(
		&venue.ID, &venue.Name, &venue.Description, &venue.Address, &venue.Currency,
		&venue.MinimumOrderAmount, &venue.IsAcceptingOrders,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Venue{}, ErrVenueNotFound
	}
	if err != nil {
		return domain.Venue{}, fmt.Errorf("get venue: %w", err)
	}
	return venue, nil
}

func (r *Repository) GetMenu(ctx context.Context, venueID string) (domain.Menu, error) {
	menu := domain.Menu{VenueID: venueID}
	var version *int64
	var updatedAt *time.Time
	err := r.db.QueryRow(ctx, "SELECT menu_version, menu_updated_at FROM venues WHERE id = $1", venueID).Scan(&version, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Menu{}, ErrVenueNotFound
	}
	if err != nil {
		return domain.Menu{}, fmt.Errorf("get menu version: %w", err)
	}
	if version == nil {
		return domain.Menu{}, ErrMenuNotFound
	}
	menu.Version = *version
	menu.UpdatedAt = *updatedAt

	categoryRows, err := r.db.Query(ctx, `
		SELECT external_id, name FROM menu_categories
		WHERE venue_id = $1 AND is_active
		ORDER BY position, external_id`, venueID)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("list menu categories: %w", err)
	}
	for categoryRows.Next() {
		var category domain.MenuCategory
		if err := categoryRows.Scan(&category.ExternalID, &category.Name); err != nil {
			categoryRows.Close()
			return domain.Menu{}, fmt.Errorf("scan menu category: %w", err)
		}
		category.Items = []domain.MenuItem{}
		menu.Categories = append(menu.Categories, category)
	}
	if err := categoryRows.Err(); err != nil {
		categoryRows.Close()
		return domain.Menu{}, fmt.Errorf("iterate menu categories: %w", err)
	}
	categoryRows.Close()

	itemRows, err := r.db.Query(ctx, `
		SELECT c.external_id, i.id, i.external_id, i.name, i.description, i.price_amount, i.currency, i.is_available
		FROM menu_items i
		JOIN menu_categories c ON c.id = i.category_id
		WHERE i.venue_id = $1 AND i.is_active AND c.is_active
		ORDER BY c.position, c.external_id, i.position, i.external_id`, venueID)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("list menu items: %w", err)
	}
	categories := make(map[string]int, len(menu.Categories))
	for index, category := range menu.Categories {
		categories[category.ExternalID] = index
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var categoryID string
		var item domain.MenuItem
		if err := itemRows.Scan(&categoryID, &item.ID, &item.ExternalID, &item.Name, &item.Description, &item.Price.Amount, &item.Price.Currency, &item.IsAvailable); err != nil {
			return domain.Menu{}, fmt.Errorf("scan menu item: %w", err)
		}
		menu.Categories[categories[categoryID]].Items = append(menu.Categories[categories[categoryID]].Items, item)
	}
	if err := itemRows.Err(); err != nil {
		return domain.Menu{}, fmt.Errorf("iterate menu items: %w", err)
	}
	return menu, nil
}
