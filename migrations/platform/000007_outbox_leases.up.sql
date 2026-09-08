ALTER TABLE outbox
    ADD COLUMN locked_until timestamptz,
    ADD COLUMN lock_token uuid;

CREATE INDEX outbox_claim_idx
    ON outbox (available_at, locked_until, created_at)
    WHERE processed_at IS NULL;
