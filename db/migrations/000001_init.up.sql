-- GEO Intelligence P0 V1.0 baseline schema
-- MySQL 8.4 LTS; IDs are UUIDv7 stored as BINARY(16); timestamps are UTC.
SET NAMES utf8mb4;
SET time_zone = '+00:00';

CREATE TABLE tenants (
  id BINARY(16) PRIMARY KEY, name VARCHAR(200) NOT NULL, status VARCHAR(32) NOT NULL,
  plan_code VARCHAR(64) NOT NULL, data_region VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;
CREATE TABLE users (
  id BINARY(16) PRIMARY KEY, oidc_subject VARCHAR(255) NOT NULL, email_normalized VARCHAR(320) NOT NULL,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_users_subject (oidc_subject), UNIQUE KEY uq_users_email (email_normalized)
) ENGINE=InnoDB;
CREATE TABLE memberships (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, user_id BINARY(16) NOT NULL,
  role VARCHAR(32) NOT NULL, status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_memberships_tenant_user (tenant_id,user_id),
  CONSTRAINT fk_membership_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_membership_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE invitations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, email_normalized VARCHAR(320) NOT NULL,
  role VARCHAR(32) NOT NULL, token_hash BINARY(32) NOT NULL, status VARCHAR(32) NOT NULL,
  expires_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_invitation_token (token_hash), KEY ix_invitation_tenant_email (tenant_id,email_normalized),
  CONSTRAINT fk_invitation_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE service_accounts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, name VARCHAR(120) NOT NULL,
  status VARCHAR(32) NOT NULL, credential_ref VARCHAR(500), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_service_account_name (tenant_id,name),
  CONSTRAINT fk_service_account_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE api_tokens (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, principal_type VARCHAR(32) NOT NULL,
  principal_id BINARY(16) NOT NULL, token_hash BINARY(32) NOT NULL, scopes JSON NOT NULL,
  expires_at DATETIME(6), last_used_at DATETIME(6), revoked_at DATETIME(6), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_api_token_hash (token_hash), KEY ix_api_token_principal (tenant_id,principal_type,principal_id),
  CONSTRAINT fk_api_token_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;

CREATE TABLE projects (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, name VARCHAR(200) NOT NULL,
  status VARCHAR(32) NOT NULL, timezone VARCHAR(64) NOT NULL, default_locale VARCHAR(35) NOT NULL,
  version BIGINT UNSIGNED NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, archived_at DATETIME(6),
  UNIQUE KEY uq_project_name (tenant_id,name), UNIQUE KEY uq_project_tenant_id (tenant_id,id),
  CONSTRAINT fk_project_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE project_memberships (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, user_id BINARY(16) NOT NULL,
  role_override VARCHAR(32), permissions_json JSON, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_project_membership (tenant_id,project_id,user_id),
  CONSTRAINT fk_pm_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id),
  CONSTRAINT fk_pm_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;
CREATE TABLE project_config_versions (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  version INT UNSIGNED NOT NULL, snapshot_json JSON NOT NULL, content_hash BINARY(32) NOT NULL,
  published_by BINARY(16) NOT NULL, published_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_project_config_version (project_id,version), UNIQUE KEY uq_project_config_hash (project_id,content_hash),
  CONSTRAINT fk_pcv_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE brands (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  canonical_name VARCHAR(300) NOT NULL, official_domain VARCHAR(253) NOT NULL, description TEXT,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  KEY ix_brand_project (tenant_id,project_id), CONSTRAINT fk_brand_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE brand_aliases (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, brand_id BINARY(16) NOT NULL,
  alias VARCHAR(300) NOT NULL, alias_normalized VARCHAR(300) NOT NULL, alias_type VARCHAR(32) NOT NULL,
  locale VARCHAR(35), enabled BOOLEAN NOT NULL DEFAULT TRUE, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_brand_alias (brand_id,alias_normalized), KEY ix_brand_alias_tenant (tenant_id,brand_id),
  CONSTRAINT fk_alias_brand FOREIGN KEY (brand_id) REFERENCES brands(id)
) ENGINE=InnoDB;
CREATE TABLE products (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, brand_id BINARY(16) NOT NULL,
  name VARCHAR(300) NOT NULL, description TEXT, verification_status VARCHAR(32) NOT NULL,
  source_type VARCHAR(32), source_id BINARY(16), created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  KEY ix_product_brand (tenant_id,brand_id), CONSTRAINT fk_product_brand FOREIGN KEY (brand_id) REFERENCES brands(id)
) ENGINE=InnoDB;
CREATE TABLE brand_context_items (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, brand_id BINARY(16) NOT NULL,
  item_type VARCHAR(32) NOT NULL, value VARCHAR(500) NOT NULL, verification_status VARCHAR(32) NOT NULL,
  source_type VARCHAR(32), source_id BINARY(16), created_at DATETIME(6) NOT NULL,
  KEY ix_context_brand (tenant_id,brand_id,item_type), CONSTRAINT fk_context_brand FOREIGN KEY (brand_id) REFERENCES brands(id)
) ENGINE=InnoDB;
CREATE TABLE competitors (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  name VARCHAR(300) NOT NULL, domain VARCHAR(253), status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_competitor_name (project_id,name), KEY ix_competitor_project (tenant_id,project_id),
  CONSTRAINT fk_competitor_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;

CREATE TABLE websites (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  base_url VARCHAR(2048) NOT NULL, normalized_host VARCHAR(253) NOT NULL, ownership_status VARCHAR(32) NOT NULL,
  crawl_policy JSON NOT NULL, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_website_host (project_id,normalized_host), KEY ix_website_project (tenant_id,project_id),
  CONSTRAINT fk_website_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE crawl_runs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, website_id BINARY(16) NOT NULL,
  status VARCHAR(32) NOT NULL, config_snapshot JSON NOT NULL, discovered_count INT UNSIGNED NOT NULL DEFAULT 0,
  fetched_count INT UNSIGNED NOT NULL DEFAULT 0, failed_count INT UNSIGNED NOT NULL DEFAULT 0,
  started_at DATETIME(6), finished_at DATETIME(6), created_at DATETIME(6) NOT NULL,
  KEY ix_crawl_project_status (tenant_id,project_id,status), CONSTRAINT fk_crawl_website FOREIGN KEY (website_id) REFERENCES websites(id)
) ENGINE=InnoDB;
CREATE TABLE crawl_urls (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, crawl_run_id BINARY(16) NOT NULL,
  original_url TEXT NOT NULL, normalized_url TEXT NOT NULL, normalized_url_hash BINARY(32) NOT NULL,
  state VARCHAR(32) NOT NULL, depth SMALLINT UNSIGNED NOT NULL, priority INT NOT NULL DEFAULT 0,
  lease_owner VARCHAR(200), lease_expires_at DATETIME(6), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_crawl_url (crawl_run_id,normalized_url_hash), KEY ix_crawl_url_claim (crawl_run_id,state,priority),
  CONSTRAINT fk_crawl_url_run FOREIGN KEY (crawl_run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB;
CREATE TABLE website_pages (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, website_id BINARY(16) NOT NULL,
  canonical_url TEXT, normalized_url_hash BINARY(32) NOT NULL, latest_content_id BINARY(16), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_page_url (website_id,normalized_url_hash), CONSTRAINT fk_page_website FOREIGN KEY (website_id) REFERENCES websites(id)
) ENGINE=InnoDB;
CREATE TABLE page_contents (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, page_id BINARY(16) NOT NULL, crawl_run_id BINARY(16) NOT NULL,
  http_status SMALLINT UNSIGNED, final_url TEXT, title VARCHAR(1000), language VARCHAR(35), content_hash BINARY(32) NOT NULL,
  raw_object_key VARCHAR(1024), rendered_object_key VARCHAR(1024), body_object_key VARCHAR(1024), parser_version VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL, UNIQUE KEY uq_page_content (page_id,crawl_run_id,content_hash),
  CONSTRAINT fk_content_page FOREIGN KEY (page_id) REFERENCES website_pages(id), CONSTRAINT fk_content_run FOREIGN KEY (crawl_run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB;
ALTER TABLE website_pages ADD CONSTRAINT fk_page_latest_content FOREIGN KEY (latest_content_id) REFERENCES page_contents(id);
CREATE TABLE fetch_attempts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, crawl_url_id BINARY(16) NOT NULL,
  attempt_no SMALLINT UNSIGNED NOT NULL, status VARCHAR(32) NOT NULL, http_status SMALLINT UNSIGNED,
  error_code VARCHAR(64), metadata_json JSON, started_at DATETIME(6) NOT NULL, finished_at DATETIME(6),
  UNIQUE KEY uq_fetch_attempt (crawl_url_id,attempt_no), CONSTRAINT fk_fetch_url FOREIGN KEY (crawl_url_id) REFERENCES crawl_urls(id)
) ENGINE=InnoDB;
CREATE TABLE page_links (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, source_content_id BINARY(16) NOT NULL,
  target_url TEXT NOT NULL, target_url_hash BINARY(32) NOT NULL, target_page_id BINARY(16), rel VARCHAR(255), anchor_text TEXT,
  KEY ix_link_source (source_content_id), CONSTRAINT fk_link_content FOREIGN KEY (source_content_id) REFERENCES page_contents(id)
) ENGINE=InnoDB;
CREATE TABLE structured_data_items (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, content_id BINARY(16) NOT NULL,
  schema_type VARCHAR(128), payload_object_key VARCHAR(1024) NOT NULL, validation_status VARCHAR(32) NOT NULL,
  errors_json JSON, created_at DATETIME(6) NOT NULL, CONSTRAINT fk_structured_content FOREIGN KEY (content_id) REFERENCES page_contents(id)
) ENGINE=InnoDB;

CREATE TABLE findings (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  source_type VARCHAR(32) NOT NULL, source_id BINARY(16) NOT NULL, source_version VARCHAR(64),
  rule_code VARCHAR(100) NOT NULL, severity VARCHAR(16) NOT NULL, confidence DECIMAL(6,5), status VARCHAR(32) NOT NULL,
  summary VARCHAR(1000) NOT NULL, evidence_json JSON NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  KEY ix_finding_project (tenant_id,project_id,status,severity), CONSTRAINT fk_finding_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE content_analysis_runs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, crawl_run_id BINARY(16) NOT NULL,
  analyzer_version VARCHAR(64) NOT NULL, config_hash BINARY(32) NOT NULL, status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, finished_at DATETIME(6), UNIQUE KEY uq_content_analysis (crawl_run_id,analyzer_version,config_hash),
  CONSTRAINT fk_analysis_crawl FOREIGN KEY (crawl_run_id) REFERENCES crawl_runs(id)
) ENGINE=InnoDB;
CREATE TABLE content_signals (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, analysis_run_id BINARY(16) NOT NULL, page_content_id BINARY(16) NOT NULL,
  rule_code VARCHAR(100) NOT NULL, value_json JSON NOT NULL, confidence DECIMAL(6,5), created_at DATETIME(6) NOT NULL,
  KEY ix_signal_page (analysis_run_id,page_content_id,rule_code), CONSTRAINT fk_signal_run FOREIGN KEY (analysis_run_id) REFERENCES content_analysis_runs(id),
  CONSTRAINT fk_signal_content FOREIGN KEY (page_content_id) REFERENCES page_contents(id)
) ENGINE=InnoDB;

CREATE TABLE entities (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  entity_type VARCHAR(64) NOT NULL, canonical_name VARCHAR(500) NOT NULL, normalized_name VARCHAR(500) NOT NULL,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  KEY ix_entity_project_name (tenant_id,project_id,normalized_name), CONSTRAINT fk_entity_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE entity_aliases (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, entity_id BINARY(16) NOT NULL,
  alias VARCHAR(500) NOT NULL, locale VARCHAR(35), confidence DECIMAL(6,5), source_id BINARY(16), created_at DATETIME(6) NOT NULL,
  KEY ix_entity_alias (entity_id,alias(191)), CONSTRAINT fk_entity_alias_entity FOREIGN KEY (entity_id) REFERENCES entities(id)
) ENGINE=InnoDB;
CREATE TABLE entity_relations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, subject_id BINARY(16) NOT NULL, predicate VARCHAR(100) NOT NULL,
  object_id BINARY(16) NOT NULL, status VARCHAR(32) NOT NULL, confidence DECIMAL(6,5), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_entity_relation (subject_id,predicate,object_id), CONSTRAINT fk_relation_subject FOREIGN KEY (subject_id) REFERENCES entities(id),
  CONSTRAINT fk_relation_object FOREIGN KEY (object_id) REFERENCES entities(id)
) ENGINE=InnoDB;
CREATE TABLE entity_evidence (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, target_type VARCHAR(32) NOT NULL, target_id BINARY(16) NOT NULL,
  source_type VARCHAR(32) NOT NULL, source_id BINARY(16) NOT NULL, quote_text TEXT, locator VARCHAR(1000), extractor_version VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL, KEY ix_entity_evidence_target (target_type,target_id)
) ENGINE=InnoDB;
CREATE TABLE knowledge_snapshots (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, version INT UNSIGNED NOT NULL,
  content_hash BINARY(32) NOT NULL, snapshot_object_key VARCHAR(1024) NOT NULL, published_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_knowledge_version (project_id,version), CONSTRAINT fk_knowledge_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;

CREATE TABLE question_sets (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, name VARCHAR(200) NOT NULL,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, KEY ix_question_set_project (tenant_id,project_id),
  CONSTRAINT fk_question_set_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE question_set_versions (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, question_set_id BINARY(16) NOT NULL, version INT UNSIGNED NOT NULL,
  content_hash BINARY(32) NOT NULL, published_at DATETIME(6) NOT NULL, UNIQUE KEY uq_question_set_version (question_set_id,version),
  CONSTRAINT fk_qsv_set FOREIGN KEY (question_set_id) REFERENCES question_sets(id)
) ENGINE=InnoDB;
CREATE TABLE questions (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, version_id BINARY(16) NOT NULL,
  text_value TEXT NOT NULL, text_hash BINARY(32) NOT NULL, language VARCHAR(35) NOT NULL, topic VARCHAR(200), intent VARCHAR(64),
  funnel_stage VARCHAR(32), question_type VARCHAR(32) NOT NULL, weight DECIMAL(10,4) NOT NULL DEFAULT 1,
  status VARCHAR(32) NOT NULL, attributes_json JSON, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_question_text (version_id,text_hash), CONSTRAINT fk_question_version FOREIGN KEY (version_id) REFERENCES question_set_versions(id)
) ENGINE=InnoDB;
CREATE TABLE query_clusters (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, question_version_id BINARY(16) NOT NULL,
  name VARCHAR(300) NOT NULL, representative_question_id BINARY(16), algorithm_version VARCHAR(64) NOT NULL, locked BOOLEAN NOT NULL DEFAULT FALSE,
  CONSTRAINT fk_cluster_version FOREIGN KEY (question_version_id) REFERENCES question_set_versions(id)
) ENGINE=InnoDB;
CREATE TABLE cluster_members (
  cluster_id BINARY(16) NOT NULL, question_id BINARY(16) NOT NULL, similarity DECIMAL(6,5), manually_locked BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (cluster_id,question_id), CONSTRAINT fk_cm_cluster FOREIGN KEY (cluster_id) REFERENCES query_clusters(id),
  CONSTRAINT fk_cm_question FOREIGN KEY (question_id) REFERENCES questions(id)
) ENGINE=InnoDB;

CREATE TABLE engine_configs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, provider VARCHAR(32) NOT NULL, channel VARCHAR(32) NOT NULL,
  credential_ref VARCHAR(500) NOT NULL, region VARCHAR(64), limits_json JSON NOT NULL, status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, KEY ix_engine_tenant (tenant_id,status),
  CONSTRAINT fk_engine_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE measurement_protocols (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL,
  semantic_version VARCHAR(32) NOT NULL, config_json JSON NOT NULL, config_hash BINARY(32) NOT NULL, status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, published_at DATETIME(6), UNIQUE KEY uq_protocol_version (project_id,semantic_version),
  CONSTRAINT fk_protocol_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE protocol_engines (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, protocol_id BINARY(16) NOT NULL, engine_config_id BINARY(16) NOT NULL,
  model_selector VARCHAR(200) NOT NULL, channel VARCHAR(32) NOT NULL, parameters_json JSON NOT NULL,
  UNIQUE KEY uq_protocol_engine (protocol_id,engine_config_id,model_selector,channel),
  CONSTRAINT fk_pe_protocol FOREIGN KEY (protocol_id) REFERENCES measurement_protocols(id), CONSTRAINT fk_pe_engine FOREIGN KEY (engine_config_id) REFERENCES engine_configs(id)
) ENGINE=InnoDB;
CREATE TABLE audits (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, protocol_id BINARY(16) NOT NULL,
  question_version_id BINARY(16) NOT NULL, knowledge_snapshot_id BINARY(16) NOT NULL, status VARCHAR(32) NOT NULL,
  planned_count INT UNSIGNED NOT NULL DEFAULT 0, completed_count INT UNSIGNED NOT NULL DEFAULT 0, eligible_count INT UNSIGNED NOT NULL DEFAULT 0,
  budget_microunits BIGINT UNSIGNED NOT NULL, currency CHAR(3) NOT NULL, idempotency_key VARCHAR(200) NOT NULL,
  created_at DATETIME(6) NOT NULL, started_at DATETIME(6), finished_at DATETIME(6),
  UNIQUE KEY uq_audit_idempotency (tenant_id,idempotency_key), KEY ix_audit_project (tenant_id,project_id,status),
  CONSTRAINT fk_audit_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id), CONSTRAINT fk_audit_protocol FOREIGN KEY (protocol_id) REFERENCES measurement_protocols(id)
) ENGINE=InnoDB;
CREATE TABLE audit_samples (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, audit_id BINARY(16) NOT NULL, question_id BINARY(16) NOT NULL,
  engine_config_id BINARY(16) NOT NULL, model_selector VARCHAR(200) NOT NULL, channel VARCHAR(32) NOT NULL,
  region VARCHAR(64), language VARCHAR(35) NOT NULL, sample_no SMALLINT UNSIGNED NOT NULL, sample_key BINARY(32) NOT NULL,
  status VARCHAR(32) NOT NULL, dimensions_json JSON NOT NULL, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_sample_key (sample_key), KEY ix_sample_audit_status (audit_id,status), CONSTRAINT fk_sample_audit FOREIGN KEY (audit_id) REFERENCES audits(id)
) ENGINE=InnoDB;
CREATE TABLE ai_requests (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, sample_id BINARY(16) NOT NULL, provider VARCHAR(32) NOT NULL,
  request_object_key VARCHAR(1024) NOT NULL, request_hash BINARY(32) NOT NULL, parameters_json JSON NOT NULL,
  idempotency_key VARCHAR(200) NOT NULL, sent_at DATETIME(6), created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_ai_request_sample (sample_id), CONSTRAINT fk_request_sample FOREIGN KEY (sample_id) REFERENCES audit_samples(id)
) ENGINE=InnoDB;
CREATE TABLE ai_responses (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, sample_id BINARY(16) NOT NULL, request_id BINARY(16) NOT NULL,
  provider_request_id VARCHAR(255), resolved_model VARCHAR(255), transport_status VARCHAR(32) NOT NULL,
  raw_object_key VARCHAR(1024), raw_hash BINARY(32), usage_json JSON, cost_microunits BIGINT UNSIGNED,
  currency CHAR(3), finish_reason VARCHAR(64), latency_ms INT UNSIGNED, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_ai_response_sample (sample_id), CONSTRAINT fk_response_sample FOREIGN KEY (sample_id) REFERENCES audit_samples(id),
  CONSTRAINT fk_response_request FOREIGN KEY (request_id) REFERENCES ai_requests(id)
) ENGINE=InnoDB;
CREATE TABLE evaluation_runs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, audit_id BINARY(16) NOT NULL,
  judge_version VARCHAR(64) NOT NULL, eligibility_rule_version VARCHAR(64) NOT NULL, config_hash BINARY(32) NOT NULL,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, finished_at DATETIME(6),
  UNIQUE KEY uq_evaluation_run (audit_id,judge_version,config_hash), CONSTRAINT fk_eval_run_audit FOREIGN KEY (audit_id) REFERENCES audits(id)
) ENGINE=InnoDB;
CREATE TABLE answer_evaluations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, response_id BINARY(16) NOT NULL, evaluation_run_id BINARY(16) NOT NULL,
  content_validity VARCHAR(32) NOT NULL, answer_outcome VARCHAR(32) NOT NULL, eligible_for_core_metrics BOOLEAN NOT NULL,
  eligibility_rule_version VARCHAR(64) NOT NULL, recommendation_label VARCHAR(32), sentiment VARCHAR(32), confidence DECIMAL(6,5),
  evidence_json JSON, created_at DATETIME(6) NOT NULL, UNIQUE KEY uq_answer_evaluation (response_id,evaluation_run_id),
  CONSTRAINT fk_eval_response FOREIGN KEY (response_id) REFERENCES ai_responses(id), CONSTRAINT fk_eval_run FOREIGN KEY (evaluation_run_id) REFERENCES evaluation_runs(id)
) ENGINE=InnoDB;
CREATE TABLE brand_mentions (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, evaluation_id BINARY(16) NOT NULL, entity_id BINARY(16),
  start_offset INT UNSIGNED, end_offset INT UNSIGNED, mention_type VARCHAR(32) NOT NULL, confidence DECIMAL(6,5), evidence_text TEXT,
  KEY ix_mention_eval (evaluation_id), CONSTRAINT fk_mention_eval FOREIGN KEY (evaluation_id) REFERENCES answer_evaluations(id)
) ENGINE=InnoDB;
CREATE TABLE citations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, evaluation_id BINARY(16) NOT NULL,
  raw_url TEXT NOT NULL, normalized_url TEXT, normalized_url_hash BINARY(32), domain VARCHAR(253), source_type VARCHAR(32) NOT NULL,
  support_label VARCHAR(32), confidence DECIMAL(6,5), created_at DATETIME(6) NOT NULL,
  KEY ix_citation_eval (evaluation_id), CONSTRAINT fk_citation_eval FOREIGN KEY (evaluation_id) REFERENCES answer_evaluations(id)
) ENGINE=InnoDB;

CREATE TABLE metric_snapshots (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, audit_id BINARY(16) NOT NULL, evaluation_run_id BINARY(16) NOT NULL,
  metric_version VARCHAR(64) NOT NULL, status VARCHAR(32) NOT NULL, generated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_metric_snapshot (audit_id,evaluation_run_id,metric_version), CONSTRAINT fk_metric_audit FOREIGN KEY (audit_id) REFERENCES audits(id)
) ENGINE=InnoDB;
CREATE TABLE metric_values (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, snapshot_id BINARY(16) NOT NULL, metric_code VARCHAR(64) NOT NULL,
  aggregation_method VARCHAR(32) NOT NULL, dimension_hash BINARY(32) NOT NULL, dimensions_json JSON NOT NULL,
  numerator DECIMAL(20,6), denominator DECIMAL(20,6), value_decimal DECIMAL(12,10), ci_low DECIMAL(12,10), ci_high DECIMAL(12,10),
  interval_method VARCHAR(32) NOT NULL, interval_metadata JSON, created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_metric_value (snapshot_id,metric_code,aggregation_method,dimension_hash), CONSTRAINT fk_metric_snapshot FOREIGN KEY (snapshot_id) REFERENCES metric_snapshots(id)
) ENGINE=InnoDB;
CREATE TABLE reports (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, audit_id BINARY(16) NOT NULL,
  version INT UNSIGNED NOT NULL, dependency_versions JSON NOT NULL, template_version VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL, created_at DATETIME(6) NOT NULL, published_at DATETIME(6),
  UNIQUE KEY uq_report_version (audit_id,version), CONSTRAINT fk_report_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE report_artifacts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, report_id BINARY(16) NOT NULL, artifact_type VARCHAR(32) NOT NULL,
  object_key VARCHAR(1024) NOT NULL, sha256 BINARY(32) NOT NULL, size_bytes BIGINT UNSIGNED NOT NULL,
  renderer_metadata JSON NOT NULL, created_at DATETIME(6) NOT NULL, UNIQUE KEY uq_report_artifact (report_id,artifact_type),
  CONSTRAINT fk_artifact_report FOREIGN KEY (report_id) REFERENCES reports(id)
) ENGINE=InnoDB;
CREATE TABLE optimization_tasks (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, project_id BINARY(16) NOT NULL, finding_id BINARY(16),
  title VARCHAR(500) NOT NULL, description TEXT, status VARCHAR(32) NOT NULL, priority VARCHAR(16) NOT NULL,
  assignee_id BINARY(16), due_at DATETIME(6), version BIGINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  KEY ix_task_project (tenant_id,project_id,status,priority), CONSTRAINT fk_task_project FOREIGN KEY (tenant_id,project_id) REFERENCES projects(tenant_id,id)
) ENGINE=InnoDB;
CREATE TABLE task_events (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, task_id BINARY(16) NOT NULL,
  from_state VARCHAR(32), to_state VARCHAR(32), actor_id BINARY(16) NOT NULL, event_type VARCHAR(64) NOT NULL,
  reason TEXT, payload_json JSON, occurred_at DATETIME(6) NOT NULL, KEY ix_task_event (task_id,occurred_at),
  CONSTRAINT fk_task_event_task FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;
CREATE TABLE task_acceptance_items (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, task_id BINARY(16) NOT NULL,
  description VARCHAR(1000) NOT NULL, status VARCHAR(32) NOT NULL, position INT UNSIGNED NOT NULL,
  CONSTRAINT fk_task_acceptance FOREIGN KEY (task_id) REFERENCES optimization_tasks(id)
) ENGINE=InnoDB;

CREATE TABLE workflows (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, workflow_type VARCHAR(64) NOT NULL,
  resource_type VARCHAR(64) NOT NULL, resource_id BINARY(16) NOT NULL, status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL, started_at DATETIME(6), finished_at DATETIME(6), KEY ix_workflow_resource (tenant_id,resource_type,resource_id)
) ENGINE=InnoDB;
CREATE TABLE jobs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, workflow_id BINARY(16) NOT NULL,
  stage VARCHAR(64) NOT NULL, job_type VARCHAR(64) NOT NULL, idempotency_key VARCHAR(255) NOT NULL, state VARCHAR(32) NOT NULL,
  lease_owner VARCHAR(200), lease_expires_at DATETIME(6), attempt_count SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  max_attempts SMALLINT UNSIGNED NOT NULL DEFAULT 3, next_attempt_at DATETIME(6), version BIGINT UNSIGNED NOT NULL DEFAULT 1,
  payload_json JSON NOT NULL, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_job_idempotency (tenant_id,idempotency_key), KEY ix_job_claim (state,next_attempt_at,lease_expires_at),
  CONSTRAINT fk_job_workflow FOREIGN KEY (workflow_id) REFERENCES workflows(id)
) ENGINE=InnoDB;
CREATE TABLE job_attempts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, job_id BINARY(16) NOT NULL,
  attempt_no SMALLINT UNSIGNED NOT NULL, worker_id VARCHAR(200), state VARCHAR(32) NOT NULL,
  error_code VARCHAR(64), error_detail JSON, started_at DATETIME(6) NOT NULL, finished_at DATETIME(6),
  UNIQUE KEY uq_job_attempt (job_id,attempt_no), CONSTRAINT fk_attempt_job FOREIGN KEY (job_id) REFERENCES jobs(id)
) ENGINE=InnoDB;
CREATE TABLE outbox_events (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, event_type VARCHAR(100) NOT NULL,
  aggregate_type VARCHAR(64) NOT NULL, aggregate_id BINARY(16) NOT NULL, payload_json JSON NOT NULL,
  occurred_at DATETIME(6) NOT NULL, published_at DATETIME(6), attempt_count SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  KEY ix_outbox_publish (published_at,occurred_at)
) ENGINE=InnoDB;
CREATE TABLE inbox_events (
  consumer_name VARCHAR(100) NOT NULL, event_id BINARY(16) NOT NULL, processed_at DATETIME(6) NOT NULL,
  PRIMARY KEY (consumer_name,event_id)
) ENGINE=InnoDB;
CREATE TABLE budget_accounts (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, scope_type VARCHAR(32) NOT NULL, scope_id BINARY(16) NOT NULL,
  period_start DATE NOT NULL, period_end DATE NOT NULL, currency CHAR(3) NOT NULL,
  soft_limit_microunits BIGINT UNSIGNED, hard_limit_microunits BIGINT UNSIGNED NOT NULL,
  reserved_microunits BIGINT UNSIGNED NOT NULL DEFAULT 0, settled_microunits BIGINT UNSIGNED NOT NULL DEFAULT 0,
  UNIQUE KEY uq_budget_scope_period (tenant_id,scope_type,scope_id,period_start,period_end,currency)
) ENGINE=InnoDB;
CREATE TABLE budget_reservations (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, budget_account_id BINARY(16) NOT NULL, request_id BINARY(16),
  amount_microunits BIGINT UNSIGNED NOT NULL, status VARCHAR(32) NOT NULL, expires_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL,
  CONSTRAINT fk_reservation_budget FOREIGN KEY (budget_account_id) REFERENCES budget_accounts(id)
) ENGINE=InnoDB;
CREATE TABLE cost_ledger (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, budget_account_id BINARY(16) NOT NULL, request_id BINARY(16),
  entry_type VARCHAR(32) NOT NULL, amount_microunits BIGINT NOT NULL, currency CHAR(3) NOT NULL, pricing_version VARCHAR(64),
  created_at DATETIME(6) NOT NULL, KEY ix_cost_budget (budget_account_id,created_at),
  CONSTRAINT fk_ledger_budget FOREIGN KEY (budget_account_id) REFERENCES budget_accounts(id)
) ENGINE=InnoDB;
CREATE TABLE audit_logs (
  id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, actor_type VARCHAR(32) NOT NULL, actor_id BINARY(16),
  action VARCHAR(100) NOT NULL, resource_type VARCHAR(64), resource_id BINARY(16), result VARCHAR(32) NOT NULL,
  ip_hash BINARY(32), trace_id CHAR(32), payload_summary JSON, occurred_at DATETIME(6) NOT NULL,
  KEY ix_audit_log_tenant_time (tenant_id,occurred_at), KEY ix_audit_log_resource (tenant_id,resource_type,resource_id)
) ENGINE=InnoDB;
