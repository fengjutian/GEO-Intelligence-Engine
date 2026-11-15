-- Project-level signal alerts: share drop, competitor overtake, crawler silence.

CREATE TABLE signal_alerts (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  kind VARCHAR(48) NOT NULL,
  severity VARCHAR(16) NOT NULL,
  message VARCHAR(500) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'open',
  details_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  resolved_at DATETIME(6) NULL,
  KEY ix_signal_alert_project (tenant_id, project_id, status, created_at)
) ENGINE=InnoDB;
