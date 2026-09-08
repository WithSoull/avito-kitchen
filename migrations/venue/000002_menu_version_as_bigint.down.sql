ALTER TABLE venue_orders DROP CONSTRAINT venue_orders_menu_version_check;
ALTER TABLE venue_orders ALTER COLUMN menu_version TYPE text USING menu_version::text;

ALTER TABLE venue_menu_versions DROP CONSTRAINT venue_menu_versions_version_check;
ALTER TABLE venue_menu_versions ALTER COLUMN version TYPE text USING version::text;
