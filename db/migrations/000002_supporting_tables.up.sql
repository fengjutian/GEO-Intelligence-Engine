-- P0 V1.0 supporting tables referenced by Feature Specs.
CREATE TABLE competitor_aliases (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, competitor_id BINARY(16) NOT NULL,
  alias VARCHAR(300) NOT NULL, alias_normalized VARCHAR(300) NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE KEY uq_competitor_alias (competitor_id,alias_normalized), CONSTRAINT fk_ca_competitor FOREIGN KEY (competitor_id) REFERENCES competitors(id)
) ENGINE=InnoDB;
CREATE TABLE competitor_products (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, competitor_id BINARY(16) NOT NULL,
  name VARCHAR(300) NOT NULL, mapped_product_id BINARY(16), status VARCHAR(32) NOT NULL,
  CONSTRAINT fk_cp_competitor FOREIGN KEY (competitor_id) REFERENCES competitors(id), CONSTRAINT fk_cp_product FOREIGN KEY (mapped_product_id) REFERENCES products(id)
) ENGINE=InnoDB;
CREATE TABLE content_blocks (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, page_content_id BINARY(16) NOT NULL,
  block_type VARCHAR(32) NOT NULL, text_hash BINARY(32) NOT NULL, locator VARCHAR(1000), position INT UNSIGNED NOT NULL,
  text_object_key VARCHAR(1024), CONSTRAINT fk_block_content FOREIGN KEY (page_content_id) REFERENCES page_contents(id)
) ENGINE=InnoDB;
CREATE TABLE evidence_references (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, finding_id BINARY(16) NOT NULL,
  source_type VARCHAR(32) NOT NULL, source_id BINARY(16) NOT NULL, locator VARCHAR(1000), quote_text TEXT, content_hash BINARY(32),
  CONSTRAINT fk_evidence_finding FOREIGN KEY (finding_id) REFERENCES findings(id)
) ENGINE=InnoDB;
CREATE TABLE entity_extraction_runs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, crawl_run_id BINARY(16) NOT NULL,
  extractor_version VARCHAR(64) NOT NULL, config_hash BINARY(32) NOT NULL, status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, finished_at DATETIME(6), UNIQUE KEY uq_extraction_run (crawl_run_id,extractor_version,config_hash),
  CONSTRAINT fk_extraction_crawl FOREIGN KEY (crawl_run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB;
CREATE TABLE entity_candidates (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, extraction_run_id BINARY(16) NOT NULL,
  candidate_json JSON NOT NULL, match_entity_id BINARY(16), confidence DECIMAL(6,5), status VARCHAR(32) NOT NULL,
  CONSTRAINT fk_candidate_run FOREIGN KEY (extraction_run_id) REFERENCES entity_extraction_runs(id), CONSTRAINT fk_candidate_entity FOREIGN KEY (match_entity_id) REFERENCES entities(id)
) ENGINE=InnoDB;
CREATE TABLE entity_merge_events (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, source_entity_id BINARY(16) NOT NULL, target_entity_id BINARY(16) NOT NULL,
  actor_id BINARY(16) NOT NULL, reason TEXT NOT NULL, status VARCHAR(32) NOT NULL, occurred_at DATETIME(6) NOT NULL, undone_at DATETIME(6)
) ENGINE=InnoDB;
CREATE TABLE question_relations (
  parent_question_id BINARY(16) NOT NULL, child_question_id BINARY(16) NOT NULL, relation_type VARCHAR(32) NOT NULL,
  PRIMARY KEY (parent_question_id,child_question_id,relation_type), CONSTRAINT fk_qr_parent FOREIGN KEY (parent_question_id) REFERENCES questions(id),
  CONSTRAINT fk_qr_child FOREIGN KEY (child_question_id) REFERENCES questions(id)
) ENGINE=InnoDB;
CREATE TABLE question_generation_runs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, knowledge_snapshot_id BINARY(16) NOT NULL,
  generator_version VARCHAR(64) NOT NULL, status VARCHAR(32) NOT NULL, config_hash BINARY(32) NOT NULL, created_at DATETIME(6) NOT NULL, finished_at DATETIME(6),
  CONSTRAINT fk_qgr_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id), CONSTRAINT fk_qgr_knowledge FOREIGN KEY (knowledge_snapshot_id) REFERENCES knowledge_snapshots(id)
) ENGINE=InnoDB;
CREATE TABLE model_catalog (
  id BINARY(16) PRIMARY KEY, provider VARCHAR(32) NOT NULL, model_id VARCHAR(255) NOT NULL, capabilities_json JSON NOT NULL,
  pricing_version VARCHAR(64), status VARCHAR(32) NOT NULL, updated_at DATETIME(6) NOT NULL, UNIQUE KEY uq_model_catalog (provider,model_id)
) ENGINE=InnoDB;
CREATE TABLE model_alias_versions (
  id BINARY(16) PRIMARY KEY, alias VARCHAR(200) NOT NULL, provider VARCHAR(32) NOT NULL, resolved_model VARCHAR(255) NOT NULL,
  version INT UNSIGNED NOT NULL, effective_at DATETIME(6) NOT NULL, UNIQUE KEY uq_model_alias_version (alias,provider,version)
) ENGINE=InnoDB;
CREATE TABLE provider_attempts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, request_id BINARY(16) NOT NULL, attempt_no SMALLINT UNSIGNED NOT NULL,
  state VARCHAR(32) NOT NULL, provider_request_id VARCHAR(255), error_code VARCHAR(64), error_detail JSON,
  started_at DATETIME(6) NOT NULL, finished_at DATETIME(6), UNIQUE KEY uq_provider_attempt (request_id,attempt_no),
  CONSTRAINT fk_provider_attempt_request FOREIGN KEY (request_id) REFERENCES ai_requests(id)
) ENGINE=InnoDB;
CREATE TABLE provider_health_events (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, engine_config_id BINARY(16) NOT NULL,
  event_type VARCHAR(64) NOT NULL, observed_at DATETIME(6) NOT NULL, details_json JSON,
  KEY ix_provider_health (engine_config_id,observed_at), CONSTRAINT fk_health_engine FOREIGN KEY (engine_config_id) REFERENCES engine_configs(id)
) ENGINE=InnoDB;
CREATE TABLE review_queue_items (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, evaluation_id BINARY(16) NOT NULL,
  reason_code VARCHAR(64) NOT NULL, priority VARCHAR(16) NOT NULL, status VARCHAR(32) NOT NULL, assignee_id BINARY(16),
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, KEY ix_review_queue (tenant_id,status,priority),
  CONSTRAINT fk_review_evaluation FOREIGN KEY (evaluation_id) REFERENCES answer_evaluations(id)
) ENGINE=InnoDB;
CREATE TABLE human_annotations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, review_item_id BINARY(16) NOT NULL, annotator_id BINARY(16) NOT NULL,
  labels_json JSON NOT NULL, reason TEXT, created_at DATETIME(6) NOT NULL, CONSTRAINT fk_annotation_review FOREIGN KEY (review_item_id) REFERENCES review_queue_items(id)
) ENGINE=InnoDB;
CREATE TABLE adjudications (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, review_item_id BINARY(16) NOT NULL, adjudicator_id BINARY(16) NOT NULL,
  result_json JSON NOT NULL, reason TEXT NOT NULL, created_at DATETIME(6) NOT NULL, CONSTRAINT fk_adjudication_review FOREIGN KEY (review_item_id) REFERENCES review_queue_items(id)
) ENGINE=InnoDB;
CREATE TABLE metric_evidence_sets (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, metric_value_id BINARY(16) NOT NULL,
  query_hash BINARY(32) NOT NULL, member_count INT UNSIGNED NOT NULL, members_object_key VARCHAR(1024),
  UNIQUE KEY uq_metric_evidence (metric_value_id), CONSTRAINT fk_metric_evidence_value FOREIGN KEY (metric_value_id) REFERENCES metric_values(id)
) ENGINE=InnoDB;
CREATE TABLE metric_quality_flags (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, metric_value_id BINARY(16) NOT NULL,
  flag_code VARCHAR(64) NOT NULL, details_json JSON, UNIQUE KEY uq_metric_flag (metric_value_id,flag_code),
  CONSTRAINT fk_metric_flag_value FOREIGN KEY (metric_value_id) REFERENCES metric_values(id)
) ENGINE=InnoDB;
CREATE TABLE report_sections (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, report_id BINARY(16) NOT NULL, section_code VARCHAR(64) NOT NULL,
  data_snapshot_object_key VARCHAR(1024) NOT NULL, content_hash BINARY(32) NOT NULL,
  UNIQUE KEY uq_report_section (report_id,section_code), CONSTRAINT fk_section_report FOREIGN KEY (report_id) REFERENCES reports(id)
) ENGINE=InnoDB;
CREATE TABLE report_access_events (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, report_id BINARY(16) NOT NULL, actor_id BINARY(16) NOT NULL,
  action VARCHAR(32) NOT NULL, occurred_at DATETIME(6) NOT NULL, KEY ix_report_access (report_id,occurred_at),
  CONSTRAINT fk_access_report FOREIGN KEY (report_id) REFERENCES reports(id)
) ENGINE=InnoDB;
CREATE TABLE finding_priorities (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, finding_id BINARY(16) NOT NULL, calculation_version VARCHAR(64) NOT NULL,
  components_json JSON NOT NULL, calculated_priority VARCHAR(16) NOT NULL, override_priority VARCHAR(16), override_reason TEXT,
  overridden_by BINARY(16), created_at DATETIME(6) NOT NULL, UNIQUE KEY uq_finding_priority_version (finding_id,calculation_version),
  CONSTRAINT fk_priority_finding FOREIGN KEY (finding_id) REFERENCES findings(id)
) ENGINE=InnoDB;
CREATE TABLE task_evidence_links (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, task_id BINARY(16) NOT NULL,
  source_type VARCHAR(32) NOT NULL, source_id BINARY(16) NOT NULL, source_version VARCHAR(64), content_hash BINARY(32),
  CONSTRAINT fk_task_evidence_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;
CREATE TABLE task_assignments (
  task_id BINARY(16) NOT NULL, user_id BINARY(16) NOT NULL, assignment_role VARCHAR(32) NOT NULL,
  PRIMARY KEY (task_id,user_id,assignment_role), CONSTRAINT fk_assignment_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id),
  CONSTRAINT fk_assignment_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE task_comments (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, task_id BINARY(16) NOT NULL, author_id BINARY(16) NOT NULL,
  body TEXT NOT NULL, created_at DATETIME(6) NOT NULL, CONSTRAINT fk_comment_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;
CREATE TABLE task_attachments (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, task_id BINARY(16) NOT NULL,
  object_key VARCHAR(1024) NOT NULL, sha256 BINARY(32) NOT NULL, scan_status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL,
  CONSTRAINT fk_attachment_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;
