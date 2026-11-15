ALTER TABLE source_mentions
  DROP KEY uq_mention_dedupe,
  DROP COLUMN dedupe_hash,
  DROP COLUMN discovery,
  DROP COLUMN authority;

ALTER TABLE bot_log_events
  DROP COLUMN ingest_source;
