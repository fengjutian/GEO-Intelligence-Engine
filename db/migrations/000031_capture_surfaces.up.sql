-- Capture surface registry: which provider/product pairs are allowed for capture.

CREATE TABLE capture_surfaces (
  id BINARY(16) PRIMARY KEY,
  provider VARCHAR(64) NOT NULL,
  product VARCHAR(64) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_capture_surface (provider, product)
) ENGINE=InnoDB;

INSERT INTO capture_surfaces (id, provider, product, enabled, created_at) VALUES
  (UUID_TO_BIN(UUID()), 'openai', 'chatgpt', TRUE, UTC_TIMESTAMP(6)),
  (UUID_TO_BIN(UUID()), 'perplexity', 'perplexity', TRUE, UTC_TIMESTAMP(6)),
  (UUID_TO_BIN(UUID()), 'google', 'ai-overview', TRUE, UTC_TIMESTAMP(6)),
  (UUID_TO_BIN(UUID()), 'microsoft', 'copilot', TRUE, UTC_TIMESTAMP(6)),
  (UUID_TO_BIN(UUID()), 'bytedance', 'doubao', TRUE, UTC_TIMESTAMP(6));
