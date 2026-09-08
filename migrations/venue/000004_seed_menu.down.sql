DELETE FROM venue_menu_items WHERE external_id IN ('margherita', 'pepperoni', 'cola');
DELETE FROM venue_menu_versions WHERE version = 1;

ALTER TABLE venue_menu_items
    DROP COLUMN position,
    DROP COLUMN category_name,
    DROP COLUMN category_external_id;
