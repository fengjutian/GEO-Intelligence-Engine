ALTER TABLE knowledge_snapshots DROP COLUMN content_json;
ALTER TABLE audit_samples
  DROP KEY ix_audit_sample_claim,
  DROP COLUMN last_error_code,
  DROP COLUMN attempt_count,
  DROP COLUMN lease_expires_at,
  DROP COLUMN lease_owner;
