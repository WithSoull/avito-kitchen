ALTER TABLE orders
    ADD COLUMN cancellation_status text NOT NULL DEFAULT 'none'
    CHECK (cancellation_status IN ('none', 'requested', 'confirmed', 'rejected', 'failed'));

ALTER TABLE orders DROP CONSTRAINT orders_status_check;

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check CHECK (status IN (
        'pending_confirmation',
        'accepted',
        'preparing',
        'ready',
        'completed',
        'rejected',
        'confirmation_expired',
        'cancelled'
    ));
