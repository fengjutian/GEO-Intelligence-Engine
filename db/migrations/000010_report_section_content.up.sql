-- FEAT-11: keep generated report section data inline until object storage is enabled.
ALTER TABLE report_sections ADD COLUMN content_json JSON NULL AFTER content_hash;
