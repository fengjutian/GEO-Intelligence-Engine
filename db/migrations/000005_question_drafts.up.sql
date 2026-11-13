-- FEAT-05 requires editable draft versions and immutable published versions.
ALTER TABLE question_set_versions
  MODIFY published_at DATETIME(6) NULL,
  ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'published' AFTER content_hash,
  ADD KEY ix_question_set_version_status (question_set_id,status);
