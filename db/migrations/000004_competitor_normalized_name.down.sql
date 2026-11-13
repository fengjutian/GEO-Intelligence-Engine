ALTER TABLE competitors
  DROP INDEX uq_competitor_normalized_name,
  DROP COLUMN name_normalized,
  ADD UNIQUE KEY uq_competitor_name (project_id,name);
