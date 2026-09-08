ALTER TABLE venues ADD COLUMN menu_snapshot_hash text;
ALTER TABLE menu_items ADD COLUMN position integer NOT NULL DEFAULT 0;

INSERT INTO venues (
    id, external_id, name, description, address, currency,
    minimum_order_amount, is_accepting_orders
) VALUES (
    '00000000-0000-4000-8000-000000000001',
    'demo-venue',
    'Тестовая кухня',
    'Пример отдельно запущенного заведения',
    'Москва, Тестовая улица, 1',
    'RUB',
    50000,
    false
)
ON CONFLICT (id) DO NOTHING;
