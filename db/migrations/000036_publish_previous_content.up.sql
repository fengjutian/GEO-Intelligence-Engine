-- Filesystem connector keeps the previous file so rollback restores it instead of deleting.
ALTER TABLE publish_events
  ADD COLUMN previous_content MEDIUMBLOB NULL AFTER payload_hash;
