ALTER TABLE outbox
    ADD CONSTRAINT outbox_event_type_check CHECK (event_type IN ('confirm_order', 'cancel_order'));
