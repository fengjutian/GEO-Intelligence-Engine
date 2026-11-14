-- FEAT-04: keep the references moved by a merge so the merge can be undone.
ALTER TABLE entity_merge_events ADD COLUMN snapshot_json JSON NULL AFTER reason;
