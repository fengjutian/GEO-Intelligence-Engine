DROP TABLE IF EXISTS finding_evidence_chain_events;
ALTER TABLE finding_evidence_chains
  DROP COLUMN version,
  DROP COLUMN decision_reason,
  DROP COLUMN decision_source,
  DROP COLUMN comparison_json,
  DROP COLUMN evidence_snapshot_json,
  DROP COLUMN target_delta,
  DROP COLUMN metric_code,
  DROP COLUMN status;
