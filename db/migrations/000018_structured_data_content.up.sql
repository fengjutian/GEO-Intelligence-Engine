-- FEAT-04: persist inline JSON-LD so entities can be re-extracted without object storage.
ALTER TABLE structured_data_items ADD COLUMN content_json JSON NULL AFTER validation_status;
