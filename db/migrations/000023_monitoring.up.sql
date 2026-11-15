CREATE TABLE monitoring_schedules (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  protocol_id BINARY(16) NOT NULL,
  interval_minutes INT UNSIGNED NOT NULL,
  next_run_at DATETIME(6) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  last_audit_id BINARY(16) NULL,
  consecutive_failures INT UNSIGNED NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY ix_monitoring_due (enabled,next_run_at),
  CONSTRAINT fk_monitoring_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_monitoring_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id),
  CONSTRAINT fk_monitoring_protocol FOREIGN KEY (protocol_id) REFERENCES measurement_protocols(id)
) ENGINE=InnoDB;

CREATE TABLE monitoring_alerts (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  schedule_id BINARY(16) NOT NULL,
  audit_id BINARY(16) NULL,
  kind VARCHAR(32) NOT NULL,
  message TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'open',
  created_at DATETIME(6) NOT NULL,
  resolved_at DATETIME(6) NULL,
  KEY ix_monitoring_alert_status (tenant_id,status,created_at),
  CONSTRAINT fk_monitoring_alert_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_monitoring_alert_schedule FOREIGN KEY (schedule_id) REFERENCES monitoring_schedules(id)
) ENGINE=InnoDB;
