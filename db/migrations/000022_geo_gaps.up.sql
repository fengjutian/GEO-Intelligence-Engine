-- GEO gap closure: crawler bot logs, external source mentions, optimization drafts and experiments.

CREATE TABLE bot_log_events (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  bot_name VARCHAR(64) NOT NULL,
  user_agent VARCHAR(512) NOT NULL,
  path VARCHAR(1024) NOT NULL,
  http_status INT UNSIGNED NOT NULL,
  occurred_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  KEY ix_botlog_project (tenant_id, project_id, occurred_at),
  KEY ix_botlog_bot (tenant_id, project_id, bot_name)
) ENGINE=InnoDB;

CREATE TABLE source_mentions (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  source_url VARCHAR(1024) NOT NULL,
  title VARCHAR(500) NOT NULL,
  snippet TEXT,
  source_type VARCHAR(32) NOT NULL,
  sentiment VARCHAR(16) NOT NULL DEFAULT 'neutral',
  published_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  KEY ix_mention_project (tenant_id, project_id, source_type, created_at)
) ENGINE=InnoDB;

CREATE TABLE content_drafts (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  page_content_id BINARY(16) NULL,
  kind VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  title VARCHAR(500) NOT NULL,
  payload_json JSON NOT NULL,
  base_hash BINARY(32) NULL,
  version BIGINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY ix_draft_project (tenant_id, project_id, status)
) ENGINE=InnoDB;

CREATE TABLE experiments (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  name VARCHAR(200) NOT NULL,
  hypothesis TEXT NOT NULL,
  metric_code VARCHAR(64) NOT NULL,
  baseline_audit_id BINARY(16) NULL,
  treatment_audit_id BINARY(16) NULL,
  status VARCHAR(32) NOT NULL,
  conclusion VARCHAR(32) NULL,
  stats_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY ix_experiment_project (tenant_id, project_id, status)
) ENGINE=InnoDB;
