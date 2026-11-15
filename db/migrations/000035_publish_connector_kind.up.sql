-- Publish targets gain a connector kind; filesystem writes into a static site content directory.
ALTER TABLE publish_targets
  ADD COLUMN kind VARCHAR(32) NOT NULL DEFAULT 'webhook' AFTER project_id,
  ADD COLUMN config_json JSON NULL AFTER endpoint;
