ALTER TABLE venue_outbox
    ADD COLUMN locked_until timestamptz,
    ADD COLUMN lock_token uuid;

CREATE INDEX venue_outbox_claim_idx
    ON venue_outbox (available_at, locked_until, created_at)
    WHERE processed_at IS NULL;
