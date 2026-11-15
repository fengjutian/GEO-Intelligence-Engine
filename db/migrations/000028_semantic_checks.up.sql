-- Semantic fact verification: verify model answer claims against brand and third-party evidence.

CREATE TABLE semantic_check_runs (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  audit_id BINARY(16) NOT NULL,
  status VARCHAR(32) NOT NULL,
  model VARCHAR(255) NOT NULL,
  claim_count INT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL,
  finished_at DATETIME(6),
  KEY ix_semantic_run_project (tenant_id, project_id, created_at),
  KEY ix_semantic_run_audit (tenant_id, audit_id)
) ENGINE=InnoDB;

CREATE TABLE semantic_checks (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  run_id BINARY(16) NOT NULL,
  audit_id BINARY(16) NOT NULL,
  claim TEXT NOT NULL,
  verdict VARCHAR(32) NOT NULL,
  confidence DECIMAL(6,5) NOT NULL,
  similarity DECIMAL(6,5) NOT NULL DEFAULT 0,
  evidence_source VARCHAR(32) NOT NULL,
  evidence_text TEXT,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  created_at DATETIME(6) NOT NULL,
  KEY ix_semantic_check_project (tenant_id, project_id, status, created_at)
) ENGINE=InnoDB;
