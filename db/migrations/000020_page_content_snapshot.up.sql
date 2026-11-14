-- FEAT-10: keep the deterministic content snapshot so rules can be re-run.
ALTER TABLE page_contents ADD COLUMN content_json JSON NULL AFTER parser_version;
