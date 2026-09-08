DROP INDEX venue_outbox_claim_idx;

ALTER TABLE venue_outbox
    DROP COLUMN lock_token,
    DROP COLUMN locked_until;
