-- FEAT-07/08 execution: lease fields for sample claiming and immutable knowledge content.
ALTER TABLE audit_samples
  ADD COLUMN lease_owner VARCHAR(200) NULL AFTER status,
  ADD COLUMN lease_expires_at DATETIME(6) NULL AFTER lease_owner,
  ADD COLUMN attempt_count SMALLINT UNSIGNED NOT NULL DEFAULT 0 AFTER lease_expires_at,
  ADD COLUMN last_error_code VARCHAR(64) NULL AFTER attempt_count,
  ADD KEY ix_audit_sample_claim (status,lease_expires_at);

ALTER TABLE knowledge_snapshots
  ADD COLUMN content_json JSON NULL AFTER snapshot_object_key;
