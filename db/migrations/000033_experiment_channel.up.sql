-- Experiments must compare one channel at a time; mixing model_api with captures
-- silently skews baseline and treatment rates.

ALTER TABLE experiments
  ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'model_api' AFTER metric_code;
