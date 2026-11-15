DROP TABLE IF EXISTS capture_provenance;
ALTER TABLE ai_responses MODIFY COLUMN request_id BINARY(16) NOT NULL;
