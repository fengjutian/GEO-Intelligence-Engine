ALTER TABLE finding_evidence_chains
  ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'evidence_ready' AFTER project_id,
  ADD COLUMN metric_code VARCHAR(64) NOT NULL DEFAULT 'mention_rate' AFTER verification_plan,
  ADD COLUMN target_delta DECIMAL(12,10) NOT NULL DEFAULT 0.0100000000 AFTER metric_code,
  ADD COLUMN evidence_snapshot_json JSON NULL AFTER target_delta,
  ADD COLUMN comparison_json JSON NULL AFTER evidence_snapshot_json,
  ADD COLUMN decision_source VARCHAR(32) NOT NULL DEFAULT 'pending' AFTER comparison_json,
  ADD COLUMN decision_reason TEXT NULL AFTER decision_source,
  ADD COLUMN version BIGINT UNSIGNED NOT NULL DEFAULT 1 AFTER decision_reason;

CREATE TABLE finding_evidence_chain_events (
  id BINARY(16) PRIMARY KEY,
  tenant_id BINARY(16) NOT NULL,
  finding_id BINARY(16) NOT NULL,
  actor_id BINARY(16) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  from_status VARCHAR(32),
  to_status VARCHAR(32),
  reason TEXT,
  payload_json JSON,
  occurred_at DATETIME(6) NOT NULL,
  KEY ix_finding_chain_event (finding_id,occurred_at,id),
  CONSTRAINT fk_finding_chain_event_chain FOREIGN KEY (finding_id) REFERENCES finding_evidence_chains(finding_id)
) ENGINE=InnoDB;

UPDATE finding_evidence_chains c
JOIN questions q ON q.id=c.question_id
JOIN ai_responses r ON r.id=c.response_id
JOIN audit_samples s ON s.id=r.sample_id
JOIN audits a ON a.id=s.audit_id
JOIN measurement_protocols mp ON mp.id=a.protocol_id
JOIN engine_configs ec ON ec.id=s.engine_config_id
JOIN page_contents pc ON pc.id=c.page_content_id
SET c.evidence_snapshot_json=JSON_OBJECT(
  'question',JSON_OBJECT('id',BIN_TO_UUID(q.id),'text',q.text_value),
  'answer',JSON_OBJECT('id',BIN_TO_UUID(r.id),'auditId',BIN_TO_UUID(a.id),'provider',ec.provider,'channel',s.channel,'model',s.model_selector,'content',COALESCE(r.content_text,''),'contentHash',SHA2(COALESCE(r.content_text,''),256)),
  'page',JSON_OBJECT('contentId',BIN_TO_UUID(pc.id),'url',COALESCE(pc.final_url,''),'title',COALESCE(pc.title,''),'contentHash',LOWER(HEX(pc.content_hash))),
  'protocol',JSON_OBJECT('auditId',BIN_TO_UUID(a.id),'questionVersionId',BIN_TO_UUID(a.question_version_id),'protocolVersion',mp.semantic_version,'protocolHash',LOWER(HEX(mp.config_hash)),'metricVersion','','judgeVersion','','promptTemplateVersion',COALESCE(JSON_UNQUOTE(JSON_EXTRACT(mp.config_json,'$.promptTemplateVersion')),''),'channel',s.channel,'modelSelector',s.model_selector,'region',COALESCE(s.region,''),'language',s.language),
  'baseline',JSON_OBJECT('metricCode',c.metric_code),
  'capturedAt',DATE_FORMAT(c.created_at,'%Y-%m-%dT%H:%i:%s.%fZ')
)
WHERE c.evidence_snapshot_json IS NULL;
