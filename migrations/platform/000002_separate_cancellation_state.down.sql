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
        'cancellation_requested',
        'cancelled'
    ));

ALTER TABLE orders DROP COLUMN cancellation_status;
