ALTER TABLE orders DROP CONSTRAINT orders_menu_version_check;
ALTER TABLE orders ALTER COLUMN menu_version TYPE text USING menu_version::text;

ALTER TABLE venues DROP CONSTRAINT venues_menu_version_check;
ALTER TABLE venues ALTER COLUMN menu_version TYPE text USING menu_version::text;
