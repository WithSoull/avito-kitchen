CREATE TABLE venue_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    is_accepting_orders boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO venue_settings (singleton, is_accepting_orders) VALUES (true, true);

ALTER TABLE venue_order_items ADD COLUMN position integer NOT NULL DEFAULT 0;
ALTER TABLE venue_orders ADD COLUMN decision_details jsonb NOT NULL DEFAULT '{}'::jsonb;
