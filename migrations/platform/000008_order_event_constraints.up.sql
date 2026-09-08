ALTER TABLE partner_order_events
    ADD CONSTRAINT partner_order_events_type_check CHECK (event_type IN (
        'order_preparing',
        'order_ready',
        'order_completed',
        'order_cancelled'
    ));
