CREATE INDEX order_items_order_idx ON order_items (order_id);
CREATE INDEX order_status_history_order_idx
    ON order_status_history (order_id, recorded_at);
CREATE INDEX menu_categories_listing_idx
    ON menu_categories (venue_id, position)
    WHERE is_active;
