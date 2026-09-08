CREATE TABLE venues (
    id uuid PRIMARY KEY,
    external_id text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    address text NOT NULL DEFAULT '',
    currency char(3) NOT NULL DEFAULT 'RUB',
    minimum_order_amount bigint NOT NULL DEFAULT 0 CHECK (minimum_order_amount >= 0),
    is_accepting_orders boolean NOT NULL DEFAULT false,
    availability_reason text NOT NULL DEFAULT '',
    menu_version text,
    menu_updated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE menu_categories (
    id uuid PRIMARY KEY,
    venue_id uuid NOT NULL REFERENCES venues(id),
    external_id text NOT NULL,
    name text NOT NULL,
    position integer NOT NULL DEFAULT 0,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (venue_id, external_id)
);

CREATE TABLE menu_items (
    id uuid PRIMARY KEY,
    venue_id uuid NOT NULL REFERENCES venues(id),
    category_id uuid NOT NULL REFERENCES menu_categories(id),
    external_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    price_amount bigint NOT NULL CHECK (price_amount >= 0),
    currency char(3) NOT NULL,
    is_available boolean NOT NULL DEFAULT false,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (venue_id, external_id)
);

CREATE TABLE orders (
    id uuid PRIMARY KEY,
    customer_ref text NOT NULL,
    venue_id uuid NOT NULL REFERENCES venues(id),
    external_order_id text,
    status text NOT NULL CHECK (status IN (
        'pending_confirmation',
        'accepted',
        'preparing',
        'ready',
        'completed',
        'rejected',
        'confirmation_expired',
        'cancellation_requested',
        'cancelled'
    )),
    rejection_reason text,
    menu_version text NOT NULL,
    items_amount bigint NOT NULL CHECK (items_amount >= 0),
    delivery_amount bigint NOT NULL CHECK (delivery_amount >= 0),
    total_amount bigint NOT NULL CHECK (total_amount >= 0),
    currency char(3) NOT NULL,
    delivery jsonb NOT NULL,
    idempotency_key text NOT NULL,
    request_hash text NOT NULL,
    tracking_token_hash text NOT NULL UNIQUE,
    venue_event_sequence bigint NOT NULL DEFAULT 0 CHECK (venue_event_sequence >= 0),
    confirmation_deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (customer_ref, idempotency_key)
);

CREATE TABLE order_items (
    id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(id),
    menu_item_id uuid NOT NULL REFERENCES menu_items(id),
    external_item_id text NOT NULL,
    name text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price bigint NOT NULL CHECK (unit_price >= 0),
    total_price bigint NOT NULL CHECK (total_price >= 0),
    currency char(3) NOT NULL
);

CREATE TABLE order_status_history (
    id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(id),
    status text NOT NULL,
    source text NOT NULL CHECK (source IN ('customer', 'platform', 'venue')),
    reason text,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE partner_order_events (
    event_id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (order_id, sequence)
);

CREATE TABLE outbox (
    id uuid PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX menu_items_available_idx
    ON menu_items (venue_id, category_id)
    WHERE is_active AND is_available;

CREATE INDEX orders_customer_created_idx
    ON orders (customer_ref, created_at DESC);

CREATE INDEX orders_venue_status_idx
    ON orders (venue_id, status, created_at);

CREATE INDEX outbox_pending_idx
    ON outbox (available_at, created_at)
    WHERE processed_at IS NULL;
