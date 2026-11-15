-- SPEC-11 channel capture: responses without a model request, plus provenance.

ALTER TABLE ai_responses MODIFY COLUMN request_id BINARY(16) NULL;

CREATE TABLE capture_provenance (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  audit_id BINARY(16) NOT NULL,
  capture_id BINARY(16) NOT NULL,
  response_id BINARY(16) NOT NULL,
  batch_id BINARY(16) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  surface_json JSON NOT NULL,
  trust_tier VARCHAR(2) NOT NULL,
  method VARCHAR(32) NOT NULL,
  observed_at DATETIME(6) NOT NULL,
  session_hash VARCHAR(128) NULL,
  screenshot_sha256 VARCHAR(80) NULL,
  consent_basis VARCHAR(32) NOT NULL,
  consent_reference VARCHAR(200) NOT NULL,
  agent_id VARCHAR(120) NOT NULL,
  agent_version VARCHAR(60) NOT NULL,
  signature_verified BOOLEAN NOT NULL DEFAULT FALSE,
  revoked_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_capture_id (tenant_id, capture_id),
  KEY ix_capture_audit (tenant_id, audit_id, observed_at),
  CONSTRAINT fk_capture_response FOREIGN KEY (response_id) REFERENCES ai_responses(id)
) ENGINE=InnoDB;
