package repository

import (
	"context"
	"fmt"

	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

func (r *Repository) CurrentMenu(ctx context.Context) (domain.MenuSnapshot, error) {
	var version *int64
	if err := r.db.QueryRow(ctx, "SELECT max(version) FROM venue_menu_versions").Scan(&version); err != nil {
		return domain.MenuSnapshot{}, fmt.Errorf("get venue menu version: %w", err)
	}
	if version == nil {
		return domain.MenuSnapshot{}, ErrMenuNotFound
	}

	rows, err := r.db.Query(ctx, `
		SELECT category_external_id, category_name, external_id, name, description,
		       price_amount, currency, is_available AND (stock_quantity IS NULL OR stock_quantity > 0)
		FROM venue_menu_items
		ORDER BY category_external_id, position, external_id`)
	if err != nil {
		return domain.MenuSnapshot{}, fmt.Errorf("list venue menu items: %w", err)
	}
	defer rows.Close()

	snapshot := domain.MenuSnapshot{Version: *version, Categories: []domain.MenuCategory{}}
	categoryIndexes := make(map[string]int)
	for rows.Next() {
		var categoryID, categoryName string
		var item domain.MenuItem
		if err := rows.Scan(&categoryID, &categoryName, &item.ExternalID, &item.Name, &item.Description, &item.Price.Amount, &item.Price.Currency, &item.IsAvailable); err != nil {
			return domain.MenuSnapshot{}, fmt.Errorf("scan venue menu item: %w", err)
		}
		categoryIndex, exists := categoryIndexes[categoryID]
		if !exists {
			categoryIndex = len(snapshot.Categories)
			categoryIndexes[categoryID] = categoryIndex
			snapshot.Categories = append(snapshot.Categories, domain.MenuCategory{ExternalID: categoryID, Name: categoryName, Items: []domain.MenuItem{}})
		}
		snapshot.Categories[categoryIndex].Items = append(snapshot.Categories[categoryIndex].Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.MenuSnapshot{}, fmt.Errorf("iterate venue menu items: %w", err)
	}
	return snapshot, nil
}
