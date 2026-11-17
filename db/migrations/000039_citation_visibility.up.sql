ALTER TABLE citations
  ADD COLUMN first_citation_sentence_index INT UNSIGNED NULL AFTER confidence,
  ADD COLUMN sentence_count INT UNSIGNED NULL AFTER first_citation_sentence_index,
  ADD COLUMN citation_word_count DECIMAL(18,6) NOT NULL DEFAULT 0 AFTER sentence_count,
  ADD COLUMN answer_word_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER citation_word_count,
  ADD COLUMN citation_word_share DECIMAL(18,10) NOT NULL DEFAULT 0 AFTER answer_word_count,
  ADD COLUMN position_adjusted_word_share DECIMAL(18,10) NOT NULL DEFAULT 0 AFTER citation_word_share;
