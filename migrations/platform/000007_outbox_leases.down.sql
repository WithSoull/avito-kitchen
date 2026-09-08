DROP INDEX outbox_claim_idx;
ALTER TABLE outbox DROP COLUMN lock_token, DROP COLUMN locked_until;
