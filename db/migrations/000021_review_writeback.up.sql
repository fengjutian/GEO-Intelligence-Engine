ALTER TABLE answer_evaluations
  ADD COLUMN human_decision_json JSON NULL AFTER evidence_json,
  ADD COLUMN human_adjudication_id BINARY(16) NULL AFTER human_decision_json,
  ADD COLUMN human_reviewed_at DATETIME(6) NULL AFTER human_adjudication_id,
  ADD KEY ix_eval_human_adjudication (human_adjudication_id);
