ALTER TABLE venue_menu_items
    ADD COLUMN category_external_id text NOT NULL DEFAULT 'main',
    ADD COLUMN category_name text NOT NULL DEFAULT 'Основное меню',
    ADD COLUMN position integer NOT NULL DEFAULT 0;

INSERT INTO venue_menu_versions (version) VALUES (1)
ON CONFLICT (version) DO NOTHING;

INSERT INTO venue_menu_items (
    external_id, name, description, price_amount, currency,
    stock_quantity, is_available, category_external_id, category_name, position
) VALUES
    ('margherita', 'Маргарита', 'Томаты, моцарелла и базилик', 59000, 'RUB', 20, true, 'pizza', 'Пицца', 10),
    ('pepperoni', 'Пепперони', 'Пепперони и моцарелла', 69000, 'RUB', 20, true, 'pizza', 'Пицца', 20),
    ('cola', 'Кола', 'Газированный напиток, 0.5 л', 15000, 'RUB', 50, true, 'drinks', 'Напитки', 10)
ON CONFLICT (external_id) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    price_amount = EXCLUDED.price_amount,
    currency = EXCLUDED.currency,
    stock_quantity = EXCLUDED.stock_quantity,
    is_available = EXCLUDED.is_available,
    category_external_id = EXCLUDED.category_external_id,
    category_name = EXCLUDED.category_name,
    position = EXCLUDED.position,
    updated_at = now();
