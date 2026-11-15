ALTER TABLE ai_responses
  ADD COLUMN content_text LONGTEXT NULL AFTER finish_reason;
