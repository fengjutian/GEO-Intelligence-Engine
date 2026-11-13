ALTER TABLE projects
  ADD COLUMN region VARCHAR(35) NOT NULL DEFAULT 'global' AFTER default_locale;
