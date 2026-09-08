ALTER TABLE venues
    ALTER COLUMN menu_version TYPE bigint
    USING menu_version::bigint;

ALTER TABLE venues
    ADD CONSTRAINT venues_menu_version_check
    CHECK (menu_version IS NULL OR menu_version > 0);

ALTER TABLE orders
    ALTER COLUMN menu_version TYPE bigint
    USING menu_version::bigint;

ALTER TABLE orders
    ADD CONSTRAINT orders_menu_version_check
    CHECK (menu_version > 0);
