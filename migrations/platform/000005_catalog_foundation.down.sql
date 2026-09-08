DELETE FROM venues
WHERE id = '00000000-0000-4000-8000-000000000001'
  AND external_id = 'demo-venue'
  AND menu_version IS NULL;

ALTER TABLE menu_items DROP COLUMN position;
ALTER TABLE venues DROP COLUMN menu_snapshot_hash;
