-- FEAT-09: keep metric evidence members inline until object storage is enabled.
ALTER TABLE metric_evidence_sets ADD COLUMN content_json JSON NULL AFTER members_object_key;
