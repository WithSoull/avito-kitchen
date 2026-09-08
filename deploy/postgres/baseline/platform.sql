DO $$
DECLARE
    legacy_schema_present boolean;
    legacy_schema_complete boolean;
BEGIN
    SELECT to_regclass('public.venues') IS NOT NULL
        OR to_regclass('public.orders') IS NOT NULL
    INTO legacy_schema_present;

    SELECT to_regclass('public.venues') IS NOT NULL
        AND to_regclass('public.menu_categories') IS NOT NULL
        AND to_regclass('public.menu_items') IS NOT NULL
        AND to_regclass('public.orders') IS NOT NULL
        AND to_regclass('public.order_items') IS NOT NULL
        AND to_regclass('public.order_status_history') IS NOT NULL
        AND to_regclass('public.partner_order_events') IS NOT NULL
        AND to_regclass('public.outbox') IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = 'orders'
              AND column_name = 'menu_version' AND data_type = 'bigint'
        )
    INTO legacy_schema_complete;

    IF to_regclass('public.schema_migrations') IS NULL AND legacy_schema_complete THEN
        CREATE TABLE schema_migrations (
            version bigint NOT NULL PRIMARY KEY,
            dirty boolean NOT NULL
        );
        INSERT INTO schema_migrations (version, dirty) VALUES (3, false);
    ELSIF to_regclass('public.schema_migrations') IS NULL AND legacy_schema_present THEN
        RAISE EXCEPTION 'refusing to baseline incomplete platform schema';
    END IF;
END
$$;
