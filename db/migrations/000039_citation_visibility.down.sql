ALTER TABLE citations
  DROP COLUMN position_adjusted_word_share,
  DROP COLUMN citation_word_share,
  DROP COLUMN answer_word_count,
  DROP COLUMN citation_word_count,
  DROP COLUMN sentence_count,
  DROP COLUMN first_citation_sentence_index;
