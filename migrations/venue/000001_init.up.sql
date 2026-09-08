CREATE TABLE venue_menu_versions (
    version text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE venue_menu_items (
    external_id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    price_amount bigint NOT NULL CHECK (price_amount >= 0),
    currency char(3) NOT NULL DEFAULT 'RUB',
    stock_quantity integer CHECK (stock_quantity IS NULL OR stock_quantity >= 0),
    is_available boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE venue_orders (
    id uuid PRIMARY KEY,
    platform_order_id uuid NOT NULL UNIQUE,
    menu_version text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('accepted', 'rejected')),
    decision_reason text,
    status text NOT NULL CHECK (status IN (
        'accepted',
        'preparing',
        'ready',
        'completed',
        'rejected',
        'cancelled'
    )),
    items_amount bigint,
    currency char(3),
    next_event_sequence bigint NOT NULL DEFAULT 1 CHECK (next_event_sequence > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE venue_order_items (
    id uuid PRIMARY KEY,
    venue_order_id uuid NOT NULL REFERENCES venue_orders(id),
    external_item_id text NOT NULL,
    name text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price bigint NOT NULL CHECK (unit_price >= 0),
    total_price bigint NOT NULL CHECK (total_price >= 0),
    currency char(3) NOT NULL
);

CREATE TABLE venue_outbox (
    id uuid PRIMARY KEY,
    venue_order_id uuid NOT NULL REFERENCES venue_orders(id),
    event_id uuid NOT NULL UNIQUE,
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (venue_order_id, sequence)
);

CREATE INDEX venue_orders_status_idx
    ON venue_orders (status, created_at);

CREATE INDEX venue_outbox_pending_idx
    ON venue_outbox (available_at, created_at)
    WHERE processed_at IS NULL;
