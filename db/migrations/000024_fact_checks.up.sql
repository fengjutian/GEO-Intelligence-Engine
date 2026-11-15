CREATE TABLE fact_check_runs (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  analysis_run_id BINARY(16) NOT NULL,
  status VARCHAR(32) NOT NULL,
  model VARCHAR(255) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  finished_at DATETIME(6),
  KEY ix_fact_check_run_tenant (tenant_id,project_id,created_at),
  CONSTRAINT fk_fact_check_run_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_fact_check_run_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id),
  CONSTRAINT fk_fact_check_run_analysis FOREIGN KEY (analysis_run_id) REFERENCES content_analysis_runs(id)
) ENGINE=InnoDB;

CREATE TABLE fact_checks (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  run_id BINARY(16) NOT NULL,
  page_content_id BINARY(16) NOT NULL,
  claim TEXT NOT NULL,
  verdict VARCHAR(32) NOT NULL,
  confidence DECIMAL(6,5) NOT NULL,
  evidence TEXT,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  created_at DATETIME(6) NOT NULL,
  KEY ix_fact_check_status (tenant_id,project_id,status),
  CONSTRAINT fk_fact_check_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_fact_check_run FOREIGN KEY (run_id) REFERENCES fact_check_runs(id),
  CONSTRAINT fk_fact_check_content FOREIGN KEY (page_content_id) REFERENCES page_contents(id)
) ENGINE=InnoDB;
