ALTER TABLE competitors
  ADD COLUMN name_normalized VARCHAR(300) NOT NULL DEFAULT '' AFTER name;

UPDATE competitors SET name_normalized=LOWER(TRIM(name));

ALTER TABLE competitors
  DROP INDEX uq_competitor_name,
  ADD UNIQUE KEY uq_competitor_normalized_name (project_id,name_normalized);
