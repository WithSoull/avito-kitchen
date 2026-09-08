ALTER TABLE venue_menu_versions
    ALTER COLUMN version TYPE bigint
    USING version::bigint;

ALTER TABLE venue_menu_versions
    ADD CONSTRAINT venue_menu_versions_version_check
    CHECK (version > 0);

ALTER TABLE venue_orders
    ALTER COLUMN menu_version TYPE bigint
    USING menu_version::bigint;

ALTER TABLE venue_orders
    ADD CONSTRAINT venue_orders_menu_version_check
    CHECK (menu_version > 0);
