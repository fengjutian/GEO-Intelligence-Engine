-- FEAT-06: keep the canonical create-request hash so an Idempotency-Key reused with a
-- different protocol/project body is rejected instead of silently returning the old audit.
ALTER TABLE audits ADD COLUMN request_hash BINARY(32) NULL AFTER idempotency_key;
