DO $$
DECLARE
    legacy_schema_present boolean;
    legacy_schema_complete boolean;
BEGIN
    SELECT to_regclass('public.venue_orders') IS NOT NULL
        OR to_regclass('public.venue_menu_items') IS NOT NULL
    INTO legacy_schema_present;

    SELECT to_regclass('public.venue_menu_versions') IS NOT NULL
        AND to_regclass('public.venue_menu_items') IS NOT NULL
        AND to_regclass('public.venue_orders') IS NOT NULL
        AND to_regclass('public.venue_order_items') IS NOT NULL
        AND to_regclass('public.venue_outbox') IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = 'venue_orders'
              AND column_name = 'menu_version' AND data_type = 'bigint'
        )
    INTO legacy_schema_complete;

    IF to_regclass('public.schema_migrations') IS NULL AND legacy_schema_complete THEN
        CREATE TABLE schema_migrations (
            version bigint NOT NULL PRIMARY KEY,
            dirty boolean NOT NULL
        );
        INSERT INTO schema_migrations (version, dirty) VALUES (2, false);
    ELSIF to_regclass('public.schema_migrations') IS NULL AND legacy_schema_present THEN
        RAISE EXCEPTION 'refusing to baseline incomplete venue schema';
    END IF;
END
$$;
