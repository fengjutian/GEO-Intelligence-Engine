ALTER TABLE question_set_versions
  DROP KEY ix_question_set_version_status,
  DROP COLUMN status,
  MODIFY published_at DATETIME(6) NOT NULL;
