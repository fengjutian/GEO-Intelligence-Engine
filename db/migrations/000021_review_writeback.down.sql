ALTER TABLE answer_evaluations
  DROP KEY ix_eval_human_adjudication,
  DROP COLUMN human_reviewed_at,
  DROP COLUMN human_adjudication_id,
  DROP COLUMN human_decision_json;
