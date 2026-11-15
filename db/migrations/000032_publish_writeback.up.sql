-- Publish write-back: deliver approved drafts to an external endpoint and keep an audit trail.

CREATE TABLE publish_targets (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  endpoint VARCHAR(1024) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_publish_target (tenant_id, project_id)
) ENGINE=InnoDB;

CREATE TABLE publish_events (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  draft_id BINARY(16) NOT NULL,
  target_id BINARY(16) NOT NULL,
  action VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  http_status INT NULL,
  error_code VARCHAR(64) NULL,
  payload_hash BINARY(32) NULL,
  response_snippet VARCHAR(500) NULL,
  created_at DATETIME(6) NOT NULL,
  KEY ix_publish_event_project (tenant_id, project_id, created_at)
) ENGINE=InnoDB;
