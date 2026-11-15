-- Crawler log auto-ingestion and source discovery metadata.

ALTER TABLE bot_log_events
  ADD COLUMN ingest_source VARCHAR(32) NOT NULL DEFAULT 'api' AFTER http_status;

ALTER TABLE source_mentions
  ADD COLUMN authority DECIMAL(4,2) NOT NULL DEFAULT 0.50 AFTER sentiment,
  ADD COLUMN discovery VARCHAR(32) NOT NULL DEFAULT 'manual' AFTER authority,
  ADD COLUMN dedupe_hash BINARY(32) NULL AFTER discovery,
  ADD UNIQUE KEY uq_mention_dedupe (tenant_id, project_id, dedupe_hash);
